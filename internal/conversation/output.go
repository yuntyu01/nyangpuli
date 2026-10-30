package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

type Candidate struct {
	Class      string `json:"class"`
	Key        string `json:"key"`
	Value      string `json:"value"`
	Origin     string `json:"origin"`
	Quote      string `json:"quote"`
	Subject    string `json:"subject"`
	Situation  string `json:"situation"`
	OccurredAt string `json:"occurred_at"`
}

type Tried struct {
	What   string `json:"what"`
	Result string `json:"result"`
	Source string `json:"source"`
}

// Assessment is the model's short structured judgment before the reply. It is
// an AI note, not a user fact, and is stored with the message for review.
type Assessment struct {
	Want              string   `json:"want"`
	Topics            []string `json:"topics"`
	TopicCertainty    string   `json:"topic_certainty"`
	Tried             []Tried  `json:"tried"`
	Avoid             []string `json:"avoid"`
	Capacity          string   `json:"capacity"`
	Unknowns          []string `json:"unknowns"`
	UserFeelings      []string `json:"user_feelings"`
	GuessesAboutOther []string `json:"guesses_about_other"`
}

type Output struct {
	Assessment       Assessment  `json:"assessment"`
	ResponseMode     string      `json:"response_mode"`
	AskedQuestion    bool        `json:"asked_question"`
	Reply            string      `json:"reply"`
	EvidenceIDs      []string    `json:"evidence_ids"`
	MemoryCandidates []Candidate `json:"memory_candidates"`
}

var (
	wants      = map[string]bool{"vent": true, "solution": true, "mixed": true, "unclear": true}
	capacities = map[string]bool{"listen_only": true, "one_small_step": true, "options": true}
	modes      = map[string]bool{"brief_empathy": true, "reflect": true, "options": true, "one_step": true, "clarify": true, "safety": true}
	topics     = map[string]bool{"contact": true, "conflict": true, "hurt": true, "some": true, "breakup": true, "reunion": true, "self_feelings": true, "partner_feelings": true, "vague_distress": true, "other": true}
	certainty  = map[string]bool{"low": true, "medium": true, "high": true}
)

// Check records one code-verifiable property of the output. Passing these does
// not show that the answer is helpful or free of invented content; those are
// evaluated separately with conversation cases.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fixed, warn
	Detail string `json:"detail,omitempty"`
}

var (
	ErrOutputFormat = errors.New("model output is not the required JSON object")
	ErrEmptyReply   = errors.New("model reply is empty or too long")
)

var factKeys = map[string]bool{"relationship_status": true, "contact_boundary": true, "preference": true, "personal_fact": true}
var episodeKeys = map[string]bool{"event": true, "feeling": true, "attempt": true, "wish": true}

const maxReplyRunes = 2000

// Parse decodes and checks the model output against the turn context. Format
// and empty-reply errors fail the turn. Unknown evidence IDs are removed and
// candidates that cannot be verified as user statements become unconfirmed
// AI inferences; each adjustment is reported as a check.
func Parse(text string, c Context) (Output, []Check, error) {
	var out Output
	d := json.NewDecoder(bytes.NewReader([]byte(trimJSON(text))))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return Output{}, nil, ErrOutputFormat
	}
	if d.More() {
		return Output{}, nil, ErrOutputFormat
	}
	a := &out.Assessment
	if !wants[a.Want] || !capacities[a.Capacity] || !modes[out.ResponseMode] {
		return Output{}, nil, ErrOutputFormat
	}
	checks := []Check{{Name: "json_schema", Status: "pass"}}
	out.Reply = strings.TrimSpace(out.Reply)
	if out.Reply == "" || utf8.RuneCountInString(out.Reply) > maxReplyRunes {
		return Output{}, nil, ErrEmptyReply
	}
	checks = append(checks, Check{Name: "reply_length", Status: "pass"})
	allowed := c.EvidenceIDs()
	checks = append(checks, assessmentChecks(&out, allowed)...)
	kept, unknown := []string{}, []string{}
	seen := map[string]bool{}
	for _, id := range out.EvidenceIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if allowed[id] {
			kept = append(kept, id)
		} else {
			unknown = append(unknown, id)
		}
	}
	out.EvidenceIDs = kept
	if len(unknown) > 0 {
		checks = append(checks, Check{Name: "evidence_ids_known", Status: "fixed", Detail: "removed unknown ids: " + strings.Join(unknown, ",")})
	} else {
		checks = append(checks, Check{Name: "evidence_ids_known", Status: "pass"})
	}
	candidates, adjusted := []Candidate{}, []string{}
	for _, cand := range out.MemoryCandidates {
		cand.Key = strings.TrimSpace(cand.Key)
		cand.Value = strings.TrimSpace(cand.Value)
		valid := cand.Value != "" && utf8.RuneCountInString(cand.Value) <= 500 && utf8.RuneCountInString(cand.Subject) <= 50 && utf8.RuneCountInString(cand.Situation) <= 300
		switch cand.Class {
		case memory.ClassFact:
			valid = valid && factKeys[cand.Key]
		case memory.ClassEpisode:
			valid = valid && episodeKeys[cand.Key]
		default:
			valid = false
		}
		if !valid || (cand.Origin != memory.OriginStated && cand.Origin != memory.OriginAIInference) {
			adjusted = append(adjusted, "dropped invalid candidate")
			continue
		}
		if cand.Origin == memory.OriginStated && !quoted(cand.Quote, c.Current.Content) {
			cand.Origin = memory.OriginAIInference
			adjusted = append(adjusted, "unquoted statement kept as unconfirmed inference: "+cand.Key)
		}
		candidates = append(candidates, cand)
		if len(candidates) == 8 {
			break
		}
	}
	out.MemoryCandidates = candidates
	if len(adjusted) > 0 {
		checks = append(checks, Check{Name: "memory_candidates_sourced", Status: "fixed", Detail: strings.Join(adjusted, "; ")})
	} else {
		checks = append(checks, Check{Name: "memory_candidates_sourced", Status: "pass"})
	}
	return out, checks, nil
}

// assessmentChecks normalizes enum lists and flags mismatches between the
// stated judgment and the reply. Mismatches are warnings for review; they are
// not proof that the reply suits the person.
func assessmentChecks(out *Output, allowed map[string]bool) []Check {
	a := &out.Assessment
	fixed := []string{}
	kept := []string{}
	for _, t := range a.Topics {
		if topics[t] {
			kept = append(kept, t)
		} else {
			fixed = append(fixed, "unknown topic "+t)
		}
	}
	a.Topics = kept
	if !certainty[a.TopicCertainty] {
		fixed = append(fixed, "topic_certainty set to low")
		a.TopicCertainty = "low"
	}
	for i := range a.Tried {
		if a.Tried[i].Source != "" && !allowed[a.Tried[i].Source] {
			fixed = append(fixed, "unknown tried source "+a.Tried[i].Source)
			a.Tried[i].Source = ""
		}
	}
	checks := []Check{}
	if len(fixed) > 0 {
		checks = append(checks, Check{Name: "assessment_fields", Status: "fixed", Detail: strings.Join(fixed, "; ")})
	} else {
		checks = append(checks, Check{Name: "assessment_fields", Status: "pass"})
	}
	questions := strings.Count(out.Reply, "?") + strings.Count(out.Reply, "？")
	if (questions > 0) != out.AskedQuestion {
		checks = append(checks, Check{Name: "asked_question_flag", Status: "warn", Detail: "flag does not match question marks in reply"})
	} else {
		checks = append(checks, Check{Name: "asked_question_flag", Status: "pass"})
	}
	listening := a.Want == "vent" || a.Capacity == "listen_only"
	switch {
	case listening && (out.ResponseMode == "options" || out.ResponseMode == "one_step"):
		checks = append(checks, Check{Name: "mode_matches_request", Status: "warn", Detail: "advice mode while the user wants to be heard"})
	case listening && questions > 1:
		checks = append(checks, Check{Name: "mode_matches_request", Status: "warn", Detail: "several questions while the user wants to be heard"})
	default:
		checks = append(checks, Check{Name: "mode_matches_request", Status: "pass"})
	}
	return checks
}

// quoted requires a non-trivial verbatim excerpt of the user's message.
func quoted(quote, message string) bool {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	q := norm(quote)
	return utf8.RuneCountInString(q) >= 2 && strings.Contains(norm(message), q)
}

// MemoryFact converts a candidate into a stored memory tied to its message.
// Facts use the time the user said them, so a later statement or correction
// wins. Episodes keep their stated date when it is valid and not in the future.
func (cand Candidate) MemoryFact(source Message, index int) (memory.Fact, memory.Meta) {
	observed := source.CreatedAt.UTC()
	if cand.Class == memory.ClassEpisode && cand.OccurredAt != "" {
		if t, err := time.Parse("2006-01-02", cand.OccurredAt); err == nil && !t.After(observed) {
			observed = t.UTC()
		}
	}
	note := "AI 추정: 사용자 확인 전"
	if cand.Origin == memory.OriginStated {
		note = "사용자 발화: " + strings.Join(strings.Fields(cand.Quote), " ")
	}
	fact := memory.Fact{Key: cand.Key, Value: cand.Value, SourceEventID: "message:" + source.ID + ":" + itoa(index), ObservedAt: observed, Note: note}
	return fact, memory.Meta{Class: cand.Class, Origin: cand.Origin, Subject: strings.TrimSpace(cand.Subject), Situation: strings.TrimSpace(cand.Situation), SourceMessageID: source.ID}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// ParseSummary accepts {"summary": "..."} with bounded length.
func ParseSummary(text string) (string, error) {
	var out struct {
		Summary string `json:"summary"`
	}
	d := json.NewDecoder(bytes.NewReader([]byte(trimJSON(text))))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.More() {
		return "", ErrOutputFormat
	}
	out.Summary = strings.TrimSpace(out.Summary)
	if out.Summary == "" || utf8.RuneCountInString(out.Summary) > 3000 {
		return "", ErrEmptyReply
	}
	return out.Summary, nil
}
