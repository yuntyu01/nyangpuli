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

func exampleRequest() Request {
	h := 5
	return Request{CaseID: "saju-calendar-001", Question: "연락을 다시 시작하기 전에 무엇을 확인할까?", Birth: Birth{Year: 1992, Month: 10, Day: 24, Hour: &h, Minute: 30, TimeZone: "Asia/Seoul"}, Date: &Date{Year: 2026, Month: 6, Day: 28}}
}

func TestUnknownTimeAndInvalidDatesRejected(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Request)
	}{
		{"unknown time", func(r *Request) { r.Birth.Hour = nil }},
		{"non leap February", func(r *Request) { r.Birth.Year = 2023; r.Birth.Month = 2; r.Birth.Day = 29 }},
		{"missing timezone", func(r *Request) { r.Birth.TimeZone = "" }},
		{"invalid daily date", func(r *Request) { r.Date.Day = 31 }},
		{"empty question", func(r *Request) { r.Question = " " }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := exampleRequest()
			test.mutate(&r)
			if r.Validate() == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	r := exampleRequest()
	r.Birth.Year = 2024
	r.Birth.Month = 2
	r.Birth.Day = 29
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTiaoHouCoverageAndNoInventedFallback(t *testing.T) {
	n := 0
	for _, stem := range []rune("甲乙丙丁戊己庚辛壬癸") {
		for _, branch := range []rune("子丑寅卯辰巳午未申酉戌亥") {
			rule, err := LookupRule(string(stem), string(branch))
			if err != nil {
				t.Fatal(err)
			}
			if rule.OriginalNote == "" || rule.Status != "experimental_unverified_source_table" {
				t.Fatal("missing provenance/caveat")
			}
			for _, candidate := range rule.PriorityStems {
				if !strings.Contains("甲乙丙丁戊己庚辛壬癸", candidate) {
					t.Fatalf("invalid stem %q", candidate)
				}
			}
			n++
		}
	}
	if n != 120 {
		t.Fatal(n)
	}
	if _, err := LookupRule("계", "10"); err == nil {
		t.Fatal("must not reinterpret numeric month as month branch")
	}
	if _, err := LookupRule("?", "戌"); err == nil {
		t.Fatal("unknown stem must fail")
	}
}

func TestAdapterUnavailableDoesNotFabricateChart(t *testing.T) {
	_, err := New(t.TempDir()).Compare(context.Background(), exampleRequest())
	if err == nil {
		t.Fatal("missing real engine must fail")
	}
}

// Opt in explicitly; the default suite must not install dependencies or access a network.
func TestIntegrationActualOhMySajuAndDaily(t *testing.T) {
	if os.Getenv("SAJU_INTEGRATION") != "1" {
		t.Skip("set SAJU_INTEGRATION=1 after scripts/setup-saju.sh")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	engine := New(root)
	req := exampleRequest()
	response, err := engine.Compare(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var chart struct {
		Pillars map[string]struct {
			Korean string `json:"korean"`
		} `json:"pillars"`
	}
	if err := json.Unmarshal(response.Chart, &chart); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"year": "임신", "month": "경술", "day": "계유", "hour": "을묘"} {
		if got := chart.Pillars[k].Korean; got != want {
			t.Fatalf("%s got %s want %s", k, got, want)
		}
	}
	if !reflect.DeepEqual(response.Rule.PriorityStems, []string{"辛", "甲", "壬"}) {
		t.Fatalf("wrong calculated month-branch lookup: %+v", response.Rule)
	}
	var daily struct {
		Representative struct {
			Policy  string `json:"policy"`
			Instant string `json:"instantUtc"`
		} `json:"representative"`
		Pillars map[string]struct {
			Korean string `json:"korean"`
		} `json:"pillars"`
	}
	if err := json.Unmarshal(response.Daily, &daily); err != nil {
		t.Fatal(err)
	}
	if daily.Representative.Policy != "local-civil-noon" || daily.Representative.Instant != "2026-06-28T03:00:00.000Z" || daily.Pillars["day"].Korean != "계유" {
		t.Fatalf("wrong daily transit %+v", daily)
	}
	var base, enh map[string]json.RawMessage
	if err := json.Unmarshal(response.Baseline.Evidence, &base); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response.Enhanced.Evidence, &enh); err != nil {
		t.Fatal(err)
	}
	if string(base["chart"]) != string(enh["chart"]) || string(base["daily"]) != string(enh["daily"]) {
		t.Fatal("comparison conditions changed chart or daily data")
	}
	if len(base["experimental_tiao_hou"]) != 0 || len(enh["experimental_tiao_hou"]) == 0 {
		t.Fatal("rule must be the only added evidence")
	}
	if response.Status != "evidence_and_prompts_only_no_model_call" {
		t.Fatal("must disclose no generated answer")
	}
	// These are regression expectations from the pinned upstream fixture, not independent forecasting evidence.
	for _, tc := range []struct {
		day         int
		year, month string
	}{{3, "계묘", "을축"}, {5, "갑진", "병인"}} {
		r := exampleRequest()
		h := 12
		r.Birth = Birth{Year: 2024, Month: 2, Day: tc.day, Hour: &h, TimeZone: "Asia/Seoul"}
		r.Date = nil
		v, err := engine.Compare(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(v.Chart, &chart); err != nil {
			t.Fatal(err)
		}
		if chart.Pillars["year"].Korean != tc.year || chart.Pillars["month"].Korean != tc.month {
			t.Fatalf("Lichun side mismatch day %d: %+v", tc.day, chart)
		}
	}
	invalidZone := exampleRequest()
	invalidZone.Birth.TimeZone = "Mars/Olympus"
	if _, err := engine.Compare(context.Background(), invalidZone); err == nil {
		t.Fatal("invalid IANA zone accepted")
	}
}
