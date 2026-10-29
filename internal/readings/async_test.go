package readings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestAsyncSummaryReconnectAndAccountDeletion(t *testing.T) {
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
	a := fmt.Sprintf("async-%d", time.Now().UnixNano())
	b := a + "-b"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	g := &pgGenerator{started: make(chan struct{}, 2), release: make(chan struct{})}
	s := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: g}
	requestCtx, cancel := context.WithCancel(ctx)
	input := json.RawMessage(`{"question":"case"}`)
	job, err := s.StartSummary(requestCtx, a, "key", "counsel", "enhanced", input)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if job.ID == "" || job.State != "pending_or_interrupted" {
		t.Fatal(job)
	}
	<-g.started
	duplicate, err := s.StartSummary(ctx, a, "key", "counsel", "enhanced", input)
	if err != nil || duplicate.ID != job.ID || g.calls.Load() != 1 {
		t.Fatalf("duplicate %#v %v", duplicate, err)
	}
	byKey, err := s.SummaryStatusByKey(ctx, a, "key")
	if err != nil || byKey.ID != job.ID {
		t.Fatalf("lookup %#v %v", byKey, err)
	}
	if _, err = s.SummaryStatusByID(ctx, b, job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("isolation %v", err)
	}
	close(g.release)
	deadline := time.Now().Add(4 * time.Second)
	for {
		status, e := s.SummaryStatusByID(ctx, a, job.ID)
		if e != nil {
			t.Fatal(e)
		}
		if status.State == "ready" {
			if status.Summary == nil || status.Summary.Summary != "summary" || status.Summary.Detail != "" {
				t.Fatal(status)
			}
			raw, _ := json.Marshal(status)
			if strings.Contains(string(raw), "frozen") {
				t.Fatal("private prompt leaked")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still %s", status.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	g2 := &pgGenerator{started: make(chan struct{}, 1), release: make(chan struct{})}
	s2 := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: g2}
	job2, err := s2.StartSummary(ctx, a, "delete-key", "counsel", "enhanced", input)
	if err != nil {
		t.Fatal(err)
	}
	<-g2.started
	if err = (persistence.Store{DB: db}).DeleteAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	close(g2.release)
	// The completed provider response may never recreate deleted records.
	deadline = time.Now().Add(4 * time.Second)
	for {
		var n int
		err = db.QueryRow(`SELECT count(*) FROM generation_jobs WHERE id=$1`, job2.ID).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("deleted job remains")
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = s2.SummaryStatusByID(ctx, a, job2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted lookup %v", err)
	}
}
