package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

type fakeJobs struct{}

func (fakeJobs) Create(context.Context, string, string, string, string, json.RawMessage) (readings.View, error) {
	return readings.View{}, nil
}
func (fakeJobs) Get(string, string) (readings.View, error)           { return readings.View{}, nil }
func (fakeJobs) List(string) ([]readings.View, error)                { return nil, nil }
func (fakeJobs) UnlockForTest(string, string) (readings.View, error) { return readings.View{}, nil }
func (fakeJobs) Detail(context.Context, string, string) (readings.View, error) {
	return readings.View{}, nil
}
func (fakeJobs) StartSummary(_ context.Context, user, key, domain, variant string, input json.RawMessage) (readings.JobStatus, error) {
	if user != "alice" || key != "key" || domain != "counsel" || variant != "enhanced" || !json.Valid(input) {
		return readings.JobStatus{}, errors.New("invalid")
	}
	return readings.JobStatus{ID: "job-1", State: "pending_or_interrupted"}, nil
}
func (fakeJobs) SummaryStatusByKey(_ context.Context, user, key string) (readings.JobStatus, error) {
	if user != "alice" || key != "key" {
		return readings.JobStatus{}, readings.ErrNotFound
	}
	return readings.JobStatus{ID: "job-1", State: "ready"}, nil
}
func (fakeJobs) SummaryStatusByID(_ context.Context, user, id string) (readings.JobStatus, error) {
	if user != "alice" || id != "job-1" {
		return readings.JobStatus{}, readings.ErrNotFound
	}
	return readings.JobStatus{ID: id, State: "ready"}, nil
}
func TestReadingJobHTTPContract(t *testing.T) {
	handler := (&Server{Tokens: map[string]string{"token": "alice", "other": "bob"}, Readings: fakeJobs{}}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/v1/reading-jobs", bytes.NewBufferString(`{"domain":"counsel","variant":"enhanced","input":{"question":"q"},"use_memory":false}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Idempotency-Key", "key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatalf("start %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		path, token, key string
		want             int
	}{{"/v1/reading-jobs/job-1", "token", "", 200}, {"/v1/reading-jobs", "token", "key", 200}, {"/v1/reading-jobs/job-1", "other", "", 404}, {"/v1/reading-jobs/job-1", "", "", 401}} {
		req = httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		req.Header.Set("Idempotency-Key", tc.key)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s: %d want %d", tc.path, w.Code, tc.want)
		}
	}
}
