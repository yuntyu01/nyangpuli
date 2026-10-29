// Package daily defines a cache key for a user's local calendar day.
package daily

import (
	"errors"
	"strings"
	"time"
)

type Key struct {
	AccountID      string
	ProfileID      string
	ProfileVersion int
	Date           string
	TimeZone       string
	EngineVersion  string
	SourceVersion  string
	PromptVersion  string
	ModelVersion   string
}

// LocalDate uses the supplied instant and profile timezone, never the host zone.
func LocalDate(now time.Time, zone string) (string, error) {
	if zone == "" || zone == "Local" {
		return "", errors.New("explicit IANA timezone required")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", errors.New("invalid IANA timezone")
	}
	return now.In(loc).Format("2006-01-02"), nil
}
func (k Key) Validate() error {
	fields := []string{k.AccountID, k.ProfileID, k.TimeZone, k.EngineVersion, k.SourceVersion, k.PromptVersion, k.ModelVersion}
	for _, v := range fields {
		if strings.TrimSpace(v) == "" || len(v) > 256 {
			return errors.New("missing or invalid cache key component")
		}
	}
	if k.ProfileVersion < 1 {
		return errors.New("invalid profile version")
	}
	if _, err := LocalDate(time.Now(), k.TimeZone); err != nil {
		return err
	}
	date, err := time.Parse("2006-01-02", k.Date)
	if err != nil || date.Format("2006-01-02") != k.Date || date.Year() < 1801 || date.Year() > 2100 {
		return errors.New("invalid local date")
	}
	return nil
}
