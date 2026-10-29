package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"context"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestPostgresMemoryItemAPI(t *testing.T) {
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
	a := fmt.Sprintf("api-item-%d", time.Now().UnixNano())
	b := a + "b"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	h := (&Server{Memory: &memory.PostgresStore{DB: db}, Tokens: map[string]string{"a": a, "b": b}}).Handler()
	body := `{"key":"contact_boundary","value":"allowed","source_event_id":"event","observed_at":"2026-10-01T12:00:00Z"}`
	w := call(h, http.MethodPost, "/v1/memory", "a", body)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var record memory.Record
	if err = json.Unmarshal(w.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	path := "/v1/memory/" + record.ID
	if w = call(h, http.MethodPut, path, "b", body); w.Code != 404 {
		t.Fatalf("other update: %d", w.Code)
	}
	body = `{"key":"contact_boundary","value":"blocked","source_event_id":"event","observed_at":"2026-10-01T12:00:00Z"}`
	if w = call(h, http.MethodPut, path, "a", body); w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if w = call(h, http.MethodGet, path+"/revisions", "b", ""); w.Code != 404 {
		t.Fatalf("other history: %d", w.Code)
	}
	if w = call(h, http.MethodGet, path+"/revisions", "a", ""); w.Code != 200 {
		t.Fatalf("history: %d %s", w.Code, w.Body.String())
	}
	if w = call(h, http.MethodDelete, path, "a", ""); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if w = call(h, http.MethodGet, path+"/revisions", "a", ""); w.Code != 404 {
		t.Fatalf("deleted history: %d", w.Code)
	}
}
