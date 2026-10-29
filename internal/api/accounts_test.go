package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestPostgresAccountHTTPGuestLinkAndLogin(t *testing.T) {
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
	store := persistence.Store{DB: db}
	handler := (&Server{Auth: store, Accounts: store}).Handler()
	call := func(path, token string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	w := call("/v1/auth/guest", "", nil)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Token string `json:"token"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Token == "" {
		t.Fatal("missing guest session")
	}
	guest := result.Token
	id, err := store.Authenticate(ctx, guest)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id=$1`, id)
	creds := map[string]string{"username": fmt.Sprintf("api%d", time.Now().UnixNano()), "password": "test-password-1234"}
	w = call("/v1/auth/link", "", creds)
	if w.Code != 401 {
		t.Fatal("link without session", w.Code)
	}
	w = call("/v1/auth/link", guest, creds)
	if w.Code != 200 {
		t.Fatal("link", w.Code, w.Body.String())
	}
	w = call("/v1/auth/logout", guest, nil)
	if w.Code != 200 {
		t.Fatal("logout", w.Code)
	}
	w = call("/v1/auth/login", "", creds)
	if w.Code != 200 {
		t.Fatal("login", w.Code, w.Body.String())
	}
	result.Token = ""
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Token == "" {
		t.Fatal("missing login session")
	}
	recovered, err := store.Authenticate(ctx, result.Token)
	if err != nil || recovered != id {
		t.Fatalf("account recovery %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer "+result.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var identity map[string]string
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &identity) != nil || len(identity) != 1 || identity["account_id"] != id {
		t.Fatalf("recovered account identity: %d", w.Code)
	}
}
