package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDryRunMakesNoCalls(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-input", "../../fixtures/counsel-conversation-evaluation.json"}, &out); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Scope   string `json:"scope"`
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Results) == 0 || report.Scope != "synthetic_dialogues_not_user_outcomes" {
		t.Fatalf("%v %s", err, out.String()[:200])
	}
	for _, r := range report.Results {
		if r.Status != "not_run" {
			t.Fatalf("dry run status %s", r.Status)
		}
	}
	if err := run([]string{"-input", "../../fixtures/counsel-conversation-evaluation.json", "-blind-out", t.TempDir() + "/b"}, &out); err == nil {
		t.Fatal("blind export without generation accepted")
	}
	if err := run([]string{"-input", "../../fixtures/counsel-conversation-evaluation.json", "-generate"}, &out); err == nil {
		t.Fatal("generation without output paths accepted")
	}
}
