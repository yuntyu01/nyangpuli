package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/conversation"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

func TestConversationRoutesRequirePostgres(t *testing.T) {
	h := (&Server{Tokens: map[string]string{"t": "alice"}}).Handler()
	if w := call(h, "GET", "/v1/conversations", "t", ""); w.Code != 503 {
		t.Fatalf("no service %d", w.Code)
	}
	if w := call(h, "GET", "/v1/conversations", "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated %d", w.Code)
	}
}

func TestConversationHTTPWithLinkedReading(t *testing.T) {
	dsn := os.Getenv("NYANG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NYANG_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = persistence.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	stamp := fmt.Sprint(time.Now().UnixNano())
	alice, bob := "conv-http-a-"+stamp, "conv-http-b-"+stamp
	for _, u := range []string{alice, bob} {
		if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1)`, u); err != nil {
			t.Fatal(err)
		}
		defer db.Exec(`DELETE FROM accounts WHERE id=$1`, u)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		text := "카드 요약입니다."
		if strings.Contains(req.Messages[len(req.Messages)-1].Content, "CONTEXT_JSON") {
			text = `{"reply":"그 카드 이야기를 이어서 해 볼까요.","evidence_ids":["msg:1"],"memory_candidates":[{"class":"episode","key":"feeling","value":"상대가 날 피하는 것 같다","origin":"ai_inference","quote":"","subject":"partner","situation":"","occurred_at":""}]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "scripted-test", "choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}})
	}))
	defer provider.Close()
	client, err := generation.New(generation.Config{APIBase: provider.URL, Model: "scripted-test", MaxCompletionTokens: 200}, provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	mem := &memory.PostgresStore{DB: db}
	p := persistence.Store{DB: db}
	reads := NewPostgresReadingService(db, l, mem, client, false)
	convs := NewConversationService(db, mem, client, reads)
	s := &Server{Lab: l, Memory: mem, TarotStore: p, Readings: reads, Conversations: convs, Tokens: map[string]string{"ta": alice, "tb": bob}}
	h := s.Handler()
	session, _ := l.Tarot.NewSession("요즘 관계를 어떻게 볼까요?", "love", "single-focus")
	tid, err := p.CreateTarot(ctx, alice, session)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.SelectTarot(ctx, alice, tid, l.Tarot, 4); err != nil {
		t.Fatal(err)
	}
	w := call(h, "POST", "/v1/tarot/sessions/"+tid+"/reading", "ta", "")
	var reading readings.View
	if err = json.Unmarshal(w.Body.Bytes(), &reading); err != nil || w.Code != 201 {
		t.Fatalf("reading %d %s", w.Code, w.Body.String())
	}
	if w = call(h, "POST", "/v1/conversations", "tb", `{"linked_reading_id":"`+reading.ID+`"}`); w.Code != 404 {
		t.Fatalf("bob linked alice's reading %d", w.Code)
	}
	w = call(h, "POST", "/v1/conversations", "ta", `{"linked_reading_id":"`+reading.ID+`"}`)
	var conv conversation.Conversation
	if err = json.Unmarshal(w.Body.Bytes(), &conv); err != nil || w.Code != 201 || len(conv.Links) != 1 || conv.Links[0].Domain != "tarot" {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var snap readings.LinkSnapshot
	if err = json.Unmarshal(conv.Links[0].Snapshot, &snap); err != nil || snap.Summary != "카드 요약입니다." || len(snap.Evidence) == 0 {
		t.Fatalf("snapshot %v %s", err, conv.Links[0].Snapshot)
	}
	send := func(token, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/conversations/"+conv.ID+"/messages", bytes.NewBufferString(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if w = send("tb", "k1", `{"content":"안녕"}`); w.Code != 404 {
		t.Fatalf("bob wrote %d", w.Code)
	}
	if w = send("ta", "k1", `{"content":"이 카드 보니까 마음이 복잡해"}`); w.Code != 202 {
		t.Fatalf("send %d %s", w.Code, w.Body.String())
	}
	convs.(*conversation.Service).Wait()
	w = call(h, "GET", "/v1/conversations/"+conv.ID, "ta", "")
	var got conversation.Conversation
	if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Messages) != 2 || got.Messages[1].State != "complete" {
		t.Fatalf("get %d %s", w.Code, w.Body.String())
	}
	w = call(h, "GET", "/v1/memory/inferences", "ta", "")
	var inferred []memory.Record
	if err = json.Unmarshal(w.Body.Bytes(), &inferred); err != nil || len(inferred) != 1 || inferred[0].Origin != "ai_inference" {
		t.Fatalf("inferences %s", w.Body.String())
	}
	if w = call(h, "POST", "/v1/memory/"+inferred[0].ID+"/confirm", "tb", ""); w.Code != 404 {
		t.Fatalf("bob confirmed %d", w.Code)
	}
	if w = call(h, "POST", "/v1/memory/"+inferred[0].ID+"/confirm", "ta", ""); w.Code != 200 {
		t.Fatalf("confirm %d %s", w.Code, w.Body.String())
	}
	if w = call(h, "DELETE", "/v1/conversations/"+conv.ID, "tb", ""); w.Code != 404 {
		t.Fatalf("bob deleted %d", w.Code)
	}
	if w = call(h, "DELETE", "/v1/conversations/"+conv.ID, "ta", ""); w.Code != 200 {
		t.Fatalf("delete %d", w.Code)
	}
	current, _ := mem.Current(alice)
	if len(current) != 1 || current[0].SourceMessageID != "" {
		t.Fatalf("confirmed memory should outlive the conversation with its source detached: %+v", current)
	}
}
