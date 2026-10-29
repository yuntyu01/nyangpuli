-- Local development only; no store transaction or paid entitlement is implied.
CREATE TABLE dev_reading_unlocks (
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 reading_job_id text NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
 PRIMARY KEY(account_id,reading_job_id)
);
CREATE TRIGGER active_account_dev_unlock BEFORE INSERT OR UPDATE ON dev_reading_unlocks FOR EACH ROW EXECUTE FUNCTION require_active_account();
