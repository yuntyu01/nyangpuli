package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestAccountDeletionAndStaleJobPostgres(t *testing.T) {
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
	id := fmt.Sprint(time.Now().UnixNano())
	a, b := "del-a-"+id, "del-b-"+id
	statements := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO accounts(id) VALUES($1),($2)`, []any{a, b}},
		{`INSERT INTO products(id) VALUES($1)`, []any{"prod-" + id}},
		{`INSERT INTO profiles(id,account_id,payload) VALUES($1,$2,'{"private":"birth"}')`, []any{"profile-" + id, a}},
		{`INSERT INTO conversations(id,account_id) VALUES($1,$2)`, []any{"conversation-" + id, a}},
		{`INSERT INTO messages(id,conversation_id,role,content) VALUES($1,$2,'user','private')`, []any{"message-" + id, "conversation-" + id}},
		{`INSERT INTO memories(id,account_id,kind,value,source_message_id) VALUES($1,$2,'fact','{"secret":true}',$3)`, []any{"memory-" + id, a, "message-" + id}},
		{`INSERT INTO memory_history(memory_id,value) VALUES($1,'{"private":"previous"}')`, []any{"memory-" + id}},
		{`INSERT INTO daily_cache(account_id,profile_id,profile_version,local_date,time_zone,engine_version,source_version,prompt_version,model_version,result) VALUES($1,$2,1,'2026-10-29','Asia/Seoul','1','1','1','1','{"summary":"private"}')`, []any{a, "profile-" + id}},
		{`INSERT INTO readings(id,account_id,request_key,request_hash,status,evidence,summary) VALUES($1,$2,'key','hash','ready','{"private":true}','private')`, []any{"reading-" + id, a}},
		{`INSERT INTO generation_jobs(id,account_id,reading_id,request_key,state) VALUES($1,$2,$3,'key','pending')`, []any{"job-" + id, a, "reading-" + id}},
		{`INSERT INTO dev_reading_unlocks(account_id,reading_job_id) VALUES($1,$2)`, []any{a, "job-" + id}},
		{`INSERT INTO dev_purchase_transactions(transaction_id,account_id,reading_job_id,status,revision) VALUES($1,$2,$3,'purchased',2)`, []any{"devtx-" + id, a, "job-" + id}},
		{`INSERT INTO tarot_sessions(id,account_id,question,cards) VALUES($1,$2,'private','[]')`, []any{"tarot-" + id, a}},
		{`INSERT INTO purchase_transactions(provider,transaction_id,account_id,product_id,status) VALUES('mock',$1,$2,$3,'purchased')`, []any{"tx-" + id, a, "prod-" + id}},
		{`INSERT INTO entitlements(account_id,reading_id,provider,transaction_id) VALUES($1,$2,'mock',$3)`, []any{a, "reading-" + id, "tx-" + id}},
	}
	defer func() {
		db.ExecContext(ctx, `DELETE FROM purchase_transactions WHERE provider='mock' AND transaction_id=$1`, "tx-"+id)
		db.ExecContext(ctx, `DELETE FROM products WHERE id=$1`, "prod-"+id)
		db.ExecContext(ctx, `DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	}()
	for _, s := range statements {
		if _, err = db.ExecContext(ctx, s.q, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	store := Store{DB: db}
	token, err := store.CreateSession(ctx, a, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteAccount(ctx, a); err != nil {
		t.Fatal("deletion must be idempotent", err)
	}
	if _, err = store.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
	for _, table := range []string{"profiles", "conversations", "memories", "readings", "generation_jobs", "tarot_sessions", "entitlements", "auth_sessions", "account_identities", "daily_cache", "dev_reading_unlocks", "dev_purchase_transactions", "password_credentials", "memory_deletion_watermarks"} {
		var n int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE account_id=$1`, a).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d %v", table, n, err)
		}
	}
	var related int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE id=$1`, "message-"+id).Scan(&related); err != nil || related != 0 {
		t.Fatalf("message: %d %v", related, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM memory_history WHERE memory_id=$1`, "memory-"+id).Scan(&related); err != nil || related != 0 {
		t.Fatalf("memory history: %d %v", related, err)
	}
	var account sql.NullString
	if err = db.QueryRowContext(ctx, `SELECT account_id FROM purchase_transactions WHERE provider='mock' AND transaction_id=$1`, "tx-"+id).Scan(&account); err != nil || account.Valid {
		t.Fatal(account, err)
	}
	var active bool
	if err = db.QueryRowContext(ctx, `SELECT deleted_at IS NULL FROM accounts WHERE id=$1`, b).Scan(&active); err != nil || !active {
		t.Fatal("another user was affected", err)
	}
	if err = store.WithActiveAccount(ctx, a, 0, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO readings(id,account_id,request_key,request_hash,status,evidence) VALUES($1,$2,'late','hash','ready','{}')`, "late-"+id, a)
		return e
	}); !errors.Is(err, ErrAccountDeleted) {
		t.Fatal("late job", err)
	}
	// Even a stale writer that omitted the helper cannot bypass the DB guard.
	if _, err = db.ExecContext(ctx, `INSERT INTO profiles(id,account_id,payload) VALUES($1,$2,'{}')`, "late-"+id, a); err == nil {
		t.Fatal("stale writer resurrected private data")
	}
}

func TestDeletionSerializesWithRunningWritePostgres(t *testing.T) {
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
	account := fmt.Sprint("del-concurrent-", time.Now().UnixNano())
	if _, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1)`, account); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	started, release, written, deleted := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan error, 1)
	go func() {
		written <- (Store{DB: db}).WithActiveAccount(ctx, account, 0, func(tx *sql.Tx) error {
			close(started)
			<-release
			_, err := tx.ExecContext(ctx, `INSERT INTO profiles(id,account_id,payload) VALUES($1,$2,'{"private":true}')`, "profile-"+account, account)
			return err
		})
	}()
	<-started
	go func() { deleted <- (Store{DB: db}).DeleteAccount(ctx, account) }()
	select {
	case err := <-deleted:
		t.Fatalf("deletion completed before in-flight write released lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err = <-written; err != nil {
		t.Fatal(err)
	}
	if err = <-deleted; err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM profiles WHERE account_id=$1`, account).Scan(&n); err != nil || n != 0 {
		t.Fatalf("retained private data: %d %v", n, err)
	}
}
