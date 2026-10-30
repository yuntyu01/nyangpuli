package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

type memoryItemStore interface {
	Correct(string, string, memory.Fact) (memory.Record, error)
	Revisions(string, string) ([]memory.Revision, error)
	Delete(string, string) error
	Inferences(string) ([]memory.Record, error)
	Confirm(string, string) (memory.Record, error)
}

func (s *Server) handleMemoryItem(w http.ResponseWriter, r *http.Request, user string) bool {
	const prefix = "/v1/memory/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	tail := strings.TrimPrefix(r.URL.Path, prefix)
	if tail == "history" || tail == "remote" {
		return false
	}
	parts := strings.Split(tail, "/")
	if len(parts) == 0 || parts[0] == "" {
		fail(w, 404, "route not found")
		return true
	}
	store, ok := s.Memory.(memoryItemStore)
	if !ok {
		fail(w, 503, "individual memory editing requires PostgreSQL")
		return true
	}
	var err error
	switch {
	case len(parts) == 1 && parts[0] == "inferences" && r.Method == http.MethodGet:
		var result []memory.Record
		result, err = store.Inferences(user)
		if err == nil {
			write(w, 200, result)
			return true
		}
	case len(parts) == 2 && parts[1] == "confirm" && r.Method == http.MethodPost:
		var result memory.Record
		result, err = store.Confirm(user, parts[0])
		if err == nil {
			write(w, 200, result)
			return true
		}
	case len(parts) == 1 && r.Method == http.MethodPut:
		var fact memory.Fact
		if !read(w, r, &fact) {
			return true
		}
		var result memory.Record
		result, err = store.Correct(user, parts[0], fact)
		if err == nil {
			write(w, 200, result)
			return true
		}
	case len(parts) == 1 && r.Method == http.MethodDelete:
		err = store.Delete(user, parts[0])
		if err == nil {
			write(w, 200, map[string]string{"status": "deleted"})
			return true
		}
	case len(parts) == 2 && parts[1] == "revisions" && r.Method == http.MethodGet:
		var result []memory.Revision
		result, err = store.Revisions(user, parts[0])
		if err == nil {
			write(w, 200, result)
			return true
		}
	default:
		fail(w, 404, "route not found")
		return true
	}
	switch {
	case errors.Is(err, memory.ErrNotFound), errors.Is(err, memory.ErrScope):
		fail(w, 404, "memory not found")
	case errors.Is(err, memory.ErrInvalid):
		fail(w, 400, "invalid memory")
	case errors.Is(err, memory.ErrConflict):
		fail(w, 409, "memory event conflict")
	default:
		fail(w, 500, "memory operation failed")
	}
	return true
}
