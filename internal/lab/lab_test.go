package lab

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestProvenanceMatchesCondition(t *testing.T) {
	l, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.Compare(context.Background(), "counsel", "c1", []byte(`{"question":"서운해","topic":"express_hurt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(r.Inputs[0].Sources, " "), "BCcampus") {
		t.Fatal("baseline falsely cites BCcampus")
	}
	if !strings.Contains(strings.Join(r.Inputs[1].Sources, " "), "BCcampus") {
		t.Fatal("missing enhanced source")
	}
	var facts map[string]any
	if err := json.Unmarshal(r.Inputs[0].CaseContext, &facts); err != nil {
		t.Fatal(err)
	}
	if facts["topic"] != "express_hurt" {
		t.Fatal("shared review context missing")
	}
}
