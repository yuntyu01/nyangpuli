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
	if err := validateUser(user); err != nil {
		return Record{}, err
	}
	if err := validateFact(fact); err != nil {
		return Record{}, err
	}
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
	record := Record{ID: hex.EncodeToString(random), Fact: fact, RecordedAt: time.Now().UTC()}
	var watermark sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT (SELECT deleted_at FROM memory_deletion_watermarks WHERE account_id=$1 AND kind=$2)`, user, fact.Key).Scan(&watermark)
	if err != nil {
		return Record{}, err
	}
	valid := !watermark.Valid || fact.ObservedAt.After(watermark.Time)
	if current != nil && fact.ObservedAt.After(current.ObservedAt) {
		record.Supersedes = current.ID
	}
	data, err := json.Marshal(fact)
	if err != nil {
		return Record{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memories(id,account_id,kind,value,occurred_at,recorded_at,confirmed,source_event_id,supersedes,valid) VALUES($1,$2,$3,$4,$5,$6,true,$7,$8,$9)`, record.ID, user, fact.Key, data, fact.ObservedAt, record.RecordedAt, fact.SourceEventID, record.Supersedes, valid)
	if err != nil {
		return Record{}, err
	}
	if err = invalidateMemorySnapshots(ctx, tx, user); err != nil {
		return Record{}, err
	}
	if err = tx.Commit(); err != nil {
		return Record{}, err
	}
	if !valid {
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
	rows, err := q.QueryContext(ctx, `SELECT id,value,recorded_at,COALESCE(supersedes,''),legacy_recorded_at FROM memories WHERE account_id=$1 AND valid AND confirmed AND ($2='' OR kind=$2) ORDER BY occurred_at,recorded_at,id`, user, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Record{}
	for rows.Next() {
		var r Record
		var data []byte
		var legacyRecordedAt sql.NullString
		if err = rows.Scan(&r.ID, &data, &r.RecordedAt, &r.Supersedes, &legacyRecordedAt); err != nil {
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
