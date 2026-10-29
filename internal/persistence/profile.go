package persistence

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/yuntyu01/nyangpuli-lab/internal/saju"
)

var ErrProfileNotFound = errors.New("profile not found")
var ErrProfileConflict = errors.New("profile version changed")
var ErrInvalidProfile = errors.New("invalid birth profile")

type Profile struct {
	ID      string     `json:"id"`
	Version int        `json:"version"`
	Birth   saju.Birth `json:"birth"`
}

func validateBirth(b saju.Birth) error {
	if err := (saju.Request{CaseID: "profile", Question: "profile", Birth: b}).Validate(); err != nil {
		return ErrInvalidProfile
	}
	return nil
}
func (s Store) CreateProfile(ctx context.Context, account string, b saju.Birth) (Profile, error) {
	if err := validateBirth(b); err != nil {
		return Profile{}, err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return Profile{}, err
	}
	id := hex.EncodeToString(random)
	raw, err := json.Marshal(b)
	if err != nil {
		return Profile{}, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO profiles(id,account_id,payload) VALUES($1,$2,$3)`, id, account, raw)
	if err != nil {
		return Profile{}, err
	}
	return Profile{ID: id, Version: 1, Birth: b}, nil
}
func (s Store) GetProfile(ctx context.Context, account, id string) (Profile, error) {
	var p Profile
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT p.id,p.version,p.payload FROM profiles p JOIN accounts a ON a.id=p.account_id AND a.deleted_at IS NULL WHERE p.id=$1 AND p.account_id=$2`, id, account).Scan(&p.ID, &p.Version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrProfileNotFound
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(raw, &p.Birth); err != nil {
		return Profile{}, err
	}
	if err = validateBirth(p.Birth); err != nil {
		return Profile{}, err
	}
	return p, nil
}
func (s Store) UpdateProfile(ctx context.Context, account, id string, version int, b saju.Birth) (Profile, error) {
	if version < 1 {
		return Profile{}, ErrProfileConflict
	}
	if err := validateBirth(b); err != nil {
		return Profile{}, err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return Profile{}, err
	}
	var next int
	err = s.DB.QueryRowContext(ctx, `UPDATE profiles SET payload=$4,version=version+1,updated_at=now() WHERE id=$1 AND account_id=$2 AND version=$3 RETURNING version`, id, account, version, raw).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrProfileConflict
	}
	if err != nil {
		return Profile{}, err
	}
	return Profile{ID: id, Version: next, Birth: b}, nil
}

// ListProfiles returns only active account-owned profiles. The cap avoids an
// unbounded response; an over-cap result is an error rather than silent loss.
func (s Store) ListProfiles(ctx context.Context, account string) ([]Profile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id,p.version,p.payload FROM profiles p JOIN accounts a ON a.id=p.account_id AND a.deleted_at IS NULL WHERE p.account_id=$1 ORDER BY p.created_at,p.id LIMIT 101`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Profile, 0)
	for rows.Next() {
		var p Profile
		var raw []byte
		if err = rows.Scan(&p.ID, &p.Version, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &p.Birth); err != nil {
			return nil, err
		}
		if err = validateBirth(p.Birth); err != nil {
			return nil, err
		}
		out = append(out, p)
		if len(out) > 100 {
			return nil, errors.New("profile count exceeds supported limit")
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
