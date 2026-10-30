package persistence

import (
	"context"
	"database/sql"
	"errors"
)

var ErrAccountDeleted = errors.New("account unavailable")

// DeleteAccount transactionally removes private SQL data and revokes all
// sessions, retaining an opaque account tombstone and detached purchase audit
// records. Object storage is not yet wired and must be added before attachments
// are accepted. It is idempotent for an existing deleted account.
func (s Store) DeleteAccount(ctx context.Context, accountID string) error {
	if accountID == "" {
		return ErrAccountDeleted
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var deleted sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&deleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAccountDeleted
		}
		return err
	}
	if deleted.Valid {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET deleted_at=now(), deletion_generation=deletion_generation+1 WHERE id=$1`, accountID); err != nil {
		return err
	}
	// Memories are removed before conversations to avoid ON DELETE SET NULL on a
	// now-deleted account. Remaining dependent rows cascade from these deletes.
	statements := []string{
		`DELETE FROM auth_sessions WHERE account_id=$1`,
		`DELETE FROM account_identities WHERE account_id=$1`,
		`DELETE FROM password_credentials WHERE account_id=$1`,
		`DELETE FROM memories WHERE account_id=$1`,
		`DELETE FROM memory_deletion_watermarks WHERE account_id=$1`,
		`DELETE FROM memory_suppressions WHERE account_id=$1`,
		`DELETE FROM conversations WHERE account_id=$1`,
		`DELETE FROM profiles WHERE account_id=$1`,
		`DELETE FROM generation_jobs WHERE account_id=$1`,
		`DELETE FROM readings WHERE account_id=$1`,
		`DELETE FROM tarot_sessions WHERE account_id=$1`,
		`DELETE FROM entitlements WHERE account_id=$1`,
		`UPDATE purchase_transactions SET account_id=NULL WHERE account_id=$1`,
	}
	for _, query := range statements {
		if _, err = tx.ExecContext(ctx, query, accountID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// WithActiveAccount runs a job's database write while holding the same account
// lock as deletion. The job must pass the generation captured before work began.
// External work should occur outside the transaction; a stale result is rejected
// here once deletion has committed.
func (s Store) WithActiveAccount(ctx context.Context, accountID string, generation int64, write func(*sql.Tx) error) error {
	if accountID == "" || write == nil {
		return ErrAccountDeleted
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var deleted sql.NullTime
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT deleted_at,deletion_generation FROM accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&deleted, &current)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (deleted.Valid || current != generation)) {
		return ErrAccountDeleted
	}
	if err != nil {
		return err
	}
	if err = write(tx); err != nil {
		return err
	}
	return tx.Commit()
}
