package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestPostgresMemoryIsolationConcurrencyAndHistory(t *testing.T) {
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
	a := fmt.Sprintf("memory-a-%d", time.Now().UnixNano())
	b := a + "-b"
	_, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	store := &PostgresStore{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	fact := Fact{Key: "relationship", Value: "current", SourceEventID: "event1", ObservedAt: now}
	var wg sync.WaitGroup
	results := make(chan Record, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := store.Set(a, fact); results <- r; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first := ""
	for r := range results {
		if first == "" {
			first = r.ID
		}
		if first != r.ID {
			t.Fatalf("duplicate event created distinct IDs")
		}
	}
	_, err = store.Set(a, Fact{Key: "relationship", Value: "old", SourceEventID: "old", ObservedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Set(b, Fact{Key: "relationship", Value: "other user", SourceEventID: "event1", ObservedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(a)
	if err != nil || len(current) != 1 || current[0].Value != "current" {
		t.Fatalf("current: %#v %v", current, err)
	}
	other, err := store.Current(b)
	if err != nil || len(other) != 1 || other[0].Value != "other user" {
		t.Fatalf("other: %#v %v", other, err)
	}
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	persisted, err := (&PostgresStore{DB: db2}).History(a, "")
	if err != nil || len(persisted) != 2 {
		t.Fatalf("history: %#v %v", persisted, err)
	}
	if err = store.DeleteUser(a); err != nil {
		t.Fatal(err)
	}
	current, err = store.Current(a)
	if err != nil || len(current) != 0 {
		t.Fatalf("deletion: %#v %v", current, err)
	}
	other, err = store.Current(b)
	if err != nil || len(other) != 1 {
		t.Fatalf("cross-account deletion: %#v %v", other, err)
	}
}
