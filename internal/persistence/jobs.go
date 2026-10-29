package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrJobConflict = errors.New("request key already belongs to different input")
var ErrJobNotPending = errors.New("job is no longer pending or account was deleted")

type Job struct {
	ID, State, RequestHash string
	Evidence, Result       json.RawMessage
	DeletionGeneration     int64
}

// ClaimJob creates one durable claim per account/request key. The caller must
// only invoke a provider when created is true. A pre-existing pending job has
// an ambiguous outcome after a crash and must never be automatically retried.
// The transaction ends before any provider call.
func (s Store) ClaimJob(ctx context.Context, account, id, key, requestHash string, evidence json.RawMessage) (job Job, created bool, err error) {
	if account == "" || id == "" || key == "" || requestHash == "" || !json.Valid(evidence) {
		return job, false, errors.New("invalid job")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return job, false, err
	}
	defer tx.Rollback()
	var generation int64
	err = tx.QueryRowContext(ctx, `SELECT deletion_generation FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, account).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return job, false, ErrUnauthenticated
	}
	if err != nil {
		return job, false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO generation_jobs(id,account_id,request_key,state,request_hash,deletion_generation,evidence) VALUES($1,$2,$3,'pending',$4,$5,$6) ON CONFLICT(account_id,request_key) DO NOTHING`, id, account, key, requestHash, generation, []byte(evidence))
	if err != nil {
		return job, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return job, false, err
	}
	var evidenceBytes, resultBytes []byte
	err = tx.QueryRowContext(ctx, `SELECT id,state,COALESCE(request_hash,''),deletion_generation,evidence,result FROM generation_jobs WHERE account_id=$1 AND request_key=$2`, account, key).Scan(&job.ID, &job.State, &job.RequestHash, &job.DeletionGeneration, &evidenceBytes, &resultBytes)
	job.Evidence = evidenceBytes
	job.Result = resultBytes
	if err != nil {
		return job, false, err
	}
	if job.RequestHash != requestHash {
		return job, false, ErrJobConflict
	}
	if err = tx.Commit(); err != nil {
		return job, false, err
	}
	return job, n == 1, nil
}

// FinishJob must be invoked only after a provider response. On process or
// transport failure use state "unknown", not a retryable failure. Account row
// locking serializes completion with deletion and the generation guard blocks
// stale workers from restoring deleted private data.
func (s Store) FinishJob(ctx context.Context, account, id string, generation int64, state string, result json.RawMessage) error {
	if state != "ready" && state != "unknown" && state != "failed" {
		return errors.New("invalid completion state")
	}
	if len(result) > 0 && !json.Valid(result) {
		return errors.New("invalid result")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT deletion_generation FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, account).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrJobNotPending
	}
	if err != nil {
		return err
	}
	if current != generation {
		return ErrJobNotPending
	}
	var payload any
	if len(result) > 0 {
		payload = []byte(result)
	}
	res, err := tx.ExecContext(ctx, `UPDATE generation_jobs SET state=$1,result=$2,updated_at=now() WHERE id=$3 AND account_id=$4 AND deletion_generation=$5 AND state='pending'`, state, payload, id, account, generation)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrJobNotPending
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	return nil
}
