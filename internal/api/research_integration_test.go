package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

func TestCounselResearchInReadingPathHonorsStoredBoundary(t *testing.T) {
	l, err := lab.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	m, err := memory.Open("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Set("alice", memory.Fact{Key: "contact_boundary", Value: "연락하지 않음", SourceEventID: "boundary-event", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	b := readingBuilder{lab: l, memory: m}
	ctx := context.WithValue(context.Background(), readingUserKey{}, "alice")
	input := json.RawMessage(`{"question":"연락 기준을 정하고 싶어요","include_research":true,"facts":[{"key":"safety","value":"no_threat","source_id":"answer1"},{"key":"dialogue_consent","value":"mutual","source_id":"answer2"},{"key":"contact_boundary","value":"allowed","source_id":"answer3"}]}`)
	build := func(use bool) string {
		raw, e := json.Marshal(readingEnvelope{Input: input, UseMemory: use})
		if e != nil {
			t.Fatal(e)
		}
		result, e := b.Build(ctx, "counsel", "enhanced", raw)
		if e != nil {
			t.Fatal(e)
		}
		return strings.Join(result.Sources, " ")
	}
	if !strings.Contains(build(false), "dugyeol-contact-001@2026-10-08-v1") {
		t.Fatal("confirmed situation did not select source")
	}
	if strings.Contains(build(true), "dugyeol:") {
		t.Fatal("stored boundary overridden")
	}
}
