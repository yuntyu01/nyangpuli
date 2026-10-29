package generation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sampleInputs() []Input {
	return []Input{{CaseID: "case-1", VariantID: "base", Question: "연락 기준을 어떻게 합의할까?", SystemPrompt: "Do not invent facts.", UserPrompt: "평일에는 답장이 늦어 서운해요.", Sources: []string{"source@pin"}}}
}

func TestOptInAndContract(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing auth")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "fixed-model" || body["store"] != false || body["max_completion_tokens"] != float64(1024) {
			t.Errorf("wrong config: %v", body)
		}
		if _, ok := body["temperature"]; ok {
			t.Error("unsupported default temperature was sent")
		}
		if len(body["messages"].([]any)) != 2 {
			t.Error("messages missing")
		}
		_, _ = w.Write([]byte(`{"id":"id1","model":"fixed-model-snapshot","choices":[{"message":{"content":"서로 가능한 연락 시간을 먼저 물어보세요."},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))
	}))
	defer s.Close()
	c, err := New(Config{APIBase: s.URL + "/v1", APIKey: "test-secret", Model: "fixed-model", MaxCompletionTokens: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Run(context.Background(), sampleInputs(), c, false)
	if err != nil || calls != 0 || got[0].Status != "not_run" {
		t.Fatalf("disabled generation: %v %v calls=%d", got, err, calls)
	}
	got, err = Run(context.Background(), sampleInputs(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got[0].Status != "generated" || got[0].Usage.TotalTokens != 30 || len(got[0].PromptSHA256) != 64 {
		t.Fatalf("unexpected result: %+v", got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "test-secret") {
		t.Fatal("secret leaked")
	}
}

func TestProviderErrorsDoNotLeakOrRetry(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`secret echoed private prompt`))
	}))
	defer s.Close()
	c, _ := New(Config{APIBase: s.URL, Model: "test", MaxCompletionTokens: 10}, nil)
	got, err := Run(context.Background(), sampleInputs(), c, true)
	if err == nil || calls != 1 || got[0].Status != "failed" {
		t.Fatal("failure not captured exactly once")
	}
	if strings.Contains(err.Error(), "private prompt") {
		t.Fatal("provider error body leaked")
	}
}

func TestRejectTruncatedAnswer(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"test","choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`))
	}))
	defer s.Close()
	c, _ := New(Config{APIBase: s.URL, Model: "test", MaxCompletionTokens: 10}, nil)
	got, err := Run(context.Background(), sampleInputs(), c, true)
	if err == nil || got[0].Status != "failed" || got[0].Text != "partial" {
		t.Fatal("truncated answer falsely marked complete")
	}
}

func TestNoRedirect(t *testing.T) {
	redirectCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++ }))
	defer destination.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer s.Close()
	c, _ := New(Config{APIBase: s.URL, Model: "test", MaxCompletionTokens: 10}, nil)
	_, err := Run(context.Background(), sampleInputs(), c, true)
	if err == nil || redirectCalls != 0 {
		t.Fatal("redirect followed")
	}
}

func TestValidationAndEnv(t *testing.T) {
	_, err := Run(context.Background(), sampleInputs(), nil, true)
	if err == nil {
		t.Fatal("missing client accepted")
	}
	inputs := append(sampleInputs(), sampleInputs()[0])
	_, err = Run(context.Background(), inputs, nil, false)
	if err == nil {
		t.Fatal("duplicate case accepted")
	}
	for _, base := range []string{"http://example.com/v1", "https://user:secret@example.com/v1", "https://example.com/v1?key=secret"} {
		_, err := New(Config{APIBase: base, Model: "test", APIKey: "key", MaxCompletionTokens: 10}, nil)
		if err == nil {
			t.Fatalf("unsafe base accepted: %s", base)
		}
	}
	t.Setenv("NYANG_LLM_API_BASE", "https://example.com/v1")
	t.Setenv("NYANG_LLM_MODEL", "chosen-model")
	t.Setenv("NYANG_LLM_API_KEY", "secret")
	t.Setenv("NYANG_LLM_MAX_TOKENS", "1024")
	t.Setenv("NYANG_LLM_TEMPERATURE", "")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.Model != "chosen-model" {
		t.Fatalf("env configuration: %+v %v", cfg, err)
	}
	t.Setenv("NYANG_LLM_TEMPERATURE", "NaN")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("NaN accepted")
	}
}
