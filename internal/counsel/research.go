package counsel

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"regexp"
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

// Each entry is an explicitly reviewed video-to-group assignment. A group may
// have several known originals, re-edits or derivatives; every known member
// must be added with the same split. Unknown relationships are not inferred.
// Registry membership does not establish that the human review is complete:
// potential aliases and derived comments still require independent review.
type researchSource struct {
	group string
	split string
}

var reviewedResearchSources = map[string]researchSource{
	"ojyMbYPzuTg": {group: "ojyMbYPzuTg", split: "retrieval"},
	"wYDE9EGcUxU": {group: "wYDE9EGcUxU", split: "retrieval"},
	"Lyd0Ag8DY7E": {group: "Lyd0Ag8DY7E", split: "retrieval"},
	"_aDIzBDUXGY": {group: "_aDIzBDUXGY", split: "evaluation"},
}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// researchCandidate fails closed: unregistered videos, inconsistent claims,
// and any group containing an evaluation member cannot enter retrieval.
func researchCandidate(c ResearchCard, excluded map[string]bool, sources map[string]researchSource) bool {
	u, err := url.Parse(c.VideoURL)
	if err != nil || u.Scheme != "https" || (u.Host != "www.youtube.com" && u.Host != "youtube.com") || u.User != nil || u.Path != "/watch" || u.Fragment != "" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["v"]) != 1 || !videoIDPattern.MatchString(query.Get("v")) {
		return false
	}
	source, ok := sources[query.Get("v")]
	if !ok || source.group == "" || source.split != "retrieval" || c.SourceGroup != source.group || c.Split != source.split || excluded[source.group] {
		return false
	}
	// Fail closed on inconsistent group assignments, even when the individual
	// retrieval entry claims a different split from another reviewed member.
	for _, member := range sources {
		if member.group == source.group && member.split != "retrieval" {
			return false
		}
	}
	return true
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
		if c.Topic != topic || !researchCandidate(c, excludedGroups, reviewedResearchSources) || (c.Status != "accepted" && c.Status != "conditional") {
			continue
		}
		result = append(result, c)
	}
	return result
}

// Reserve the entire source group, including derived cards and re-edits, for
// evaluation. Expand this reviewed mapping before adding any further sources.
func evaluationSourceGroups() map[string]bool {
	groups := make(map[string]bool)
	for _, source := range reviewedResearchSources {
		if source.split == "evaluation" {
			groups[source.group] = true
		}
	}
	return groups
}
