-- A deletion marker prevents delayed past facts from silently becoming current.
-- It contains only the key and deletion time, not the deleted value or event ID.
CREATE TABLE memory_deletion_watermarks (
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 kind text NOT NULL,
 deleted_at timestamptz NOT NULL,
 PRIMARY KEY(account_id,kind)
);
CREATE TRIGGER active_account_memory_watermark BEFORE INSERT OR UPDATE ON memory_deletion_watermarks FOR EACH ROW EXECUTE FUNCTION require_active_account();
