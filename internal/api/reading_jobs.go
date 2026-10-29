package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

type asyncReadingJobs interface {
	StartSummary(context.Context, string, string, string, string, json.RawMessage) (readings.JobStatus, error)
	SummaryStatusByKey(context.Context, string, string) (readings.JobStatus, error)
	SummaryStatusByID(context.Context, string, string) (readings.JobStatus, error)
}

func (s *Server) handleReadingJobs(w http.ResponseWriter, r *http.Request, user string) bool {
	path := r.URL.Path
	if path != "/v1/reading-jobs" && !strings.HasPrefix(path, "/v1/reading-jobs/") {
		return false
	}
	jobs, ok := s.Readings.(asyncReadingJobs)
	if !ok {
		fail(w, 503, "durable generation jobs are not configured")
		return true
	}
	if path == "/v1/reading-jobs" && r.Method == http.MethodPost {
		var req struct {
			Domain    string          `json:"domain"`
			Variant   string          `json:"variant"`
			Input     json.RawMessage `json:"input"`
			UseMemory bool            `json:"use_memory"`
		}
		if !read(w, r, &req) {
			return true
		}
		if req.Domain == "daily" || req.Domain == "tarot" {
			fail(w, 400, "daily and tarot readings require their saved profile or session")
			return true
		}
		envelope, _ := json.Marshal(readingEnvelope{Input: req.Input, UseMemory: req.UseMemory})
		ctx := context.WithValue(r.Context(), readingUserKey{}, user)
		status, err := jobs.StartSummary(ctx, user, r.Header.Get("Idempotency-Key"), req.Domain, req.Variant, envelope)
		if err != nil {
			readingError(w, err)
		} else {
			write(w, http.StatusAccepted, status)
		}
		return true
	}
	if r.Method == http.MethodGet {
		var status readings.JobStatus
		var err error
		if path == "/v1/reading-jobs" {
			status, err = jobs.SummaryStatusByKey(r.Context(), user, r.Header.Get("Idempotency-Key"))
		} else {
			id := strings.TrimPrefix(path, "/v1/reading-jobs/")
			if id == "" || strings.Contains(id, "/") {
				fail(w, 404, "route not found")
				return true
			}
			status, err = jobs.SummaryStatusByID(r.Context(), user, id)
		}
		if err != nil {
			readingError(w, err)
		} else {
			write(w, 200, status)
		}
		return true
	}
	fail(w, 405, "method not allowed")
	return true
}
