package readings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestMemoryChangeInvalidatesStoredAndPendingSnapshots(t *testing.T) {
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
	user := fmt.Sprintf("snapshot-%d", time.Now().UnixNano())
	other := user + "-other"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, user, other); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, user, other)
	m := &memory.PostgresStore{DB: db}
	fact := memory.Fact{Key: "contact_boundary", Value: "allowed", SourceEventID: "event", ObservedAt: time.Now().Add(-time.Hour)}
	saved, err := m.Set(user, fact)
	if err != nil {
		t.Fatal(err)
	}
	g := &pgGenerator{}
	s := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: g}
	input := json.RawMessage(`{"input":{"question":"secret"},"use_memory":true}`)
	ready, err := s.Create(ctx, user, "ready", "counsel", "enhanced", input)
	if err != nil {
		t.Fatal(err)
	}
	noMemory, err := s.Create(ctx, user, "independent", "counsel", "enhanced", json.RawMessage(`{"input":{"question":"independent"},"use_memory":false}`))
	if err != nil {
		t.Fatal(err)
	}
	otherReady, err := s.Create(ctx, other, "other", "counsel", "enhanced", input)
	if err != nil {
		t.Fatal(err)
	}
	waiting := &pgGenerator{started: make(chan struct{}, 1), release: make(chan struct{})}
	s.Generator = waiting
	job, err := s.StartSummary(ctx, user, "pending", "counsel", "enhanced", input)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiting.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	fact.Value = "blocked"
	if _, err = m.Correct(user, saved.ID, fact); err != nil {
		t.Fatal(err)
	}
	close(waiting.release)
	time.Sleep(100 * time.Millisecond)
	for _, id := range []string{ready.ID, job.ID} {
		var state string
		var cleaned bool
		if err = db.QueryRow(`SELECT state,evidence IS NULL AND result IS NULL FROM generation_jobs WHERE id=$1`, id).Scan(&state, &cleaned); err != nil {
			t.Fatal(err)
		}
		if state != "invalidated" || !cleaned {
			t.Fatalf("retained snapshot: %s %v", state, cleaned)
		}
		if _, err = s.Get(user, id); !errors.Is(err, ErrInvalidated) {
			t.Fatalf("retrieval %v", err)
		}
	}
	if _, err = s.Get(user, noMemory.ID); err != nil {
		t.Fatalf("independent reading affected: %v", err)
	}
	if _, err = s.Get(other, otherReady.ID); err != nil {
		t.Fatalf("other account affected: %v", err)
	}
	status, err := s.SummaryStatusByID(ctx, user, job.ID)
	if err != nil || status.State != "invalidated" {
		t.Fatalf("status: %#v %v", status, err)
	}
	// A version captured before a memory update may never claim a stale snapshot.
	var stale int64
	if err = db.QueryRow(`SELECT memory_generation FROM accounts WHERE id=$1`, user).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if err = m.Delete(user, saved.ID); err != nil {
		t.Fatal(err)
	}
	_, created, err := (persistence.Store{DB: db}).ClaimJobWithMemory(ctx, user, "stale-"+user, "summary:stale", "hash", json.RawMessage(`{"secret":"old"}`), &stale)
	if !errors.Is(err, persistence.ErrMemoryChanged) || created {
		t.Fatalf("stale claim: %v %v", created, err)
	}
}
