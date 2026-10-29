package saju_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/saju"
)

func TestCompatibilityLabRejectsUnrecognizedInput(t *testing.T) {
	l, err := lab.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Compare(context.Background(), "compatibility", "pair-test", []byte(`{"user_id":"other-user","question":"질문"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown client identity field should fail before chart calculation: %v", err)
	}
}

func TestIntegrationCompatibilityActualOhMySaju(t *testing.T) {
	if os.Getenv("SAJU_INTEGRATION") != "1" {
		t.Skip("set SAJU_INTEGRATION=1 after scripts/setup-saju.sh")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(filepath.Join(root, "fixtures", "compatibility.json"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := lab.New(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.Compare(context.Background(), "compatibility", "synthetic-pair-integration", input)
	if err != nil {
		t.Fatal(err)
	}
	pair, ok := r.Evidence.(saju.CompatibilityResponse)
	if !ok || r.Status != "two_charts_reference_only" || pair.Status != r.Status {
		t.Fatalf("incorrect scope: %+v", r)
	}
	if len(r.Inputs) != 2 || pair.Baseline.ID != "ohmysaju_pair_only" || pair.Enhanced.ID != "ohmysaju_pair_plus_yueyuan_tiaohou" {
		t.Fatal("expected the two explicit comparison conditions")
	}
	for _, in := range r.Inputs {
		if in.CaseID != "synthetic-pair-integration" || string(in.CaseContext) != string(input) || in.UserPrompt == "" {
			t.Fatal("shared case identity/context was not retained")
		}
	}
	if strings.Contains(strings.Join(r.Inputs[0].Sources, " "), "yueyuan") || !strings.Contains(strings.Join(r.Inputs[1].Sources, " "), "yueyuan") {
		t.Fatal("source attribution does not match the condition")
	}
	var req saju.CompatibilityRequest
	if err := json.Unmarshal(input, &req); err != nil {
		t.Fatal(err)
	}
	// Repeating individual original-engine calls verifies identity routing, not
	// independent astrological truth or predictive accuracy.
	for _, person := range []struct {
		name  string
		birth saju.Birth
		chart json.RawMessage
	}{{"A", req.PersonA, pair.Charts.PersonA}, {"B", req.PersonB, pair.Charts.PersonB}} {
		want, err := saju.New(root).Compare(context.Background(), saju.Request{CaseID: "independent-routing-check", Question: req.Question, Birth: person.birth})
		if err != nil {
			t.Fatal(err)
		}
		if string(person.chart) != string(want.Chart) {
			t.Fatalf("person %s was assigned a different chart", person.name)
		}
	}
	if string(pair.Charts.PersonA) == string(pair.Charts.PersonB) {
		t.Fatal("different fixture births unexpectedly returned identical charts")
	}
}
