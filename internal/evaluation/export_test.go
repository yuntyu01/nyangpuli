package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
)

func fixture() []generation.Result {
	cfg := &generation.Config{APIBase: "https://provider.example/v1", Model: "fixed-model", MaxCompletionTokens: 1024}
	return []generation.Result{
		{Input: generation.Input{CaseID: "C1", VariantID: "baseline-secret", Question: "서운함을 어떻게 표현할까?", SystemPrompt: "hidden-system", UserPrompt: "hidden-user", Sources: []string{"private-source-a"}}, Status: "generated", Text: "답변 하나", PromptSHA256: "hash-a", Config: cfg, ReturnedModel: "fixed-snapshot", FinishReason: "stop"},
		{Input: generation.Input{CaseID: "C1", VariantID: "enhanced-secret", Question: "서운함을 어떻게 표현할까?", SystemPrompt: "hidden-system2", UserPrompt: "hidden-user2", Sources: []string{"private-source-b"}}, Status: "generated", Text: "답변 둘", PromptSHA256: "hash-b", Config: cfg, ReturnedModel: "fixed-snapshot", FinishReason: "stop"},
	}
}

func TestBlindingReproducibleNoMetadataLeaks(t *testing.T) {
	rows := fixture()
	rows[0].Input.CaseContext = json.RawMessage(`{"cards":[{"id":6,"position":"self","reversed":false}],"facts":["상대가 주말에만 통화할 수 있다고 말했다"]}`)
	// Different key order and whitespace must not make identical cases differ.
	rows[1].Input.CaseContext = json.RawMessage(`{ "facts": ["상대가 주말에만 통화할 수 있다고 말했다"], "cards": [{"reversed":false,"position":"self","id":6}] }`)
	a, key, err := Build(rows, 42)
	if err != nil {
		t.Fatal(err)
	}
	rows[0], rows[1] = rows[1], rows[0]
	b, key2, err := Build(rows, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(key, key2) {
		t.Fatal("seed/order reproducibility failed")
	}
	data, _ := json.Marshal(a)
	if !strings.Contains(string(data), "상대가 주말에만 통화할 수 있다고 말했다") || !strings.Contains(string(data), `"reversed":false`) || !strings.Contains(string(data), `"position":"self"`) {
		t.Fatal("shared facts/cards/positions missing from blind review")
	}
	for _, secret := range []string{"baseline-secret", "enhanced-secret", "private-source", "hidden-user", "hidden-system", "hash-a", "fixed-model", "provider.example"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("metadata leaked: %s", secret)
		}
	}
	if len(key.Entries) != 2 || key.Entries[0].Label == key.Entries[1].Label {
		t.Fatal("answer mapping incomplete")
	}
	byVariant := map[string]string{}
	for _, r := range rows {
		byVariant[r.Input.VariantID] = r.Text
	}
	for i, e := range key.Entries {
		if byVariant[e.VariantID] != a.Cases[0].Answers[i].Text {
			t.Fatal("mapping incorrect")
		}
	}
}

func TestRejectInvalidComparisons(t *testing.T) {
	tests := map[string]func([]generation.Result) []generation.Result{
		"not_run":           func(r []generation.Result) []generation.Result { r[0].Status = "not_run"; return r },
		"truncated":         func(r []generation.Result) []generation.Result { r[0].FinishReason = "length"; return r },
		"mismatch question": func(r []generation.Result) []generation.Result { r[0].Input.Question = "다른 질문"; return r },
		"snapshot drift":    func(r []generation.Result) []generation.Result { r[0].ReturnedModel = "different-snapshot"; return r },
		"duplicate":         func(r []generation.Result) []generation.Result { return []generation.Result{r[0], r[0]} },
		"one arm":           func(r []generation.Result) []generation.Result { return r[:1] },
		"mismatch context": func(r []generation.Result) []generation.Result {
			r[0].Input.CaseContext = json.RawMessage(`{"facts":["연락하지 말라는 말을 들었다"]}`)
			r[1].Input.CaseContext = json.RawMessage(`{"facts":["연락해도 된다는 말을 들었다"]}`)
			return r
		},
		"invalid context": func(r []generation.Result) []generation.Result {
			r[0].Input.CaseContext = json.RawMessage(`{"invalid"`)
			return r
		},
		"mismatch model": func(r []generation.Result) []generation.Result {
			copy := *r[0].Config
			copy.Model = "different"
			r[0].Config = &copy
			return r
		},
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Build(fn(fixture()), 9); err == nil {
				t.Fatal("invalid comparison accepted")
			}
		})
	}
}

func TestExportSeparateFilesAndRefuseOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trial")
	if err := Export(dir, fixture(), 7); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"blind.json", "answer-key.json", "raw-results.json", "review.csv"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("public output permissions %s", name)
		}
	}
	key, _ := os.ReadFile(filepath.Join(dir, "answer-key.json"))
	if err := Export(dir, fixture(), 8); err == nil {
		t.Fatal("overwrote existing run")
	}
	still, _ := os.ReadFile(filepath.Join(dir, "answer-key.json"))
	if string(key) != string(still) {
		t.Fatal("changed existing key")
	}
	csv, _ := os.ReadFile(filepath.Join(dir, "review.csv"))
	if strings.Contains(string(csv), "secret") {
		t.Fatal("condition in judge sheet")
	}
}
