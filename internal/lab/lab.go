// Package lab routes the same fixed input through baseline and enhanced variants.
package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/yuntyu01/nyangpuli-lab/internal/counsel"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/saju"
	"github.com/yuntyu01/nyangpuli-lab/internal/tarot"
)

type Lab struct {
	Root  string
	Tarot *tarot.Engine
}
type MemorySelection struct {
	Available    int      `json:"available"`
	Selected     int      `json:"selected"`
	ExcludedKeys []string `json:"excluded_keys,omitempty"`
	Policy       string   `json:"policy"`
}
type Result struct {
	MemorySelection *MemorySelection   `json:"memory_selection,omitempty"`
	Domain          string             `json:"domain"`
	Status          string             `json:"status"`
	Evidence        any                `json:"evidence"`
	Inputs          []generation.Input `json:"generation_inputs"`
}

func New(root string) (*Lab, error) {
	t, err := tarot.New()
	if err != nil {
		return nil, err
	}
	return &Lab{Root: root, Tarot: t}, nil
}

// Decode rejects unknown fields and trailing JSON, including client-supplied user IDs.
func Decode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("exactly one JSON object is required")
	}
	return nil
}
func (l *Lab) Compare(ctx context.Context, domain, caseID string, data []byte) (Result, error) {
	r := Result{Domain: domain, Status: "evidence_only_not_quality_evaluation"}
	if caseID == "" {
		caseID = "local-case"
	}
	switch domain {
	case "tarot":
		var req tarot.Request
		if err := Decode(data, &req); err != nil {
			return r, err
		}
		res, err := l.Tarot.Compare(req)
		if err != nil {
			return r, err
		}
		r.Evidence = res
		for _, v := range []tarot.Evidence{res.Baseline, res.Enhanced} {
			r.Inputs = append(r.Inputs, generation.Input{CaseID: caseID, VariantID: v.ID, Question: req.Question, UserPrompt: v.Prompt, Sources: tarotSources(v.ID)})
		}
	case "saju", "daily":
		var req saju.Request
		if err := Decode(data, &req); err != nil {
			return r, err
		}
		if domain == "daily" && req.Date == nil {
			return r, fmt.Errorf("daily requires an explicit date")
		}
		req.CaseID = caseID
		res, err := saju.New(l.Root).Compare(ctx, req)
		if err != nil {
			return r, err
		}
		r.Evidence = res
		for _, v := range []saju.Variant{res.Baseline, res.Enhanced} {
			r.Inputs = append(r.Inputs, generation.Input{CaseID: caseID, VariantID: v.ID, Question: req.Question, UserPrompt: v.Prompt, Sources: sajuSources(v.ID)})
		}
	case "compatibility":
		var req saju.CompatibilityRequest
		if err := Decode(data, &req); err != nil {
			return r, err
		}
		req.CaseID = caseID
		res, err := saju.New(l.Root).CompareCompatibility(ctx, req)
		if err != nil {
			return r, err
		}
		r.Status = res.Status
		r.Evidence = res
		for _, v := range []saju.Variant{res.Baseline, res.Enhanced} {
			r.Inputs = append(r.Inputs, generation.Input{CaseID: caseID, VariantID: v.ID, Question: req.Question, UserPrompt: v.Prompt, Sources: sajuSources(v.ID)})
		}
	case "counsel":
		var req counsel.Request
		if err := Decode(data, &req); err != nil {
			return r, err
		}
		res, err := counsel.Compare(req)
		if err != nil {
			return r, err
		}
		r.Evidence = res
		for _, v := range []counsel.Variant{res.Baseline, res.Enhanced} {
			sources := counselSources(v.ID)
			for _, card := range v.ResearchEvidence {
				sources = append(sources, "dugyeol:"+card.ID+"@"+card.Version)
			}
			r.Inputs = append(r.Inputs, generation.Input{CaseID: caseID, VariantID: v.ID, Question: req.Question, SystemPrompt: v.SystemPrompt, UserPrompt: v.UserPrompt, Sources: sources})
		}
	default:
		return r, fmt.Errorf("unknown domain %q (tarot, saju, daily, compatibility, counsel)", domain)
	}
	for i := range r.Inputs {
		r.Inputs[i].CaseContext = append(json.RawMessage(nil), data...)
	}
	return r, nil
}

func tarotSources(id string) []string {
	if id == "tarotoo_baseline" {
		return []string{"tarotoo@83202267973950eb5bf034f144b9405db9cb1e7e:card-meanings", "arcanite@c025b97c49e564e16019cafba381375fd1697a17:spread-positions-only"}
	}
	return []string{"arcanite@c025b97c49e564e16019cafba381375fd1697a17:card-meanings-and-spreads"}
}
func sajuSources(id string) []string {
	sources := []string{"ohmysaju@a8c0cf64adfde8fb1ce253112529edd8e90cc94b:calendar"}
	if id == "ohmysaju_plus_yueyuan_tiaohou" || id == "ohmysaju_pair_plus_yueyuan_tiaohou" {
		sources = append(sources, "yueyuan@723b73cb4f21eda2b8e75aeadba680c198c72dc6:experimental-tiaohou-table")
	}
	return sources
}
func counselSources(id string) []string {
	if id == "bccampus_cards" {
		return []string{"BCcampus Communication, Healthy Relationships, and Consent (2024); CC-BY-4.0; Korean adaptation"}
	}
	return []string{"nyangpuli:common-counsel-instructions; no external reference cards"}
}
