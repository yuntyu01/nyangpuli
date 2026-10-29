package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

type readingUserKey struct{}

// The envelope is fingerprinted BEFORE reading mutable memory. An idempotent
// retry replays its original snapshot even if the user's memory later changes.
type readingEnvelope struct {
	Input     json.RawMessage `json:"input"`
	UseMemory bool            `json:"use_memory"`
}

type readingBuilder struct {
	lab    *lab.Lab
	memory *memory.Store
}

func (b readingBuilder) Build(ctx context.Context, domain, variant string, data json.RawMessage) (generation.Input, error) {
	var envelope readingEnvelope
	if err := lab.Decode(data, &envelope); err != nil {
		return generation.Input{}, fmt.Errorf("%w: invalid envelope", readings.ErrInvalidInput)
	}
	index := 0
	switch variant {
	case "baseline":
	case "enhanced":
		index = 1
	default:
		return generation.Input{}, fmt.Errorf("%w: variant must be baseline or enhanced (not a quality ranking)", readings.ErrInvalidInput)
	}
	input := envelope.Input
	if envelope.UseMemory {
		if domain != "counsel" {
			return generation.Input{}, fmt.Errorf("%w: memory currently supported for counsel only", readings.ErrInvalidInput)
		}
		user, ok := ctx.Value(readingUserKey{}).(string)
		if !ok || user == "" || b.memory == nil {
			return generation.Input{}, readings.ErrNotConfigured
		}
		var cr counsel.Request
		if err := lab.Decode(input, &cr); err != nil {
			return generation.Input{}, fmt.Errorf("%w: invalid counsel input", readings.ErrInvalidInput)
		}
		facts, err := b.memory.Current(user)
		if err != nil {
			return generation.Input{}, errors.New("memory read failed")
		}
		selected, _, err := selectMemory(facts, len(cr.Facts))
		if err != nil {
			return generation.Input{}, fmt.Errorf("%w: %s", readings.ErrInvalidInput, err)
		}
		cr.Facts = append(cr.Facts, selected...)
		input, err = json.Marshal(cr)
		if err != nil {
			return generation.Input{}, err
		}
	}
	result, err := b.lab.Compare(ctx, domain, "reading-snapshot", input)
	if err != nil {
		return generation.Input{}, fmt.Errorf("%w: %s", readings.ErrInvalidInput, err)
	}
	if len(result.Inputs) != 2 {
		return generation.Input{}, errors.New("expected two research variants")
	}
	return result.Inputs[index], nil
}

type readingGenerator struct{ client *generation.Client }

func (g readingGenerator) Generate(ctx context.Context, in generation.Input) (generation.Result, error) {
	results, err := generation.Run(ctx, []generation.Input{in}, g.client, true)
	if len(results) == 0 {
		return generation.Result{}, err
	}
	return results[0], err
}

// No provider defaults. A nil client allows archive reads but no generation.
func NewReadingService(l *lab.Lab, m *memory.Store, client *generation.Client, path string) (*readings.Service, error) {
	var generator readings.Generator
	if client != nil {
		generator = readingGenerator{client: client}
	}
	return readings.New(readingBuilder{lab: l, memory: m}, generator, path)
}

func (s *Server) handleReadings(w http.ResponseWriter, r *http.Request, user string) bool {
	path := r.URL.Path
	isDev := strings.HasPrefix(path, "/v1/dev/readings/")
	if path != "/v1/readings" && !strings.HasPrefix(path, "/v1/readings/") && !isDev {
		return false
	}
	if isDev && !s.AllowTestUnlock {
		fail(w, 404, "route not found")
		return true
	}
	if s.Readings == nil {
		fail(w, 503, "reading service is not configured")
		return true
	}
	ctx := context.WithValue(r.Context(), readingUserKey{}, user)
	if path == "/v1/readings" {
		switch r.Method {
		case "POST":
			var req struct {
				Domain    string          `json:"domain"`
				Variant   string          `json:"variant"`
				Input     json.RawMessage `json:"input"`
				UseMemory bool            `json:"use_memory"`
			}
			if !read(w, r, &req) {
				return true
			}
			envelope, _ := json.Marshal(readingEnvelope{Input: req.Input, UseMemory: req.UseMemory})
			v, err := s.Readings.Create(ctx, user, r.Header.Get("Idempotency-Key"), req.Domain, req.Variant, envelope)
			if err != nil {
				readingError(w, err)
			} else {
				write(w, 201, v)
			}
		case "GET":
			v, err := s.Readings.List(user)
			if err != nil {
				readingError(w, err)
			} else {
				write(w, 200, v)
			}
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	prefix := "/v1/readings/"
	if isDev {
		prefix = "/v1/dev/readings/"
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 1 && parts[0] != "" && !isDev && r.Method == "GET" {
		v, err := s.Readings.Get(user, parts[0])
		if err != nil {
			readingError(w, err)
		} else {
			write(w, 200, v)
		}
		return true
	}
	if len(parts) == 2 && parts[0] != "" && r.Method == "POST" {
		var v readings.View
		var err error
		if isDev && parts[1] == "unlock" {
			v, err = s.Readings.UnlockForTest(user, parts[0])
		} else if !isDev && parts[1] == "detail" {
			v, err = s.Readings.Detail(ctx, user, parts[0])
		} else {
			fail(w, 404, "route not found")
			return true
		}
		if err != nil {
			readingError(w, err)
		} else {
			write(w, 200, v)
		}
		return true
	}
	fail(w, 404, "route not found")
	return true
}

func readingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, readings.ErrNotConfigured):
		fail(w, 503, "generation not configured; opt in with serve --enable-generation")
	case errors.Is(err, readings.ErrInvalidInput):
		fail(w, 400, "invalid reading input, variant or idempotency key")
	case errors.Is(err, readings.ErrNotFound):
		fail(w, 404, "reading not found")
	case errors.Is(err, readings.ErrLocked):
		fail(w, 402, "detail is locked; production payment integration is not implemented")
	case errors.Is(err, readings.ErrOutcomeUnknown):
		fail(w, 409, "generation outcome is unknown; reconciliation required before retry")
	case errors.Is(err, readings.ErrConflict):
		fail(w, 409, "idempotency key already belongs to different input")
	case errors.Is(err, readings.ErrGeneration):
		fail(w, 502, "generation failed; no completed result saved")
	default:
		fail(w, 500, "reading operation failed")
	}
}
