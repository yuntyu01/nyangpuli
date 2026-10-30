package lab

import (
	"context"
	"fmt"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/tarot"
)

// Policy describes the product's default interpretation sources. Baseline and
// enhanced remain comparison conditions for internal evaluation; they are not
// product tiers and the product does not combine every available source.
type Policy struct {
	ID       string   `json:"id"`
	Domain   string   `json:"domain"`
	Sources  []string `json:"sources"`
	Excluded []string `json:"excluded"`
	Missing  string   `json:"missing_policy"`
}

const (
	SajuPolicyID    = "saju-product-v1"
	CounselPolicyID = "counsel-product-v1"
)

func Policies() []Policy {
	return []Policy{
		{ID: tarot.ProductPolicyID, Domain: "tarot", Sources: tarotProductSources(), Excluded: []string{"tarotoo card meanings (baseline comparison only)", "arcanite card-pair relationships (enhanced comparison only)"}, Missing: "unwritten position text uses the same card's core meaning and is flagged; unwritten question context is omitted and flagged; nothing is generated to fill gaps"},
		{ID: SajuPolicyID, Domain: "saju,daily,compatibility", Sources: sajuSources("ohmysaju_baseline"), Excluded: []string{"yueyuan tiaohou table (experimental comparison only; not a verified interpretation engine)"}, Missing: "unsupported inputs such as unknown birth time are rejected; no value is guessed"},
		{ID: CounselPolicyID, Domain: "counsel (single question)", Sources: []string{"nyangpuli:common-counsel-instructions", "conditional reference cards recorded per answer"}, Excluded: []string{"reference cards whose topic or safety, consent and contact conditions are not confirmed"}, Missing: "references are omitted and the omission is recorded; the app's counseling uses conversations (counsel-conversation-v2)"},
	}
}

func tarotProductSources() []string {
	return []string{"arcanite@c025b97c49e564e16019cafba381375fd1697a17:core-position-question-context", "policy:" + tarot.ProductPolicyID}
}

// Product builds the single generation input for the product policy.
func (l *Lab) Product(ctx context.Context, domain, caseID string, data []byte) (generation.Input, error) {
	if domain == "tarot" {
		var req tarot.Request
		if err := Decode(data, &req); err != nil {
			return generation.Input{}, err
		}
		ev, normalized, err := l.Tarot.Product(req)
		if err != nil {
			return generation.Input{}, err
		}
		return generation.Input{CaseID: caseID, VariantID: ev.ID, Question: normalized.Question, UserPrompt: ev.Prompt, Sources: tarotProductSources(), CaseContext: append([]byte(nil), data...)}, nil
	}
	result, err := l.Compare(ctx, domain, caseID, data)
	if err != nil {
		return generation.Input{}, err
	}
	if len(result.Inputs) != 2 {
		return generation.Input{}, fmt.Errorf("expected two research variants")
	}
	switch domain {
	case "saju", "daily", "compatibility":
		in := result.Inputs[0]
		in.VariantID = SajuPolicyID
		in.Sources = append(in.Sources, "policy:"+SajuPolicyID)
		return in, nil
	case "counsel":
		in := result.Inputs[1]
		in.VariantID = CounselPolicyID
		in.Sources = append(in.Sources, "policy:"+CounselPolicyID)
		return in, nil
	}
	return generation.Input{}, fmt.Errorf("no product policy for %q", domain)
}
