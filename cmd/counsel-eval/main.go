// Command counsel-eval prepares paired counseling evaluation inputs. A model is
// never invoked without an explicit opt-in and a call-count ceiling.
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
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	f := flag.NewFlagSet("counsel-eval", flag.ContinueOnError)
	path := f.String("input", "fixtures/counsel-research-evaluation.json", "synthetic evaluation fixture")
	root := f.String("root", ".", "repository root")
	out := f.String("out", "", "new private results JSON file")
	blind := f.String("blind-out", "", "new private blinded review directory")
	enabled := f.Bool("generate", false, "explicitly call the configured model; incurs cost")
	maxCalls := f.Int("max-calls", 0, "hard ceiling on number of provider calls")
	seed := f.Int64("seed", 20261029, "blind randomization seed")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *enabled && (*out == "" || *blind == "") {
		return errors.New("generation requires --out and --blind-out to retain partial results and a blinded review")
	}
	if !*enabled && *blind != "" {
		return errors.New("blind review requires real generated results; dry run prepares inputs only")
	}
	file, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return errors.New("fixture exceeds 1 MiB")
	}
	var fixture evaluation.ResearchFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		return err
	}
	l, err := lab.New(*root)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	inputs, err := evaluation.PrepareResearch(ctx, l, fixture)
	if err != nil {
		return err
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
		client, e = generation.New(cfg, nil)
		if e != nil {
			return e
		}
	}
	if *blind != "" {
		if _, e := os.Stat(*blind); e == nil {
			return errors.New("blind directory exists; no model calls made")
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	var dest io.Writer = os.Stdout
	var output *os.File
	if *out != "" {
		if err = os.MkdirAll(filepath.Dir(*out), 0700); err != nil {
			return err
		}
		output, err = os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer output.Close()
		dest = output
	}
	results, genErr := generation.Run(ctx, inputs, client, *enabled)
	envelope := struct {
		FixtureVersion string              `json:"fixture_version"`
		Results        []generation.Result `json:"results"`
	}{fixture.Version, results}
	enc := json.NewEncoder(dest)
	enc.SetIndent("", "  ")
	if err = enc.Encode(envelope); err != nil {
		return err
	}
	if output != nil {
		if err = output.Sync(); err != nil {
			return err
		}
	}
	if genErr != nil {
		return genErr
	}
	if *blind != "" {
		return evaluation.Export(*blind, results, *seed)
	}
	return nil
}
