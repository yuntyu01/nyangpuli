package readings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
)

type testBuilder struct {
	calls  int
	marker string
	err    error
}

func (b *testBuilder) Build(_ context.Context, domain, variant string, input json.RawMessage) (generation.Input, error) {
	b.calls++
	if b.err != nil {
		return generation.Input{}, b.err
	}
	return generation.Input{CaseID: "test-case", VariantID: variant, Question: "합성 질문", UserPrompt: "고정 근거 " + b.marker, SystemPrompt: "기존 지침", CaseContext: append(json.RawMessage(nil), input...), Sources: []string{"test-source@v1"}}, nil
}

type testGenerator struct {
	calls   []generation.Input
	results []generation.Result
	errs    []error
	mutate  bool
}

func (g *testGenerator) Generate(_ context.Context, in generation.Input) (generation.Result, error) {
	g.calls = append(g.calls, cloneInput(in))
	i := len(g.calls) - 1
	if g.mutate {
		in.Sources[0] = "changed"
		if len(in.CaseContext) > 0 {
			in.CaseContext[0] = '!'
		}
	}
	var err error
	if i < len(g.errs) {
		err = g.errs[i]
	}
	if i < len(g.results) {
		return g.results[i], err
	}
	text := "모의 제공자 시험용 요약"
	if strings.HasSuffix(in.VariantID, ":detail") {
		text = "모의 제공자 시험용 상세풀이"
	}
	return generation.Result{Status: "generated", Text: text, FinishReason: "stop", Usage: generation.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, err
}

func newTestService(t *testing.T, path string) (*Service, *testBuilder, *testGenerator) {
	t.Helper()
	b := &testBuilder{marker: "BEFORE"}
	g := &testGenerator{}
	s, err := New(b, g, path)
	if err != nil {
		t.Fatal(err)
	}
	return s, b, g
}
func createTest(t *testing.T, s *Service, user, key string) View {
	t.Helper()
	v, err := s.Create(context.Background(), user, key, "counsel", "baseline", json.RawMessage(`{"input":{"question":"합성 질문"},"use_memory":false}`))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSummaryLockedDetailThenReplay(t *testing.T) {
	s, b, g := newTestService(t, "")
	v := createTest(t, s, "alice", "request1")
	if v.Status != "summary_ready" || !v.DetailLocked || v.Summary == "" || v.Detail != "" || len(g.calls) != 1 || b.calls != 1 {
		t.Fatal(v, b.calls, len(g.calls))
	}
	if _, err := s.Detail(context.Background(), "alice", v.ID); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if len(g.calls) != 1 {
		t.Fatal("detail generated before entitlement")
	}
	got, err := s.Get("alice", v.ID)
	if err != nil || got.Detail != "" {
		t.Fatal(got, err)
	}
	encoded, _ := json.Marshal(got)
	for _, forbidden := range []string{"BEFORE", "frozen_input", "system_prompt", "idempotency_key", `"user"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("internal state leaked", forbidden)
		}
	}
	if _, err := s.UnlockForTest("alice", v.ID); err != nil {
		t.Fatal(err)
	}
	if len(g.calls) != 1 {
		t.Fatal("unlock generated reading")
	}
	full, err := s.Detail(context.Background(), "alice", v.ID)
	if err != nil || full.Status != "detail_ready" || full.DetailLocked || full.Detail == "" || full.EntitlementSource != "test_only_not_payment" {
		t.Fatal(full, err)
	}
	replay, err := s.Detail(context.Background(), "alice", v.ID)
	if err != nil || replay.Detail != full.Detail || len(g.calls) != 2 {
		t.Fatal(replay, err, len(g.calls))
	}
	if got := createTest(t, s, "alice", "request1"); got.ID != v.ID || got.Detail != full.Detail || len(g.calls) != 2 {
		t.Fatal(got)
	}
	if full.SummaryUsage.TotalTokens != 15 || full.DetailUsage.TotalTokens != 15 {
		t.Fatal(full)
	}
}

func TestFrozenEvidenceSummaryAndNoAliasing(t *testing.T) {
	s, b, g := newTestService(t, "")
	g.mutate = true
	v := createTest(t, s, "alice", "request1")
	v.SourceIDs[0] = "caller-change"
	b.marker = "AFTER"
	if _, err := s.UnlockForTest("alice", v.ID); err != nil {
		t.Fatal(err)
	}
	full, err := s.Detail(context.Background(), "alice", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.calls != 1 || full.SourceIDs[0] != "test-source@v1" {
		t.Fatal(b.calls, full.SourceIDs)
	}
	in := g.calls[1]
	if !strings.Contains(in.UserPrompt, "BEFORE") || strings.Contains(in.UserPrompt, "AFTER") || !strings.Contains(in.UserPrompt, v.Summary) || !json.Valid(in.CaseContext) || in.Sources[0] != "test-source@v1" {
		t.Fatal("snapshot not frozen", in)
	}
	if !strings.Contains(g.calls[0].SystemPrompt, "무료 요약") || !strings.Contains(in.SystemPrompt, "상세풀이") || !strings.Contains(in.SystemPrompt, "분량은 아래 현재 단계") {
		t.Fatal("stage instructions missing")
	}
}

func TestUserIsolationAndConflictingIdempotencyKey(t *testing.T) {
	s, _, g := newTestService(t, "")
	a := createTest(t, s, "alice", "shared-key")
	if _, err := s.Get("bob", a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Detail(context.Background(), "bob", a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.UnlockForTest("bob", a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	rows, err := s.List("bob")
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	for _, input := range []json.RawMessage{json.RawMessage(`{"input":{"question":"다른 질문"},"use_memory":false}`), json.RawMessage(`{"input":{"question":"합성 질문"},"use_memory":true}`)} {
		if _, err := s.Create(context.Background(), "alice", "shared-key", "counsel", "baseline", input); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	b := createTest(t, s, "bob", "shared-key")
	if b.ID == a.ID || len(g.calls) != 2 {
		t.Fatal(b.ID, a.ID, len(g.calls))
	}
	rows, err = s.List("alice")
	if err != nil || len(rows) != 1 || rows[0].ID != a.ID {
		t.Fatal(rows, err)
	}
}

func TestCanonicalInputReplayAndConcurrentRequests(t *testing.T) {
	s, b, g := newTestService(t, "")
	const count = 12
	var wg sync.WaitGroup
	ids := make(chan string, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.Create(context.Background(), "alice", "same-key", "counsel", "baseline", json.RawMessage(`{"use_memory":false, "input":{"question":"합성 질문"}}`))
			ids <- v.ID
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id string
	for value := range ids {
		if id == "" {
			id = value
		}
		if id != value {
			t.Fatal("duplicate reading IDs")
		}
	}
	if b.calls != 1 || len(g.calls) != 1 {
		t.Fatal(b.calls, len(g.calls))
	}
	if v := createTest(t, s, "alice", "same-key"); v.ID != id {
		t.Fatal(v.ID, id)
	}
	if _, err := s.UnlockForTest("alice", id); err != nil {
		t.Fatal(err)
	}
	errs = make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Detail(context.Background(), "alice", id); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(g.calls) != 2 {
		t.Fatal("duplicate detail calls", len(g.calls))
	}
}

func TestPersistRestartWithNoProviderStillReadsArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "readings.json")
	s, _, _ := newTestService(t, path)
	v := createTest(t, s, "alice", "key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("store should be private", info.Mode())
	}
	restarted, err := New(nil, nil, path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Get("alice", v.ID)
	if err != nil || got.Summary != v.Summary {
		t.Fatal(got, err)
	}
	if _, err := restarted.UnlockForTest("alice", v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Detail(context.Background(), "alice", v.ID); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	g := &testGenerator{}
	again, err := New(nil, g, path)
	if err != nil {
		t.Fatal(err)
	}
	full, err := again.Detail(context.Background(), "alice", v.ID)
	if err != nil || full.Detail == "" || len(g.calls) != 1 {
		t.Fatal(full, err)
	}
	offline, err := New(nil, nil, path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = offline.Detail(context.Background(), "alice", v.ID)
	if err != nil || got.Detail != full.Detail {
		t.Fatal(got, err)
	}
}

func TestFailedAndTruncatedSummaryRetryUsesFrozenEvidence(t *testing.T) {
	for _, kind := range []string{"error", "length", "empty", "not_run"} {
		t.Run(kind, func(t *testing.T) {
			s, b, g := newTestService(t, "")
			bad := generation.Result{Status: "generated", Text: "UNACCEPTED_PARTIAL", FinishReason: "stop", Usage: generation.Usage{PromptTokens: 20, CompletionTokens: 2, TotalTokens: 22}}
			switch kind {
			case "error":
				g.errs = []error{errors.New("provider private message")}
			case "length":
				bad.FinishReason = "length"
			case "empty":
				bad.Text = " "
			case "not_run":
				bad.Status = "not_run"
			}
			g.results = []generation.Result{bad}
			_, err := s.Create(context.Background(), "alice", "key", "counsel", "baseline", json.RawMessage(`{"q":"test"}`))
			if !errors.Is(err, ErrGeneration) {
				t.Fatal(err)
			}
			rows, _ := s.List("alice")
			wantStatus := "summary_failed"
			if kind == "error" {
				wantStatus = "summary_unknown"
			}
			if len(rows) != 1 || rows[0].Status != wantStatus || rows[0].Summary != "" {
				t.Fatal(rows)
			}
			if _, err := s.UnlockForTest("alice", rows[0].ID); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			b.marker = "AFTER"
			v, err := s.Create(context.Background(), "alice", "key", "counsel", "baseline", json.RawMessage(`{"q":"test"}`))
			if kind == "error" {
				if !errors.Is(err, ErrOutcomeUnknown) || len(g.calls) != 1 {
					t.Fatal(err, len(g.calls))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if b.calls != 1 || len(g.calls) != 2 || !strings.Contains(g.calls[1].UserPrompt, "BEFORE") || v.SummaryUsage.TotalTokens != 37 {
				t.Fatal(v, b.calls, len(g.calls))
			}
		})
	}
}

func TestDetailFailureDoesNotEraseSummaryOrEntitlement(t *testing.T) {
	s, _, g := newTestService(t, "")
	v := createTest(t, s, "alice", "key")
	if _, err := s.UnlockForTest("alice", v.ID); err != nil {
		t.Fatal(err)
	}
	g.errs = []error{nil, errors.New("fail")}
	if _, err := s.Detail(context.Background(), "alice", v.ID); !errors.Is(err, ErrGeneration) {
		t.Fatal(err)
	}
	failed, err := s.Get("alice", v.ID)
	if err != nil || failed.Status != "detail_unknown" || failed.Summary != v.Summary || failed.Detail != "" || failed.DetailLocked {
		t.Fatal(failed, err)
	}
	full, err := s.Detail(context.Background(), "alice", v.ID)
	if !errors.Is(err, ErrOutcomeUnknown) || full.Detail != "" || len(g.calls) != 2 {
		t.Fatal(full, err, len(g.calls))
	}
}

func TestInterruptedPendingCallIsNeverRepeatedAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.json")
	s, _, g := newTestService(t, path)
	g.errs = []error{errors.New("connection lost")}
	input := json.RawMessage(`{"q":"test"}`)
	if _, err := s.Create(context.Background(), "alice", "key", "counsel", "baseline", input); !errors.Is(err, ErrGeneration) {
		t.Fatal(err)
	}
	// A pending state is what remains when a process dies after sending but
	// before recording a response, including a successful provider response.
	for _, r := range s.state.Records {
		r.Status = "summary_pending"
		if err := s.save(r); err != nil {
			t.Fatal(err)
		}
	}
	restarted, err := New(nil, g, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Create(context.Background(), "alice", "key", "counsel", "baseline", input); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if len(g.calls) != 1 {
		t.Fatal("interrupted request was repeated")
	}
}

func TestInvalidInputNoCallsAndCancellation(t *testing.T) {
	s, b, g := newTestService(t, "")
	for _, raw := range []string{"", `null`, `[]`, `{} {}`, `{"broken":`, strings.Repeat("x", 128*1024+1)} {
		if _, err := s.Create(context.Background(), "alice", "key", "counsel", "baseline", json.RawMessage(raw)); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(raw[:min(len(raw), 30)], err)
		}
	}
	if _, err := s.Create(context.Background(), "", "key", "counsel", "baseline", json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Create(context.Background(), "alice", "", "counsel", "baseline", json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Create(ctx, "alice", "key", "counsel", "baseline", json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if b.calls != 0 || len(g.calls) != 0 {
		t.Fatal("invalid request made calls", b.calls, len(g.calls))
	}
	offline, err := New(b, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := offline.Create(context.Background(), "alice", "key", "counsel", "baseline", json.RawMessage(`{}`)); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	if b.calls != 0 {
		t.Fatal("offline request built evidence")
	}
}

func TestCorruptStoreAndWriteFailureDoNotGenerate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"records":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil, nil, path); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "parent")
	s, _, g := newTestService(t, filepath.Join(parent, "readings.json"))
	if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(context.Background(), "alice", "key", "counsel", "baseline", json.RawMessage(`{}`)); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
	if len(g.calls) != 0 {
		t.Fatal("generation made before storing snapshot")
	}
}
