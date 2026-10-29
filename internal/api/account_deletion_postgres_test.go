package api

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestAccountDeleteRealPostgresSession(t *testing.T) {
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
	user := fmt.Sprintf("api-del-%d", time.Now().UnixNano())
	other := user + "-other"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, user, other); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, user, other)
	store := persistence.Store{DB: db}
	token, err := store.CreateSession(ctx, user, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := store.CreateSession(ctx, other, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO profiles(id,account_id,payload) VALUES($1,$2,'{"birth":"private"}'),($3,$4,'{}')`, "p-"+user, user, "p-"+other, other); err != nil {
		t.Fatal(err)
	}
	h := (&Server{Auth: store, AccountDeletion: store, PostgresOnlyStorage: true}).Handler()
	w := call(h, "DELETE", "/v1/account", token, `{"user_id":"`+other+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = store.Authenticate(ctx, token); err == nil {
		t.Fatal("deleted session still authenticates")
	}
	if _, err = store.Authenticate(ctx, otherToken); err != nil {
		t.Fatal("other session affected", err)
	}
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM profiles WHERE account_id=$1`, user).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM profiles WHERE account_id=$1`, other).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
