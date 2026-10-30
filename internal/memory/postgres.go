package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// PostgresStore stores explicit facts independently from common knowledge.
// Writers lock the owning account row, so event conflict checks are atomic
// across server processes and a concurrent account deletion cannot race a write.
type PostgresStore struct{ DB *sql.DB }

func (s *PostgresStore) Set(user string, fact Fact) (Record, error) {
	return s.set(user, fact, Meta{Class: ClassFact, Origin: OriginConfirmed}, true)
}

// RecordFromConversation stores a memory extracted from a user message. Only
// statements quoted from that message are confirmed; AI inferences are kept
// unconfirmed and never returned by Current. A deleted memory's message/key
// pair is suppressed and returns ErrSuppressed instead of being recreated.
func (s *PostgresStore) RecordFromConversation(user string, fact Fact, meta Meta) (Record, error) {
	if meta.SourceMessageID == "" || (meta.Class != ClassFact && meta.Class != ClassEpisode) || (meta.Origin != OriginStated && meta.Origin != OriginAIInference) {
		return Record{}, ErrInvalid
	}
	return s.set(user, fact, meta, meta.Origin == OriginStated)
}

var ErrSuppressed = errors.New("memory was deleted for this source message")

func (s *PostgresStore) set(user string, fact Fact, meta Meta, confirmed bool) (Record, error) {
	if err := validateUser(user); err != nil {
		return Record{}, err
	}
	if err := validateFact(fact); err != nil {
		return Record{}, err
	}
	if len(meta.Subject) > 200 || len(meta.Situation) > 1000 {
		return Record{}, ErrInvalid
	}
	episode := meta.Class == ClassEpisode
	fact.ObservedAt = fact.ObservedAt.UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, user).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrScope
	}
	if err != nil {
		return Record{}, err
	}
	if meta.SourceMessageID != "" {
		var suppressed bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memory_suppressions WHERE account_id=$1 AND source_message_id=$2 AND kind=$3)`, user, meta.SourceMessageID, fact.Key).Scan(&suppressed); err != nil {
			return Record{}, err
		}
		if suppressed {
			return Record{}, ErrSuppressed
		}
	}
	// Any earlier row with this event ID, confirmed or not, makes a replay
	// idempotent and prevents a retry from overwriting a later correction.
	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE account_id=$1 AND kind=$2 AND source_event_id=$3 AND NOT confirmed`, user, fact.Key, fact.SourceEventID).Scan(&existing); err != nil {
		return Record{}, err
	}
	if existing > 0 {
		return Record{}, fmt.Errorf("%w: source event already recorded", ErrConflict)
	}
	records, err := pgRecords(ctx, tx, user, fact.Key)
	if err != nil {
		return Record{}, err
	}
	var current *Record
	for _, r := range records {
		if r.SourceEventID == fact.SourceEventID {
			if r.Value == fact.Value && r.ObservedAt.Equal(fact.ObservedAt) && r.Note == fact.Note {
				return r, nil
			}
			return Record{}, fmt.Errorf("%w: source event already recorded", ErrConflict)
		}
		if r.Class == ClassEpisode || episode {
			continue
		}
		if r.ObservedAt.Equal(fact.ObservedAt) && r.Value != fact.Value {
			return Record{}, fmt.Errorf("%w: values share event time", ErrConflict)
		}
		if r.Current {
			rr := r
			current = &rr
		}
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return Record{}, err
	}
	record := Record{ID: hex.EncodeToString(random), Fact: fact, RecordedAt: time.Now().UTC(), Meta: meta}
	valid := true
	if !episode {
		var watermark sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT (SELECT deleted_at FROM memory_deletion_watermarks WHERE account_id=$1 AND kind=$2)`, user, fact.Key).Scan(&watermark)
		if err != nil {
			return Record{}, err
		}
		valid = !watermark.Valid || fact.ObservedAt.After(watermark.Time)
		if current != nil && fact.ObservedAt.After(current.ObservedAt) {
			record.Supersedes = current.ID
		}
	}
	data, err := json.Marshal(fact)
	if err != nil {
		return Record{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memories(id,account_id,kind,value,occurred_at,recorded_at,confirmed,source_event_id,supersedes,valid,memory_class,origin,subject,situation,source_message_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),NULLIF($14,''),NULLIF($15,''))`, record.ID, user, fact.Key, data, fact.ObservedAt, record.RecordedAt, confirmed, fact.SourceEventID, record.Supersedes, valid, meta.Class, meta.Origin, meta.Subject, meta.Situation, meta.SourceMessageID)
	if err != nil {
		return Record{}, err
	}
	// Explicit user edits keep invalidating memory-based reading snapshots.
	// Additions from conversation do not: they remove nothing a snapshot holds,
	// and invalidating would discard earlier readings on every counseling turn.
	if meta.SourceMessageID == "" {
		if err = invalidateMemorySnapshots(ctx, tx, user); err != nil {
			return Record{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Record{}, err
	}
	if !valid || !confirmed {
		return record, nil
	}
	records = markCurrent(append(records, record))
	for _, r := range records {
		if r.ID == record.ID {
			return r, nil
		}
	}
	return Record{}, errors.New("memory insertion missing")
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func pgRecords(ctx context.Context, q queryer, user, key string) ([]Record, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,value,recorded_at,COALESCE(supersedes,''),legacy_recorded_at,memory_class,origin,COALESCE(subject,''),COALESCE(situation,''),COALESCE(source_message_id,'') FROM memories WHERE account_id=$1 AND valid AND confirmed AND ($2='' OR kind=$2) ORDER BY occurred_at,recorded_at,id`, user, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Record{}
	for rows.Next() {
		var r Record
		var data []byte
		var legacyRecordedAt sql.NullString
		if err = rows.Scan(&r.ID, &data, &r.RecordedAt, &r.Supersedes, &legacyRecordedAt, &r.Class, &r.Origin, &r.Subject, &r.Situation, &r.SourceMessageID); err != nil {
			return nil, err
		}
		if legacyRecordedAt.Valid {
			r.RecordedAt, err = time.Parse(time.RFC3339Nano, legacyRecordedAt.String)
			if err != nil {
				return nil, errors.New("invalid imported recording time")
			}
		}
		if err = json.Unmarshal(data, &r.Fact); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return markCurrent(result), nil
}
func (s *PostgresStore) History(user, key string) ([]Record, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return pgRecords(ctx, s.DB, user, key)
}
func (s *PostgresStore) Current(user string) ([]Record, error) {
	records, err := s.History(user, "")
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, r := range records {
		if r.Current {
			result = append(result, r)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}
func (s *PostgresStore) DeleteUser(user string) error {
	if err := validateUser(user); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, user).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrScope
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM memories WHERE account_id=$1`, user); err != nil {
		return err
	}
	if err = invalidateMemorySnapshots(ctx, tx, user); err != nil {
		return err
	}
	return tx.Commit()
}
