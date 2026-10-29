package lab

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResearchSourcesRecordedOnlyForSelectedVariant(t *testing.T) {
	l, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	in := json.RawMessage(`{"question":"서운해요","topic":"express_hurt","include_research":true,"facts":[{"key":"safety","value":"no_threat","source_id":"e1"},{"key":"dialogue_consent","value":"mutual","source_id":"e2"},{"key":"contact_boundary","value":"allowed","source_id":"e3"}]}`)
	result, err := l.Compare(context.Background(), "counsel", "research-case", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Inputs) != 2 {
		t.Fatal("comparison missing")
	}
	if strings.Contains(strings.Join(result.Inputs[0].Sources, " "), "dugyeol:") {
		t.Fatal("baseline contains research")
	}
	if !strings.Contains(strings.Join(result.Inputs[1].Sources, " "), "dugyeol:dugyeol-hurt-001@2026-10-08-v1") {
		t.Fatal("card/version missing")
	}
}

func TestResearchComparisonPreservesSharedContext(t *testing.T) {
	l, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	data := json.RawMessage(`{"question":"서운해요","topic":"express_hurt","facts":[{"key":"safety","value":"no_threat","source_id":"e1"},{"key":"dialogue_consent","value":"mutual","source_id":"e2"},{"key":"contact_boundary","value":"allowed","source_id":"e3"}]}`)
	pair, err := l.ResearchComparison(context.Background(), "case-1", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair) != 2 || string(pair[0].CaseContext) != string(pair[1].CaseContext) || pair[0].SystemPrompt != pair[1].SystemPrompt || pair[0].VariantID == pair[1].VariantID {
		t.Fatal("invalid comparison")
	}
	if strings.Contains(strings.Join(pair[0].Sources, " "), "dugyeol:") || !strings.Contains(strings.Join(pair[1].Sources, " "), "dugyeol:") {
		t.Fatal("source split wrong")
	}
	if _, err = l.ResearchComparison(context.Background(), "case-2", json.RawMessage(`{"question":"서운해요"}`)); err == nil {
		t.Fatal("unknown circumstances should not produce a treatment")
	}
}
