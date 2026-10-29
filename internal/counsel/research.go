package counsel

import (
	_ "embed"
	"encoding/json"
)

// ResearchCard keeps actions and exceptions together; it is not an embedding of
// the workbook. Source material is an editorial draft, not evidence of efficacy.
type ResearchCard struct {
	ID                string   `json:"id"`
	Version           string   `json:"version"`
	Topic             string   `json:"topic"`
	Situation         string   `json:"situation"`
	Empathy           string   `json:"empathy"`
	Interpretation    string   `json:"interpretation"`
	Questions         []string `json:"questions"`
	Action            string   `json:"action"`
	Exceptions        []string `json:"exceptions"`
	Status            string   `json:"status"`
	Conditions        []string `json:"conditions"`
	VideoURL          string   `json:"video_url"`
	Timestamps        []string `json:"timestamps"`
	OriginalReference string   `json:"original_reference"`
	EditorialAddition string   `json:"editorial_addition"`
	Rights            string   `json:"rights"`
	SourceGroup       string   `json:"source_group"`
	Split             string   `json:"split"`
}

//go:embed research_cards.json
var researchCardsJSON []byte

func researchCards() []ResearchCard {
	var cards []ResearchCard
	if err := json.Unmarshal(researchCardsJSON, &cards); err != nil {
		panic(err)
	}
	return cards
}

// SelectResearch requires explicit, sourced facts confirming safety and consent.
// Unknown or contradictory facts disable selection. Evaluation groups and all
// their derived cards must never enter the retrieval candidates.
func SelectResearch(topic string, facts []Fact, excludedGroups map[string]bool) []ResearchCard {
	safety, consent, boundary := false, false, false
	unsafe, denied, restricted := false, false, false
	for _, f := range facts {
		switch f.Key {
		case "safety":
			if f.Value == "no_threat" {
				safety = true
			} else {
				unsafe = true
			}
		case "dialogue_consent":
			if f.Value == "mutual" {
				consent = true
			} else {
				denied = true
			}
		case "contact_boundary":
			if f.Value == "allowed" {
				boundary = true
			} else {
				restricted = true
			}
		}
	}
	if !safety || !consent || !boundary || unsafe || denied || restricted {
		return nil
	}
	result := []ResearchCard{}
	for _, c := range researchCards() {
		if c.Topic != topic || c.Split != "retrieval" || excludedGroups[c.SourceGroup] || (c.Status != "accepted" && c.Status != "conditional") {
			continue
		}
		result = append(result, c)
	}
	return result
}

// Reserve the entire source group, including derived cards and re-edits, for
// evaluation. Expand this reviewed mapping before adding any further sources.
func evaluationSourceGroups() map[string]bool {
	return map[string]bool{"_aDIzBDUXGY": true}
}
