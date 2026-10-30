package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
)

func loadConversationFixture(t *testing.T) ConversationFixture {
	t.Helper()
	data, err := os.ReadFile("../../fixtures/counsel-conversation-evaluation.json")
	if err != nil {
		t.Fatal(err)
	}
	var f ConversationFixture
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestConversationComparisonSharesContextAndChecksAssembly(t *testing.T) {
	f := loadConversationFixture(t)
	inputs, checks, err := PrepareConversations(f, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3*len(f.Cases) || len(checks) != len(f.Cases) {
		t.Fatalf("inputs %d checks %d", len(inputs), len(checks))
	}
	for _, c := range checks {
		if !c.Passed {
			t.Fatalf("context check %s: %v", c.CaseID, c.Failures)
		}
	}
	byCase := map[string]map[string]generation.Input{}
	for _, in := range inputs {
		if byCase[in.CaseID] == nil {
			byCase[in.CaseID] = map[string]generation.Input{}
		}
		byCase[in.CaseID][in.VariantID] = in
	}
	for id, conds := range byCase {
		legacy, conv, plain := conds[LegacyCondition], conds[ConversationCondition], conds[NoReferenceCondition]
		if string(legacy.CaseContext) != string(conv.CaseContext) || string(conv.CaseContext) != string(plain.CaseContext) || legacy.Question != conv.Question {
			t.Fatalf("%s: conditions do not share the same case", id)
		}
		if strings.Contains(plain.UserPrompt, `"kind": "bccampus"`) || strings.Contains(plain.UserPrompt, `"kind": "research"`) {
			t.Fatalf("%s: reference ablation still has cards", id)
		}
	}
	tried := byCase["already-tried"]
	if strings.Contains(tried[LegacyCondition].UserPrompt, "장문 카톡을 보냈지만") || !strings.Contains(tried[ConversationCondition].UserPrompt, "장문 카톡을 보냈지만") {
		t.Fatal("only the conversation condition should carry earlier attempts")
	}
	bad := f
	bad.Cases = append([]ConversationCase{}, f.Cases...)
	bad.Cases[0].ReviewFocus = nil
	if _, _, err = PrepareConversations(bad, false); err == nil {
		t.Fatal("case without review focus accepted")
	}
}

func TestConversationReviewSheet(t *testing.T) {
	cfg := &generation.Config{APIBase: "http://127.0.0.1", Model: "m", MaxCompletionTokens: 10}
	ctx := json.RawMessage(`{"dialogue":[]}`)
	results := []generation.Result{
		{Input: generation.Input{CaseID: "c1", VariantID: LegacyCondition, Question: "q", CaseContext: ctx}, Status: "generated", Text: "a", FinishReason: "stop", Config: cfg},
		{Input: generation.Input{CaseID: "c1", VariantID: ConversationCondition, Question: "q", CaseContext: ctx}, Status: "generated", Text: "b", FinishReason: "stop", Config: cfg},
	}
	dir := filepath.Join(t.TempDir(), "blind")
	if err := ExportReview(dir, results, 1, ConversationReview); err != nil {
		t.Fatal(err)
	}
	sheet, _ := os.ReadFile(filepath.Join(dir, "review.csv"))
	blind, _ := os.ReadFile(filepath.Join(dir, "blind.json"))
	if !strings.HasPrefix(string(sheet), "case_id,label,helpful_1_5,context_fit_1_5") || strings.Count(string(sheet), "\n") != 3 {
		t.Fatalf("sheet %s", sheet)
	}
	if !strings.Contains(string(blind), "답변 길이, 인용한 자료 수") || strings.Contains(string(blind), ConversationCondition) {
		t.Fatalf("blind set %s", blind)
	}
}
