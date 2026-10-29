package saju

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLunarInputValidation(t *testing.T) {
	r := exampleRequest()
	r.Birth.Calendar = "korean-lunar"
	r.Birth.MonthKind = "regular"
	r.Birth.Day = 30
	if err := r.Validate(); err != nil {
		t.Fatalf("valid syntactic lunar input: %v", err)
	}
	for _, b := range []Birth{
		{Calendar: "korean-lunar", Year: 2023, Month: 2, Day: 1, Hour: r.Birth.Hour, TimeZone: "Asia/Seoul"},
		{Calendar: "korean-lunar", MonthKind: "leap", Year: 2023, Month: 2, Day: 31, Hour: r.Birth.Hour, TimeZone: "Asia/Seoul"},
		{Calendar: "gregorian", MonthKind: "leap", Year: 2023, Month: 2, Day: 1, Hour: r.Birth.Hour, TimeZone: "Asia/Seoul"},
		{Calendar: "other", Year: 2023, Month: 2, Day: 1, Hour: r.Birth.Hour, TimeZone: "Asia/Seoul"},
	} {
		r.Birth = b
		if r.Validate() == nil {
			t.Fatalf("invalid input accepted: %+v", b)
		}
	}
}

func TestIntegrationLunarBirthUsesOriginalEngine(t *testing.T) {
	if os.Getenv("SAJU_INTEGRATION") != "1" {
		t.Skip("set SAJU_INTEGRATION=1 after scripts/setup-saju.sh")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	engine := New(root)
	hour := 5
	cases := []struct {
		lunar Birth
		solar Birth
	}{
		{Birth{Calendar: "korean-lunar", MonthKind: "regular", Year: 1992, Month: 9, Day: 29, Hour: &hour, Minute: 30, TimeZone: "Asia/Seoul"}, Birth{Year: 1992, Month: 10, Day: 24, Hour: &hour, Minute: 30, TimeZone: "Asia/Seoul"}},
		{Birth{Calendar: "korean-lunar", MonthKind: "leap", Year: 2023, Month: 2, Day: 1, Hour: &hour, TimeZone: "Asia/Seoul"}, Birth{Year: 2023, Month: 3, Day: 22, Hour: &hour, TimeZone: "Asia/Seoul"}},
	}
	for _, tc := range cases {
		req := Request{CaseID: "lunar-equivalence", Question: "test", Birth: tc.lunar}
		l, err := engine.Compare(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		req.Birth = tc.solar
		g, err := engine.Compare(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var lc, gc struct {
			Pillars    json.RawMessage `json:"pillars"`
			Chronology struct {
				Instant string `json:"instantUtc"`
			} `json:"chronology"`
		}
		if err := json.Unmarshal(l.Chart, &lc); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(g.Chart, &gc); err != nil {
			t.Fatal(err)
		}
		if lc.Chronology.Instant != gc.Chronology.Instant || !reflect.DeepEqual(lc.Pillars, gc.Pillars) {
			t.Fatalf("lunar and Gregorian differ: %s %s", lc.Chronology.Instant, gc.Chronology.Instant)
		}
	}
	bad := cases[1].lunar
	bad.Year = 2024
	if _, err := engine.Compare(context.Background(), Request{CaseID: "invalid-lunar", Question: "test", Birth: bad}); err == nil {
		t.Fatal("nonexistent leap month accepted")
	}
}

func TestAdapterFailureDoesNotExposeStderr(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "adapters", "ohmysaju")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`process.stderr.write(JSON.stringify({error:'UNEXPECTED',message:'PRIVATE_BIRTH_DETAILS'})); process.exitCode=1;`)
	if err := os.WriteFile(filepath.Join(dir, "bridge.mjs"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := New(root).Compare(context.Background(), exampleRequest())
	if err == nil {
		t.Fatal("expected adapter failure")
	}
	if strings.Contains(err.Error(), "PRIVATE_BIRTH_DETAILS") || strings.Contains(err.Error(), "UNEXPECTED") {
		t.Fatal("adapter stderr leaked")
	}
}
