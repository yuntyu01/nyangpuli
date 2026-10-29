package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresJobClaimsAndDeletionGuard(t *testing.T) {
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
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	a := fmt.Sprintf("job-a-%d", time.Now().UnixNano())
	b := a + "-b"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	store := Store{DB: db}
	var count atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, created, e := store.ClaimJob(ctx, a, fmt.Sprintf("%s-%d", a, i), "key", "hash", json.RawMessage(`{"sources":["s1"]}`))
			if created {
				count.Add(1)
			}
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for err = range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 1 {
		t.Fatalf("claims: %d", count.Load())
	}
	j, created, err := store.ClaimJob(ctx, a, a+"-other", "key", "hash", json.RawMessage(`{}`))
	if err != nil || created || j.State != "pending" {
		t.Fatalf("replay %#v %v %v", j, created, err)
	}
	_, _, err = store.ClaimJob(ctx, a, a+"-conflict", "key", "other", json.RawMessage(`{}`))
	if !errors.Is(err, ErrJobConflict) {
		t.Fatalf("conflict: %v", err)
	}
	other, created, err := store.ClaimJob(ctx, b, b+"-job", "key", "other", json.RawMessage(`{}`))
	if err != nil || !created {
		t.Fatalf("user isolation %v %v", created, err)
	}
	if err = store.FinishJob(ctx, b, other.ID, other.DeletionGeneration, "ready", json.RawMessage(`{"summary":"ok"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE accounts SET deleted_at=now(),deletion_generation=deletion_generation+1 WHERE id=$1`, a); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishJob(ctx, a, j.ID, j.DeletionGeneration, "ready", json.RawMessage(`{"summary":"stale"}`)); !errors.Is(err, ErrJobNotPending) {
		t.Fatalf("deleted job saved: %v", err)
	}
}
