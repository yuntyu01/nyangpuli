package daily

import (
	"testing"
	"time"
)

func TestLocalDateUsesUserTimeZone(t *testing.T) {
	now := time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ zone, want string }{{"Asia/Seoul", "2026-01-02"}, {"America/Los_Angeles", "2026-01-01"}} {
		got, err := LocalDate(now, tc.zone)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %s %v", tc.zone, got, err)
		}
	}
	if _, err := LocalDate(now, "Local"); err == nil {
		t.Fatal("host timezone accepted")
	}
}
