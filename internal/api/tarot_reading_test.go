package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
	"github.com/yuntyu01/nyangpuli-lab/internal/tarot"
)

type readingTarotStore struct{ session tarot.Session }

func (s *readingTarotStore) CreateTarot(context.Context, string, tarot.Session) (string, error) {
	return "", errors.New("not used")
}
func (s *readingTarotStore) GetTarot(_ context.Context, user, id string) (tarot.Session, error) {
	if user != "alice" || id != "session1" {
		return tarot.Session{}, persistence.ErrTarotNotFound
	}
	return s.session, nil
}
func (s *readingTarotStore) SelectTarot(context.Context, string, string, *tarot.Engine, int) (tarot.Session, error) {
	return tarot.Session{}, errors.New("not used")
}

func TestTarotReadingUsesSavedDrawAndReplaysSameEvidence(t *testing.T) {
	var mu sync.Mutex
	prompts := []string{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
			t.Error("invalid provider request")
			w.WriteHeader(500)
			return
		}
		mu.Lock()
		prompts = append(prompts, req.Messages[len(req.Messages)-1].Content)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "scripted-test", "choices": []any{map[string]any{"message": map[string]string{"content": "시험 답변과 행동 제안"}, "finish_reason": "stop"}}})
	}))
	defer provider.Close()
	client, err := generation.New(generation.Config{APIBase: provider.URL, Model: "scripted-test", MaxCompletionTokens: 100}, provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	mem, err := memory.Open("")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewReadingService(l, mem, client, t.TempDir()+"/readings.json")
	if err != nil {
		t.Fatal(err)
	}
	session, err := l.Tarot.NewSession("이 상황을 어떻게 볼까요?", "love", "single-focus")
	if err != nil {
		t.Fatal(err)
	}
	store := &readingTarotStore{session: session}
	s := &Server{Lab: l, Memory: mem, Readings: svc, TarotStore: store, Tokens: map[string]string{"alice-token": "alice", "bob-token": "bob"}, AllowTestUnlock: true}
	h := s.Handler()
	path := "/v1/tarot/sessions/session1/reading"
	if w := call(h, "POST", path, "alice-token", ""); w.Code != 409 {
		t.Fatalf("incomplete %d %s", w.Code, w.Body.String())
	}
	if w := call(h, "POST", path, "bob-token", ""); w.Code != 404 {
		t.Fatalf("other user %d", w.Code)
	}
	if err = l.Tarot.Select(&store.session, 7); err != nil {
		t.Fatal(err)
	}
	draw := store.session.Deck[7]
	card := l.Tarot.Cards()[draw.CardID].Name
	w := call(h, "POST", path, "alice-token", `{"cards":[{"card_id":77}]}`)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var first readings.View
	if err = json.Unmarshal(w.Body.Bytes(), &first); err != nil || first.ID == "" || !first.DetailLocked {
		t.Fatalf("summary %v %s", err, w.Body.String())
	}
	w = call(h, "POST", path, "alice-token", "")
	var again readings.View
	_ = json.Unmarshal(w.Body.Bytes(), &again)
	if w.Code != 201 || again.ID != first.ID {
		t.Fatalf("retry %d %s", w.Code, w.Body.String())
	}
	if w = call(h, "POST", "/v1/readings/"+first.ID+"/detail", "alice-token", ""); w.Code != 402 {
		t.Fatalf("locked %d", w.Code)
	}
	if w = call(h, "POST", "/v1/dev/readings/"+first.ID+"/unlock", "alice-token", ""); w.Code != 200 {
		t.Fatalf("unlock %d", w.Code)
	}
	if w = call(h, "POST", "/v1/readings/"+first.ID+"/detail", "alice-token", ""); w.Code != 200 {
		t.Fatalf("detail %d %s", w.Code, w.Body.String())
	}
	if w = call(h, "GET", "/v1/readings/"+first.ID, "bob-token", ""); w.Code != 404 {
		t.Fatalf("cross user %d", w.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 || !strings.Contains(prompts[0], card) || !strings.Contains(prompts[1], card) {
		t.Fatalf("same saved card missing or duplicate calls: count=%d card=%s", len(prompts), card)
	}
	orientation := "upright"
	if draw.Reversed {
		orientation = "reversed"
	}
	for _, prompt := range prompts {
		if !strings.Contains(strings.ToLower(prompt), orientation) && !strings.Contains(prompt, map[bool]string{true: "역방향", false: "정방향"}[draw.Reversed]) {
			t.Fatalf("orientation missing: %s", orientation)
		}
	}
}

func TestRawTarotAndDailyGenerationRejected(t *testing.T) {
	h := (&Server{Tokens: map[string]string{"token": "alice"}, Readings: fakeJobs{}}).Handler()
	for _, domain := range []string{"tarot", "daily"} {
		body := `{"domain":"` + domain + `","variant":"enhanced","input":{},"use_memory":false}`
		if w := callReading(h, "token", "key", body); w.Code != 400 {
			t.Fatalf("direct %s: %d", domain, w.Code)
		}
		if w := call(h, "POST", "/v1/reading-jobs", "token", body); w.Code != 400 {
			t.Fatalf("async %s: %d", domain, w.Code)
		}
	}
}

func TestTarotReadingPostgres(t *testing.T) {
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
	user := "tarot-reading-" + fmt.Sprint(time.Now().UnixNano())
	if _, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1)`, user); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, user)
	var mu sync.Mutex
	var calls int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "scripted-test", "choices": []any{map[string]any{"message": map[string]string{"content": "시험용 요약과 제안"}, "finish_reason": "stop"}}})
	}))
	defer provider.Close()
	client, err := generation.New(generation.Config{APIBase: provider.URL, Model: "scripted-test", MaxCompletionTokens: 100}, provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	p := persistence.Store{DB: db}
	svc := NewPostgresReadingService(db, l, nil, client, true)
	s := &Server{Lab: l, TarotStore: p, Readings: svc, Tokens: map[string]string{"token": user}, AllowTestUnlock: true}
	session, err := l.Tarot.NewSession("합성 사례 질문", "love", "single-focus")
	if err != nil {
		t.Fatal(err)
	}
	id, err := p.CreateTarot(ctx, user, session)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.SelectTarot(ctx, user, id, l.Tarot, 6); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	path := "/v1/tarot/sessions/" + id + "/reading"
	w := call(h, "POST", path, "token", "")
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var first readings.View
	if err = json.Unmarshal(w.Body.Bytes(), &first); err != nil || first.ID == "" {
		t.Fatalf("reading %v", err)
	}
	// A fresh service instance simulates a new database connection and archive retrieval.
	s.Readings = NewPostgresReadingService(db, l, nil, client, true)
	w = call(s.Handler(), "POST", path, "token", "")
	var again readings.View
	_ = json.Unmarshal(w.Body.Bytes(), &again)
	if w.Code != 201 || again.ID != first.ID {
		t.Fatalf("replay %d %s", w.Code, w.Body.String())
	}
	if w = call(s.Handler(), "GET", "/v1/readings/"+first.ID, "token", ""); w.Code != 200 {
		t.Fatalf("archive %d", w.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("provider calls %d", calls)
	}
}
