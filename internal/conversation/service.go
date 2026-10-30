package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
)

var ErrNotConfigured = errors.New("conversation generation is not configured")

type Generator interface {
	Generate(context.Context, generation.Input) (generation.Result, error)
}

// LinkSource returns a snapshot of a reading owned by the same user.
type LinkSource interface {
	LinkSnapshot(context.Context, string, string) (readings.LinkSnapshot, error)
}

// Service runs one generation call per turn. After a reply it stores sourced
// memory candidates and, when enough unsummarized messages accumulate or the
// summary became stale, rebuilds the summary with one more call. Interrupted
// or ambiguous calls are recorded as unknown and never retried automatically.
type Service struct {
	Store     Store
	Memory    *memory.PostgresStore
	Generator Generator
	Links     LinkSource
	Limits    Limits
	// Style is the character block; empty uses DefaultStyle and "none" sends
	// no style, which evaluations use to show style changes wording only.
	Style string
	// References is "conditional" (default) or "off" for comparisons.
	References string
	// Inline runs the turn worker before Send returns. Tests use it; the server
	// leaves it false so a reply survives a dropped HTTP connection.
	Inline bool
	wg     sync.WaitGroup
}

type Turn struct {
	UserMessage      Message `json:"user_message"`
	AssistantMessage Message `json:"assistant_message"`
	Replayed         bool    `json:"replayed"`
}

var linkSubjects = map[string]string{"daily": "self", "compatibility": "pair", "tarot": "self"}

func (s *Service) limits() Limits {
	if s.Limits.MaxRecent == 0 {
		return DefaultLimits
	}
	return s.Limits
}

// Create starts a conversation, optionally linked to one of the user's
// readings. A saju reading needs an explicit subject because it may describe
// someone other than the user.
func (s *Service) Create(ctx context.Context, user, readingID, subject string) (Conversation, error) {
	var link *Link
	if readingID != "" {
		l, err := s.prepareLink(ctx, user, readingID, subject)
		if err != nil {
			return Conversation{}, err
		}
		link = &l
	}
	c, err := s.Store.Create(ctx, user)
	if err != nil {
		return Conversation{}, err
	}
	if link != nil {
		stored, err := s.Store.AddLink(ctx, user, c.ID, *link)
		if err != nil {
			return Conversation{}, err
		}
		c.Links = append(c.Links, stored)
	}
	return c, nil
}

func (s *Service) AddLink(ctx context.Context, user, id, readingID, subject string) (Link, error) {
	l, err := s.prepareLink(ctx, user, readingID, subject)
	if err != nil {
		return Link{}, err
	}
	return s.Store.AddLink(ctx, user, id, l)
}

func (s *Service) prepareLink(ctx context.Context, user, readingID, subject string) (Link, error) {
	if s.Links == nil || readingID == "" || len(readingID) > 128 {
		return Link{}, ErrInvalid
	}
	snap, err := s.Links.LinkSnapshot(ctx, user, readingID)
	if err != nil {
		return Link{}, ErrNotFound
	}
	switch snap.Domain {
	case "saju", "daily", "compatibility", "tarot":
	default:
		return Link{}, ErrInvalid
	}
	if subject == "" {
		subject = linkSubjects[snap.Domain]
	}
	if subject != "self" && subject != "other" && subject != "pair" {
		return Link{}, ErrInvalid
	}
	if (snap.Domain == "compatibility") != (subject == "pair") {
		return Link{}, ErrInvalid
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return Link{}, err
	}
	return Link{ReadingID: snap.ReadingID, Domain: snap.Domain, Subject: subject, Snapshot: data}, nil
}

func (s *Service) List(ctx context.Context, user string) ([]ListItem, error) {
	return s.Store.List(ctx, user)
}

func (s *Service) Get(ctx context.Context, user, id string) (Conversation, error) {
	in, err := s.Store.inputs(ctx, user, id, 1)
	if err != nil {
		return Conversation{}, err
	}
	c, err := s.Store.Get(ctx, user, id, in.Generation)
	if err != nil {
		return Conversation{}, err
	}
	return c, nil
}

func (s *Service) Delete(ctx context.Context, user, id string) error {
	return s.Store.Delete(ctx, user, id)
}

// Send stores the user message and starts the reply. Repeating the same
// request key returns the original turn without another model call.
func (s *Service) Send(ctx context.Context, user, id, key, content string) (Turn, error) {
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > 4000 || key == "" || len(key) > 128 {
		return Turn{}, ErrInvalid
	}
	if s.Generator == nil || s.Memory == nil {
		return Turn{}, ErrNotConfigured
	}
	um, am, created, err := s.Store.BeginTurn(ctx, user, id, key, content)
	if err != nil {
		return Turn{}, err
	}
	if !created {
		return Turn{UserMessage: um, AssistantMessage: am, Replayed: true}, nil
	}
	s.wg.Add(1)
	run := func() {
		defer s.wg.Done()
		s.runTurn(user, id, um, am)
	}
	if s.Inline {
		run()
		c, err := s.Get(ctx, user, id)
		if err == nil {
			for _, m := range c.Messages {
				if m.ID == am.ID {
					am = m
				}
			}
		}
	} else {
		go run()
	}
	return Turn{UserMessage: um, AssistantMessage: am}, nil
}

// Wait blocks until started workers finish. Used by tests and shutdown.
func (s *Service) Wait() { s.wg.Wait() }

// turnMeta is returned to the client with the message. It holds checks, cited
// IDs and memory references, never memory values or the prompt.
type turnMeta struct {
	Policy       string            `json:"policy"`
	Assessment   *Assessment       `json:"assessment,omitempty"`
	ResponseMode string            `json:"response_mode,omitempty"`
	References   any               `json:"reference_usage,omitempty"`
	EvidenceIDs  []string          `json:"evidence_ids"`
	Checks       []Check           `json:"checks"`
	Context      map[string]any    `json:"context"`
	Memory       []map[string]any  `json:"memory_updates"`
	Model        string            `json:"model,omitempty"`
	PromptSHA256 string            `json:"prompt_sha256,omitempty"`
	Usage        *generation.Usage `json:"usage,omitempty"`
	Error        string            `json:"error,omitempty"`
}

func contextManifest(c Context) map[string]any {
	recent := []string{}
	for _, m := range c.Recent {
		recent = append(recent, MessageRef(m.Seq))
	}
	memories := []string{}
	for _, r := range append(append([]memory.Record{}, c.Facts...), c.Episodes...) {
		memories = append(memories, MemoryRef(r.ID))
	}
	links := []string{}
	for _, l := range c.Links {
		links = append(links, LinkRef(l.ID))
	}
	m := map[string]any{"current": MessageRef(c.Current.Seq), "recent": recent, "summary_status": c.SummaryStatus, "memory": memories, "memory_not_shown": c.SkippedMemory, "links": links, "redacted": c.Redacted}
	if c.Summary != nil {
		m["summary_through"] = MessageRef(c.Summary.ThroughSeq)
	}
	if c.Omitted != nil {
		m["omitted"] = []string{MessageRef(c.Omitted.From), MessageRef(c.Omitted.To)}
	}
	return m
}

func (s *Service) runTurn(user, id string, um, am Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	meta := turnMeta{Policy: PolicyID, EvidenceIDs: []string{}, Checks: []Check{}, Memory: []map[string]any{}}
	finish := func(state, content string) {
		data, _ := json.Marshal(meta)
		saveCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = s.Store.finish(saveCtx, user, am.ID, state, content, data)
	}
	in, err := s.Store.inputs(ctx, user, id, 200)
	if err != nil {
		meta.Error = "context_unavailable"
		finish("failed", "")
		return
	}
	records, err := s.Memory.Current(user)
	if err != nil {
		meta.Error = "memory_unavailable"
		finish("failed", "")
		return
	}
	c := Assemble(um, in.History, in.Summary, in.Generation, records, in.Links, in.Suppressed, s.limits())
	c.PreviousNotes = previousNotes(in.History, um.Seq)
	c.Cards, c.Research, c.ReferenceUsage = counsel.ConversationReferences(referenceText(c), counselFacts(c.Facts), s.References)
	meta.Context = contextManifest(c)
	meta.References = c.ReferenceUsage
	style := s.Style
	if style == "" {
		style = DefaultStyle
	} else if style == "none" {
		style = ""
	}
	input, err := TurnInput(id, c, style)
	if err != nil {
		meta.Error = "context_encoding"
		finish("failed", "")
		return
	}
	result, genErr := s.Generator.Generate(ctx, input)
	meta.Model, meta.PromptSHA256 = result.ReturnedModel, result.PromptSHA256
	if result.Usage != (generation.Usage{}) {
		u := result.Usage
		meta.Usage = &u
	}
	if genErr != nil || ctx.Err() != nil {
		meta.Error = "provider_outcome_unknown"
		finish("unknown", "")
		return
	}
	if result.Status != "generated" || result.FinishReason != "stop" {
		meta.Error = "generation_incomplete"
		finish("failed", "")
		return
	}
	out, checks, err := Parse(result.Text, c)
	if err != nil {
		meta.Checks = append(meta.Checks, Check{Name: "json_schema", Status: "fail", Detail: err.Error()})
		meta.Error = "output_rejected"
		finish("failed", "")
		return
	}
	meta.Checks, meta.EvidenceIDs = checks, out.EvidenceIDs
	meta.Assessment, meta.ResponseMode = &out.Assessment, out.ResponseMode
	for i, cand := range out.MemoryCandidates {
		fact, m := cand.MemoryFact(um, i)
		update := map[string]any{"key": cand.Key, "class": cand.Class, "origin": cand.Origin}
		rec, err := s.Memory.RecordFromConversation(user, fact, m)
		switch {
		case err == nil:
			update["status"], update["memory_id"] = "stored", rec.ID
		case errors.Is(err, memory.ErrSuppressed):
			update["status"] = "suppressed_after_deletion"
		case errors.Is(err, memory.ErrConflict):
			update["status"] = "already_recorded"
		default:
			update["status"] = "rejected"
		}
		meta.Memory = append(meta.Memory, update)
	}
	finish("complete", out.Reply)
	s.refreshSummary(ctx, user, id)
}

// refreshSummary rebuilds the summary when unsummarized messages pass the
// threshold, or when a memory correction or deletion made it stale.
func (s *Service) refreshSummary(ctx context.Context, user, id string) {
	in, err := s.Store.inputs(ctx, user, id, 200)
	if err != nil {
		return
	}
	limits := s.limits()
	complete := []Message{}
	for _, m := range in.History {
		if m.State == "complete" {
			if in.Suppressed[m.ID] {
				m.Content = redactedText
			}
			complete = append(complete, m)
		}
	}
	valid := in.Summary != nil && in.Summary.MemoryGeneration == in.Generation
	var covered int64
	var previous *Summary
	if valid {
		covered, previous = in.Summary.ThroughSeq, in.Summary
	}
	pending := []Message{}
	for _, m := range complete {
		if m.Seq > covered {
			pending = append(pending, m)
		}
	}
	stale := in.Summary != nil && !valid
	if len(pending) == 0 || (!stale && len(pending) < limits.RefreshAfter) {
		return
	}
	records, err := s.Memory.Current(user)
	if err != nil {
		return
	}
	facts := []memory.Record{}
	for _, r := range records {
		if r.Class != memory.ClassEpisode {
			facts = append(facts, r)
		}
	}
	input, err := SummaryInput(id, previous, pending, facts)
	if err != nil {
		return
	}
	result, genErr := s.Generator.Generate(ctx, input)
	if genErr != nil || result.Status != "generated" || result.FinishReason != "stop" {
		return
	}
	text, err := ParseSummary(result.Text)
	if err != nil {
		return
	}
	meta, _ := json.Marshal(map[string]any{"model": result.ReturnedModel, "prompt_sha256": result.PromptSHA256, "rebuilt_from_scratch": previous == nil, "messages": len(pending)})
	saveCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_, _ = s.Store.saveSummary(saveCtx, user, id, text, pending[len(pending)-1].Seq, in.Generation, meta)
}

// previousNotes passes the last reply's assessment forward, labeled as an AI
// note, so failed methods and refused actions are not suggested again.
func previousNotes(history []Message, before int64) any {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role != "assistant" || m.State != "complete" || m.Seq >= before {
			continue
		}
		var meta struct {
			Assessment *Assessment `json:"assessment"`
		}
		if json.Unmarshal(m.Meta, &meta) != nil || meta.Assessment == nil {
			return nil
		}
		a := meta.Assessment
		return map[string]any{"from": MessageRef(m.Seq), "kind": "ai_note_not_user_fact", "want": a.Want, "capacity": a.Capacity, "tried": a.Tried, "avoid": a.Avoid, "unknowns": a.Unknowns}
	}
	return nil
}

// referenceText uses the user's own recent words for the topic rules.
func referenceText(c Context) string {
	parts := []string{}
	for _, m := range c.Recent {
		if m.Role == "user" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(append(parts, c.Current.Content), "\n")
}

func counselFacts(records []memory.Record) []counsel.Fact {
	out := []counsel.Fact{}
	for _, r := range records {
		out = append(out, counsel.Fact{Key: r.Key, Value: r.Value, SourceID: MemoryRef(r.ID), ObservedAt: r.ObservedAt})
	}
	return out
}
