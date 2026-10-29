package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/saju"
)

func TestLunarProfileFailsClosedWithoutEngine(t *testing.T) {
	hour := 12
	b := saju.Birth{Calendar: "korean-lunar", MonthKind: "regular", Year: 2023, Month: 1, Day: 1, Hour: &hour, TimeZone: "Asia/Seoul"}
	w := httptest.NewRecorder()
	s := &Server{}
	if s.validateLunarBirth(w, httptest.NewRequest("POST", "/v1/profiles", nil), b) || w.Code != 503 {
		t.Fatalf("unavailable engine: %d", w.Code)
	}
}

func TestLunarProfileChecksRealCalendarBeforeSaving(t *testing.T) {
	if os.Getenv("SAJU_INTEGRATION") != "1" {
		t.Skip("set SAJU_INTEGRATION=1 after scripts/setup-saju.sh")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	hour := 12
	s := &Server{Lab: &lab.Lab{Root: root}}
	b := saju.Birth{Calendar: "korean-lunar", MonthKind: "leap", Year: 2023, Month: 2, Day: 1, Hour: &hour, TimeZone: "Asia/Seoul"}
	w := httptest.NewRecorder()
	if !s.validateLunarBirth(w, httptest.NewRequest("POST", "/v1/profiles", nil), b) {
		t.Fatalf("valid lunar date: %d", w.Code)
	}
	b.Year = 2024
	w = httptest.NewRecorder()
	if s.validateLunarBirth(w, httptest.NewRequest("POST", "/v1/profiles", nil), b) || w.Code != 400 {
		t.Fatalf("nonexistent leap month: %d", w.Code)
	}
}
