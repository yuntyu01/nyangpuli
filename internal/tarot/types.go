// Package tarot builds reproducible evidence bundles. It does not predict events.
package tarot

type Draw struct {
	CardID   int  `json:"card_id"`
	Reversed bool `json:"reversed"`
}

// Cards are ordered by the selected spread's position list. IDs are Tarotoo IDs 0..77.
type Request struct {
	Question     string `json:"question"`
	QuestionType string `json:"question_type"`
	Spread       string `json:"spread"`
	Cards        []Draw `json:"cards"`
}

type Position struct {
	Name        string `json:"name"`
	Description string `json:"short_description"`
	Mapping     string `json:"rag_mapping"`
}

type Spread struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Positions []Position `json:"positions"`
}

type CardInfo struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	ArcaniteID string `json:"arcanite_id"`
}

type CardEvidence struct {
	CardID                 int    `json:"card_id"`
	Name                   string `json:"name"`
	Orientation            string `json:"orientation"`
	Position               string `json:"position"`
	PositionMapping        string `json:"position_mapping"`
	Meaning                string `json:"meaning"`
	QuestionContext        string `json:"question_context,omitempty"`
	PositionInterpretation string `json:"position_interpretation,omitempty"`
	PositionFallback       bool   `json:"position_fallback"`
	Source                 string `json:"source"`
}

// Relationship is directional as defined in the source. Its condition is card ID
// only; orientation and position are NOT additional matching conditions.
type Relationship struct {
	From           string `json:"from"`
	To             string `json:"to"`
	Kind           string `json:"kind"`
	Interpretation string `json:"interpretation"`
	ConditionScope string `json:"condition_scope"`
}

type Evidence struct {
	ID            string         `json:"id"`
	Cards         []CardEvidence `json:"cards"`
	Relationships []Relationship `json:"relationships"`
	Warnings      []string       `json:"warnings"`
	Prompt        string         `json:"prompt"`
}

type Coverage struct {
	TarotooCards               int `json:"tarotoo_cards"`
	ArcaniteCards              int `json:"arcanite_cards"`
	Spreads                    int `json:"spreads"`
	RelationshipEntries        int `json:"relationship_entries"`
	PlaceholderRelationships   int `json:"placeholder_relationships"`
	UnknownRelationshipTargets int `json:"unknown_relationship_targets"`
}

type Response struct {
	Mode          string            `json:"mode"`
	Request       Request           `json:"request"`
	Baseline      Evidence          `json:"baseline"`
	Enhanced      Evidence          `json:"enhanced"`
	Coverage      Coverage          `json:"coverage"`
	Limitations   []string          `json:"limitations"`
	SourceCommits map[string]string `json:"source_commits"`
}
