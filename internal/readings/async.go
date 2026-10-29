package readings

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

// JobStatus exposes no frozen prompt, personal memory or locked detail.
// pending_or_interrupted means either a worker is running or the process died;
// it is never authorization to repeat a potentially billable provider call.
type JobStatus struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Summary *View  `json:"summary,omitempty"`
}

func jobStatus(j persistence.Job) (JobStatus, error) {
	out := JobStatus{ID: j.ID, State: j.State}
	switch j.State {
	case "pending":
		out.State = "pending_or_interrupted"
	case "ready":
		r, err := decodeJob(j)
		if err != nil {
			return out, err
		}
		v := view(r)
		v.Detail = ""
		v.DetailLocked = true
		v.EntitlementSource = "none"
		out.Summary = &v
	case "failed", "unknown", "invalidated":
	default:
		return JobStatus{}, ErrStorage
	}
	return out, nil
}

// StartSummary reserves before launching any provider request. The worker is
// independent of the HTTP connection but lives only as long as this process;
// interrupted jobs require reconciliation rather than an automatic retry.
func (s *PostgresService) StartSummary(ctx context.Context, user, key, domain, variant string, input json.RawMessage) (JobStatus, error) {
	if err := validateIdentity(user, key); err != nil {
		return JobStatus{}, err
	}
	if strings.TrimSpace(domain) == "" || len(domain) > 64 || len(variant) > 128 {
		return JobStatus{}, ErrInvalidInput
	}
	canonical, err := canonicalObject(input)
	if err != nil {
		return JobStatus{}, err
	}
	raw, _ := json.Marshal(struct {
		Domain, Variant string
		Input           json.RawMessage
	}{domain, variant, canonical})
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	jobKey := "summary:" + key
	if j, e := s.jobByKey(ctx, user, jobKey); e == nil {
		if j.RequestHash != hash {
			return JobStatus{}, ErrConflict
		}
		return jobStatus(j)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return JobStatus{}, ErrStorage
	}
	if s.Builder == nil || s.Generator == nil {
		return JobStatus{}, ErrNotConfigured
	}
	memoryVersion, err := s.captureMemoryGeneration(ctx, user, domain, canonical)
	if err != nil {
		return JobStatus{}, err
	}
	frozen, err := s.Builder.Build(ctx, domain, variant, canonical)
	if err != nil {
		return JobStatus{}, ErrInvalidInput
	}
	if err = validateFrozen(frozen); err != nil {
		return JobStatus{}, err
	}
	id, err := randomID()
	if err != nil {
		return JobStatus{}, err
	}
	now := time.Now().UTC()
	r := record{View: View{ID: id, Domain: domain, Variant: frozen.VariantID, Status: "summary_pending", DetailLocked: true, SourceIDs: append([]string{}, frozen.Sources...), CreatedAt: now, UpdatedAt: now}, User: user, IdempotencyKey: key, RequestHash: hash, Frozen: cloneInput(frozen)}
	evidence, err := json.Marshal(r)
	if err != nil {
		return JobStatus{}, ErrStorage
	}
	j, created, err := (persistence.Store{DB: s.DB}).ClaimJobWithMemory(ctx, user, id, jobKey, hash, evidence, memoryVersion)
	if errors.Is(err, persistence.ErrJobConflict) || errors.Is(err, persistence.ErrMemoryChanged) {
		return JobStatus{}, ErrConflict
	}
	if err != nil {
		return JobStatus{}, ErrStorage
	}
	if created {
		go s.generateSummary(user, r, j.DeletionGeneration)
	}
	return jobStatus(j)
}
func (s *PostgresService) generateSummary(user string, r record, accountGeneration int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	saveCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_ = (persistence.Store{DB: s.DB}).FinishJob(saveCtx, user, r.ID, accountGeneration, state, data)
}
func (s *PostgresService) SummaryStatusByKey(ctx context.Context, user, key string) (JobStatus, error) {
	if err := validateIdentity(user, key); err != nil {
		return JobStatus{}, err
	}
	j, err := s.jobByKey(ctx, user, "summary:"+key)
	if errors.Is(err, sql.ErrNoRows) {
		return JobStatus{}, ErrNotFound
	}
	if err != nil {
		return JobStatus{}, ErrStorage
	}
	return jobStatus(j)
}
func (s *PostgresService) SummaryStatusByID(ctx context.Context, user, id string) (JobStatus, error) {
	if err := validateUser(user); err != nil {
		return JobStatus{}, err
	}
	var j persistence.Job
	var ev, res []byte
	err := s.DB.QueryRowContext(ctx, `SELECT j.id,j.state,COALESCE(j.request_hash,''),j.deletion_generation,j.evidence,j.result FROM generation_jobs j JOIN accounts a ON a.id=j.account_id WHERE j.id=$1 AND j.account_id=$2 AND j.request_key LIKE 'summary:%' AND a.deleted_at IS NULL`, id, user).Scan(&j.ID, &j.State, &j.RequestHash, &j.DeletionGeneration, &ev, &res)
	if errors.Is(err, sql.ErrNoRows) {
		return JobStatus{}, ErrNotFound
	}
	if err != nil {
		return JobStatus{}, ErrStorage
	}
	j.Evidence = ev
	j.Result = res
	return jobStatus(j)
}
