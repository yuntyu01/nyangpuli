package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

func TestTarotSessionsPostgres(t *testing.T) {
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
	if err := persistence.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	a, b := "tarot-a-"+suffix, "tarot-b-"+suffix
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id IN ($1,$2)`, a, b)
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	newHandler := func() http.Handler {
		return (&Server{Lab: l, TarotStore: persistence.Store{DB: db}, Tokens: map[string]string{"a": a, "b": b}}).Handler()
	}
	h := newHandler()
	w := call(h, "POST", "/v1/tarot/sessions", "a", `{"question":"어떻게 대화할까요?","question_type":"love","spread":"past-present-future"}`)
	if w.Code != 201 || strings.Contains(w.Body.String(), `"deck"`) {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.ID == "" {
		t.Fatalf("id %v", err)
	}
	path := "/v1/tarot/sessions/" + v.ID
	if w := call(h, "GET", path, "b", ""); w.Code != 404 {
		t.Fatalf("cross user got %d", w.Code)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- call(h, "POST", path+"/select", "a", `{"position":7}`).Code }()
	}
	wg.Wait()
	close(codes)
	got := map[int]int{}
	for code := range codes {
		got[code]++
	}
	if got[200] != 1 || got[409] != 1 {
		t.Fatalf("concurrent duplicate: %v", got)
	}
	for _, p := range []int{8, 9} {
		w = call(h, "POST", path+"/select", "a", fmt.Sprintf(`{"position":%d}`, p))
		if w.Code != 200 {
			t.Fatalf("select %d: %d %s", p, w.Code, w.Body.String())
		}
	}
	before := w.Body.String()
	w = call(newHandler(), "GET", path, "a", "")
	if w.Code != 200 || w.Body.String() != before || !strings.Contains(w.Body.String(), `"complete":true`) || strings.Contains(w.Body.String(), `"deck"`) {
		t.Fatalf("reread %d %s", w.Code, w.Body.String())
	}
}
