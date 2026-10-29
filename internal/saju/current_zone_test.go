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

func TestDailyTimeZoneValidation(t *testing.T) {
	req := exampleRequest()
	for _, z := range []string{"Local", "Invalid/Zone"} {
		req.DailyTimeZone = z
		if req.Validate() == nil {
			t.Fatalf("accepted zone %q", z)
		}
	}
	req.DailyTimeZone = "America/New_York"
	req.Date = nil
	if req.Validate() == nil {
		t.Fatal("daily zone without date accepted")
	}
}

// The transit is computed by the same public original engine as a separate
// civil-noon chart in the user's current zone. Natal relationships and ten gods
// are deliberately not derived from the transit chart's unrelated day master.
func TestIntegrationDailyCurrentTimeZoneUsesOriginalEngine(t *testing.T) {
	if os.Getenv("SAJU_INTEGRATION") != "1" {
		t.Skip("set SAJU_INTEGRATION=1 after scripts/setup-saju.sh")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	engine := New(root)
	hour := 5
	birth := Birth{Year: 1992, Month: 10, Day: 24, Hour: &hour, Minute: 30, TimeZone: "Asia/Seoul"}
	lunar := Birth{Calendar: "korean-lunar", MonthKind: "regular", Year: 1992, Month: 9, Day: 29, Hour: &hour, Minute: 30, TimeZone: "Asia/Seoul"}
	cases := []struct {
		birth         Birth
		date          Date
		zone, instant string
	}{
		{birth, Date{Year: 2025, Month: 3, Day: 9}, "America/New_York", "2025-03-09T16:00:00.000Z"},
		{birth, Date{Year: 2025, Month: 11, Day: 2}, "America/New_York", "2025-11-02T17:00:00.000Z"},
		{lunar, Date{Year: 2026, Month: 1, Day: 1}, "Pacific/Honolulu", "2026-01-01T22:00:00.000Z"},
	}
	for _, tc := range cases {
		req := Request{CaseID: "daily-zone", Question: "test", Birth: tc.birth, Date: &tc.date, DailyTimeZone: tc.zone}
		got, err := engine.Compare(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		natal, err := engine.Compare(context.Background(), Request{CaseID: "natal", Question: "test", Birth: tc.birth})
		if err != nil {
			t.Fatal(err)
		}
		var chart, natalChart map[string]any
		if err = json.Unmarshal(got.Chart, &chart); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(natal.Chart, &natalChart); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(chart, natalChart) {
			t.Fatal("natal chart changed")
		}
		if !strings.Contains(strings.Join(got.Warnings, " "), "관계·십성은 계산하지 않음") {
			t.Fatal("missing uncomputed-relationship warning")
		}
		var d struct {
			Representative struct {
				Instant  string `json:"instantUtc"`
				TimeZone string `json:"timeZone"`
			} `json:"representative"`
			Pillars       map[string]json.RawMessage `json:"pillars"`
			TenGods       json.RawMessage            `json:"tenGods"`
			Relationships json.RawMessage            `json:"relationships"`
		}
		if err = json.Unmarshal(got.Daily, &d); err != nil {
			t.Fatal(err)
		}
		if d.Representative.Instant != tc.instant || d.Representative.TimeZone != tc.zone {
			t.Fatalf("noon: %+v", d.Representative)
		}
		if string(d.TenGods) != "null" || string(d.Relationships) != "null" {
			t.Fatal("uncomputed natal relationships were presented")
		}
		noon := 12
		transit, err := engine.Compare(context.Background(), Request{CaseID: "transit", Question: "test", Birth: Birth{Year: tc.date.Year, Month: tc.date.Month, Day: tc.date.Day, Hour: &noon, TimeZone: tc.zone}})
		if err != nil {
			t.Fatal(err)
		}
		var transitChart struct {
			Pillars map[string]json.RawMessage `json:"pillars"`
		}
		if err = json.Unmarshal(transit.Chart, &transitChart); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"year", "month", "day"} {
			if !reflect.DeepEqual(d.Pillars[name], transitChart.Pillars[name]) {
				t.Fatalf("%s pillar differs from original engine", name)
			}
		}
	}
}
