package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
)

func TestResearchFixturePreparesPairedAndExceptionCases(t *testing.T) {
	data, err := os.ReadFile("../../fixtures/counsel-research-evaluation.json")
	if err != nil {
		t.Fatal(err)
	}
	var f ResearchFixture
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := PrepareResearch(context.Background(), l, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != len(f.Cases)*2 || len(inputs) < 10 {
		t.Fatal("missing paired cases")
	}
	for i, c := range f.Cases {
		left, right := inputs[2*i], inputs[2*i+1]
		if left.CaseID != c.ID || right.CaseID != c.ID || left.VariantID != "without_research" || right.VariantID != "with_research" || string(left.CaseContext) != string(right.CaseContext) || left.SystemPrompt != right.SystemPrompt {
			t.Fatalf("conditions differ for %s", c.ID)
		}
		if strings.Contains(strings.Join(left.Sources, " "), "dugyeol:") {
			t.Fatal("control includes a research card")
		}
		hasCard := strings.Contains(strings.Join(right.Sources, " "), "dugyeol:")
		if c.ExpectCard != nil && hasCard != *c.ExpectCard {
			t.Fatalf("card expectation mismatch for %s", c.ID)
		}
		if c.ExpectCard == nil && !hasCard {
			t.Fatalf("missing treatment for %s", c.ID)
		}
	}
	// A case whose context is mislabelled must fail rather than produce misleading results.
	f.Cases[len(f.Cases)-1].ExpectCard = nil
	if _, err = PrepareResearch(context.Background(), l, f); err == nil {
		t.Fatal("mismatched card expectation accepted")
	}
}
