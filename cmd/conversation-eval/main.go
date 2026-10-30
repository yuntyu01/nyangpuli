// Command conversation-eval compares the earlier single-question counseling
// prompt with the conversation prompt on synthetic dialogues. Without
// --generate it only checks the assembled contexts and prints the inputs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/evaluation"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	f := flag.NewFlagSet("conversation-eval", flag.ContinueOnError)
	path := f.String("input", "fixtures/counsel-conversation-evaluation.json", "synthetic conversation fixture")
	out := f.String("out", "", "new private results JSON file")
	blind := f.String("blind-out", "", "new private blinded review directory")
	ablation := f.Bool("reference-ablation", false, "add a conversation condition without reference cards")
	enabled := f.Bool("generate", false, "explicitly call the configured model; incurs cost")
	maxCalls := f.Int("max-calls", 0, "hard ceiling on number of provider calls")
	seed := f.Int64("seed", 20261030, "blind randomization seed")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *enabled && (*out == "" || *blind == "") {
		return errors.New("generation requires --out and --blind-out to retain partial results and a blinded review")
	}
	if !*enabled && *blind != "" {
		return errors.New("blind review requires real generated results; dry run prepares inputs only")
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return errors.New("fixture exceeds 1 MiB")
	}
	var fixture evaluation.ConversationFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		return err
	}
	inputs, checks, err := evaluation.PrepareConversations(fixture, *ablation)
	if err != nil {
		return err
	}
	failed := 0
	for _, c := range checks {
		if !c.Passed {
			failed++
		}
	}
	if *enabled && (*maxCalls <= 0 || len(inputs) > *maxCalls) {
		return fmt.Errorf("%d calls exceed the explicit --max-calls ceiling", len(inputs))
	}
	var client *generation.Client
	if *enabled {
		cfg, e := generation.ConfigFromEnv()
		if e != nil {
			return e
		}
		if client, e = generation.New(cfg, nil); e != nil {
			return e
		}
		if _, e = os.Stat(*blind); e == nil {
			return errors.New("blind directory exists; no model calls made")
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	dest := stdout
	if *out != "" {
		if err = os.MkdirAll(filepath.Dir(*out), 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		dest = file
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	results, genErr := generation.Run(ctx, inputs, client, *enabled)
	enc := json.NewEncoder(dest)
	enc.SetIndent("", "  ")
	if err = enc.Encode(struct {
		FixtureVersion string                    `json:"fixture_version"`
		Scope          string                    `json:"scope"`
		ContextChecks  []evaluation.ContextCheck `json:"context_checks"`
		Results        []generation.Result       `json:"results"`
	}{fixture.Version, "synthetic_dialogues_not_user_outcomes", checks, results}); err != nil {
		return err
	}
	if genErr != nil {
		return genErr
	}
	if failed > 0 {
		return fmt.Errorf("%d context checks failed", failed)
	}
	if *blind != "" {
		return evaluation.ExportReview(*blind, results, *seed, evaluation.ConversationReview)
	}
	return nil
}
