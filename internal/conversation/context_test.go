package conversation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

func msgs(n int) []Message {
	out := []Message{}
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		out = append(out, Message{ID: "m" + itoa(i), Seq: int64(i), Role: role, Content: "내용" + itoa(i), State: "complete", CreatedAt: base.Add(time.Duration(i) * time.Minute)})
	}
	return out
}

func seqs(ms []Message) []int64 {
	out := []int64{}
	for _, m := range ms {
		out = append(out, m.Seq)
	}
	return out
}

func TestLaggingSummaryNeverDropsLatestMessages(t *testing.T) {
	history := msgs(20)
	current := Message{ID: "m21", Seq: 21, Role: "user", Content: "지금 말", State: "complete"}
	limits := Limits{MinRecent: 2, MaxRecent: 30, MaxFacts: 16, MaxEpisodes: 12}
	// The summary covers only up to 5; messages 6..20 must all be raw.
	c := Assemble(current, history, &Summary{Content: "요약", ThroughSeq: 5, MemoryGeneration: 3}, 3, nil, nil, nil, limits)
	if c.SummaryStatus != "current" || len(c.Recent) != 15 || c.Recent[0].Seq != 6 || c.Recent[14].Seq != 20 || c.Omitted != nil {
		t.Fatalf("recent %v omitted %+v", seqs(c.Recent), c.Omitted)
	}
	// With a smaller raw window the gap is reported, never hidden.
	limits.MaxRecent = 6
	c = Assemble(current, history, &Summary{Content: "요약", ThroughSeq: 5, MemoryGeneration: 3}, 3, nil, nil, nil, limits)
	if c.Recent[len(c.Recent)-1].Seq != 20 || c.Omitted == nil || c.Omitted.From != 6 || c.Omitted.To != 14 {
		t.Fatalf("recent %v omitted %+v", seqs(c.Recent), c.Omitted)
	}
	p, _ := json.Marshal(c.Payload())
	if !strings.Contains(string(p), `"omitted_messages":{"from":"msg:6"`) {
		t.Fatalf("payload does not report gap: %s", p)
	}
}

func TestStaleSummaryIgnoredAndPendingRepliesSkipped(t *testing.T) {
	history := msgs(6)
	history = append(history, Message{ID: "p", Seq: 7, Role: "assistant", State: "pending"}, Message{ID: "u", Seq: 8, Role: "assistant", State: "unknown"})
	current := Message{ID: "m9", Seq: 9, Role: "user", Content: "지금", State: "complete"}
	c := Assemble(current, history, &Summary{Content: "정정 전 요약", ThroughSeq: 4, MemoryGeneration: 1}, 2, nil, nil, nil, DefaultLimits)
	if c.Summary != nil || c.SummaryStatus != "stale_ignored" {
		t.Fatalf("stale summary used: %+v", c.Summary)
	}
	for _, m := range c.Recent {
		if m.State != "complete" {
			t.Fatalf("incomplete reply in context: %+v", m)
		}
	}
	p, _ := json.Marshal(c.Payload())
	if strings.Contains(string(p), "정정 전 요약") {
		t.Fatal("stale summary text leaked")
	}
}

func TestRedactedMessagesAndContradictoryEpisodes(t *testing.T) {
	history := msgs(4)
	current := Message{ID: "m5", Seq: 5, Role: "user", Content: "지금", State: "complete"}
	at := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	records := []memory.Record{
		{ID: "a", Fact: memory.Fact{Key: "wish", Value: "다시 만나고 싶다", ObservedAt: at}, Current: true, Meta: memory.Meta{Class: memory.ClassEpisode, Origin: memory.OriginStated, Situation: "혼자 있는 밤"}},
		{ID: "b", Fact: memory.Fact{Key: "feeling", Value: "연락은 부담스럽다", ObservedAt: at.Add(48 * time.Hour)}, Current: true, Meta: memory.Meta{Class: memory.ClassEpisode, Origin: memory.OriginStated, Situation: "상대 SNS를 본 뒤"}},
		{ID: "c", Fact: memory.Fact{Key: "relationship_status", Value: "헤어짐", ObservedAt: at}, Current: true},
		{ID: "old", Fact: memory.Fact{Key: "relationship_status", Value: "연애 중", ObservedAt: at.Add(-time.Hour)}, Current: false},
	}
	c := Assemble(current, history, nil, 0, records, nil, map[string]bool{"m3": true}, DefaultLimits)
	if len(c.Episodes) != 2 || len(c.Facts) != 1 || c.Facts[0].Value != "헤어짐" {
		t.Fatalf("memory selection facts=%v episodes=%v", c.Facts, c.Episodes)
	}
	p, _ := json.Marshal(c.Payload())
	if strings.Contains(string(p), "내용3") || !strings.Contains(string(p), "msg:3") || strings.Contains(string(p), "연애 중") {
		t.Fatalf("redaction or superseded fact failed: %s", p)
	}
	ids := c.EvidenceIDs()
	for _, id := range []string{"msg:5", "msg:1", "mem:a", "mem:b", "mem:c"} {
		if !ids[id] {
			t.Fatalf("missing evidence id %s", id)
		}
	}
	if ids["mem:old"] {
		t.Fatal("superseded fact citable")
	}
}

func TestParseChecksEvidenceAndCandidateSources(t *testing.T) {
	current := Message{ID: "m3", Seq: 3, Role: "user", Content: "어제 또 연락했는데 답이 없었어. 이제 연락은 안 하고 싶어.", State: "complete", CreatedAt: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	c := Assemble(current, msgs(2), nil, 0, nil, nil, nil, DefaultLimits)
	text := "```json\n" + `{"assessment":{"want":"vent","topics":["contact"],"topic_certainty":"medium","tried":[],"avoid":[],"capacity":"listen_only","unknowns":[],"user_feelings":[],"guesses_about_other":[]},"response_mode":"brief_empathy","asked_question":false,"reply":"그랬구나.","evidence_ids":["msg:3","mem:invented","msg:3"],"memory_candidates":[
	{"class":"episode","key":"attempt","value":"어제 연락했지만 답이 없었다","origin":"user_stated","quote":"어제 또 연락했는데 답이 없었어","subject":"partner","situation":"","occurred_at":"2026-10-02"},
	{"class":"fact","key":"contact_boundary","value":"연락하지 않기로 함","origin":"user_stated","quote":"연락은 절대 안 해","subject":"self","situation":"","occurred_at":""},
	{"class":"episode","key":"personality","value":"회피형","origin":"ai_inference","quote":"","subject":"partner","situation":"","occurred_at":""},
	{"class":"episode","key":"feeling","value":"상대가 아직 좋아한다","origin":"ai_inference","quote":"","subject":"partner","situation":"","occurred_at":""}]}` + "\n```"
	out, checks, err := Parse(text, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.EvidenceIDs) != 1 || out.EvidenceIDs[0] != "msg:3" {
		t.Fatalf("evidence %v", out.EvidenceIDs)
	}
	if len(out.MemoryCandidates) != 3 || out.MemoryCandidates[0].Origin != "user_stated" || out.MemoryCandidates[1].Origin != "ai_inference" || out.MemoryCandidates[2].Origin != "ai_inference" {
		t.Fatalf("candidates %+v", out.MemoryCandidates)
	}
	status := map[string]string{}
	for _, ch := range checks {
		status[ch.Name] = ch.Status
	}
	if status["evidence_ids_known"] != "fixed" || status["memory_candidates_sourced"] != "fixed" {
		t.Fatalf("checks %+v", checks)
	}
	fact, meta := out.MemoryCandidates[0].MemoryFact(current, 0)
	if !fact.ObservedAt.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)) || meta.SourceMessageID != "m3" || fact.SourceEventID != "message:m3:0" {
		t.Fatalf("memory fact %+v %+v", fact, meta)
	}
	for _, bad := range []string{"", "답변", `{"reply":"x","evidence_ids":[],"memory_candidates":[]}`, `{"reply":""}`, `{"reply":"x","extra":1}`, `{"assessment":{"want":"vent","topics":["contact"],"topic_certainty":"medium","tried":[],"avoid":[],"capacity":"listen_only","unknowns":[],"user_feelings":[],"guesses_about_other":[]},"response_mode":"brief_empathy","asked_question":false,"reply":"x"}{"assessment":{"want":"vent","topics":["contact"],"topic_certainty":"medium","tried":[],"avoid":[],"capacity":"listen_only","unknowns":[],"user_feelings":[],"guesses_about_other":[]},"response_mode":"brief_empathy","asked_question":false,"reply":"y"}`} {
		if _, _, err := Parse(bad, c); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestStyleChangesWordingOnly(t *testing.T) {
	current := Message{ID: "m3", Seq: 3, Role: "user", Content: "연락 문제로 속상해", State: "complete"}
	c := Assemble(current, msgs(2), nil, 0, nil, nil, nil, DefaultLimits)
	plain, err := TurnInput("conv", c, "")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := TurnInput("conv", c, DefaultStyle)
	if err != nil {
		t.Fatal(err)
	}
	if plain.UserPrompt != cat.UserPrompt || strings.Join(plain.Sources, ",") != strings.Join(cat.Sources, ",") || cat.SystemPrompt != plain.SystemPrompt+"\n\n"+DefaultStyle {
		t.Fatal("character style changed context, evidence or rules")
	}
	for _, banned := range []string{"MBTI", "애착"} {
		if strings.Contains(plain.UserPrompt, banned) {
			t.Fatalf("type-based input %s", banned)
		}
	}
}

func TestAssessmentChecksFlagMismatches(t *testing.T) {
	current := Message{ID: "m1", Seq: 1, Role: "user", Content: "그냥 들어줘", State: "complete"}
	c := Assemble(current, nil, nil, 0, nil, nil, nil, DefaultLimits)
	out := `{"assessment":{"want":"vent","topics":["vague_distress","astrology"],"topic_certainty":"sure","tried":[{"what":"산책","result":"별로","source":"msg:99"}],"avoid":["연락하라는 말"],"capacity":"listen_only","unknowns":[],"user_feelings":["지침"],"guesses_about_other":[]},"response_mode":"options","asked_question":false,"reply":"세 가지를 해 보세요. 왜 그런가요? 언제부터요?","evidence_ids":["msg:1"],"memory_candidates":[]}`
	got, checks, err := Parse(out, c)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, ch := range checks {
		status[ch.Name] = ch.Status
	}
	if status["assessment_fields"] != "fixed" || status["asked_question_flag"] != "warn" || status["mode_matches_request"] != "warn" {
		t.Fatalf("checks %+v", checks)
	}
	if len(got.Assessment.Topics) != 1 || got.Assessment.TopicCertainty != "low" || got.Assessment.Tried[0].Source != "" {
		t.Fatalf("normalized %+v", got.Assessment)
	}
	bad := strings.Replace(out, `"want":"vent"`, `"want":"advice"`, 1)
	if _, _, err = Parse(bad, c); err == nil {
		t.Fatal("invalid want accepted")
	}
}
