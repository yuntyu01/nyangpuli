package counsel

// ReferenceUsage records whether external reference cards were given to a
// conversation turn and why, so answers with and without them can be compared.
type ReferenceUsage struct {
	Mode     string   `json:"mode"` // conditional or off
	Used     bool     `json:"used"`
	Topic    string   `json:"topic"`
	Method   string   `json:"method"`
	Boundary string   `json:"contact_boundary_signal"`
	CardIDs  []string `json:"card_ids"`
	Sources  []string `json:"sources"`
	Reason   string   `json:"reason"`
}

// ConversationReferences applies the same conditions as the single-question
// comparison: a matched topic, no restricted or uncertain contact boundary and
// no safety concern. Research cards additionally need confirmed safety, mutual
// consent and allowed contact. With mode "off" nothing is selected.
func ConversationReferences(text string, facts []Fact, mode string) ([]Card, []ResearchCard, ReferenceUsage) {
	usage := ReferenceUsage{Mode: mode, CardIDs: []string{}, Sources: []string{}}
	if mode == "off" {
		usage.Reason = "references disabled for this comparison condition"
		return nil, nil, usage
	}
	usage.Mode = "conditional"
	topic, method, _ := selectTopic("", text)
	usage.Topic, usage.Method = topic, method
	usage.Boundary = contactBoundary(Request{Question: text, Facts: facts})
	for _, f := range facts {
		if f.Key == "safety" && f.Value != "no_threat" {
			usage.Reason = "safety concern recorded"
			return nil, nil, usage
		}
	}
	if topic == "unclassified" {
		usage.Reason = "no topic matched"
		return nil, nil, usage
	}
	if usage.Boundary == "restricted" || usage.Boundary == "uncertain" {
		usage.Reason = "contact boundary restricted or uncertain"
		return nil, nil, usage
	}
	cards := []Card{}
	for _, c := range Cards() {
		if c.Topic == topic {
			cards = append(cards, c)
			usage.CardIDs = append(usage.CardIDs, c.ID+"@"+c.Version)
			usage.Sources = append(usage.Sources, c.Source.Title+" ("+c.Source.License+")")
		}
	}
	research := SelectResearch(topic, facts, evaluationSourceGroups())
	for _, c := range research {
		usage.CardIDs = append(usage.CardIDs, c.ID+"@"+c.Version)
		usage.Sources = append(usage.Sources, c.VideoURL)
	}
	usage.Used = len(usage.CardIDs) > 0
	usage.Reason = "topic matched and no restricting condition"
	return cards, research, usage
}
