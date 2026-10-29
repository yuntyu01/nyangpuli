package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Revision is an older value of a corrected memory, retained only until that
// memory is deleted. Its source and observed time describe the original event.
type Revision struct {
	Fact      Fact      `json:"fact"`
	ChangedAt time.Time `json:"changed_at"`
}

func activeMemoryAccount(ctx context.Context, tx *sql.Tx, user string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, user).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrScope
	}
	return err
}

// Correct changes only an existing user-confirmed fact. Source identity and key
// remain stable; historical values are kept transactionally, not fed to search.
func (s *PostgresStore) Correct(user, id string, fact Fact) (Record, error) {
	if err := validateUser(user); err != nil {
		return Record{}, err
	}
	if id == "" {
		return Record{}, ErrInvalid
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
	if err = activeMemoryAccount(ctx, tx, user); err != nil {
		return Record{}, err
	}
	var oldBytes []byte
	err = tx.QueryRowContext(ctx, `SELECT value FROM memories WHERE id=$1 AND account_id=$2 AND valid AND confirmed FOR UPDATE`, id, user).Scan(&oldBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	var old Fact
	if err = json.Unmarshal(oldBytes, &old); err != nil {
		return Record{}, err
	}
	if fact.Key != old.Key || fact.SourceEventID != old.SourceEventID {
		return Record{}, fmt.Errorf("%w: key and event identity cannot change", ErrInvalid)
	}
	var conflicts bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memories WHERE account_id=$1 AND kind=$2 AND id<>$3 AND valid AND confirmed AND occurred_at=$4 AND value->>'value'<>$5)`, user, fact.Key, id, fact.ObservedAt, fact.Value).Scan(&conflicts)
	if err != nil {
		return Record{}, err
	}
	if conflicts {
		return Record{}, ErrConflict
	}
	if old.Value != fact.Value || old.Note != fact.Note || !old.ObservedAt.Equal(fact.ObservedAt) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_history(memory_id,value) VALUES($1,$2)`, id, oldBytes); err != nil {
			return Record{}, err
		}
		b, e := json.Marshal(fact)
		if e != nil {
			return Record{}, e
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memories SET value=$1,occurred_at=$2 WHERE id=$3 AND account_id=$4`, b, fact.ObservedAt, id, user); err != nil {
			return Record{}, err
		}
	}
	records, err := pgRecords(ctx, tx, user, fact.Key)
	if err != nil {
		return Record{}, err
	}
	if err = tx.Commit(); err != nil {
		return Record{}, err
	}
	for _, r := range records {
		if r.ID == id {
			return r, nil
		}
	}
	return Record{}, ErrNotFound
}

func (s *PostgresStore) Revisions(user, id string) ([]Revision, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var exists bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memories m JOIN accounts a ON a.id=m.account_id WHERE m.id=$1 AND m.account_id=$2 AND a.deleted_at IS NULL)`, id, user).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT h.value,h.changed_at FROM memory_history h JOIN memories m ON m.id=h.memory_id JOIN accounts a ON a.id=m.account_id WHERE m.id=$1 AND m.account_id=$2 AND a.deleted_at IS NULL ORDER BY h.changed_at,h.id`, id, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var b []byte
		var r Revision
		if err = rows.Scan(&b, &r.ChangedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &r.Fact); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Delete removes the item and correction history. If the deleted item was the
// latest, older values are invalidated rather than silently resurfacing as the
// user's current relationship status or contact boundary.
func (s *PostgresStore) Delete(user, id string) error {
	if err := validateUser(user); err != nil {
		return err
	}
	if id == "" {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = activeMemoryAccount(ctx, tx, user); err != nil {
		return err
	}
	var key string
	err = tx.QueryRowContext(ctx, `SELECT kind FROM memories WHERE id=$1 AND account_id=$2 FOR UPDATE`, id, user).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	records, err := pgRecords(ctx, tx, user, key)
	if err != nil {
		return err
	}
	current := false
	for _, r := range records {
		if r.ID == id && r.Current {
			current = true
		}
	}
	if current {
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_deletion_watermarks(account_id,kind,deleted_at) VALUES($1,$2,now()) ON CONFLICT(account_id,kind) DO UPDATE SET deleted_at=EXCLUDED.deleted_at`, user, key); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memories SET valid=false WHERE account_id=$1 AND kind=$2 AND id<>$3`, user, key, id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM memories WHERE id=$1 AND account_id=$2`, id, user); err != nil {
		return err
	}
	return tx.Commit()
}
