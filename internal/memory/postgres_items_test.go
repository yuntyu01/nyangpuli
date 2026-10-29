package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestPostgresMemoryCorrectionAndDeletion(t *testing.T) {
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
	a := fmt.Sprintf("item-%d", time.Now().UnixNano())
	b := a + "-b"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	s := &PostgresStore{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	old, err := s.Set(a, Fact{Key: "contact_boundary", Value: "allowed", SourceEventID: "old", ObservedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.Set(a, Fact{Key: "contact_boundary", Value: "blocked", SourceEventID: "latest", ObservedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	corrected := old.Fact
	corrected.Value = "limited"
	got, err := s.Correct(a, old.ID, corrected)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current {
		t.Fatal("old correction became current")
	}
	current, err := s.Current(a)
	if err != nil || len(current) != 1 || current[0].Value != "blocked" {
		t.Fatalf("latest boundary lost: %#v %v", current, err)
	}
	revisions, err := s.Revisions(a, old.ID)
	if err != nil || len(revisions) != 1 || revisions[0].Fact.Value != "allowed" || revisions[0].Fact.SourceEventID != "old" {
		t.Fatalf("revision: %#v %v", revisions, err)
	}
	if _, err = s.Correct(b, old.ID, corrected); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user correction: %v", err)
	}
	if _, err = s.Revisions(b, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user history: %v", err)
	}
	if err = s.Delete(b, latest.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user deletion: %v", err)
	}
	if err = s.Delete(a, latest.ID); err != nil {
		t.Fatal(err)
	}
	current, err = s.Current(a)
	if err != nil || len(current) != 0 {
		t.Fatalf("deleted latest resurfaced old boundary: %#v %v", current, err)
	}
	late, err := s.Set(a, Fact{Key: "contact_boundary", Value: "allowed", SourceEventID: "late-old", ObservedAt: now.Add(-2 * time.Hour)})
	if err != nil || late.Current {
		t.Fatalf("late old fact: %#v %v", late, err)
	}
	current, err = s.Current(a)
	if err != nil || len(current) != 0 {
		t.Fatalf("late old event resurrected boundary: %#v %v", current, err)
	}
	if err = s.Delete(a, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revisions(a, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted history survived: %v", err)
	}
}
