package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

const readingBody = `{"domain":"counsel","variant":"enhanced","input":{"question":"연락 기준을 함께 정하고 싶어","topic":"negotiate_contact"},"use_memory":true}`

func callReading(h http.Handler, token, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/v1/readings", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestReadingHTTPFlowFreeSummaryLockedDetailAndFrozenMemory(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Error("wrong provider route", r.URL.Path)
		}
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		prompts = append(prompts, req.Messages[len(req.Messages)-1].Content)
		n := len(prompts)
		mu.Unlock()
		// Explicit scripted provider for transport/flow tests, not an AI assessment.
		text := "TEST-SUMMARY: 서로 연락에 관해 합의한 내용을 먼저 확인해요."
		if n > 1 {
			text = "TEST-DETAIL: 가능한 연락 기준을 함께 정리해요."
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "scripted-test-provider", "choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30, "total_tokens": 130}})
	}))
	defer provider.Close()
	client, err := generation.New(generation.Config{APIBase: provider.URL, Model: "scripted-test-provider", MaxCompletionTokens: 100}, provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	m, err := memory.Open("")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewReadingService(l, m, client, t.TempDir()+"/readings.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Lab: l, Memory: m, Readings: svc, Tokens: map[string]string{"alice-token": "alice", "bob-token": "bob"}}
	h := s.Handler()
	setFact := func(value, observed string) {
		t.Helper()
		w := call(h, "POST", "/v1/memory", "alice-token", fmt.Sprintf(`{"key":"relationship_status","value":%q,"source_event_id":%q,"observed_at":%q}`, value, "event-"+value, observed))
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	setFact("ORIGINAL_SNAPSHOT", "2026-10-20T12:00:00Z")
	if w := callReading(h, "alice-token", "", readingBody); w.Code != 400 {
		t.Fatal("missing key", w.Code, w.Body.String())
	}
	w := callReading(h, "alice-token", "request-1", readingBody)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v readings.View
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	id, ok := response["id"].(string)
	if !ok || id == "" {
		t.Fatal("missing id", w.Body.String())
	}
	for _, private := range []string{"ORIGINAL_SNAPSHOT", "system_prompt", "user_prompt", "REFERENCE_CARDS"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private prompt leak", private)
		}
	}
	if w := call(h, "POST", "/v1/readings/"+id+"/detail", "alice-token", ""); w.Code != 402 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(h, "POST", "/v1/dev/readings/"+id+"/unlock", "alice-token", ""); w.Code != 404 {
		t.Fatal("dev unlock on by default", w.Code)
	}
	setFact("LATER_CHANGED_MEMORY", "2026-10-21T12:00:00Z")
	w = callReading(h, "alice-token", "request-1", readingBody)
	if w.Code != 201 || !strings.Contains(w.Body.String(), id) {
		t.Fatal("retry changed snapshot", w.Code, w.Body.String())
	}
	if w := callReading(h, "alice-token", "request-1", strings.Replace(readingBody, "함께 정하고 싶어", "다르게 정하고 싶어", 1)); w.Code != 409 {
		t.Fatal("key conflict", w.Code)
	}
	s.AllowTestUnlock = true
	if w := call(h, "POST", "/v1/dev/readings/"+id+"/unlock", "bob-token", ""); w.Code != 404 {
		t.Fatal("cross-user unlock", w.Code)
	}
	if w := call(h, "POST", "/v1/dev/readings/"+id+"/unlock", "alice-token", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for i := 0; i < 2; i++ {
		if w := call(h, "POST", "/v1/readings/"+id+"/detail", "alice-token", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "TEST-DETAIL") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := call(h, "GET", "/v1/readings/"+id, "bob-token", ""); w.Code != 404 {
		t.Fatal("cross-user get", w.Code)
	}
	if w := call(h, "GET", "/v1/readings", "bob-token", ""); w.Code != 200 || strings.Contains(w.Body.String(), id) {
		t.Fatal("cross-user archive", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 {
		t.Fatal("duplicate generation", len(prompts))
	}
	if !strings.Contains(prompts[1], "TEST-SUMMARY") || !strings.Contains(prompts[1], "ORIGINAL_SNAPSHOT") || strings.Contains(prompts[1], "LATER_CHANGED_MEMORY") {
		t.Fatal("detail did not use frozen context")
	}
}

func TestReadingGenerationMustBeExplicitlyConfigured(t *testing.T) {
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := memory.Open("")
	svc, err := NewReadingService(l, m, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	h := (&Server{Lab: l, Memory: m, Readings: svc, Tokens: map[string]string{"token": "alice"}}).Handler()
	if w := callReading(h, "token", "key", readingBody); w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(h, "GET", "/v1/readings", "token", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := callReading(h, "token", "key", `{"user_id":"bob","domain":"counsel"}`); w.Code != 400 {
		t.Fatal("identity accepted", w.Code)
	}
}
