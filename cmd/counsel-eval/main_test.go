package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDryRunAndCallCeiling(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "prepared.json")
	fixture := "../../fixtures/counsel-research-evaluation.json"
	if err := run([]string{"--root", "../..", "--input", fixture, "--out", out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 10 {
		t.Fatalf("prepared %d rows", len(got.Results))
	}
	for _, r := range got.Results {
		if r.Status != "not_run" {
			t.Fatal("unexpected model call")
		}
	}
	if err = run([]string{"--root", "../..", "--input", fixture, "--out", out}); err == nil {
		t.Fatal("overwrote results")
	}
	// Exceeding the ceiling fails before configuration or creating outputs.
	blocked := filepath.Join(dir, "blocked.json")
	if err = run([]string{"--root", "../..", "--input", fixture, "--out", blocked, "--blind-out", filepath.Join(dir, "blind"), "--generate", "--max-calls", "1"}); err == nil {
		t.Fatal("call ceiling ignored")
	}
	if _, err = os.Stat(blocked); !os.IsNotExist(err) {
		t.Fatal("created output despite budget rejection")
	}
}

func TestMockProviderGeneratesPairedBlindReview(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"snapshot","choices":[{"message":{"content":"상황을 먼저 확인해 보세요."},"finish_reason":"stop"}],"usage":{"total_tokens":10}}`)
	}))
	defer server.Close()
	t.Setenv("NYANG_LLM_API_BASE", server.URL)
	t.Setenv("NYANG_LLM_MODEL", "mock-model")
	t.Setenv("NYANG_LLM_API_KEY", "")
	dir := t.TempDir()
	err := run([]string{"--root", "../..", "--input", "../../fixtures/counsel-research-evaluation.json", "--out", filepath.Join(dir, "raw.json"), "--blind-out", filepath.Join(dir, "blind"), "--generate", "--max-calls", "10"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 10 {
		t.Fatalf("calls=%d", calls)
	}
	for _, name := range []string{"blind.json", "review.csv", "answer-key.json"} {
		if _, err = os.Stat(filepath.Join(dir, "blind", name)); err != nil {
			t.Fatal(err)
		}
	}
}
