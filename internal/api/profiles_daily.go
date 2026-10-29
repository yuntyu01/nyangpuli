package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/daily"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/readings"
	"github.com/yuntyu01/nyangpuli-lab/internal/saju"
)

type ProfileDailyStore interface {
	CreateProfile(context.Context, string, saju.Birth) (persistence.Profile, error)
	ListProfiles(context.Context, string) ([]persistence.Profile, error)
	GetProfile(context.Context, string, string) (persistence.Profile, error)
	UpdateProfile(context.Context, string, string, int, saju.Birth) (persistence.Profile, error)
	GetDaily(context.Context, daily.Key) (json.RawMessage, error)
	PutDaily(context.Context, daily.Key, json.RawMessage) (json.RawMessage, error)
}

func (s *Server) handleProfileDaily(w http.ResponseWriter, r *http.Request, user string) bool {
	path := r.URL.Path
	if path != "/v1/profiles" && !strings.HasPrefix(path, "/v1/profiles/") && path != "/v1/daily" {
		return false
	}
	if s.ProfileDaily == nil {
		fail(w, 503, "profile storage unavailable")
		return true
	}
	if path == "/v1/daily" {
		if r.Method != http.MethodPost {
			fail(w, 405, "method not allowed")
			return true
		}
		var req struct {
			ProfileID string `json:"profile_id"`
			TimeZone  string `json:"time_zone"`
		}
		if !read(w, r, &req) {
			return true
		}
		if req.ProfileID == "" {
			fail(w, 400, "profile_id required")
			return true
		}
		profile, err := s.ProfileDaily.GetProfile(r.Context(), user, req.ProfileID)
		if err != nil {
			profileError(w, err)
			return true
		}
		if s.Readings == nil || s.DailyModelVersion == "" {
			fail(w, 503, "daily generation is not configured")
			return true
		}
		date, err := daily.LocalDate(time.Now(), req.TimeZone)
		if err != nil {
			fail(w, 400, "valid current IANA time_zone required")
			return true
		}
		// The pinned engine calculates the daily noon in the natal zone only.
		// Do not present that result as the current zone when they differ.
		if req.TimeZone != profile.Birth.TimeZone {
			fail(w, 422, "current timezone differs from birth timezone; daily calculation unavailable")
			return true
		}
		key := daily.Key{AccountID: user, ProfileID: profile.ID, ProfileVersion: profile.Version, Date: date, TimeZone: req.TimeZone, EngineVersion: saju.OhMySajuCommit, SourceVersion: saju.YueYuanCommit, PromptVersion: "daily-summary-v1", ModelVersion: s.DailyModelVersion}
		cached, err := s.ProfileDaily.GetDaily(r.Context(), key)
		if err == nil {
			var v readings.View
			if json.Unmarshal(cached, &v) != nil {
				fail(w, 500, "daily cache invalid")
			} else {
				write(w, 200, dailySummary(v))
			}
			return true
		}
		if !errors.Is(err, persistence.ErrDailyNotFound) {
			fail(w, 500, "daily cache unavailable")
			return true
		}
		day, err := time.Parse("2006-01-02", date)
		if err != nil {
			fail(w, 500, "daily date invalid")
			return true
		}
		input, _ := json.Marshal(saju.Request{Question: "오늘의 전반적인 흐름과 연애에서 참고할 행동을 알려 주세요.", Birth: profile.Birth, Date: &saju.Date{Year: day.Year(), Month: int(day.Month()), Day: day.Day()}})
		envelope, _ := json.Marshal(readingEnvelope{Input: input})
		rawKey, _ := json.Marshal(key)
		digest := sha256.Sum256(rawKey)
		idempotencyKey := "daily-" + hex.EncodeToString(digest[:])
		ctx := context.WithValue(r.Context(), readingUserKey{}, user)
		v, err := s.Readings.Create(ctx, user, idempotencyKey, "daily", "baseline", envelope)
		if err != nil {
			readingError(w, err)
			return true
		}
		v = dailySummary(v)
		raw, err := json.Marshal(v)
		if err != nil {
			fail(w, 500, "daily result invalid")
			return true
		}
		saved, err := s.ProfileDaily.PutDaily(r.Context(), key, raw)
		if errors.Is(err, persistence.ErrDailyNotFound) {
			fail(w, 409, "profile changed; request again")
			return true
		}
		if err != nil {
			fail(w, 500, "daily cache unavailable")
			return true
		}
		if json.Unmarshal(saved, &v) != nil {
			fail(w, 500, "daily result invalid")
			return true
		}
		write(w, 200, dailySummary(v))
		return true
	}
	if path == "/v1/profiles" {
		if r.Method == http.MethodGet {
			profiles, err := s.ProfileDaily.ListProfiles(r.Context(), user)
			if err != nil {
				fail(w, 500, "profile storage error")
			} else {
				write(w, 200, profiles)
			}
			return true
		}
		if r.Method != http.MethodPost {
			fail(w, 405, "method not allowed")
			return true
		}
		var req struct {
			Birth saju.Birth `json:"birth"`
		}
		if !read(w, r, &req) {
			return true
		}
		p, err := s.ProfileDaily.CreateProfile(r.Context(), user, req.Birth)
		if err != nil {
			profileError(w, err)
		} else {
			write(w, 201, p)
		}
		return true
	}
	id := strings.TrimPrefix(path, "/v1/profiles/")
	if id == "" || strings.Contains(id, "/") {
		fail(w, 404, "not found")
		return true
	}
	switch r.Method {
	case http.MethodGet:
		p, err := s.ProfileDaily.GetProfile(r.Context(), user, id)
		if err != nil {
			profileError(w, err)
		} else {
			write(w, 200, p)
		}
	case http.MethodPut:
		var req struct {
			Version int        `json:"version"`
			Birth   saju.Birth `json:"birth"`
		}
		if !read(w, r, &req) {
			return true
		}
		p, err := s.ProfileDaily.UpdateProfile(r.Context(), user, id, req.Version, req.Birth)
		if err != nil {
			profileError(w, err)
		} else {
			write(w, 200, p)
		}
	default:
		fail(w, 405, "method not allowed")
	}
	return true
}
func profileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, persistence.ErrProfileNotFound):
		fail(w, 404, "profile not found")
	case errors.Is(err, persistence.ErrProfileConflict):
		fail(w, 409, "profile changed or unavailable")
	case errors.Is(err, persistence.ErrInvalidProfile):
		fail(w, 400, "invalid birth profile")
	default:
		fail(w, 500, "profile storage error")
	}
}

// Daily is always a free summary. Explicitly copy allowed fields so neither a
// previously unlocked View nor stale cached JSON can leak a paid detail.
func dailySummary(v readings.View) readings.View {
	return readings.View{ID: v.ID, Domain: v.Domain, Variant: v.Variant, Status: "summary_ready", Summary: v.Summary, DetailLocked: true, EntitlementSource: "none", SourceIDs: append([]string(nil), v.SourceIDs...), SummaryUsage: v.SummaryUsage, SummaryModel: v.SummaryModel, SummaryPromptHash: v.SummaryPromptHash, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
