CREATE TABLE password_credentials (
 username text PRIMARY KEY,
 account_id text NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
 salt bytea NOT NULL,
 password_hash bytea NOT NULL,
 failed_attempts integer NOT NULL DEFAULT 0,
 locked_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER active_account_password BEFORE INSERT OR UPDATE ON password_credentials FOR EACH ROW EXECUTE FUNCTION require_active_account();
