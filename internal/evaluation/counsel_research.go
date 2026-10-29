package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
)

// ResearchFixture is a set of synthetic cases, not an effect estimate. Facts
// added to a case are included identically in both conditions.
type ResearchFixture struct {
	Version     string         `json:"version"`
	Status      string         `json:"status"`
	Conditions  []string       `json:"conditions"`
	Metrics     []string       `json:"metrics"`
	SharedFacts []counsel.Fact `json:"shared_confirmed_facts"`
	Cases       []ResearchCase `json:"cases"`
}
type ResearchCase struct {
	ID              string         `json:"id"`
	Topic           string         `json:"topic"`
	Question        string         `json:"question"`
	AdditionalFacts []counsel.Fact `json:"additional_facts,omitempty"`
	// Omitted means the original eligible-card cases; explicit false covers
	// safety or consent exceptions where both arms must receive no card.
	ExpectCard *bool `json:"expect_card,omitempty"`
}

// PrepareResearch produces two inputs per case without calling a model. It
// rejects a changed safety expectation or duplicate case ID rather than scoring
// a case under an unintended condition.
func PrepareResearch(ctx context.Context, l *lab.Lab, fixture ResearchFixture) ([]generation.Input, error) {
	if l == nil || strings.TrimSpace(fixture.Version) == "" || len(fixture.Cases) == 0 || len(fixture.Cases) > 100 {
		return nil, errors.New("invalid research fixture")
	}
	seen := map[string]bool{}
	inputs := make([]generation.Input, 0, 2*len(fixture.Cases))
	for _, c := range fixture.Cases {
		if strings.TrimSpace(c.ID) == "" || seen[c.ID] || strings.TrimSpace(c.Question) == "" {
			return nil, errors.New("missing or duplicate research case")
		}
		seen[c.ID] = true
		facts := append([]counsel.Fact{}, fixture.SharedFacts...)
		facts = append(facts, c.AdditionalFacts...)
		input, err := json.Marshal(counsel.Request{Question: c.Question, Topic: c.Topic, Facts: facts})
		if err != nil {
			return nil, err
		}
		expected := true
		if c.ExpectCard != nil {
			expected = *c.ExpectCard
		}
		pair, err := l.ResearchComparisonForEvaluation(ctx, c.ID, input, expected)
		if err != nil {
			return nil, fmt.Errorf("case %s: %w", c.ID, err)
		}
		inputs = append(inputs, pair...)
	}
	return inputs, nil
}
