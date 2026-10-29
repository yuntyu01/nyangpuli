package tarot

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Session is the persisted server-generated deck. Do not send Deck to clients:
// clients select opaque positions; selection does not influence the shuffle.
type Session struct {
	Question     string `json:"question"`
	QuestionType string `json:"question_type"`
	Spread       string `json:"spread"`
	Deck         []Draw `json:"deck"`
	Selected     []int  `json:"selected"`
}

// NewSession uses a cryptographic shuffle and independently fixes orientation.
// Persist the entire session before allowing the user to select positions.
func (e *Engine) NewSession(question, questionType, spread string) (Session, error) {
	question = strings.TrimSpace(question)
	if question == "" || len(question) > 12000 {
		return Session{}, errors.New("question required (maximum 12000 bytes)")
	}
	s, ok := e.spreads[spread]
	if !ok || len(s.Positions) == 0 || len(s.Positions) > 78 {
		return Session{}, errors.New("unknown or unsupported spread")
	}
	deck := make([]Draw, 78)
	for i := range deck {
		deck[i].CardID = i
	}
	for i := len(deck) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return Session{}, err
		}
		j := int(n.Int64())
		deck[i], deck[j] = deck[j], deck[i]
	}
	for i := range deck {
		n, err := rand.Int(rand.Reader, big.NewInt(2))
		if err != nil {
			return Session{}, err
		}
		deck[i].Reversed = n.Int64() == 1
	}
	return Session{Question: question, QuestionType: questionType, Spread: spread, Deck: deck, Selected: []int{}}, nil
}

// Select takes a deck position once and preserves both card and orientation.
func (e *Engine) Select(s *Session, position int) error {
	if s == nil || len(s.Deck) != 78 {
		return errors.New("invalid session")
	}
	spread, ok := e.spreads[s.Spread]
	if !ok {
		return errors.New("unknown spread")
	}
	if position < 0 || position >= len(s.Deck) {
		return errors.New("invalid position")
	}
	if len(s.Selected) >= len(spread.Positions) {
		return errors.New("selection complete")
	}
	for _, existing := range s.Selected {
		if existing == position {
			return errors.New("position already selected")
		}
	}
	s.Selected = append(s.Selected, position)
	return nil
}

// Request returns a complete selection suitable for Compare, without redrawing.
func (e *Engine) SessionRequest(s Session) (Request, error) {
	spread, ok := e.spreads[s.Spread]
	if !ok || len(s.Deck) != 78 || len(s.Selected) != len(spread.Positions) {
		return Request{}, errors.New("incomplete session")
	}
	seenPosition, seenCard := map[int]bool{}, map[int]bool{}
	cards := make([]Draw, 0, len(s.Selected))
	for _, p := range s.Selected {
		if p < 0 || p >= len(s.Deck) || seenPosition[p] {
			return Request{}, errors.New("invalid selection")
		}
		d := s.Deck[p]
		if d.CardID < 0 || d.CardID > 77 || seenCard[d.CardID] {
			return Request{}, fmt.Errorf("invalid card")
		}
		seenPosition[p], seenCard[d.CardID] = true, true
		cards = append(cards, d)
	}
	return Request{Question: s.Question, QuestionType: s.QuestionType, Spread: s.Spread, Cards: cards}, nil
}
