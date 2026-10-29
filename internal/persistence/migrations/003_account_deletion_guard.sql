-- Keep a tombstone so stale authenticated workers cannot recreate records.
-- The row lock serializes account deletion and writes, including direct SQL.
CREATE FUNCTION require_active_account() RETURNS trigger AS $$
DECLARE account_deleted timestamptz;
BEGIN
 IF NEW.account_id IS NULL THEN RETURN NEW; END IF;
 SELECT deleted_at INTO account_deleted FROM accounts WHERE id=NEW.account_id FOR KEY SHARE;
 IF NOT FOUND OR account_deleted IS NOT NULL THEN
  RAISE EXCEPTION 'account unavailable' USING ERRCODE = '23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER active_account_identity BEFORE INSERT OR UPDATE ON account_identities FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_session BEFORE INSERT OR UPDATE ON auth_sessions FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_profile BEFORE INSERT OR UPDATE ON profiles FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_conversation BEFORE INSERT OR UPDATE ON conversations FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_memory BEFORE INSERT OR UPDATE ON memories FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_reading BEFORE INSERT OR UPDATE ON readings FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_tarot BEFORE INSERT OR UPDATE ON tarot_sessions FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_purchase BEFORE INSERT OR UPDATE ON purchase_transactions FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_entitlement BEFORE INSERT OR UPDATE ON entitlements FOR EACH ROW EXECUTE FUNCTION require_active_account();
CREATE TRIGGER active_account_job BEFORE INSERT OR UPDATE ON generation_jobs FOR EACH ROW EXECUTE FUNCTION require_active_account();
