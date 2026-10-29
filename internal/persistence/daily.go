package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/yuntyu01/nyangpuli-lab/internal/daily"
)

var ErrDailyNotFound = errors.New("daily cache entry not found")

func (s Store) GetDaily(ctx context.Context, k daily.Key) (json.RawMessage, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT c.result FROM daily_cache c JOIN profiles p ON p.id=c.profile_id AND p.account_id=c.account_id AND p.version=c.profile_version JOIN accounts a ON a.id=c.account_id AND a.deleted_at IS NULL WHERE c.account_id=$1 AND c.profile_id=$2 AND c.profile_version=$3 AND c.local_date=$4::date AND c.time_zone=$5 AND c.engine_version=$6 AND c.source_version=$7 AND c.prompt_version=$8 AND c.model_version=$9`, k.AccountID, k.ProfileID, k.ProfileVersion, k.Date, k.TimeZone, k.EngineVersion, k.SourceVersion, k.PromptVersion, k.ModelVersion).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDailyNotFound
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// PutDaily keeps the first committed value for this key. Generate outside the
// transaction; concurrent generators may incur duplicate work but never replace
// the saved result. Profile ownership and current version are checked in SQL.
func (s Store) PutDaily(ctx context.Context, k daily.Key, result json.RawMessage) (json.RawMessage, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	if !json.Valid(result) || len(result) > 1<<20 {
		return nil, errors.New("invalid daily result")
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO daily_cache(account_id,profile_id,profile_version,local_date,time_zone,engine_version,source_version,prompt_version,model_version,result)
 SELECT p.account_id,p.id,p.version,$4::date,$5,$6,$7,$8,$9,$10 FROM profiles p JOIN accounts a ON a.id=p.account_id AND a.deleted_at IS NULL WHERE p.account_id=$1 AND p.id=$2 AND p.version=$3 ON CONFLICT DO NOTHING`, k.AccountID, k.ProfileID, k.ProfileVersion, k.Date, k.TimeZone, k.EngineVersion, k.SourceVersion, k.PromptVersion, k.ModelVersion, []byte(result))
	if err != nil {
		return nil, err
	}
	return s.GetDaily(ctx, k)
}
