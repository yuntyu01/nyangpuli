package purchase

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Development is a mock store, not receipt validation or a payment adapter.
// Only expose it on explicitly enabled development servers.
type Development struct{ DB *sql.DB }
type devProof struct {
	TransactionID string `json:"transaction_id"`
	AccountID     string `json:"account_id"`
	Status        Status `json:"status"`
}
type devVerifier struct{}

func (devVerifier) Verify(_ context.Context, proof []byte) (Verified, error) {
	var p devProof
	if json.Unmarshal(proof, &p) != nil || len(p.TransactionID) > 128 || strings.TrimSpace(p.TransactionID) == "" || strings.ContainsRune(p.TransactionID, '\x00') {
		return Verified{}, ErrInvalid
	}
	revision := uint64(0)
	switch p.Status {
	case Pending:
		revision = 1
	case Purchased:
		revision = 2
	case Cancelled, Refunded:
		revision = 3
	default:
		return Verified{}, ErrInvalid
	}
	return Verified{Store: "development_mock", TransactionID: p.TransactionID, ProductID: "development_detail", AccountID: p.AccountID, Status: p.Status, Revision: revision}, nil
}

// Simulate constructs the proof inside the trusted server using authenticated
// identity; all statuses are fictional and must never unlock real purchases.
func (d Development) Simulate(ctx context.Context, user, reading, transaction string, status Status) error {
	if d.DB == nil {
		return ErrUnavailable
	}
	proof, err := json.Marshal(devProof{TransactionID: transaction, AccountID: user, Status: status})
	if err != nil {
		return ErrInvalid
	}
	return New(devVerifier{}, devRepository{d.DB}, "development_detail").VerifyAndApply(ctx, user, reading, proof)
}

type devRepository struct{ db *sql.DB }

func (r devRepository) Apply(ctx context.Context, user, reading string, p Verified) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var deleted sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT deleted_at FROM accounts WHERE id=$1 FOR UPDATE`, user).Scan(&deleted); err != nil || deleted.Valid {
		return ErrInvalid
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM generation_jobs WHERE id=$1 AND account_id=$2 AND request_key LIKE 'summary:%' AND state='ready')`, reading, user).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO dev_purchase_transactions(transaction_id,account_id,reading_job_id,status,revision) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, p.TransactionID, user, reading, p.Status, p.Revision)
	if err != nil {
		return err
	}
	var owner, job, status string
	var revision uint64
	if err = tx.QueryRowContext(ctx, `SELECT account_id,reading_job_id,status,revision FROM dev_purchase_transactions WHERE transaction_id=$1 FOR UPDATE`, p.TransactionID).Scan(&owner, &job, &status, &revision); err != nil {
		return err
	}
	if owner != user {
		return ErrOwnership
	}
	if job != reading {
		return ErrInvalid
	}
	// Stale events are ignored, terminal states cannot be reversed, and duplicate
	// events with conflicting content at one revision are rejected.
	if p.Revision < revision {
		return tx.Commit()
	}
	if p.Revision == revision {
		if status != string(p.Status) {
			return ErrInvalid
		}
		return tx.Commit()
	}
	if status == string(Refunded) || status == string(Cancelled) {
		return ErrInvalid
	}
	if p.Status == Refunded && status != string(Purchased) {
		return ErrInvalid
	}
	if p.Status == Cancelled && status == string(Purchased) {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `UPDATE dev_purchase_transactions SET status=$1,revision=$2,updated_at=now() WHERE transaction_id=$3`, p.Status, p.Revision, p.TransactionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ Repository = devRepository{}
