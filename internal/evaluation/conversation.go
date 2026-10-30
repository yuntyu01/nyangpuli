package evaluation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/conversation"
	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

// ConversationFixture holds synthetic dialogues. Each case ends with one user
// message that both conditions answer: the earlier single-question counseling
// prompt and the conversation prompt with history, memory and links.
type ConversationFixture struct {
	Version string             `json:"version"`
	Status  string             `json:"status"`
	Purpose string             `json:"purpose"`
	Cases   []ConversationCase `json:"cases"`
}

type ConversationCase struct {
	ID          string              `json:"id"`
	Situation   string              `json:"situation"`
	Turns       []FixtureTurn       `json:"turns"`
	Final       string              `json:"final"`
	Memory      []FixtureMemory     `json:"memory"`
	Summary     *FixtureSummary     `json:"summary,omitempty"`
	Links       []conversation.Link `json:"links,omitempty"`
	Expect      ContextExpectation  `json:"expect"`
	ReviewFocus []string            `json:"review_focus"`
}

type FixtureTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type FixtureMemory struct {
	ID         string    `json:"id"`
	Class      string    `json:"class"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Origin     string    `json:"origin"`
	Subject    string    `json:"subject,omitempty"`
	Situation  string    `json:"situation,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

type FixtureSummary struct {
	Through int64  `json:"through"`
	Text    string `json:"text"`
}

// ContextExpectation is checked on the assembled context without a model. It
// verifies what the conversation condition receives, not answer quality.
type ContextExpectation struct {
	MemoryInclude  []string `json:"memory_values_include,omitempty"`
	MemoryExclude  []string `json:"memory_values_exclude,omitempty"`
	MessageInclude []string `json:"messages_include,omitempty"`
	SummaryUsed    *bool    `json:"summary_used,omitempty"`
	Links          *int     `json:"links,omitempty"`
	ReferencesUsed *bool    `json:"references_used,omitempty"`
}

type ContextCheck struct {
	CaseID   string   `json:"case_id"`
	Passed   bool     `json:"passed"`
	Failures []string `json:"failures,omitempty"`
}

const (
	LegacyCondition       = "legacy_single_question"
	ConversationCondition = "conversation_v2"
	NoReferenceCondition  = "conversation_v2_no_references"
)

// ConversationReview replaces the default review sheet. Length and the number
// of sources are deliberately not scored.
var ConversationReview = Review{
	Columns: []string{"case_id", "label", "helpful_1_5", "context_fit_1_5", "invented_fact_count", "partner_mind_certainty_count", "repeated_failed_advice_count", "ignored_refusal_or_avoid_count", "unrequested_advice_or_questions", "stale_fact_count", "problem_quote", "reviewer_id"},
	Instructions: []string{
		"context의 dialogue와 review_focus를 먼저 읽고, 마지막 사용자 메시지에 대한 답변만 평가하세요.",
		"답변 길이, 인용한 자료 수, 전문 용어의 양은 점수에 반영하지 마세요. 짧은 공감이 맞는 상황에서는 짧은 답이 더 적절할 수 있습니다.",
		"가상 대화 평가입니다. 결과를 실제 사용자 효과나 미래 적중률로 표현하지 마세요.",
	},
}

// PrepareConversations builds the paired inputs and the model-free context
// checks. With references set, a third condition repeats the conversation
// prompt without reference cards.
func PrepareConversations(f ConversationFixture, withReferenceAblation bool) ([]generation.Input, []ContextCheck, error) {
	if strings.TrimSpace(f.Version) == "" || len(f.Cases) == 0 || len(f.Cases) > 100 {
		return nil, nil, errors.New("invalid conversation fixture")
	}
	seen := map[string]bool{}
	inputs, checks := []generation.Input{}, []ContextCheck{}
	for _, c := range f.Cases {
		if strings.TrimSpace(c.ID) == "" || seen[c.ID] || strings.TrimSpace(c.Final) == "" || len(c.ReviewFocus) == 0 {
			return nil, nil, fmt.Errorf("missing or duplicate conversation case %q", c.ID)
		}
		seen[c.ID] = true
		shared, err := sharedContext(c)
		if err != nil {
			return nil, nil, err
		}
		legacy, err := counsel.Compare(counsel.Request{Question: c.Final, IncludeResearch: true})
		if err != nil {
			return nil, nil, fmt.Errorf("case %s: %w", c.ID, err)
		}
		legacySources := []string{"condition:" + LegacyCondition}
		for _, card := range legacy.Enhanced.Evidence {
			legacySources = append(legacySources, card.ID+"@"+card.Version)
		}
		inputs = append(inputs, generation.Input{CaseID: c.ID, VariantID: LegacyCondition, Question: c.Final, CaseContext: shared, SystemPrompt: legacy.Enhanced.SystemPrompt, UserPrompt: legacy.Enhanced.UserPrompt, Sources: legacySources})
		modes := []string{""}
		if withReferenceAblation {
			modes = append(modes, "off")
		}
		for _, mode := range modes {
			ctx, err := caseContext(c, mode)
			if err != nil {
				return nil, nil, err
			}
			in, err := conversation.TurnInput(c.ID, ctx, conversation.DefaultStyle)
			if err != nil {
				return nil, nil, err
			}
			in.VariantID, in.CaseContext = ConversationCondition, shared
			if mode == "off" {
				in.VariantID = NoReferenceCondition
			} else {
				checks = append(checks, checkContext(c, ctx))
			}
			in.Sources = append([]string{"condition:" + in.VariantID}, in.Sources...)
			inputs = append(inputs, in)
		}
	}
	return inputs, checks, nil
}

// sharedContext is what reviewers see for every condition: the dialogue, the
// user's stored memory and the focus points. It holds no prompt.
func sharedContext(c ConversationCase) (json.RawMessage, error) {
	dialogue := []FixtureTurn{}
	dialogue = append(dialogue, c.Turns...)
	dialogue = append(dialogue, FixtureTurn{Role: "user", Text: c.Final})
	return json.Marshal(map[string]any{"situation": c.Situation, "dialogue": dialogue, "memory": c.Memory, "linked_results": len(c.Links), "review_focus": c.ReviewFocus})
}

func caseContext(c ConversationCase, references string) (conversation.Context, error) {
	start := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	history := []conversation.Message{}
	for i, t := range c.Turns {
		if t.Role != "user" && t.Role != "assistant" {
			return conversation.Context{}, fmt.Errorf("case %s: invalid role", c.ID)
		}
		history = append(history, conversation.Message{ID: fmt.Sprintf("%s-%d", c.ID, i+1), Seq: int64(i + 1), Role: t.Role, Content: t.Text, State: "complete", CreatedAt: start.Add(time.Duration(i) * time.Minute)})
	}
	current := conversation.Message{ID: c.ID + "-final", Seq: int64(len(c.Turns) + 1), Role: "user", Content: c.Final, State: "complete", CreatedAt: start.Add(time.Duration(len(c.Turns)) * time.Minute)}
	records := []memory.Record{}
	for _, m := range c.Memory {
		if m.ID == "" || m.Value == "" || (m.Class != memory.ClassFact && m.Class != memory.ClassEpisode) {
			return conversation.Context{}, fmt.Errorf("case %s: invalid memory", c.ID)
		}
		records = append(records, memory.Record{ID: m.ID, Fact: memory.Fact{Key: m.Key, Value: m.Value, SourceEventID: "fixture:" + m.ID, ObservedAt: m.ObservedAt}, Current: true, Meta: memory.Meta{Class: m.Class, Origin: m.Origin, Subject: m.Subject, Situation: m.Situation}})
	}
	var summary *conversation.Summary
	if c.Summary != nil {
		summary = &conversation.Summary{Content: c.Summary.Text, ThroughSeq: c.Summary.Through, MemoryGeneration: 1}
	}
	ctx := conversation.Assemble(current, history, summary, 1, records, c.Links, nil, conversation.DefaultLimits)
	facts := []counsel.Fact{}
	for _, r := range ctx.Facts {
		facts = append(facts, counsel.Fact{Key: r.Key, Value: r.Value, SourceID: conversation.MemoryRef(r.ID)})
	}
	text := []string{}
	for _, m := range ctx.Recent {
		if m.Role == "user" {
			text = append(text, m.Content)
		}
	}
	ctx.Cards, ctx.Research, ctx.ReferenceUsage = counsel.ConversationReferences(strings.Join(append(text, c.Final), "\n"), facts, references)
	return ctx, nil
}

func checkContext(c ConversationCase, ctx conversation.Context) ContextCheck {
	out := ContextCheck{CaseID: c.ID, Passed: true}
	fail := func(format string, args ...any) {
		out.Passed = false
		out.Failures = append(out.Failures, fmt.Sprintf(format, args...))
	}
	values := map[string]bool{}
	for _, r := range append(append([]memory.Record{}, ctx.Facts...), ctx.Episodes...) {
		values[r.Value] = true
	}
	for _, v := range c.Expect.MemoryInclude {
		if !values[v] {
			fail("memory missing %q", v)
		}
	}
	for _, v := range c.Expect.MemoryExclude {
		if values[v] {
			fail("stale memory present %q", v)
		}
	}
	texts := map[string]bool{ctx.Current.Content: true}
	for _, m := range ctx.Recent {
		texts[m.Content] = true
	}
	for _, v := range c.Expect.MessageInclude {
		if !texts[v] {
			fail("message missing %q", v)
		}
	}
	if c.Expect.SummaryUsed != nil && (ctx.Summary != nil) != *c.Expect.SummaryUsed {
		fail("summary used=%v", ctx.Summary != nil)
	}
	if c.Expect.Links != nil && len(ctx.Links) != *c.Expect.Links {
		fail("links=%d", len(ctx.Links))
	}
	if c.Expect.ReferencesUsed != nil && ctx.ReferenceUsage.Used != *c.Expect.ReferencesUsed {
		fail("references used=%v", ctx.ReferenceUsage.Used)
	}
	return out
}
