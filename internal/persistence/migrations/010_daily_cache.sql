-- The composite FK makes it impossible to attach a cache entry to a different
-- account's profile. Deleting the profile also removes cached private results.
ALTER TABLE profiles ADD CONSTRAINT profiles_id_account_unique UNIQUE (id,account_id);
CREATE TABLE daily_cache (
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 profile_id text NOT NULL,
 profile_version integer NOT NULL CHECK (profile_version > 0),
 local_date date NOT NULL,
 time_zone text NOT NULL,
 engine_version text NOT NULL,
 source_version text NOT NULL,
 prompt_version text NOT NULL,
 model_version text NOT NULL,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(account_id,profile_id,profile_version,local_date,time_zone,engine_version,source_version,prompt_version,model_version),
 FOREIGN KEY(profile_id,account_id) REFERENCES profiles(id,account_id) ON DELETE CASCADE
);
CREATE TRIGGER active_account_daily_cache BEFORE INSERT OR UPDATE ON daily_cache FOR EACH ROW EXECUTE FUNCTION require_active_account();
