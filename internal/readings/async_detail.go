package readings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

// detailJobStatus never exposes the result: callers must re-read the reading,
// where the current entitlement and account status are checked again.
func detailJobStatus(j persistence.Job) (JobStatus, error) {
	out := JobStatus{ID: j.ID, State: j.State}
	switch j.State {
	case "pending":
		out.State = "pending_or_interrupted"
	case "ready", "failed", "unknown", "invalidated", "not_started":
	default:
		return JobStatus{}, ErrStorage
	}
	return out, nil
}

// StartDetail reserves one job per reading before launching the provider.
// HTTP cancellation does not cancel the worker. A crash leaves pending work
// ambiguous; it is never automatically retried.
func (s *PostgresService) StartDetail(ctx context.Context, user, id string) (JobStatus, error) {
	version, err := s.readJobMemoryGeneration(ctx, user, id)
	if err != nil {
		return JobStatus{}, err
	}
	r, err := s.getRecord(ctx, user, id)
	if err != nil {
		return JobStatus{}, err
	}
	if !r.Unlocked {
		return JobStatus{}, ErrLocked
	}
	key := "detail:" + id
	j, err := s.jobByKey(ctx, user, key)
	if err == nil && j.State != "not_started" {
		return detailJobStatus(j)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return JobStatus{}, ErrStorage
	}
	if s.Generator == nil {
		return JobStatus{}, ErrNotConfigured
	}
	jobID, err := randomID()
	if err != nil {
		return JobStatus{}, err
	}
	evidence, err := json.Marshal(r)
	if err != nil {
		return JobStatus{}, ErrStorage
	}
	j, created, err := (persistence.Store{DB: s.DB}).ClaimDetailJobWithMemory(ctx, user, jobID, key, r.RequestHash, evidence, version, id, s.AllowTestUnlock)
	if errors.Is(err, persistence.ErrDetailNotEntitled) {
		return JobStatus{}, ErrLocked
	}
	if errors.Is(err, persistence.ErrMemoryChanged) {
		return JobStatus{}, ErrInvalidated
	}
	if errors.Is(err, persistence.ErrUnauthenticated) {
		return JobStatus{}, ErrNotFound
	}
	if errors.Is(err, persistence.ErrJobConflict) {
		return JobStatus{}, ErrConflict
	}
	if err != nil {
		return JobStatus{}, ErrStorage
	}
	if created {
		go s.generateDetail(user, id, r, j.ID, j.DeletionGeneration)
	}
	return detailJobStatus(j)
}

func (s *PostgresService) generateDetail(user, id string, r record, jobID string, accountGeneration int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// A refund between reservation and worker startup prevents a new call. A
	// refund during the call cannot undo that call but blocks every result read.
	if _, err := s.currentDetail(ctx, user, id); err != nil {
		r.Status = "detail_failed"
		data, _ := json.Marshal(r)
		save, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = (persistence.Store{DB: s.DB}).FinishJob(save, user, jobID, accountGeneration, "not_started", data)
		return
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
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	save, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_ = (persistence.Store{DB: s.DB}).FinishJob(save, user, jobID, accountGeneration, state, data)
}

func (s *PostgresService) DetailStatus(ctx context.Context, user, id string) (JobStatus, error) {
	r, err := s.getRecord(ctx, user, id)
	if err != nil {
		return JobStatus{}, err
	}
	if !r.Unlocked {
		return JobStatus{}, ErrLocked
	}
	j, err := s.jobByKey(ctx, user, "detail:"+id)
	if errors.Is(err, sql.ErrNoRows) {
		return JobStatus{}, ErrNotFound
	}
	if err != nil {
		return JobStatus{}, ErrStorage
	}
	return detailJobStatus(j)
}
