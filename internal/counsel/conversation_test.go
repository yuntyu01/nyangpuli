package counsel

import "testing"

func TestConversationReferencesAreConditional(t *testing.T) {
	_, _, u := ConversationReferences("연락을 언제 해야 할지 모르겠어", nil, "")
	if !u.Used || u.Topic != TopicContact || len(u.CardIDs) != 1 || u.Mode != "conditional" {
		t.Fatalf("matched topic %+v", u)
	}
	_, _, u = ConversationReferences("연락을 언제 해야 할지 모르겠어", []Fact{{Key: "contact_boundary", Value: "blocked", SourceID: "mem:1"}}, "")
	if u.Used || u.Reason == "" {
		t.Fatalf("restricted boundary used references %+v", u)
	}
	_, _, u = ConversationReferences("연락을 언제 해야 할지 모르겠어", nil, "off")
	if u.Used || u.Mode != "off" {
		t.Fatalf("off %+v", u)
	}
	_, _, u = ConversationReferences("그냥 요즘 다 답답해", nil, "")
	if u.Used || u.Topic != "unclassified" {
		t.Fatalf("vague distress %+v", u)
	}
	_, research, u := ConversationReferences("싸운 뒤에 화해하고 싶어", []Fact{{Key: "safety", Value: "no_threat", SourceID: "a"}, {Key: "dialogue_consent", Value: "mutual", SourceID: "b"}, {Key: "contact_boundary", Value: "allowed", SourceID: "c"}}, "")
	if !u.Used || len(research) == 0 || len(u.Sources) != len(u.CardIDs) {
		t.Fatalf("research needs confirmed conditions %+v", u)
	}
	_, research, _ = ConversationReferences("싸운 뒤에 화해하고 싶어", nil, "")
	if len(research) != 0 {
		t.Fatal("research cards without confirmed safety, consent and contact")
	}
}
