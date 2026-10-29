package counsel

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExplicitTopicAndControlledConditions(t *testing.T) {
	req := Request{Question: "약속을 바꾸고 알려주지 않아서 서운해", Topic: TopicHurt,
		Facts: []Fact{{Key: "last_event", Value: "약속 변경을 나중에 알게 됨", SourceID: "event-001", ObservedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}}}
	got, err := Compare(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "prepared_unscored" || got.RetrievalMethod != "explicit_topic" {
		t.Fatalf("unexpected status: %+v", got)
	}
	if got.Baseline.SystemPrompt != got.Enhanced.SystemPrompt {
		t.Fatal("conditions changed system instructions")
	}
	baseData := strings.Split(got.Baseline.UserPrompt, "\nREFERENCE_CARDS\n")[0]
	enhancedData := strings.Split(got.Enhanced.UserPrompt, "\nREFERENCE_CARDS\n")[0]
	if baseData != enhancedData {
		t.Fatal("conditions changed user input")
	}
	if len(got.Baseline.Evidence) != 0 || len(got.Enhanced.Evidence) != 1 {
		t.Fatal("unexpected retrieval count")
	}
	if got.Enhanced.Evidence[0].ID != "bccampus-hurt-001" {
		t.Fatal("wrong topic card")
	}
	if !strings.Contains(baseData, "event-001") || !strings.Contains(baseData, "2026-10-20") {
		t.Fatal("lost evidence provenance")
	}
	// No deterministic answer or score should be mislabeled as an evaluated model answer.
	if strings.Contains(got.Status, "passed") {
		t.Fatal("comparison pretends quality was measured")
	}
}

func TestNoContactBoundaryRemovesContactPlans(t *testing.T) {
	requests := []Request{
		{Question: "다시 연락하고 싶어", Topic: TopicContact, Facts: []Fact{{Key: "contact_boundary", Value: "no_contact", SourceID: "event-1"}}},
		{Question: "상대가 날 차단했는데 다른 계정으로 연락할까?", Topic: TopicContact},
		{Question: "연락하지 말라고 했는데 찾아가면 되나?", Topic: TopicConflict},
		{Question: "연락해도 될까?", Topic: TopicContact, Facts: []Fact{{Key: "contact_boundary", Value: "blocked", SourceID: "e1"}, {Key: "contact_boundary", Value: "allowed", SourceID: "e2"}}},
	}
	for _, req := range requests {
		got, err := Compare(req)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Baseline.UserPrompt, `"contact_boundary_signal": "restricted"`) {
			t.Fatalf("missing restrictive signal for %q", req.Question)
		}
		for _, variant := range []Variant{got.Baseline, got.Enhanced} {
			plans := strings.Join(variant.Plan, " ")
			if !strings.Contains(plans, "경계를 존중") || strings.Contains(plans, "시간대") || strings.Contains(plans, "기준 하나를 제안") {
				t.Fatalf("unsafe plan: %s", plans)
			}
		}
	}
}

func TestNegatedBoundaryPhraseDoesNotBecomeFact(t *testing.T) {
	got, err := Compare(Request{Question: "연락하지 말라고 한 적은 없어. 답장이 느려서 연락 기준을 정하고 싶어."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Baseline.UserPrompt, `"contact_boundary_signal": "restricted"`) {
		t.Fatal("negation became a confirmed boundary")
	}
	if !strings.Contains(got.Baseline.UserPrompt, "unknown_or_not_restricted") {
		t.Fatal("unknown status was treated as permission")
	}
}

func TestNoMatchAndKeywordFallback(t *testing.T) {
	got, err := Compare(Request{Question: "진로를 바꿀까?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Topic != "unclassified" || got.RetrievalMethod != "no_match" || len(got.Enhanced.Evidence) != 0 {
		t.Fatal("unrelated reference was injected")
	}
	got, err = Compare(Request{Question: "상대가 자꾸 답장을 늦게 해"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Topic != TopicContact || got.RetrievalMethod != "bounded_keyword" {
		t.Fatal("keyword route is not reproducible")
	}
	if len(got.ClarifyingQuestions) == 0 {
		t.Fatal("unknown context needs questions")
	}
}

func TestInvalidRequest(t *testing.T) {
	for _, req := range []Request{
		{},
		{Question: strings.Repeat("가", 4001)},
		{Question: "고민", Topic: "medical"},
		{Question: "고민", Facts: []Fact{{Key: "claim", Value: "설명"}}},
		{Question: "고민", Facts: make([]Fact, 51)},
	} {
		if _, err := Compare(req); err == nil {
			t.Fatalf("expected rejection for %+v", req)
		}
	}
}

func TestSourceAttributionAndImmutableCatalog(t *testing.T) {
	for _, card := range Cards() {
		if card.Source.License != "CC-BY-4.0" || card.Source.URL == "" || card.Source.Author == "" || card.Source.Copyright == "" || card.Source.Adaptation == "" {
			t.Fatalf("missing attribution: %+v", card)
		}
	}
	one, err := Compare(Request{Question: "서운해", Topic: TopicHurt})
	if err != nil {
		t.Fatal(err)
	}
	one.Enhanced.Evidence[0].Principles[0] = "tampered"
	two, err := Compare(Request{Question: "서운해", Topic: TopicHurt})
	if err != nil {
		t.Fatal(err)
	}
	if two.Enhanced.Evidence[0].Principles[0] == "tampered" {
		t.Fatal("caller changed the shared catalog")
	}
}

func TestFixtures(t *testing.T) {
	data, err := os.ReadFile("../../fixtures/counsel-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID            string  `json:"id"`
		Request       Request `json:"request"`
		ExpectedTopic string  `json:"expected_topic"`
		Restricted    bool    `json:"restricted"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			got, err := Compare(c.Request)
			if err != nil {
				t.Fatal(err)
			}
			if got.Topic != c.ExpectedTopic {
				t.Fatalf("got topic %s", got.Topic)
			}
			actualRestricted := strings.Contains(got.Baseline.UserPrompt, `"contact_boundary_signal": "restricted"`)
			if actualRestricted != c.Restricted {
				t.Fatalf("boundary mismatch: %s", got.Baseline.UserPrompt)
			}
		})
	}
}
