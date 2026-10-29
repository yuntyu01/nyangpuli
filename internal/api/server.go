// Package api exposes a local research API with server-bound user identities.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

type Authenticator interface {
	Authenticate(context.Context, string) (string, error)
}

type MemoryStore interface {
	Set(string, memory.Fact) (memory.Record, error)
	Current(string) ([]memory.Record, error)
	History(string, string) ([]memory.Record, error)
	DeleteUser(string) error
}

type Server struct {
	ProfileDaily         ProfileDailyStore
	DailyModelVersion    string
	Accounts             AccountService
	TarotStore           TarotStore
	Auth                 Authenticator
	AccountDeletion      AccountDeleter
	PostgresOnlyStorage  bool
	DevelopmentPurchaser DevelopmentPurchaser
	Lab                  *lab.Lab
	Memory               MemoryStore
	Mem0                 *memory.Mem0
	Tokens               map[string]string
	Readings             ReadingService
	AllowTestUnlock      bool
}

// Tokens maps configured token => authenticated user. Do not accept identities from bodies/headers.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/healthz" && r.Method == "GET" {
			write(w, 200, map[string]string{"status": "ok", "purpose": "research_lab"})
			return
		}
		if s.publicAccountRoutes(w, r) {
			return
		}
		var user string
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			fail(w, 401, "bearer token required")
			return
		}
		if s.Auth != nil {
			user, _ = s.Auth.Authenticate(r.Context(), token)
		} else {
			// Static token mapping is retained only for the existing local lab.
			for candidate, u := range s.Tokens {
				if len(candidate) > 0 && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
					user = u
				}
			}
		}
		if user == "" {
			fail(w, 401, "invalid token")
			return
		}
		if s.authenticatedAccountRoutes(w, r, user, token) {
			return
		}
		if s.handlePostgresDataDeletion(w, r, user) {
			return
		}
		if s.handleDevelopmentPurchase(w, r, user) {
			return
		}
		if s.handleProfileDaily(w, r, user) {
			return
		}
		if s.handleTarot(w, r, user) {
			return
		}
		if s.handleReadingJobs(w, r, user) {
			return
		}
		if s.handleMemoryItem(w, r, user) {
			return
		}
		if s.handleReadings(w, r, user) {
			return
		}
		switch {
		case r.URL.Path == "/v1/memory/remote" && r.Method == "POST":
			if s.Mem0 == nil {
				fail(w, 503, "Mem0 is not configured; no external request made")
				return
			}
			var req struct {
				Messages []memory.Message `json:"messages"`
				Infer    bool             `json:"infer"`
			}
			if !read(w, r, &req) {
				return
			}
			value, err := s.Mem0.Add(r.Context(), user, req.Messages, req.Infer)
			if err != nil {
				fail(w, 502, "Mem0 add failed")
				return
			}
			write(w, 201, value)
		case r.URL.Path == "/v1/compare/memory" && r.Method == "POST":
			var req struct {
				Query   string `json:"query"`
				UseMem0 bool   `json:"use_mem0"`
			}
			if !read(w, r, &req) {
				return
			}
			local, err := s.Memory.Current(user)
			if err != nil {
				fail(w, 500, "memory read failed")
				return
			}
			response := map[string]any{"local_current_facts": local, "mem0_status": "not_run", "limitation": "Explicit confirmed facts versus Mem0 search; this is not a retrieval quality score."}
			if req.UseMem0 {
				if s.Mem0 == nil {
					fail(w, 503, "Mem0 is not configured; no external request made")
					return
				}
				found, err := s.Mem0.Search(r.Context(), user, req.Query, 5)
				if err != nil {
					fail(w, 502, "Mem0 search failed")
					return
				}
				response["mem0_status"] = "completed"
				response["mem0_search_results"] = found
			}
			write(w, 200, response)
		case r.URL.Path == "/v1/memory" && r.Method == "GET":
			values, err := s.Memory.Current(user)
			if err != nil {
				fail(w, 500, "memory read failed")
				return
			}
			write(w, 200, values)
		case r.URL.Path == "/v1/memory/history" && r.Method == "GET":
			values, err := s.Memory.History(user, r.URL.Query().Get("key"))
			if err != nil {
				fail(w, 400, err.Error())
				return
			}
			write(w, 200, values)
		case r.URL.Path == "/v1/memory" && r.Method == "POST":
			var fact memory.Fact
			if !read(w, r, &fact) {
				return
			}
			v, err := s.Memory.Set(user, fact)
			if err != nil {
				status := 400
				if errors.Is(err, memory.ErrConflict) {
					status = 409
				}
				fail(w, status, err.Error())
				return
			}
			write(w, 201, v)
		case r.URL.Path == "/v1/memory" && r.Method == "DELETE":
			if err := s.Memory.DeleteUser(user); err != nil {
				fail(w, 500, "delete failed")
				return
			}
			write(w, 200, map[string]string{"status": "deleted_for_authenticated_user"})
		case strings.HasPrefix(r.URL.Path, "/v1/compare/") && r.Method == "POST":
			domain := strings.TrimPrefix(r.URL.Path, "/v1/compare/")
			var req struct {
				CaseID    string          `json:"case_id"`
				Input     json.RawMessage `json:"input"`
				UseMemory bool            `json:"use_memory"`
			}
			if !read(w, r, &req) {
				return
			}
			var selection *lab.MemorySelection
			if req.UseMemory {
				if domain != "counsel" {
					fail(w, 400, "use_memory currently supported for counsel only")
					return
				}
				var cr counsel.Request
				if err := lab.Decode(req.Input, &cr); err != nil {
					fail(w, 400, err.Error())
					return
				}
				facts, err := s.Memory.Current(user)
				if err != nil {
					fail(w, 500, "memory read failed")
					return
				}
				// Bound context; preserve complete facts and report exclusions rather than silently truncate.
				selected, report, selectErr := selectMemory(facts, len(cr.Facts))
				if selectErr != nil {
					fail(w, 422, selectErr.Error())
					return
				}
				selection = &report
				cr.Facts = append(cr.Facts, selected...)
				req.Input, _ = json.Marshal(cr)
			}
			result, err := s.Lab.Compare(r.Context(), domain, req.CaseID, req.Input)
			if err != nil {
				fail(w, 400, err.Error())
				return
			}
			result.MemorySelection = selection
			write(w, 200, result)
		default:
			fail(w, 404, "route not found")
		}
	})
}
func read(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 413, "body too large or unreadable")
		return false
	}
	if err := lab.Decode(data, v); err != nil {
		fail(w, 400, err.Error())
		return false
	}
	return true
}
func write(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	write(w, status, map[string]string{"error": message})
}

// This is explicit bounded selection, not vector retrieval. Boundary/state facts
// have priority, then recent event time; skipped facts remain in the user store.
func selectMemory(records []memory.Record, directCount int) ([]counsel.Fact, lab.MemorySelection, error) {
	report := lab.MemorySelection{Available: len(records), Policy: "boundary_and_state_then_recency; max16; max1000runes_per_value"}
	budget := 50 - directCount
	if budget > 16 {
		budget = 16
	}
	if budget < 0 {
		return nil, report, errors.New("direct input exceeds 50 facts")
	}
	priority := func(key string) int {
		switch key {
		case "contact_boundary":
			return 2
		case "relationship_status":
			return 1
		}
		return 0
	}
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if priority(a.Key) != priority(b.Key) {
			return priority(a.Key) > priority(b.Key)
		}
		if !a.ObservedAt.Equal(b.ObservedAt) {
			return a.ObservedAt.After(b.ObservedAt)
		}
		return a.Key < b.Key
	})
	selected := []counsel.Fact{}
	for _, f := range records {
		fits := utf8.RuneCountInString(f.Key) <= 100 && utf8.RuneCountInString(f.Value) <= 1000 && utf8.RuneCountInString(f.SourceEventID) <= 200
		if !fits || len(selected) >= budget {
			if f.Key == "contact_boundary" {
				return nil, report, errors.New("contact_boundary cannot fit in context; shorten that fact or reduce direct facts before counseling")
			}
			report.ExcludedKeys = append(report.ExcludedKeys, f.Key)
			continue
		}
		selected = append(selected, counsel.Fact{Key: f.Key, Value: f.Value, SourceID: f.SourceEventID, ObservedAt: f.ObservedAt})
	}
	report.Selected = len(selected)
	return selected, report, nil
}
