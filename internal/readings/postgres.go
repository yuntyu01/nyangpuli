package readings

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

// PostgresService stores frozen inputs and outputs in durable job records.
// Provider calls happen after the claim transaction commits. Ambiguous or
// interrupted jobs remain pending/unknown and are never automatically retried.
type PostgresService struct {
	DB              *sql.DB
	Builder         Builder
	Generator       Generator
	AllowTestUnlock bool
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (s *PostgresService) jobByKey(ctx context.Context, user, key string) (persistence.Job, error) {
	var j persistence.Job
	var ev, res []byte
	err := s.DB.QueryRowContext(ctx, `SELECT j.id,j.state,COALESCE(j.request_hash,''),j.deletion_generation,j.evidence,j.result FROM generation_jobs j JOIN accounts a ON a.id=j.account_id WHERE j.account_id=$1 AND j.request_key=$2 AND a.deleted_at IS NULL`, user, key).Scan(&j.ID, &j.State, &j.RequestHash, &j.DeletionGeneration, &ev, &res)
	j.Evidence = ev
	j.Result = res
	return j, err
}
func decodeJob(j persistence.Job) (record, error) {
	var r record
	if j.State == "pending" || j.State == "unknown" {
		return r, ErrOutcomeUnknown
	}
	if j.State != "ready" {
		return r, ErrGeneration
	}
	if json.Unmarshal(j.Result, &r) != nil {
		return r, ErrStorage
	}
	return r, nil
}
func (s *PostgresService) Create(ctx context.Context, user, key, domain, variant string, input json.RawMessage) (View, error) {
	if err := validateIdentity(user, key); err != nil {
		return View{}, err
	}
	if strings.TrimSpace(domain) == "" || len(domain) > 64 || len(variant) > 128 {
		return View{}, ErrInvalidInput
	}
	canonical, err := canonicalObject(input)
	if err != nil {
		return View{}, err
	}
	hashBytes, _ := json.Marshal(struct {
		Domain, Variant string
		Input           json.RawMessage
	}{domain, variant, canonical})
	digest := sha256.Sum256(hashBytes)
	hash := hex.EncodeToString(digest[:])
	jobKey := "summary:" + key
	if job, e := s.jobByKey(ctx, user, jobKey); e == nil {
		if job.RequestHash != hash {
			return View{}, ErrConflict
		}
		r, e := decodeJob(job)
		if e != nil {
			return View{}, e
		}
		return s.Get(user, r.ID)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return View{}, ErrStorage
	}
	if s.Builder == nil || s.Generator == nil {
		return View{}, ErrNotConfigured
	}
	frozen, err := s.Builder.Build(ctx, domain, variant, canonical)
	if err != nil {
		return View{}, fmt.Errorf("%w: source builder rejected request", ErrInvalidInput)
	}
	if err = validateFrozen(frozen); err != nil {
		return View{}, err
	}
	id, err := randomID()
	if err != nil {
		return View{}, err
	}
	now := time.Now().UTC()
	r := record{View: View{ID: id, Domain: domain, Variant: frozen.VariantID, Status: "summary_pending", DetailLocked: true, SourceIDs: append([]string{}, frozen.Sources...), CreatedAt: now, UpdatedAt: now}, User: user, IdempotencyKey: key, RequestHash: hash, Frozen: cloneInput(frozen)}
	evidence, err := json.Marshal(r)
	if err != nil {
		return View{}, ErrStorage
	}
	store := persistence.Store{DB: s.DB}
	j, created, err := store.ClaimJob(ctx, user, id, jobKey, hash, evidence)
	if errors.Is(err, persistence.ErrJobConflict) {
		return View{}, ErrConflict
	}
	if err != nil {
		return View{}, ErrStorage
	}
	if !created {
		r, err = decodeJob(j)
		if err != nil {
			return View{}, err
		}
		return s.Get(user, r.ID)
	}
	result, genErr := s.Generator.Generate(ctx, stageInput(r, false))
	r.SummaryUsage = addUsage(r.SummaryUsage, result.Usage)
	r.SummaryModel, r.SummaryPromptHash = resultModel(result), result.PromptSHA256
	r.UpdatedAt = time.Now().UTC()
	callErr := accepted(ctx, result, genErr)
	state := "ready"
	if callErr != nil {
		state = "failed"
		r.Status = "summary_failed"
		if genErr != nil || ctx.Err() != nil {
			state = "unknown"
			r.Status = "summary_unknown"
		}
	} else {
		r.Summary = result.Text
		r.Status = "summary_ready"
	}
	data, _ := json.Marshal(r)
	saveCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = store.FinishJob(saveCtx, user, id, j.DeletionGeneration, state, data); err != nil {
		return View{}, ErrStorage
	}
	if callErr != nil {
		return View{}, callErr
	}
	return view(r), nil
}
func (s *PostgresService) getRecord(ctx context.Context, user, id string) (record, error) {
	if err := validateUser(user); err != nil {
		return record{}, err
	}
	var ev, res []byte
	var state string
	err := s.DB.QueryRowContext(ctx, `SELECT j.state,j.evidence,j.result FROM generation_jobs j JOIN accounts a ON a.id=j.account_id WHERE j.id=$1 AND j.account_id=$2 AND j.request_key LIKE 'summary:%' AND a.deleted_at IS NULL`, id, user).Scan(&state, &ev, &res)
	if errors.Is(err, sql.ErrNoRows) {
		return record{}, ErrNotFound
	}
	if err != nil {
		return record{}, ErrStorage
	}
	r, err := decodeJob(persistence.Job{State: state, Evidence: ev, Result: res})
	if err != nil {
		return record{}, err
	}
	var unlocked bool
	if s.AllowTestUnlock {
		if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dev_reading_unlocks WHERE account_id=$1 AND reading_job_id=$2) OR EXISTS(SELECT 1 FROM dev_purchase_transactions WHERE account_id=$1 AND reading_job_id=$2 AND status='purchased')`, user, id).Scan(&unlocked); err != nil {
			return record{}, ErrStorage
		}
	}
	if unlocked {
		r.Unlocked = true
		r.UnlockKind = "test_only_not_payment"
		d, e := s.jobByKey(ctx, user, "detail:"+id)
		if e == nil && d.State == "ready" {
			detail, e := decodeJob(d)
			if e != nil {
				return record{}, e
			}
			r = detail
			r.Unlocked = true
			r.UnlockKind = "test_only_not_payment"
		} else if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return record{}, ErrStorage
		}
	}
	return r, nil
}
func (s *PostgresService) Get(user, id string) (View, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := s.getRecord(ctx, user, id)
	if err != nil {
		return View{}, err
	}
	return view(r), nil
}
func (s *PostgresService) List(user string) ([]View, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.DB.QueryContext(ctx, `SELECT j.id FROM generation_jobs j JOIN accounts a ON a.id=j.account_id WHERE j.account_id=$1 AND j.request_key LIKE 'summary:%' AND j.state='ready' AND a.deleted_at IS NULL ORDER BY j.created_at DESC,j.id`, user)
	if err != nil {
		return nil, ErrStorage
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return nil, ErrStorage
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, ErrStorage
	}
	rows.Close()
	out := []View{}
	for _, id := range ids {
		r, e := s.getRecord(ctx, user, id)
		if e != nil {
			return nil, e
		}
		out = append(out, view(r))
	}
	return out, nil
}
func (s *PostgresService) UnlockForTest(user, id string) (View, error) {
	if !s.AllowTestUnlock {
		return View{}, ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := s.getRecord(ctx, user, id)
	if err != nil {
		return View{}, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO dev_reading_unlocks(account_id,reading_job_id) SELECT $1,id FROM generation_jobs WHERE id=$2 AND account_id=$1 AND state='ready' ON CONFLICT DO NOTHING`, user, id)
	if err != nil {
		return View{}, ErrStorage
	}
	r.Unlocked = true
	r.UnlockKind = "test_only_not_payment"
	return view(r), nil
}
func (s *PostgresService) Detail(ctx context.Context, user, id string) (View, error) {
	r, err := s.getRecord(ctx, user, id)
	if err != nil {
		return View{}, err
	}
	if !r.Unlocked {
		return View{}, ErrLocked
	}
	if r.Detail != "" {
		return view(r), nil
	}
	if s.Generator == nil {
		return View{}, ErrNotConfigured
	}
	jobKey := "detail:" + id
	if j, e := s.jobByKey(ctx, user, jobKey); e == nil {
		_, e := decodeJob(j)
		if e != nil {
			return View{}, e
		}
		return s.currentDetail(ctx, user, id)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return View{}, ErrStorage
	}
	jobID, err := randomID()
	if err != nil {
		return View{}, err
	}
	evidence, _ := json.Marshal(r)
	store := persistence.Store{DB: s.DB}
	j, created, err := store.ClaimJob(ctx, user, jobID, jobKey, r.RequestHash, evidence)
	if err != nil {
		return View{}, ErrStorage
	}
	if !created {
		_, e := decodeJob(j)
		if e != nil {
			return View{}, e
		}
		return s.currentDetail(ctx, user, id)
	}
	result, genErr := s.Generator.Generate(ctx, stageInput(r, true))
	r.DetailUsage = addUsage(r.DetailUsage, result.Usage)
	r.DetailModel, r.DetailPromptHash = resultModel(result), result.PromptSHA256
	r.UpdatedAt = time.Now().UTC()
	callErr := accepted(ctx, result, genErr)
	state := "ready"
	if callErr != nil {
		state = "failed"
		r.Status = "detail_failed"
		if genErr != nil || ctx.Err() != nil {
			state = "unknown"
			r.Status = "detail_unknown"
		}
	} else {
		r.Detail = result.Text
		r.Status = "detail_ready"
	}
	data, _ := json.Marshal(r)
	saveCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = store.FinishJob(saveCtx, user, jobID, j.DeletionGeneration, state, data); err != nil {
		return View{}, ErrStorage
	}
	if callErr != nil {
		return View{}, callErr
	}
	return s.currentDetail(saveCtx, user, id)
}

// Re-check authorization after generation, because a refund can occur while
// the provider call runs. Never return persisted detail without entitlement.
func (s *PostgresService) currentDetail(ctx context.Context, user, id string) (View, error) {
	r, err := s.getRecord(ctx, user, id)
	if err != nil {
		return View{}, err
	}
	if !r.Unlocked {
		return View{}, ErrLocked
	}
	return view(r), nil
}
