package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/daily"
)

func TestDailyCachePostgres(t *testing.T) {
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
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	a, b, p := "daily-a-"+suffix, "daily-b-"+suffix, "daily-p-"+suffix
	if _, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	if _, err = db.ExecContext(ctx, `INSERT INTO profiles(id,account_id,payload) VALUES($1,$2,'{}')`, p, a); err != nil {
		t.Fatal(err)
	}
	k := daily.Key{AccountID: a, ProfileID: p, ProfileVersion: 1, Date: "2026-10-29", TimeZone: "Asia/Seoul", EngineVersion: "engine-a", SourceVersion: "source-a", PromptVersion: "prompt-a", ModelVersion: "model-a"}
	store := Store{DB: db}
	if _, err = store.GetDaily(ctx, k); !errors.Is(err, ErrDailyNotFound) {
		t.Fatalf("expected miss %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			value, e := store.PutDaily(ctx, k, json.RawMessage(fmt.Sprintf(`{"candidate":%d}`, i)))
			results <- string(value)
			errs <- e
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first string
	for value := range results {
		if first == "" {
			first = value
		} else if first != value {
			t.Fatalf("different result: %s vs %s", first, value)
		}
	}
	kOther := k
	kOther.AccountID = b
	if _, err = store.GetDaily(ctx, kOther); !errors.Is(err, ErrDailyNotFound) {
		t.Fatalf("cross-account read %v", err)
	}
	if _, err = store.PutDaily(ctx, kOther, json.RawMessage(`{"candidate":3}`)); !errors.Is(err, ErrDailyNotFound) {
		t.Fatalf("cross-account write %v", err)
	}
	for _, mutate := range []func(*daily.Key){func(x *daily.Key) { x.Date = "2026-10-30" }, func(x *daily.Key) { x.TimeZone = "UTC" }, func(x *daily.Key) { x.EngineVersion = "new" }, func(x *daily.Key) { x.SourceVersion = "new" }, func(x *daily.Key) { x.PromptVersion = "new" }, func(x *daily.Key) { x.ModelVersion = "new" }} {
		n := k
		mutate(&n)
		if _, err = store.GetDaily(ctx, n); !errors.Is(err, ErrDailyNotFound) {
			t.Fatalf("version key did not miss: %v", err)
		}
	}
	if _, err = db.ExecContext(ctx, `UPDATE profiles SET version=version+1 WHERE id=$1`, p); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetDaily(ctx, k); !errors.Is(err, ErrDailyNotFound) {
		t.Fatalf("stale profile read %v", err)
	}
	if _, err = store.PutDaily(ctx, k, json.RawMessage(`{}`)); !errors.Is(err, ErrDailyNotFound) {
		t.Fatalf("stale profile write %v", err)
	}
	if err = store.DeleteAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	k.ProfileVersion = 2
	if _, err = store.PutDaily(ctx, k, json.RawMessage(`{}`)); !errors.Is(err, ErrDailyNotFound) {
		t.Fatalf("post-deletion write %v", err)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM daily_cache WHERE account_id=$1`, a).Scan(&n); err != nil || n != 0 {
		t.Fatalf("deletion %d %v", n, err)
	}
}
