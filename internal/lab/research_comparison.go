package lab

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
)

// ResearchComparison changes only research card availability. Both conditions
// receive the same BCcampus references, question, facts and serialized context.
// It prepares inputs and never invokes a model.
func (l *Lab) ResearchComparison(ctx context.Context, caseID string, input json.RawMessage) ([]generation.Input, error) {
	return l.researchComparison(ctx, caseID, input, true)
}

// ResearchComparisonForEvaluation also prepares an exception case when no card
// is eligible. expectedCard must be fixed before running the comparison; a
// mismatch fails closed rather than silently changing the intended condition.
func (l *Lab) ResearchComparisonForEvaluation(ctx context.Context, caseID string, input json.RawMessage, expectedCard bool) ([]generation.Input, error) {
	return l.researchComparison(ctx, caseID, input, expectedCard)
}

func (l *Lab) researchComparison(ctx context.Context, caseID string, input json.RawMessage, expectedCard bool) ([]generation.Input, error) {
	var req counsel.Request
	if err := Decode(input, &req); err != nil {
		return nil, err
	}
	req.IncludeResearch = false
	shared, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	control, err := l.Compare(ctx, "counsel", caseID, shared)
	if err != nil {
		return nil, err
	}
	req.IncludeResearch = true
	augmented, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	treatment, err := l.Compare(ctx, "counsel", caseID, augmented)
	if err != nil {
		return nil, err
	}
	ev, ok := treatment.Evidence.(counsel.Response)
	if !ok || (len(ev.Enhanced.ResearchEvidence) > 0) != expectedCard {
		return nil, errors.New("research card selection does not match the expected evaluation condition")
	}
	left, right := control.Inputs[1], treatment.Inputs[1]
	left.VariantID = "without_research"
	right.VariantID = "with_research"
	left.CaseContext = append(json.RawMessage(nil), shared...)
	right.CaseContext = append(json.RawMessage(nil), shared...)
	return []generation.Input{left, right}, nil
}
