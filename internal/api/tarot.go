package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/tarot"
)

type TarotStore interface {
	CreateTarot(context.Context, string, tarot.Session) (string, error)
	GetTarot(context.Context, string, string) (tarot.Session, error)
	SelectTarot(context.Context, string, string, *tarot.Engine, int) (tarot.Session, error)
}

// tarotView never exposes the shuffled deck. Revealed cards come only from
// selected positions; the remaining positions stay hidden on every GET.
func tarotView(id string, s tarot.Session, engine *tarot.Engine) (map[string]any, error) {
	cards := engine.Cards()
	selected := make([]map[string]any, 0, len(s.Selected))
	for _, position := range s.Selected {
		if position < 0 || position >= len(s.Deck) {
			return nil, errors.New("corrupt session")
		}
		d := s.Deck[position]
		if d.CardID < 0 || d.CardID >= len(cards) {
			return nil, errors.New("corrupt card")
		}
		selected = append(selected, map[string]any{"position": position, "card_id": d.CardID, "name": cards[d.CardID].Name, "reversed": d.Reversed})
	}
	v := map[string]any{"id": id, "question": s.Question, "question_type": s.QuestionType, "spread": s.Spread, "selected": selected, "complete": false}
	req, err := engine.SessionRequest(s)
	if err == nil {
		result, err := engine.Compare(req)
		if err != nil {
			return nil, err
		}
		v["complete"] = true
		v["meanings"] = result.Baseline.Cards
	}
	return v, nil
}

func (s *Server) handleTarot(w http.ResponseWriter, r *http.Request, user string) bool {
	const base = "/v1/tarot/sessions"
	if r.URL.Path != base && !strings.HasPrefix(r.URL.Path, base+"/") {
		return false
	}
	if s.TarotStore == nil || s.Lab == nil || s.Lab.Tarot == nil {
		fail(w, 503, "tarot storage unavailable")
		return true
	}
	engine := s.Lab.Tarot
	if r.URL.Path == base && r.Method == http.MethodPost {
		var req struct {
			Question     string `json:"question"`
			QuestionType string `json:"question_type"`
			Spread       string `json:"spread"`
		}
		if !read(w, r, &req) {
			return true
		}
		session, err := engine.NewSession(req.Question, req.QuestionType, req.Spread)
		if err != nil {
			fail(w, 400, err.Error())
			return true
		}
		id, err := s.TarotStore.CreateTarot(r.Context(), user, session)
		if err != nil {
			fail(w, 500, "tarot storage failed")
			return true
		}
		v, err := tarotView(id, session, engine)
		if err != nil {
			fail(w, 500, "tarot session invalid")
			return true
		}
		write(w, 201, v)
		return true
	}
	tail := strings.TrimPrefix(r.URL.Path, base+"/")
	selectPath := strings.HasSuffix(tail, "/select")
	readingPath := strings.HasSuffix(tail, "/reading")
	id := tail
	if selectPath {
		id = strings.TrimSuffix(tail, "/select")
	} else if readingPath {
		id = strings.TrimSuffix(tail, "/reading")
	}
	if id == "" || strings.Contains(id, "/") {
		fail(w, 404, "not found")
		return true
	}
	var session tarot.Session
	var err error
	if readingPath && r.Method == http.MethodPost {
		if s.Readings == nil {
			fail(w, 503, "reading service is not configured")
			return true
		}
		session, err = s.TarotStore.GetTarot(r.Context(), user, id)
		if err != nil {
			if errors.Is(err, persistence.ErrTarotNotFound) {
				fail(w, 404, "not found")
			} else {
				fail(w, 500, "tarot storage failed")
			}
			return true
		}
		req, err := engine.SessionRequest(session)
		if err != nil {
			fail(w, 409, "complete card selection first")
			return true
		}
		input, err := json.Marshal(req)
		if err != nil {
			fail(w, 500, "tarot session invalid")
			return true
		}
		envelope, err := json.Marshal(readingEnvelope{Input: input, UseMemory: false})
		if err != nil {
			fail(w, 500, "tarot session invalid")
			return true
		}
		// A server-derived key makes retries and repeated taps on the same
		// completed draw resolve to one reading, independently per account.
		ctx := context.WithValue(r.Context(), readingUserKey{}, user)
		v, err := s.Readings.Create(ctx, user, "tarot-session:"+id, "tarot", "enhanced", envelope)
		if err != nil {
			readingError(w, err)
		} else {
			write(w, 201, v)
		}
		return true
	} else if selectPath && r.Method == http.MethodPost {
		var req struct {
			Position *int `json:"position"`
		}
		if !read(w, r, &req) {
			return true
		}
		if req.Position == nil {
			fail(w, 400, "position required")
			return true
		}
		session, err = s.TarotStore.SelectTarot(r.Context(), user, id, engine, *req.Position)
	} else if !selectPath && !readingPath && r.Method == http.MethodGet {
		session, err = s.TarotStore.GetTarot(r.Context(), user, id)
	} else {
		fail(w, 404, "not found")
		return true
	}
	if err != nil {
		switch {
		case errors.Is(err, persistence.ErrTarotNotFound):
			fail(w, 404, "not found")
		case errors.Is(err, persistence.ErrTarotSelection):
			fail(w, 409, "invalid selection")
		default:
			fail(w, 500, "tarot storage failed")
		}
		return true
	}
	v, err := tarotView(id, session, engine)
	if err != nil {
		fail(w, 500, "tarot session invalid")
		return true
	}
	write(w, 200, v)
	return true
}
