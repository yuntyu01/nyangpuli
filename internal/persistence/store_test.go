package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Run with an isolated disposable PostgreSQL database via NYANG_TEST_DATABASE_URL.
func TestPostgresMigrationsAndSessionIsolation(t *testing.T) {
	dsn := os.Getenv("NYANG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NYANG_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	// Concurrent migration calls and repeated startup must all succeed.
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- Migrate(ctx, db) }()
	}
	wg.Wait()
	close(errs)
	for err = range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	a, b := "test-a-"+suffix, "test-b-"+suffix
	_, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES ($1),($2)`, a, b)
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	store := Store{DB: db}
	token, err := store.CreateSession(ctx, a, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Authenticate(ctx, token)
	if err != nil || got != a {
		t.Fatalf("session: %q %v", got, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO profiles(id,account_id,payload) VALUES($1,$2,'{}')`, "p-"+suffix, a)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM profiles WHERE account_id=$1`, b).Scan(&n); err != nil || n != 0 {
		t.Fatalf("isolation %d %v", n, err)
	}
	// A fresh connection sees committed state, then revocation prevents reuse.
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	got, err = (Store{DB: db2}).Authenticate(ctx, token)
	if err != nil || got != a {
		t.Fatalf("restart persistence: %s %v", got, err)
	}
	if err = store.RevokeSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revocation: %v", err)
	}
}
