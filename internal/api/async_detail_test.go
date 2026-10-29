package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

type fakeDetailJobs struct{ fakeJobs }

func (fakeDetailJobs) StartDetail(_ context.Context, user, id string) (readings.JobStatus, error) {
	if user != "alice" {
		return readings.JobStatus{}, readings.ErrNotFound
	}
	if id == "locked" {
		return readings.JobStatus{}, readings.ErrLocked
	}
	if id != "r1" {
		return readings.JobStatus{}, readings.ErrNotFound
	}
	return readings.JobStatus{ID: "job1", State: "pending_or_interrupted"}, nil
}
func (fakeDetailJobs) DetailStatus(_ context.Context, user, id string) (readings.JobStatus, error) {
	if user != "alice" {
		return readings.JobStatus{}, readings.ErrNotFound
	}
	if id == "locked" {
		return readings.JobStatus{}, readings.ErrLocked
	}
	if id != "r1" {
		return readings.JobStatus{}, readings.ErrNotFound
	}
	return readings.JobStatus{ID: "job1", State: "ready"}, nil
}
func TestAsyncDetailHTTPStatusNeverReturnsContent(t *testing.T) {
	h := (&Server{Tokens: map[string]string{"a": "alice", "b": "bob"}, Readings: fakeDetailJobs{}}).Handler()
	for _, tc := range []struct {
		method, path, token string
		code                int
		state               string
	}{
		{"POST", "/v1/readings/r1/detail-jobs", "a", 202, "pending_or_interrupted"},
		{"GET", "/v1/readings/r1/detail-jobs", "a", 200, "ready"},
		{"GET", "/v1/readings/locked/detail-jobs", "a", 402, ""},
		{"POST", "/v1/readings/r1/detail-jobs", "b", 404, ""},
		{"POST", "/v1/readings/r1/detail-jobs", "", 401, ""},
		{"DELETE", "/v1/readings/r1/detail-jobs", "a", 405, ""},
	} {
		w := call(h, tc.method, tc.path, tc.token, "")
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if tc.state != "" {
			var got readings.JobStatus
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.State != tc.state || got.Summary != nil {
				t.Fatalf("state %+v %v", got, err)
			}
			if strings.Contains(w.Body.String(), "detail\"") {
				t.Fatal("detail leaked")
			}
		}
	}
	// The JSON lab implementation has no durable worker.
	h = (&Server{Tokens: map[string]string{"a": "alice"}, Readings: fakeJobs{}}).Handler()
	if w := call(h, "POST", "/v1/readings/r1/detail-jobs", "a", ""); w.Code != 503 {
		t.Fatal("non-durable mode", w.Code)
	}
}
