package legacyimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

type builder struct{}

func (builder) Build(_ context.Context, _ string, _ string, data json.RawMessage) (generation.Input, error) {
	return generation.Input{CaseID: "test", VariantID: "enhanced", Question: "합성 질문", UserPrompt: "자료", CaseContext: data, Sources: []string{"fixture@1"}}, nil
}

type generator struct{ uncertain bool }

func (g generator) Generate(context.Context, generation.Input) (generation.Result, error) {
	if g.uncertain {
		return generation.Result{}, errors.New("simulated ambiguous transport")
	}
	return generation.Result{Status: "generated", Text: "합성 답변", FinishReason: "stop"}, nil
}
func TestImportPostgresDryRunAndReplay(t *testing.T) {
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
	account := "legacy-test-" + fmt.Sprint(time.Now().UnixNano())
	if _, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1)`, account); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	dir := t.TempDir()
	mp := filepath.Join(dir, "memory.json")
	rp := filepath.Join(dir, "readings.json")
	m, err := memory.Open(mp)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var originals []memory.Record
	for i := 0; i < 2; i++ {
		record, err := m.Set("alice", memory.Fact{Key: "boundary", Value: fmt.Sprintf("boundary%d", i), SourceEventID: fmt.Sprintf("event%d", i), ObservedAt: when.Add(time.Duration(i) * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, record)
	}
	original, err := readings.New(builder{}, generator{}, rp)
	if err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"input":{"question":"q"},"use_memory":false}`)
	v, err := original.Create(ctx, "alice", "key", "tarot", "enhanced", input)
	if err != nil {
		t.Fatal(err)
	}
	counsel, err := original.Create(ctx, "alice", "counsel-key", "counsel", "enhanced", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = original.UnlockForTest("alice", v.ID); err != nil {
		t.Fatal(err)
	}
	full, err := original.Detail(ctx, "alice", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := readings.New(builder{}, generator{uncertain: true}, rp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unknown.Create(ctx, "alice", "unknown", "tarot", "enhanced", input); err == nil {
		t.Fatalf("expected initial provider failure: %v", err)
	}
	mb, _ := os.ReadFile(mp)
	rb, _ := os.ReadFile(rp)
	opts := Options{MemoryPath: mp, ReadingsPath: rp, Users: map[string]string{"alice": account}, DryRun: true}
	report, err := Import(ctx, db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Users != 1 || report.MemoryRecords != 2 || report.Readings != 3 || !report.DryRun {
		t.Fatalf("report: %+v", report)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE account_id=$1`, account).Scan(&n); err != nil || n != 0 {
		t.Fatalf("dry-run mutated: %d %v", n, err)
	}
	opts.DryRun = false
	report, err = Import(ctx, db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.AlreadyImported {
		t.Fatal("first import marked repeat")
	}
	again, err := Import(ctx, db, opts)
	if err != nil || !again.AlreadyImported {
		t.Fatalf("repeat: %+v %v", again, err)
	}
	// Mapping to a different account after import cannot duplicate the source.
	other := account + "-other"
	if _, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1)`, other); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, other)
	changedMap := opts
	changedMap.Users = map[string]string{"alice": other}
	if _, err = Import(ctx, db, changedMap); err == nil {
		t.Fatal("source accepted under another mapping")
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE account_id=$1`, other).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed import mutated destination %d %v", n, err)
	}
	// A partly re-used bundle is rejected rather than duplicating or skipping data.
	modifiedPath := filepath.Join(dir, "modified-readings.json")
	if err = os.WriteFile(modifiedPath, append(rb, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	partial := opts
	partial.ReadingsPath = modifiedPath
	if _, err = Import(ctx, db, partial); err == nil {
		t.Fatal("partial previously imported bundle accepted")
	}
	pgmem := memory.PostgresStore{DB: db}
	history, err := pgmem.History(account, "boundary")
	if err != nil || len(history) != 2 || !history[1].Current || !history[1].RecordedAt.Equal(originals[1].RecordedAt) || history[1].ID != originals[1].ID || history[1].SourceEventID != originals[1].SourceEventID {
		t.Fatalf("history %+v %v", history, err)
	}
	locked := readings.PostgresService{DB: db, AllowTestUnlock: false}
	got, err := locked.Get(account, v.ID)
	if err != nil || got.Detail != "" || !got.DetailLocked {
		t.Fatalf("locked %+v %v", got, err)
	}
	allowed := readings.PostgresService{DB: db, AllowTestUnlock: true}
	got, err = allowed.Get(account, v.ID)
	if err != nil || got.Detail != full.Detail || got.DetailLocked {
		t.Fatalf("detail %+v %v", got, err)
	}
	if _, err = allowed.Create(ctx, account, "unknown", "tarot", "enhanced", input); !errors.Is(err, readings.ErrOutcomeUnknown) {
		t.Fatalf("uncertain retry: %v", err)
	}
	// Any later memory edit invalidates imported counseling snapshots, while
	// unrelated tarot results remain available.
	_, err = pgmem.Set(account, memory.Fact{Key: "boundary", Value: "changed", SourceEventID: "event-new", ObservedAt: when.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = allowed.Get(account, counsel.ID); !errors.Is(err, readings.ErrInvalidated) {
		t.Fatalf("counsel not invalidated: %v", err)
	}
	if _, err = allowed.Get(account, v.ID); err != nil {
		t.Fatalf("unrelated tarot invalidated: %v", err)
	}
	nowmem, _ := os.ReadFile(mp)
	nowread, _ := os.ReadFile(rp)
	if string(mb) != string(nowmem) || string(rb) != string(nowread) {
		t.Fatal("source changed")
	}
	if err = (persistence.Store{DB: db}).DeleteAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err = Import(ctx, db, opts); err == nil {
		t.Fatal("deleted account accepted for reimport")
	}
}

func TestImportConcurrentAndOccupiedDestination(t *testing.T) {
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
	account := "concurrent-import-" + fmt.Sprint(time.Now().UnixNano())
	occupied := account + "-occupied"
	if _, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1),($2)`, account, occupied); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM accounts WHERE id IN ($1,$2)`, account, occupied)
	path := filepath.Join(t.TempDir(), "memory.json")
	m, _ := memory.Open(path)
	_, err = m.Set("legacy", memory.Fact{Key: "status", Value: "present", SourceEventID: "one", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	// Equal event times have ambiguous ordering across JSON and SQL.
	tiePath := filepath.Join(t.TempDir(), "tie.json")
	tie, _ := memory.Open(tiePath)
	instant := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, event := range []string{"first", "second"} {
		if _, e := tie.Set("legacy", memory.Fact{Key: "status", Value: "same", SourceEventID: event, ObservedAt: instant}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := Import(ctx, db, Options{MemoryPath: tiePath, Users: map[string]string{"legacy": account}, DryRun: true}); e == nil {
		t.Fatal("equal event timestamp accepted")
	}
	opts := Options{MemoryPath: path, Users: map[string]string{"legacy": account}}
	type outcome struct {
		report Report
		err    error
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() { r, e := Import(ctx, db, opts); results <- outcome{r, e} }()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.report.AlreadyImported == second.report.AlreadyImported {
		t.Fatalf("concurrent results: %+v %+v", first, second)
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE account_id=$1`, account).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicates %d %v", count, err)
	}
	otherPath := filepath.Join(t.TempDir(), "other.json")
	other, _ := memory.Open(otherPath)
	_, err = other.Set("other", memory.Fact{Key: "status", Value: "new", SourceEventID: "two", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	existing := memory.PostgresStore{DB: db}
	_, err = existing.Set(occupied, memory.Fact{Key: "status", Value: "existing", SourceEventID: "old", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Import(ctx, db, Options{MemoryPath: otherPath, Users: map[string]string{"other": occupied}, DryRun: true}); err == nil {
		t.Fatal("occupied destination accepted")
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE account_id=$1`, occupied).Scan(&count); err != nil || count != 1 {
		t.Fatalf("occupied destination mutated %d %v", count, err)
	}
}

func TestValidateSnapshotUsesSuppliedBytesWithoutPrivateCopy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	good := []byte(`{"version":1,"users":{}}`)
	if err := validateSnapshot("memory", good); err != nil {
		t.Fatal(err)
	}
	if err := validateSnapshot("memory", []byte(`{"version":999}`)); err == nil {
		t.Fatal("invalid bytes accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected validation copy: %v %v", entries, err)
	}
}
