package readings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type pgBuilder struct{}

func (pgBuilder) Build(_ context.Context, domain, variant string, input json.RawMessage) (generation.Input, error) {
	return generation.Input{CaseID: "test", VariantID: variant, Question: "question", UserPrompt: "frozen", SystemPrompt: "test", CaseContext: input, Sources: []string{"source-v1"}}, nil
}

type pgGenerator struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (g *pgGenerator) Generate(_ context.Context, in generation.Input) (generation.Result, error) {
	g.calls.Add(1)
	if g.started != nil {
		select {
		case g.started <- struct{}{}:
		default:
		}
		<-g.release
	}
	text := "summary"
	if strings.HasSuffix(in.VariantID, ":detail") {
		text = "detail"
	}
	return generation.Result{Status: "generated", Text: text, FinishReason: "stop"}, nil
}
func TestPostgresReadingWorkflow(t *testing.T) {
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
	a := fmt.Sprintf("reading-%d", time.Now().UnixNano())
	b := a + "-b"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	g := &pgGenerator{started: make(chan struct{}, 8), release: make(chan struct{})}
	s := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: g, AllowTestUnlock: true}
	input := json.RawMessage(`{"question":"case"}`)
	first := make(chan View, 1)
	firstErr := make(chan error, 1)
	go func() { v, e := s.Create(ctx, a, "key", "counsel", "enhanced", input); first <- v; firstErr <- e }()
	<-g.started
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Create(ctx, a, "key", "counsel", "enhanced", input); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if !errors.Is(e, ErrOutcomeUnknown) {
			t.Fatalf("duplicate %v", e)
		}
	}
	if g.calls.Load() != 1 {
		t.Fatalf("calls %d", g.calls.Load())
	}
	// A different account can start a provider call while the first call is blocked.
	other := make(chan error, 1)
	go func() { _, e := s.Create(ctx, b, "key", "counsel", "enhanced", input); other <- e }()
	select {
	case <-g.started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider call held global lock")
	}
	close(g.release)
	v := <-first
	if err = <-firstErr; err != nil {
		t.Fatal(err)
	}
	if err = <-other; err != nil {
		t.Fatal(err)
	}
	if v.Summary != "summary" || !v.DetailLocked {
		t.Fatal(v)
	}
	if _, err = s.Get(b, v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ownership %v", err)
	}
	if _, err = s.Detail(ctx, a, v.ID); !errors.Is(err, ErrLocked) {
		t.Fatalf("detail lock %v", err)
	}
	if _, err = s.UnlockForTest(a, v.ID); err != nil {
		t.Fatal(err)
	}
	g.started = nil
	g.release = nil
	d, err := s.Detail(ctx, a, v.ID)
	if err != nil || d.Detail != "detail" || d.Summary != "summary" {
		t.Fatalf("detail %#v %v", d, err)
	}
	calls := g.calls.Load()
	d, err = s.Detail(ctx, a, v.ID)
	if err != nil || g.calls.Load() != calls {
		t.Fatalf("detail replay %#v %v", d, err)
	}
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restored, err := (&PostgresService{DB: db2, AllowTestUnlock: true}).Get(a, v.ID)
	if err != nil || restored.Detail != "detail" {
		t.Fatalf("restore %#v %v", restored, err)
	}
	_, err = s.Create(ctx, a, "key", "counsel", "enhanced", json.RawMessage(`{"question":"different"}`))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict %v", err)
	}
}
