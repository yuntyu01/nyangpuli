// Package conversation runs continuous counseling turns over PostgreSQL. Each
// turn separates the current message, recent raw messages, an older summary,
// personal memory and explicitly linked readings. It does not diagnose users or
// predict relationship outcomes.
package conversation

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

type Message struct {
	ID        string          `json:"id"`
	Seq       int64           `json:"seq"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	State     string          `json:"state"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type Summary struct {
	Content          string    `json:"content"`
	ThroughSeq       int64     `json:"through_seq"`
	MemoryGeneration int64     `json:"memory_generation"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Link struct {
	ID        string          `json:"id"`
	ReadingID string          `json:"reading_id"`
	Domain    string          `json:"domain"`
	Subject   string          `json:"subject"`
	Snapshot  json.RawMessage `json:"snapshot"`
	CreatedAt time.Time       `json:"created_at"`
}

type Limits struct {
	MinRecent    int // raw messages always given, even when a summary covers them
	MaxRecent    int // raw messages at most; older unsummarized ones are reported as omitted
	MaxFacts     int
	MaxEpisodes  int
	RefreshAfter int // unsummarized complete messages that trigger a summary rebuild
}

var DefaultLimits = Limits{MinRecent: 6, MaxRecent: 24, MaxFacts: 16, MaxEpisodes: 12, RefreshAfter: 12}

// SeqRange is an inclusive range of message sequence numbers.
type SeqRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// Context is the frozen input of one turn. Every item has an evidence ID that
// the model may cite; IDs it was not given are rejected by Validate.
type Context struct {
	Current       Message
	Recent        []Message
	Summary       *Summary
	SummaryStatus string
	Omitted       *SeqRange
	Facts         []memory.Record
	Episodes      []memory.Record
	SkippedMemory int
	Links         []Link
	Redacted      []string
	// Set by the service after Assemble.
	PreviousNotes  any
	Cards          []counsel.Card
	Research       []counsel.ResearchCard
	ReferenceUsage counsel.ReferenceUsage
}

const redactedText = "[사용자가 이 메시지에서 나온 기억을 삭제해 내용을 생략함]"

func MessageRef(seq int64) string { return fmt.Sprintf("msg:%d", seq) }
func MemoryRef(id string) string  { return "mem:" + id }
func LinkRef(id string) string    { return "link:" + id }

// Assemble chooses what the model sees. The current message and every complete
// message after the summary's coverage point are included first, so a summary
// that lags behind never drops the latest messages. A summary built before a
// memory correction or deletion is ignored until rebuilt.
func Assemble(current Message, history []Message, summary *Summary, memoryGeneration int64, records []memory.Record, links []Link, suppressed map[string]bool, limits Limits) Context {
	c := Context{Current: current, SummaryStatus: "none", Links: append([]Link(nil), links...), Redacted: []string{}}
	if suppressed[current.ID] {
		c.Current.Content = redactedText
		c.Redacted = append(c.Redacted, MessageRef(current.Seq))
	}
	usable := []Message{}
	for _, m := range history {
		if m.Seq >= current.Seq || m.State != "complete" || (m.Role != "user" && m.Role != "assistant") {
			continue
		}
		if suppressed[m.ID] {
			m.Content = redactedText
			c.Redacted = append(c.Redacted, MessageRef(m.Seq))
		}
		usable = append(usable, m)
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].Seq < usable[j].Seq })
	var covered int64
	if summary != nil {
		if summary.MemoryGeneration == memoryGeneration && summary.ThroughSeq < current.Seq {
			s := *summary
			c.Summary = &s
			c.SummaryStatus = "current"
			covered = summary.ThroughSeq
		} else {
			c.SummaryStatus = "stale_ignored"
		}
	}
	start := len(usable)
	for start > 0 && usable[start-1].Seq > covered {
		start--
	}
	if len(usable)-start < limits.MinRecent {
		start = len(usable) - limits.MinRecent
		if start < 0 {
			start = 0
		}
	}
	if len(usable)-start > limits.MaxRecent {
		start = len(usable) - limits.MaxRecent
	}
	c.Recent = usable[start:]
	if len(c.Recent) > 0 {
		var firstUncovered int64 = -1
		for _, m := range usable[:start] {
			if m.Seq > covered {
				firstUncovered = m.Seq
				break
			}
		}
		if firstUncovered > 0 {
			c.Omitted = &SeqRange{From: firstUncovered, To: c.Recent[0].Seq - 1}
		}
	}
	c.Facts, c.Episodes, c.SkippedMemory = selectMemory(records, limits)
	return c
}

// Boundary and relationship state first, then the most recent statements.
// Only confirmed memory reaches the model; AI inferences are not in records.
func selectMemory(records []memory.Record, limits Limits) ([]memory.Record, []memory.Record, int) {
	facts, episodes := []memory.Record{}, []memory.Record{}
	for _, r := range records {
		if !r.Current {
			continue
		}
		if r.Class == memory.ClassEpisode {
			episodes = append(episodes, r)
		} else {
			facts = append(facts, r)
		}
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
	sort.SliceStable(facts, func(i, j int) bool {
		if priority(facts[i].Key) != priority(facts[j].Key) {
			return priority(facts[i].Key) > priority(facts[j].Key)
		}
		return facts[i].ObservedAt.After(facts[j].ObservedAt)
	})
	sort.SliceStable(episodes, func(i, j int) bool { return episodes[i].ObservedAt.After(episodes[j].ObservedAt) })
	skipped := 0
	if len(facts) > limits.MaxFacts {
		skipped += len(facts) - limits.MaxFacts
		facts = facts[:limits.MaxFacts]
	}
	if len(episodes) > limits.MaxEpisodes {
		skipped += len(episodes) - limits.MaxEpisodes
		episodes = episodes[:limits.MaxEpisodes]
	}
	return facts, episodes, skipped
}

// EvidenceIDs lists every ID the model may cite for this turn.
func (c Context) EvidenceIDs() map[string]bool {
	ids := map[string]bool{MessageRef(c.Current.Seq): true}
	for _, m := range c.Recent {
		ids[MessageRef(m.Seq)] = true
	}
	if c.Summary != nil {
		ids[fmt.Sprintf("sum:%d", c.Summary.ThroughSeq)] = true
	}
	for _, r := range c.Facts {
		ids[MemoryRef(r.ID)] = true
	}
	for _, r := range c.Episodes {
		ids[MemoryRef(r.ID)] = true
	}
	for _, l := range c.Links {
		ids[LinkRef(l.ID)] = true
	}
	for _, card := range c.Cards {
		ids["ref:"+card.ID] = true
	}
	for _, card := range c.Research {
		ids["ref:"+card.ID] = true
	}
	return ids
}
