-- This table and its API are exclusively for local simulated purchases.
CREATE TABLE dev_purchase_transactions (
 transaction_id text PRIMARY KEY,
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 reading_job_id text NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
 status text NOT NULL CHECK (status IN ('pending','purchased','cancelled','refunded')),
 revision bigint NOT NULL CHECK (revision > 0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dev_purchase_entitlement ON dev_purchase_transactions(account_id,reading_job_id,status);
CREATE TRIGGER active_account_dev_purchase BEFORE INSERT OR UPDATE ON dev_purchase_transactions FOR EACH ROW EXECUTE FUNCTION require_active_account();
