package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

type dailyBuilder struct{}

func (dailyBuilder) Build(_ context.Context, domain, variant string, input json.RawMessage) (generation.Input, error) {
	return generation.Input{CaseID: "test", VariantID: variant, Question: "test", UserPrompt: "test", CaseContext: input, Sources: []string{"fixture"}}, nil
}

type dailyGenerator struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (g *dailyGenerator) Generate(_ context.Context, _ generation.Input) (generation.Result, error) {
	g.calls.Add(1)
	select {
	case g.started <- struct{}{}:
	default:
	}
	<-g.release
	return generation.Result{Status: "generated", Text: "오늘은 상황을 살펴보고 대화를 시도해 보세요.", FinishReason: "stop"}, nil
}
func TestProfileAndDailyPostgres(t *testing.T) {
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
	suffix := fmt.Sprint(time.Now().UnixNano())
	a, b := "profile-a-"+suffix, "profile-b-"+suffix
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	gen := &dailyGenerator{started: make(chan struct{}, 4), release: make(chan struct{})}
	server := &Server{ProfileDaily: persistence.Store{DB: db}, DailyModelVersion: "test-model", Readings: &readings.PostgresService{DB: db, Builder: dailyBuilder{}, Generator: gen, AllowTestUnlock: true}, Tokens: map[string]string{"a": a, "b": b}}
	h := server.Handler()
	unknown := `{"birth":{"year":1990,"month":1,"day":1,"hour":null,"minute":0,"time_zone":"Asia/Seoul"}}`
	if w := call(h, "POST", "/v1/profiles", "a", unknown); w.Code != 400 {
		t.Fatalf("unknown hour: %d", w.Code)
	}
	body := `{"birth":{"year":1990,"month":1,"day":1,"hour":12,"minute":0,"time_zone":"Asia/Seoul"}}`
	w := call(h, "POST", "/v1/profiles", "a", body)
	if w.Code != 201 {
		t.Fatalf("profile %d %s", w.Code, w.Body.String())
	}
	var profile persistence.Profile
	if err = json.Unmarshal(w.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if w = call(h, "GET", "/v1/profiles", "a", ""); w.Code != 200 || !strings.Contains(w.Body.String(), profile.ID) {
		t.Fatalf("own profile list %d", w.Code)
	}
	if w = call(h, "GET", "/v1/profiles", "b", ""); w.Code != 200 || strings.Contains(w.Body.String(), profile.ID) {
		t.Fatalf("cross user profile list %d", w.Code)
	}
	path := "/v1/profiles/" + profile.ID
	if w = call(h, "GET", path, "b", ""); w.Code != 404 {
		t.Fatalf("cross-user profile %d", w.Code)
	}
	req := fmt.Sprintf(`{"profile_id":%q,"time_zone":"Asia/Seoul"}`, profile.ID)
	if w = call(h, "POST", "/v1/daily", "a", fmt.Sprintf(`{"profile_id":%q}`, profile.ID)); w.Code != 400 {
		t.Fatalf("missing current timezone %d", w.Code)
	}
	if w = call(h, "POST", "/v1/daily", "a", fmt.Sprintf(`{"profile_id":%q,"time_zone":"UTC"}`, profile.ID)); w.Code != 422 {
		t.Fatalf("different current timezone %d", w.Code)
	}
	first := make(chan int, 1)
	go func() { first <- call(h, "POST", "/v1/daily", "a", req).Code }()
	select {
	case <-gen.started:
	case <-time.After(3 * time.Second):
		t.Fatal("generation did not start")
	}
	if w = call(h, "POST", "/v1/daily", "a", req); w.Code != 409 {
		t.Fatalf("concurrent %d %s", w.Code, w.Body.String())
	}
	if gen.calls.Load() != 1 {
		t.Fatalf("duplicate calls %d", gen.calls.Load())
	}
	close(gen.release)
	if code := <-first; code != 200 {
		t.Fatalf("first %d", code)
	}
	w = call(h, "POST", "/v1/daily", "a", req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "summary_ready") {
		t.Fatalf("cached %d %s", w.Code, w.Body.String())
	}
	if gen.calls.Load() != 1 {
		t.Fatalf("cache called model %d", gen.calls.Load())
	}
	var cached readings.View
	if err = json.Unmarshal(w.Body.Bytes(), &cached); err != nil {
		t.Fatal(err)
	}
	if _, err = server.Readings.UnlockForTest(a, cached.ID); err != nil {
		t.Fatal(err)
	}
	detail, err := server.Readings.Detail(ctx, a, cached.ID)
	if err != nil || detail.Detail == "" {
		t.Fatalf("detail setup %v", err)
	}
	if _, err = db.Exec(`DELETE FROM daily_cache WHERE account_id=$1`, a); err != nil {
		t.Fatal(err)
	}
	w = call(h, "POST", "/v1/daily", "a", req)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"detail":`) || !strings.Contains(w.Body.String(), `"detail_locked":true`) {
		t.Fatalf("detail in rebuilt cache %d %s", w.Code, w.Body.String())
	}
	var stored string
	if err = db.QueryRow(`SELECT result::text FROM daily_cache WHERE account_id=$1`, a).Scan(&stored); err != nil || strings.Contains(stored, `"detail":`) {
		t.Fatalf("detail persisted: %v", err)
	}
	if _, err = db.Exec(`DELETE FROM dev_reading_unlocks WHERE account_id=$1`, a); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE daily_cache SET result = result || '{"detail":"private-old-cache"}'::jsonb WHERE account_id=$1`, a); err == nil {
		t.Fatal("database accepted paid detail in daily cache")
	}
	w = call(h, "POST", "/v1/daily", "a", req)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"detail":`) || strings.Contains(w.Body.String(), "private-old-cache") {
		t.Fatalf("detail after revocation %d %s", w.Code, w.Body.String())
	}
	if w = call(h, "POST", "/v1/daily", "b", req); w.Code != 404 {
		t.Fatalf("cross-user daily %d", w.Code)
	}
	update := `{"version":1,"birth":{"year":1990,"month":1,"day":1,"hour":13,"minute":0,"time_zone":"Asia/Seoul"}}`
	if w = call(h, "PUT", path, "a", update); w.Code != 200 {
		t.Fatalf("update %d %s", w.Code, w.Body.String())
	}
	if w = call(h, "PUT", path, "a", update); w.Code != 409 {
		t.Fatalf("stale update %d", w.Code)
	}
	if w = call(h, "POST", "/v1/daily", "a", req); w.Code != 200 {
		t.Fatalf("new profile %d %s", w.Code, w.Body.String())
	}
	if gen.calls.Load() != 3 {
		t.Fatalf("version change did not regenerate: %d", gen.calls.Load())
	}
}

func TestDailyRealEngineWithMockProvider(t *testing.T) {
	dsn := os.Getenv("NYANG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NYANG_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistence.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	a := fmt.Sprintf("daily-engine-%d", time.Now().UnixNano())
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1)`, a); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id=$1`, a)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"model":"mock","choices":[{"message":{"content":"오늘은 일정을 살펴보고 차분하게 대화를 시작해 보세요."},"finish_reason":"stop"}]}`))
	}))
	defer provider.Close()
	client, err := generation.New(generation.Config{APIBase: provider.URL, Model: "mock", MaxCompletionTokens: 100}, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	h := (&Server{ProfileDaily: persistence.Store{DB: db}, DailyModelVersion: "mock", Readings: NewPostgresReadingService(db, l, nil, client, false), Tokens: map[string]string{"a": a}}).Handler()
	body := `{"birth":{"year":1990,"month":1,"day":1,"hour":12,"minute":0,"time_zone":"Asia/Seoul"}}`
	w := call(h, "POST", "/v1/profiles", "a", body)
	if w.Code != 201 {
		t.Fatalf("profile %d %s", w.Code, w.Body.String())
	}
	var p persistence.Profile
	if err = json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	req := fmt.Sprintf(`{"profile_id":%q,"time_zone":"Asia/Seoul"}`, p.ID)
	for i := 0; i < 2; i++ {
		w = call(h, "POST", "/v1/daily", "a", req)
		if w.Code != 200 {
			t.Fatalf("daily %d %s", w.Code, w.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls=%d", calls.Load())
	}
}
