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
	if !ok || len(ev.Enhanced.ResearchEvidence) == 0 {
		return nil, errors.New("no eligible research card; confirm context or select another case")
	}
	left, right := control.Inputs[1], treatment.Inputs[1]
	left.VariantID = "without_research"
	right.VariantID = "with_research"
	left.CaseContext = append(json.RawMessage(nil), shared...)
	right.CaseContext = append(json.RawMessage(nil), shared...)
	return []generation.Input{left, right}, nil
}
