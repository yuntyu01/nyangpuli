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
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/purchase"
)

func TestDevelopmentPurchasePostgresAndRefundDuringGeneration(t *testing.T) {
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
	a := fmt.Sprint("devpurchase-", time.Now().UnixNano())
	b := a + "-b"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	g := &pgGenerator{}
	s := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: g, AllowTestUnlock: true}
	first, err := s.Create(ctx, a, "key", "counsel", "baseline", json.RawMessage(`{"q":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(ctx, b, "key", "counsel", "baseline", json.RawMessage(`{"q":"two"}`))
	if err != nil {
		t.Fatal(err)
	}
	dev := purchase.Development{DB: db}
	tx := "tx-" + a
	if err = dev.Simulate(ctx, a, first.ID, tx, purchase.Pending); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Detail(ctx, a, first.ID); !errors.Is(err, ErrLocked) {
		t.Fatal("pending unlocked", err)
	}
	for i := 0; i < 2; i++ {
		if err = dev.Simulate(ctx, a, first.ID, tx, purchase.Purchased); err != nil {
			t.Fatal(err)
		}
	}
	if err = dev.Simulate(ctx, b, second.ID, tx, purchase.Purchased); !errors.Is(err, purchase.ErrOwnership) {
		t.Fatal("other user reused transaction", err)
	}
	rows, err := dev.List(ctx, a)
	if err != nil || len(rows) != 1 || rows[0].TransactionID != tx || rows[0].Status != purchase.Purchased {
		t.Fatal(rows, err)
	}
	rows, err = dev.List(ctx, b)
	if err != nil || len(rows) != 0 {
		t.Fatal("other user's purchases leaked", rows, err)
	}
	blocked := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: g}
	if _, err = blocked.Detail(ctx, a, first.ID); !errors.Is(err, ErrLocked) {
		t.Fatal("test entitlement accepted while disabled", err)
	}
	g.started = make(chan struct{}, 1)
	g.release = make(chan struct{})
	result := make(chan error, 1)
	go func() { _, e := s.Detail(ctx, a, first.ID); result <- e }()
	<-g.started
	if err = dev.Simulate(ctx, a, first.ID, tx, purchase.Refunded); err != nil {
		t.Fatal(err)
	}
	close(g.release)
	if err = <-result; !errors.Is(err, ErrLocked) {
		t.Fatal("refund must block detail", err)
	}
	if err = dev.Simulate(ctx, a, first.ID, tx, purchase.Purchased); err != nil {
		t.Fatal(err)
	} // stale event ignored
	v, err := s.Get(a, first.ID)
	if err != nil || !v.DetailLocked || v.Detail != "" {
		t.Fatal(v, err)
	}
	if _, err = s.Detail(ctx, a, first.ID); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	rows, err = dev.List(ctx, a)
	if err != nil || len(rows) != 1 || rows[0].Status != purchase.Refunded {
		t.Fatal(rows, err)
	}
}
