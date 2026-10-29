package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestDailySummaryMigrationPreservesSummary(t *testing.T) {
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE daily_cache(result jsonb NOT NULL) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO daily_cache(result) VALUES ('{"id":"saved","summary":"safe","detail":"private","detail_model":"secret-model","detail_prompt_sha256":"sensitive","detail_usage":{"total_tokens":42}}')`); err != nil {
		t.Fatal(err)
	}
	sqlBytes, err := migrations.ReadFile("migrations/011_daily_summary_only.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, string(sqlBytes)); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT result FROM daily_cache`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result["id"] != "saved" || result["summary"] != "safe" {
		t.Fatal("summary not preserved")
	}
	for _, key := range []string{"detail", "detail_model", "detail_prompt_sha256", "detail_usage"} {
		if _, ok := result[key]; ok {
			t.Fatalf("%s remained", key)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE daily_cache SET result=result || '{"detail":"private"}'::jsonb`); err == nil {
		t.Fatal("paid detail constraint did not reject write")
	}
}
