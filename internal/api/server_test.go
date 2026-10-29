package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

func setup(t *testing.T) http.Handler {
	t.Helper()
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	m, err := memory.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return (&Server{Lab: l, Memory: m, Tokens: map[string]string{"a-secret": "alice", "b-secret": "bob"}}).Handler()
}
func call(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAuthAndIdentityCannotBeSpoofed(t *testing.T) {
	h := setup(t)
	if w := call(h, "GET", "/v1/memory", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	body := `{"key":"relationship_status","value":"dating","source_event_id":"e1","observed_at":"2026-10-20T12:00:00Z"}`
	if w := call(h, "POST", "/v1/memory", "a-secret", body); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(h, "GET", "/v1/memory", "b-secret", ""); w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("dating")) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(h, "POST", "/v1/memory", "b-secret", `{"user_id":"alice",`+body[1:]); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := call(h, "DELETE", "/v1/memory", "b-secret", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call(h, "GET", "/v1/memory", "a-secret", ""); !bytes.Contains(w.Body.Bytes(), []byte("dating")) {
		t.Fatal("Bob deleted Alice's memory")
	}
}
func TestMemoryFeedsCounselOnlyWhenRequested(t *testing.T) {
	h := setup(t)
	call(h, "POST", "/v1/memory", "a-secret", `{"key":"contact_boundary","value":"no_contact","source_event_id":"event-block","observed_at":"2026-10-20T12:00:00Z"}`)
	input := map[string]any{"question": "다시 대화를 시작하고 싶어", "topic": "conflict_reconnect"}
	req := map[string]any{"case_id": "safe-case", "input": input, "use_memory": true}
	body, _ := json.Marshal(req)
	w := call(h, "POST", "/v1/compare/counsel", "a-secret", string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("event-block")) {
		t.Fatal("missing source memory")
	}
	w = call(h, "POST", "/v1/compare/counsel", "b-secret", string(body))
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("event-block")) {
		t.Fatal("cross-user memory leak", w.Body.String())
	}
	req["use_memory"] = false
	body, _ = json.Marshal(req)
	w = call(h, "POST", "/v1/compare/counsel", "a-secret", string(body))
	if bytes.Contains(w.Body.Bytes(), []byte("event-block")) {
		t.Fatal("memory unexpectedly enabled")
	}
}
func TestLimitsAndExplicitDailyDate(t *testing.T) {
	h := setup(t)
	if w := call(h, "POST", "/v1/compare/tarot", "a-secret", string(bytes.Repeat([]byte("x"), 130*1024))); w.Code != 413 {
		t.Fatal(w.Code)
	}
	if w := call(h, "POST", "/v1/compare/tarot", "a-secret", `{} {}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := call(h, "POST", "/v1/compare/daily", "a-secret", `{"input":{"question":"today"}}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestMemoryContextBudgetAndPriority(t *testing.T) {
	rows := []memory.Record{}
	for i := 0; i < 60; i++ {
		rows = append(rows, memory.Record{Fact: memory.Fact{Key: fmt.Sprint("fact", i), Value: "short", SourceEventID: "e", ObservedAt: time.Unix(int64(i), 0)}})
	}
	rows = append(rows, memory.Record{Fact: memory.Fact{Key: "contact_boundary", Value: "no_contact", SourceEventID: "boundary", ObservedAt: time.Unix(1, 0)}})
	rows = append(rows, memory.Record{Fact: memory.Fact{Key: "oversized", Value: strings.Repeat("가", 1001), SourceEventID: "big", ObservedAt: time.Unix(100, 0)}})
	facts, report, err := selectMemory(rows, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 16 || facts[0].Key != "contact_boundary" || report.Selected != 16 || len(report.ExcludedKeys) != 46 {
		t.Fatal(facts, report)
	}
	if _, _, err := selectMemory(rows, 50); err == nil {
		t.Fatal("boundary silently omitted")
	}
}
