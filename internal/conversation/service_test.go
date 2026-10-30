package conversation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

// scripted is a mock model. It records every input and answers turns and
// summaries with the next scripted text; it is not a model quality test.
type scripted struct {
	mu        sync.Mutex
	inputs    []generation.Input
	replies   []string
	summaries []string
	failSum   bool
}

func (g *scripted) Generate(_ context.Context, in generation.Input) (generation.Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inputs = append(g.inputs, in)
	if strings.HasSuffix(in.VariantID, ":summary") {
		if g.failSum || len(g.summaries) == 0 {
			return generation.Result{Status: "failed"}, nil
		}
		text := g.summaries[0]
		g.summaries = g.summaries[1:]
		return generation.Result{Status: "generated", FinishReason: "stop", Text: text}, nil
	}
	text := `{"assessment":{"want":"vent","topics":["contact"],"topic_certainty":"medium","tried":[],"avoid":[],"capacity":"listen_only","unknowns":[],"user_feelings":[],"guesses_about_other":[]},"response_mode":"brief_empathy","asked_question":false,"reply":"응, 듣고 있어.","evidence_ids":[],"memory_candidates":[]}`
	if len(g.replies) > 0 {
		text = g.replies[0]
		g.replies = g.replies[1:]
	}
	return generation.Result{Status: "generated", FinishReason: "stop", Text: text}, nil
}

func (g *scripted) turnInputs() []generation.Input {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := []generation.Input{}
	for _, in := range g.inputs {
		if in.VariantID == PolicyID {
			out = append(out, in)
		}
	}
	return out
}

type fakeLinks map[string]readings.LinkSnapshot

func (f fakeLinks) LinkSnapshot(_ context.Context, user, id string) (readings.LinkSnapshot, error) {
	s, ok := f[user+"/"+id]
	if !ok {
		return readings.LinkSnapshot{}, readings.ErrNotFound
	}
	return s, nil
}

func testDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("NYANG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NYANG_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = persistence.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func account(t *testing.T, db *sql.DB, prefix string) string {
	id := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	if _, err := db.Exec(`INSERT INTO accounts(id) VALUES($1)`, id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM accounts WHERE id=$1`, id) })
	return id
}

func payloadOf(t *testing.T, in generation.Input) map[string]any {
	t.Helper()
	start := strings.Index(in.UserPrompt, "CONTEXT_JSON\n")
	dec := json.NewDecoder(strings.NewReader(in.UserPrompt[start+len("CONTEXT_JSON\n"):]))
	var p map[string]any
	if err := dec.Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func assistant(t *testing.T, turn Turn) Message {
	t.Helper()
	if turn.AssistantMessage.State != "complete" {
		t.Fatalf("assistant state %s meta %s", turn.AssistantMessage.State, turn.AssistantMessage.Meta)
	}
	return turn.AssistantMessage
}

func TestConversationTurnsMemoryAndIsolation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	alice, bob := account(t, db, "conv-alice"), account(t, db, "conv-bob")
	gen := &scripted{}
	mem := &memory.PostgresStore{DB: db}
	links := fakeLinks{alice + "/r1": {ReadingID: "r1", Domain: "tarot", Summary: "카드 요약", Input: json.RawMessage(`{"cards":[{"card_id":6}]}`)}, alice + "/s1": {ReadingID: "s1", Domain: "saju", Summary: "사주 요약"}}
	svc := &Service{Store: Store{DB: db}, Memory: mem, Generator: gen, Links: links, Inline: true}

	if _, err := svc.Create(ctx, alice, "s1", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("saju link without subject: %v", err)
	}
	if _, err := svc.Create(ctx, bob, "r1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob linked alice's reading: %v", err)
	}
	conv, err := svc.Create(ctx, alice, "r1", "")
	if err != nil || len(conv.Links) != 1 || conv.Links[0].Subject != "self" {
		t.Fatalf("create %v %+v", err, conv)
	}

	first := "어제 또 연락했는데 답이 없었어. 나는 다시 만나고 싶어."
	gen.replies = []string{`{"assessment":{"want":"vent","topics":["contact"],"topic_certainty":"medium","tried":[],"avoid":[],"capacity":"listen_only","unknowns":[],"user_feelings":[],"guesses_about_other":[]},"response_mode":"brief_empathy","asked_question":false,"reply":"답이 없어서 많이 허전했겠다.","evidence_ids":["msg:1","link:` + conv.Links[0].ID + `","mem:nope"],"memory_candidates":[
	{"class":"episode","key":"attempt","value":"어제 연락했지만 답이 없었다","origin":"user_stated","quote":"어제 또 연락했는데 답이 없었어","subject":"partner","situation":"","occurred_at":""},
	{"class":"episode","key":"wish","value":"다시 만나고 싶다","origin":"user_stated","quote":"나는 다시 만나고 싶어","subject":"self","situation":"답이 없던 다음 날","occurred_at":""},
	{"class":"episode","key":"feeling","value":"상대도 아직 마음이 있다","origin":"ai_inference","quote":"","subject":"partner","situation":"","occurred_at":""}]}`}
	turn, err := svc.Send(ctx, alice, conv.ID, "k1", first)
	if err != nil {
		t.Fatal(err)
	}
	a1 := assistant(t, turn)
	var meta struct {
		EvidenceIDs []string         `json:"evidence_ids"`
		Checks      []Check          `json:"checks"`
		Memory      []map[string]any `json:"memory_updates"`
	}
	if err = json.Unmarshal(a1.Meta, &meta); err != nil || len(meta.EvidenceIDs) != 2 || len(meta.Memory) != 3 {
		t.Fatalf("meta %v %s", err, a1.Meta)
	}
	current, _ := mem.Current(alice)
	if len(current) != 2 {
		t.Fatalf("confirmed memory %+v", current)
	}
	for _, r := range current {
		if r.Origin != memory.OriginStated || r.SourceMessageID != turn.UserMessage.ID || strings.Contains(r.Value, "마음이 있다") {
			t.Fatalf("memory %+v", r)
		}
	}
	inferred, _ := mem.Inferences(alice)
	if len(inferred) != 1 || inferred[0].Value != "상대도 아직 마음이 있다" {
		t.Fatalf("inferences %+v", inferred)
	}

	// The same request key replays the stored turn without another model call.
	calls := len(gen.turnInputs())
	again, err := svc.Send(ctx, alice, conv.ID, "k1", first)
	if err != nil || !again.Replayed || again.AssistantMessage.ID != a1.ID || len(gen.turnInputs()) != calls {
		t.Fatalf("replay %v %+v", err, again)
	}
	if _, err = svc.Send(ctx, alice, conv.ID, "k1", "다른 말"); !errors.Is(err, ErrConflict) {
		t.Fatalf("key reuse: %v", err)
	}
	if _, err = svc.Get(ctx, bob, conv.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice: %v", err)
	}
	if _, err = svc.Send(ctx, bob, conv.ID, "k2", "안녕"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob wrote alice: %v", err)
	}

	// A contradictory feeling from another situation is kept beside the wish.
	gen.replies = []string{`{"assessment":{"want":"vent","topics":["contact"],"topic_certainty":"medium","tried":[],"avoid":[],"capacity":"listen_only","unknowns":[],"user_feelings":[],"guesses_about_other":[]},"response_mode":"brief_empathy","asked_question":false,"reply":"그럴 수 있어.","evidence_ids":["msg:3"],"memory_candidates":[{"class":"episode","key":"feeling","value":"연락은 부담스럽다","origin":"user_stated","quote":"연락은 좀 부담스러워","subject":"self","situation":"상대 SNS를 본 뒤","occurred_at":""}]}`}
	assistant(t, mustSend(t, svc, alice, conv.ID, "k2", "근데 SNS 보니까 연락은 좀 부담스러워"))
	current, _ = mem.Current(alice)
	values := []string{}
	for _, r := range current {
		values = append(values, r.Value)
	}
	if len(current) != 3 || !strings.Contains(strings.Join(values, "|"), "다시 만나고 싶다") || !strings.Contains(strings.Join(values, "|"), "연락은 부담스럽다") {
		t.Fatalf("episodes not kept side by side: %v", values)
	}

	inputs := gen.turnInputs()
	p := payloadOf(t, inputs[len(inputs)-1])
	recent := p["recent_messages"].([]any)
	if len(recent) != 2 || p["current_user_message"].(map[string]any)["id"] != "msg:3" {
		t.Fatalf("second turn context %v", p)
	}
	notes, _ := p["previous_turn_notes"].(map[string]any)
	if notes["from"] != "msg:2" || notes["kind"] != "ai_note_not_user_fact" || notes["capacity"] != "listen_only" {
		t.Fatalf("previous notes %v", p["previous_turn_notes"])
	}
	var second struct {
		References map[string]any `json:"reference_usage"`
		Assessment map[string]any `json:"assessment"`
	}
	got2, _ := svc.Get(ctx, alice, conv.ID)
	if err = json.Unmarshal(got2.Messages[3].Meta, &second); err != nil || second.References["mode"] != "conditional" || second.References["topic"] != "negotiate_contact" || second.Assessment["want"] != "vent" {
		t.Fatalf("reference usage or assessment not recorded: %s", got2.Messages[3].Meta)
	}
	if len(p["linked_results"].([]any)) != 1 || strings.Contains(inputs[len(inputs)-1].UserPrompt, "다시 만나고 싶다\",\"origin\":\"ai_inference") {
		t.Fatal("links or inference handling wrong")
	}
	if strings.Contains(inputs[len(inputs)-1].UserPrompt, "상대도 아직 마음이 있다") {
		t.Fatal("AI inference used as memory")
	}

	// Deleting a memory taken from message 1 suppresses that message: it is
	// redacted from later context and cannot be recorded again.
	var wishID string
	for _, r := range current {
		if r.Key == "wish" {
			wishID = r.ID
		}
	}
	if err = mem.Delete(alice, wishID); err != nil {
		t.Fatal(err)
	}
	fact, m := Candidate{Class: "episode", Key: "wish", Value: "다시 만나고 싶다", Origin: "user_stated", Quote: "나는 다시 만나고 싶어"}.MemoryFact(turn.UserMessage, 9)
	if _, err = mem.RecordFromConversation(alice, fact, m); !errors.Is(err, memory.ErrSuppressed) {
		t.Fatalf("deleted memory recreated: %v", err)
	}
	assistant(t, mustSend(t, svc, alice, conv.ID, "k3", "오늘은 그냥 좀 지쳤어"))
	inputs = gen.turnInputs()
	last := inputs[len(inputs)-1].UserPrompt
	if strings.Contains(last, "나는 다시 만나고 싶어") || !strings.Contains(last, redactedText) {
		t.Fatal("deleted memory's source message not redacted")
	}

	got, err := svc.Get(ctx, alice, conv.ID)
	if err != nil || len(got.Messages) != 6 {
		t.Fatalf("get %v %d", err, len(got.Messages))
	}
	list, _ := svc.List(ctx, alice)
	if len(list) != 1 || list[0].ID != conv.ID {
		t.Fatalf("list %+v", list)
	}
	if other, _ := svc.List(ctx, bob); len(other) != 0 {
		t.Fatal("bob sees alice's conversation")
	}
}

func mustSend(t *testing.T, svc *Service, user, id, key, text string) Turn {
	t.Helper()
	turn, err := svc.Send(context.Background(), user, id, key, text)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

func TestSummaryLagCorrectionAndRebuild(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := account(t, db, "conv-sum")
	gen := &scripted{failSum: true}
	mem := &memory.PostgresStore{DB: db}
	svc := &Service{Store: Store{DB: db}, Memory: mem, Generator: gen, Inline: true, Limits: Limits{MinRecent: 2, MaxRecent: 6, MaxFacts: 16, MaxEpisodes: 12, RefreshAfter: 4}}
	conv, err := svc.Create(ctx, user, "", "")
	if err != nil {
		t.Fatal(err)
	}
	gen.replies = []string{`{"assessment":{"want":"vent","topics":["contact"],"topic_certainty":"medium","tried":[],"avoid":[],"capacity":"listen_only","unknowns":[],"user_feelings":[],"guesses_about_other":[]},"response_mode":"brief_empathy","asked_question":false,"reply":"알려줘서 고마워.","evidence_ids":["msg:1"],"memory_candidates":[{"class":"fact","key":"relationship_status","value":"헤어진 지 한 달","origin":"user_stated","quote":"헤어진 지 한 달 됐어","subject":"self","situation":"","occurred_at":""}]}`}
	assistant(t, mustSend(t, svc, user, conv.ID, "k0", "헤어진 지 한 달 됐어"))
	// Summary generation keeps failing: the newest messages are still sent raw
	// and the unseen middle is reported instead of silently dropped.
	for i := 1; i <= 4; i++ {
		assistant(t, mustSend(t, svc, user, conv.ID, fmt.Sprintf("k%d", i), fmt.Sprintf("말 %d", i)))
	}
	inputs := gen.turnInputs()
	p := payloadOf(t, inputs[len(inputs)-1])
	recent := p["recent_messages"].([]any)
	if p["current_user_message"].(map[string]any)["id"] != "msg:9" || recent[len(recent)-1].(map[string]any)["id"] != "msg:8" || p["omitted_messages"] == nil {
		t.Fatalf("lagging context %v", p)
	}

	// Once summaries work, the next turn records its coverage point.
	gen.failSum = false
	gen.summaries = []string{`{"summary":"사용자는 헤어진 지 한 달이라고 말했다."}`}
	assistant(t, mustSend(t, svc, user, conv.ID, "k5", "말 5"))
	got, _ := svc.Get(ctx, user, conv.ID)
	if got.Summary == nil || got.Summary.ThroughSeq != 12 || !got.Summary.Current {
		t.Fatalf("summary %+v", got.Summary)
	}
	assistant(t, mustSend(t, svc, user, conv.ID, "k6", "말 6"))
	inputs = gen.turnInputs()
	p = payloadOf(t, inputs[len(inputs)-1])
	if p["summary_status"] != "current" || p["conversation_summary"].(map[string]any)["covers_through"] != "msg:12" {
		t.Fatalf("summary not used %v", p["conversation_summary"])
	}
	if recent := p["recent_messages"].([]any); recent[len(recent)-1].(map[string]any)["id"] != "msg:12" {
		t.Fatalf("latest raw message missing %v", recent)
	}

	// A correction makes the old summary stale. The next turn ignores it and
	// the rebuild uses the corrected memory, not the old statement.
	current, _ := mem.Current(user)
	if len(current) != 1 {
		t.Fatalf("memory %+v", current)
	}
	corrected := current[0].Fact
	corrected.Value = "헤어진 지 두 달"
	if _, err = mem.Correct(user, current[0].ID, corrected); err != nil {
		t.Fatal(err)
	}
	gen.summaries = []string{`{"summary":"사용자는 헤어진 지 두 달이라고 정정했다."}`}
	assistant(t, mustSend(t, svc, user, conv.ID, "k7", "말 7"))
	inputs = gen.turnInputs()
	last := inputs[len(inputs)-1].UserPrompt
	if !strings.Contains(last, `"summary_status": "stale_ignored"`) || strings.Contains(last, "헤어진 지 한 달이라고 말했다") || !strings.Contains(last, "헤어진 지 두 달") {
		t.Fatalf("stale summary or old fact used:\n%s", last)
	}
	gen.mu.Lock()
	var rebuild generation.Input
	for _, in := range gen.inputs {
		if strings.HasSuffix(in.VariantID, ":summary") {
			rebuild = in
		}
	}
	gen.mu.Unlock()
	if strings.Contains(rebuild.UserPrompt, "previous_summary\": {") || !strings.Contains(rebuild.UserPrompt, "헤어진 지 두 달") {
		t.Fatalf("rebuild input:\n%s", rebuild.UserPrompt)
	}
	got, _ = svc.Get(ctx, user, conv.ID)
	if got.Summary == nil || !got.Summary.Current || got.Summary.ThroughSeq != 16 {
		t.Fatalf("rebuilt summary %+v", got.Summary)
	}
}

func TestBusyFailedAndDeletedAccount(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := account(t, db, "conv-busy")
	gen := &scripted{}
	svc := &Service{Store: Store{DB: db}, Memory: &memory.PostgresStore{DB: db}, Generator: gen, Inline: true}
	conv, _ := svc.Create(ctx, user, "", "")
	// A pending reply blocks a second message instead of generating in parallel.
	if _, _, _, err := svc.Store.BeginTurn(ctx, user, conv.ID, "p1", "첫 말"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(ctx, user, conv.ID, "p2", "두 번째"); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy %v", err)
	}
	// After the worker timeout the interrupted reply is marked unknown, not retried.
	if _, err := db.Exec(`UPDATE messages SET created_at=now()-interval '10 minutes' WHERE conversation_id=$1`, conv.ID); err != nil {
		t.Fatal(err)
	}
	gen.replies = []string{"형식이 아닌 답변"}
	turn, err := svc.Send(ctx, user, conv.ID, "p2", "두 번째")
	if err != nil || turn.AssistantMessage.State != "failed" || turn.AssistantMessage.Content != "" {
		t.Fatalf("invalid output should fail: %v %+v", err, turn.AssistantMessage)
	}
	got, _ := svc.Get(ctx, user, conv.ID)
	if got.Messages[1].State != "unknown" {
		t.Fatalf("interrupted reply %+v", got.Messages[1])
	}
	if err = (persistence.Store{DB: db}).DeleteAccount(ctx, user); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Send(ctx, user, conv.ID, "p3", "삭제 후"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("send after deletion %v", err)
	}
}
