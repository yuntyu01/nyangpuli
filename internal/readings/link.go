package readings

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// LinkSnapshot is what a counseling conversation may copy from a reading the
// same user explicitly links. It carries the frozen evidence and the summary
// the user saw, never a detail that is locked or another account's data.
type LinkSnapshot struct {
	ReadingID string          `json:"reading_id"`
	Domain    string          `json:"domain"`
	Variant   string          `json:"variant"`
	Sources   []string        `json:"sources"`
	Question  string          `json:"question"`
	Input     json.RawMessage `json:"input,omitempty"`
	Evidence  json.RawMessage `json:"evidence,omitempty"`
	Summary   string          `json:"summary"`
}

const maxLinkEvidence = 48 * 1024

func linkSnapshot(r record) (LinkSnapshot, error) {
	if strings.TrimSpace(r.Summary) == "" {
		return LinkSnapshot{}, ErrNotFound
	}
	out := LinkSnapshot{ReadingID: r.ID, Domain: r.Domain, Variant: r.Variant, Sources: append([]string{}, r.SourceIDs...), Question: r.Frozen.Question, Summary: r.Summary}
	if json.Valid(r.Frozen.CaseContext) {
		out.Input = append(json.RawMessage(nil), r.Frozen.CaseContext...)
	}
	out.Evidence = frozenEvidence(r.Frozen.UserPrompt)
	return out, nil
}

// frozenEvidence extracts the JSON evidence that the reading prompt embedded
// after its marker. Missing or oversized evidence is omitted, not rebuilt.
func frozenEvidence(prompt string) json.RawMessage {
	for _, marker := range []string{"참고 JSON:\n", "INPUT_JSON:\n"} {
		i := strings.LastIndex(prompt, marker)
		if i < 0 {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(prompt[i+len(marker):]))
		var raw json.RawMessage
		if dec.Decode(&raw) != nil || len(raw) > maxLinkEvidence {
			return nil
		}
		var compact bytes.Buffer
		if json.Compact(&compact, raw) != nil {
			return nil
		}
		return compact.Bytes()
	}
	return nil
}

func (s *Service) LinkSnapshot(_ context.Context, user, id string) (LinkSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.owned(user, id)
	if err != nil {
		return LinkSnapshot{}, err
	}
	return linkSnapshot(r)
}

func (s *PostgresService) LinkSnapshot(ctx context.Context, user, id string) (LinkSnapshot, error) {
	r, err := s.getRecord(ctx, user, id)
	if err != nil {
		return LinkSnapshot{}, err
	}
	return linkSnapshot(r)
}
