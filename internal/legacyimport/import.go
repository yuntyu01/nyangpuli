// Package legacyimport migrates existing local JSON stores without modifying them.
package legacyimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

type Options struct {
	MemoryPath, ReadingsPath string
	Users                    map[string]string
	DryRun                   bool
}
type Report struct {
	MemoryRecords, Readings, Users int
	AlreadyImported, DryRun        bool
}
type memoryFile struct {
	Version int                        `json:"version"`
	Users   map[string][]memory.Record `json:"users"`
}
type readingFile struct {
	Version int                        `json:"version"`
	Records map[string]json.RawMessage `json:"records"`
}
type readingMeta struct {
	readings.View
	User           string `json:"user"`
	IdempotencyKey string `json:"idempotency_key"`
	RequestHash    string `json:"request_hash"`
	Unlocked       bool   `json:"unlocked"`
	UnlockKind     string `json:"unlock_kind"`
}
type source struct{ kind, hash string }

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func readFile(path string) ([]byte, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, errors.New("cannot read legacy source")
	}
	if len(b) > 32<<20 {
		return nil, errors.New("legacy source exceeds 32 MiB")
	}
	return b, nil
}

// Validate exactly the bytes that will be imported without writing a copy.
func validateSnapshot(kind string, b []byte) error {
	var err error
	if kind == "memory" {
		err = memory.ValidateSnapshot(b)
	} else {
		err = readings.ValidateSnapshot(b)
	}
	if err != nil {
		return errors.New("invalid legacy source")
	}
	return nil
}

// Import runs all selected sources as one transaction. Dry-run performs the
// same inserts and validation but rolls the transaction back. Never run while
// the legacy server is writing: it has no interprocess lock.
func Import(ctx context.Context, db *sql.DB, o Options) (Report, error) {
	report := Report{DryRun: o.DryRun}
	if db == nil || (o.MemoryPath == "" && o.ReadingsPath == "") || len(o.Users) == 0 {
		return report, errors.New("source paths, database and explicit user mapping are required")
	}
	accounts := map[string]bool{}
	for u, a := range o.Users {
		if strings.TrimSpace(u) == "" || strings.TrimSpace(a) == "" || accounts[a] {
			return report, errors.New("mapping must be nonempty and one-to-one")
		}
		accounts[a] = true
	}
	mapping, err := json.Marshal(o.Users)
	if err != nil {
		return report, errors.New("invalid user mapping")
	}
	mapHash := digest(mapping)
	var mem memoryFile
	var rd readingFile
	var sources []source
	legacyUsers := map[string]bool{}
	if o.MemoryPath != "" {
		b, e := readFile(o.MemoryPath)
		if e != nil {
			return report, e
		}
		if e = validateSnapshot("memory", b); e != nil {
			return report, errors.New("invalid legacy memory source")
		}
		if json.Unmarshal(b, &mem) != nil || mem.Version != 1 || mem.Users == nil {
			return report, errors.New("invalid legacy memory source")
		}
		sources = append(sources, source{"memory", digest(b)})
		for u, rs := range mem.Users {
			// JSON chooses the first record for equal event times, while SQL
			// can order ties differently. Do not silently change current state.
			seen := map[string]map[time.Time]bool{}
			for _, r := range rs {
				if seen[r.Key] == nil {
					seen[r.Key] = map[time.Time]bool{}
				}
				instant := r.ObservedAt.UTC()
				if seen[r.Key][instant] {
					return report, errors.New("equal event timestamps require manual review")
				}
				seen[r.Key][instant] = true
			}
			legacyUsers[u] = true
			report.MemoryRecords += len(rs)
		}
	}
	if o.ReadingsPath != "" {
		b, e := readFile(o.ReadingsPath)
		if e != nil {
			return report, e
		}
		if e = validateSnapshot("readings", b); e != nil {
			return report, errors.New("invalid legacy readings source")
		}
		if json.Unmarshal(b, &rd) != nil || rd.Version != 1 || rd.Records == nil {
			return report, errors.New("invalid legacy readings source")
		}
		sources = append(sources, source{"readings", digest(b)})
		for _, raw := range rd.Records {
			var r readingMeta
			if json.Unmarshal(raw, &r) != nil {
				return report, errors.New("invalid legacy reading")
			}
			legacyUsers[r.User] = true
			report.Readings++
		}
	}
	if len(legacyUsers) != len(o.Users) {
		return report, errors.New("mapping must cover exactly the source users")
	}
	for u := range legacyUsers {
		if o.Users[u] == "" {
			return report, errors.New("mapping must cover exactly the source users")
		}
	}
	report.Users = len(legacyUsers)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, errors.New("could not begin import")
	}
	defer tx.Rollback()
	// Serialize same-file imports before examining the ledger and account rows.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('nyang-legacy-import-v1'))`); err != nil {
		return report, errors.New("cannot lock import")
	}
	existing := 0
	for _, src := range sources {
		var previous string
		e := tx.QueryRowContext(ctx, `SELECT mapping_sha256 FROM legacy_import_sources WHERE source_kind=$1 AND source_sha256=$2`, src.kind, src.hash).Scan(&previous)
		if e == nil {
			if previous != mapHash {
				return report, errors.New("source was already imported with different mapping")
			}
			existing++
		} else if !errors.Is(e, sql.ErrNoRows) {
			return report, errors.New("import ledger unavailable; apply database migrations first")
		}
	}
	if existing > 0 && existing != len(sources) {
		return report, errors.New("partial previously imported sources require manual review")
	}
	ordered := make([]string, 0, len(accounts))
	for a := range accounts {
		ordered = append(ordered, a)
	}
	sort.Strings(ordered)
	for _, a := range ordered {
		var id string
		if e := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, a).Scan(&id); e != nil {
			return report, errors.New("mapped target account missing or deleted")
		}
		if existing == len(sources) {
			continue
		}
		var occupied bool
		e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memories WHERE account_id=$1) OR EXISTS(SELECT 1 FROM generation_jobs WHERE account_id=$1) OR EXISTS(SELECT 1 FROM memory_deletion_watermarks WHERE account_id=$1)`, a).Scan(&occupied)
		if e != nil || occupied {
			return report, errors.New("mapped account already has memory or generation data; refusing merge")
		}
	}
	if existing == len(sources) {
		report.AlreadyImported = true
		return report, nil
	}
	for u, rs := range mem.Users {
		a := o.Users[u]
		for _, r := range rs {
			fact, e := json.Marshal(r.Fact)
			if e != nil {
				return report, errors.New("invalid memory record")
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO memories(id,account_id,kind,value,occurred_at,recorded_at,confirmed,source_event_id,supersedes,valid,legacy_recorded_at) VALUES($1,$2,$3,$4,$5,$6,true,$7,$8,true,$9)`, r.ID, a, r.Key, fact, r.ObservedAt, r.RecordedAt, r.SourceEventID, r.Supersedes, r.RecordedAt.Format(time.RFC3339Nano))
			if e != nil {
				return report, errors.New("memory import conflict; no changes saved")
			}
		}
	}
	for _, raw := range rd.Records {
		if e := importReading(ctx, tx, raw, o.Users); e != nil {
			return report, e
		}
	}
	for _, src := range sources {
		if _, err = tx.ExecContext(ctx, `INSERT INTO legacy_import_sources(source_kind,source_sha256,mapping_sha256) VALUES($1,$2,$3)`, src.kind, src.hash, mapHash); err != nil {
			return report, errors.New("import ledger conflict")
		}
	}
	// Detect a source edited during this run. The JSON server must still be
	// stopped: these checks cannot provide an interprocess file lock.
	for _, src := range sources {
		path := o.MemoryPath
		if src.kind == "readings" {
			path = o.ReadingsPath
		}
		b, e := readFile(path)
		if e != nil || digest(b) != src.hash {
			return report, errors.New("legacy source changed during import; no changes saved")
		}
	}
	if o.DryRun {
		return report, nil
	}
	if err = tx.Commit(); err != nil {
		return report, errors.New("could not commit import")
	}
	return report, nil
}

func importReading(ctx context.Context, tx *sql.Tx, raw json.RawMessage, users map[string]string) error {
	var r readingMeta
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &r) != nil || json.Unmarshal(raw, &fields) != nil {
		return errors.New("invalid reading record")
	}
	account := users[r.User]
	if account == "" {
		return errors.New("reading owner not mapped")
	}
	accountJSON, _ := json.Marshal(account)
	fields["user"] = accountJSON
	// Summary storage must never carry detail or an entitlement; the latter is
	// restored only as an explicitly development-only unlock below.
	summary := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		summary[k] = v
	}
	summary["unlocked"] = json.RawMessage(`false`)
	delete(summary, "detail")
	delete(summary, "unlock_kind")
	state := "ready"
	switch r.Status {
	case "summary_pending", "summary_unknown":
		state = "unknown"
	case "summary_failed":
		state = "failed"
	case "summary_ready":
	case "detail_pending", "detail_unknown", "detail_failed", "detail_ready":
		summary["status"] = json.RawMessage(`"summary_ready"`)
	default:
		return errors.New("unsupported reading status")
	}
	summaryRaw, e := json.Marshal(summary)
	if e != nil {
		return errors.New("invalid summary")
	}
	var gen, memGen int64
	if e = tx.QueryRowContext(ctx, `SELECT deletion_generation,memory_generation FROM accounts WHERE id=$1 AND deleted_at IS NULL`, account).Scan(&gen, &memGen); e != nil {
		return errors.New("account unavailable")
	}
	// All imported counsel snapshots are conservatively memory-dependent.
	var memoryVersion any
	if r.Domain == "counsel" {
		memoryVersion = memGen
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO generation_jobs(id,account_id,request_key,state,request_hash,deletion_generation,evidence,result,memory_generation,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$10)`, r.ID, account, "summary:"+r.IdempotencyKey, state, r.RequestHash, gen, summaryRaw, memoryVersion, r.CreatedAt, r.UpdatedAt)
	if e != nil {
		return errors.New("reading import conflict; no changes saved")
	}
	if r.Unlocked {
		if r.UnlockKind != "test_only_not_payment" {
			return errors.New("unsupported entitlement; manual review required")
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO dev_reading_unlocks(account_id,reading_job_id) VALUES($1,$2)`, account, r.ID); e != nil {
			return errors.New("development entitlement import conflict")
		}
	}
	if strings.HasPrefix(r.Status, "detail_") {
		detailState := "ready"
		switch r.Status {
		case "detail_pending", "detail_unknown":
			detailState = "unknown"
		case "detail_failed":
			detailState = "failed"
		}
		full, e := json.Marshal(fields)
		if e != nil {
			return errors.New("invalid detail")
		}
		dh := sha256.Sum256([]byte("legacy-detail\x00" + account + "\x00" + r.ID))
		detailID := hex.EncodeToString(dh[:16])
		_, e = tx.ExecContext(ctx, `INSERT INTO generation_jobs(id,account_id,request_key,state,request_hash,deletion_generation,evidence,result,memory_generation,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$10)`, detailID, account, "detail:"+r.ID, detailState, r.RequestHash, gen, full, memoryVersion, r.CreatedAt, r.UpdatedAt)
		if e != nil {
			return errors.New("detail import conflict; no changes saved")
		}
	}
	return nil
}
