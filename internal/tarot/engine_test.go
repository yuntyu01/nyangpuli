package tarot

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/third_party/arcanite"
	"github.com/yuntyu01/nyangpuli-lab/third_party/tarotoo"
)

func engineForTest(t *testing.T) *Engine {
	t.Helper()
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPinnedDataAndCoverage(t *testing.T) {
	const want = "0298c51ed61374e440612c3454bd5e8e1fb25abe959e59d6442f6cd7a61beb41"
	if got := fmt.Sprintf("%x", sha256.Sum256(tarotoo.Cards)); got != want {
		t.Fatalf("Tarotoo source drift: %s", got)
	}
	e := engineForTest(t)
	c := e.Coverage()
	if c.TarotooCards != 78 || c.ArcaniteCards != 78 || c.Spreads != 11 {
		t.Fatalf("incomplete corpus: %+v", c)
	}
	// These counts are a reproducible source audit, not expert quality scores.
	if c.RelationshipEntries != 1379 || c.PlaceholderRelationships != 676 || c.UnknownRelationshipTargets != 23 {
		t.Fatalf("source census changed: %+v", c)
	}
	if e.Cards()[10].ArcaniteID != "wheel_of_fortune" || e.Cards()[77].ArcaniteID != "king_of_pentacles" {
		t.Fatal("cross-source ID mapping")
	}
}

func TestArcaniteImportedFileHashes(t *testing.T) {
	b, err := arcanite.Files.ReadFile("upstream-files.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest []struct {
		Path   string `json:"local_path"`
		SHA256 string `json:"sha256"`
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range manifest {
		path := strings.TrimPrefix(entry.Path, "third_party/arcanite/")
		if path == "LICENSE" {
			continue
		}
		content, err := arcanite.Files.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(content)) != entry.SHA256 {
			t.Fatalf("source drift: %s", path)
		}
		checked++
	}
	if checked != 79 {
		t.Fatalf("expected 78 cards + spreads, got %d", checked)
	}
}

func TestOrientationAndQuestionContext(t *testing.T) {
	e := engineForTest(t)
	request := Request{Question: "이 관계에서 무엇을 돌아볼까?", QuestionType: "love", Spread: "single-focus", Cards: []Draw{{CardID: 13}}}
	u, err := e.Compare(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Cards[0].Reversed = true
	r, err := e.Compare(request)
	if err != nil {
		t.Fatal(err)
	}
	if u.Request.Cards[0].Reversed {
		t.Fatal("result aliases caller cards")
	}
	if !strings.Contains(r.Enhanced.Cards[0].Meaning, "Stagnation") || r.Enhanced.Cards[0].Orientation != "reversed" {
		t.Fatal("reversed Death meaning not selected")
	}
	if u.Baseline.Cards[0].Meaning == r.Baseline.Cards[0].Meaning || u.Enhanced.Cards[0].QuestionContext == r.Enhanced.Cards[0].QuestionContext {
		t.Fatal("orientation failed to change meaning/context")
	}
	if r.Mode != "evidence_only" || !strings.Contains(r.Enhanced.Prompt, "실제 사건") {
		t.Fatal("misleading execution mode or missing source framing")
	}
}

func TestPositionsResolveIndependently(t *testing.T) {
	e := engineForTest(t)
	r, err := e.Compare(Request{Question: "어떤 변화를 돌아볼까?", Spread: "past-present-future", Cards: []Draw{{CardID: 13}, {CardID: 16}, {CardID: 21}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Enhanced.Cards[0].Position != "Past" || !strings.Contains(r.Enhanced.Cards[0].PositionInterpretation, "in your past") {
		t.Fatal("past-position meaning not selected")
	}
	if r.Enhanced.Cards[2].Position != "Future" {
		t.Fatal("spread order lost")
	}
	a := e.arc[13]
	text, fallback := positionText(a, "question_contexts.spiritual", true)
	if fallback || text != a.Contexts["spiritual"].Reversed {
		t.Fatal("question_contexts mapping was incorrectly searched inside position_interpretations")
	}
	text, fallback = positionText(a, "missing.position", true)
	if !fallback || text != a.Core["reversed"].Essence {
		t.Fatal("missing position must explicitly fall back")
	}
}

func TestMissingRelationsAreSkippedAndOneWayRelationsSurvive(t *testing.T) {
	e := engineForTest(t)
	r, err := e.Compare(Request{Question: "변화를 어떻게 받아들일까?", Spread: "past-present-future", Cards: []Draw{{CardID: 13}, {CardID: 16}, {CardID: 21}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Enhanced.Relationships) == 0 {
		t.Fatal("Death/Tower has a usable defined relationship")
	}
	for _, rel := range r.Enhanced.Relationships {
		if missingText(rel.Interpretation) || rel.ConditionScope != "card_ids_only" {
			t.Fatal("placeholder or false scope leaked")
		}
	}
	if !strings.Contains(strings.Join(r.Enhanced.Warnings, " "), "Skipped missing relationship") {
		t.Fatal("missing relationships silently ignored")
	}
	// A->B is directional and must survive even when A sorts after B.
	f := &Engine{arc: map[int]arcCard{0: {ID: "z", Relationships: map[string]map[string]relationText{"clarifies": {"a": {Interpretation: "One-way fixture."}}}}, 1: {ID: "a"}}}
	x, _ := f.relationships([]Draw{{CardID: 0}, {CardID: 1}}, nil)
	y, _ := f.relationships([]Draw{{CardID: 1}, {CardID: 0}}, nil)
	if len(x) != 1 || x[0].From != "z" || !reflect.DeepEqual(x, y) {
		t.Fatal("one-way relation discarded by alphabetical deduplication")
	}
}

func TestAllCardsBothOrientationsAndSpreads(t *testing.T) {
	e := engineForTest(t)
	for _, s := range e.Spreads() {
		for id := 0; id < 78; id++ {
			for _, rev := range []bool{false, true} {
				cards := make([]Draw, len(s.Positions))
				for i := range cards {
					cards[i] = Draw{CardID: (id + i) % 78, Reversed: rev}
				}
				r, err := e.Compare(Request{Question: "내가 확인할 점은?", QuestionType: "love", Spread: s.ID, Cards: cards})
				if err != nil {
					t.Fatalf("%s id=%d reversed=%v: %v", s.ID, id, rev, err)
				}
				if len(r.Enhanced.Cards) != len(s.Positions) {
					t.Fatal("lost position")
				}
				for _, c := range r.Enhanced.Cards {
					if missingText(c.Meaning) || missingText(c.PositionInterpretation) {
						t.Fatalf("missing material: %+v", c)
					}
				}
			}
		}
	}
}

func TestInvalidRequests(t *testing.T) {
	e := engineForTest(t)
	for name, r := range map[string]Request{
		"blank":              {Question: "  ", Cards: []Draw{{CardID: 0}}},
		"too long":           {Question: strings.Repeat("냥", 4001), Cards: []Draw{{CardID: 0}}},
		"bad card":           {Question: "질문", Cards: []Draw{{CardID: -1}}},
		"bad spread":         {Question: "질문", Spread: "invented", Cards: []Draw{{CardID: 0}}},
		"count":              {Question: "질문", Spread: "relationship-spread", Cards: []Draw{{CardID: 0}}},
		"duplicate":          {Question: "질문", Spread: "past-present-future", Cards: []Draw{{CardID: 1}, {CardID: 1}, {CardID: 2}}},
		"unsupported domain": {Question: "질문", QuestionType: "health", Cards: []Draw{{CardID: 0}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := e.Compare(r); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestDeterministicAndReadOnlyCatalog(t *testing.T) {
	e := engineForTest(t)
	r := Request{Question: "상대에게 연락하기 전에 무엇을 확인할까?", QuestionType: "love", Spread: "situation-action-outcome", Cards: []Draw{{CardID: 6}, {CardID: 13, Reversed: true}, {CardID: 21}}}
	a, err := e.Compare(r)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		b, err := e.Compare(r)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatal("nondeterministic map iteration leaked")
		}
	}
	s := e.Spreads()
	s[0].Positions[0].Name = "changed"
	if e.Spreads()[0].Positions[0].Name == "changed" {
		t.Fatal("catalog mutates internal state")
	}
}
