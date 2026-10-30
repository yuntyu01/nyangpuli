-- Continuous counseling uses the existing conversations/messages tables.
-- Messages are ordered by a per-conversation sequence; a client request key
-- makes a resent user message resolve to the same turn.
ALTER TABLE conversations ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE messages ADD COLUMN seq bigint;
ALTER TABLE messages ADD COLUMN request_key text;
ALTER TABLE messages ADD COLUMN state text NOT NULL DEFAULT 'complete' CHECK (state IN ('complete','pending','failed','unknown'));
ALTER TABLE messages ADD COLUMN meta jsonb NOT NULL DEFAULT '{}';
ALTER TABLE messages ADD CONSTRAINT messages_role_check CHECK (role IN ('user','assistant'));
CREATE UNIQUE INDEX messages_conversation_seq ON messages(conversation_id,seq);
CREATE UNIQUE INDEX messages_conversation_request ON messages(conversation_id,role,request_key) WHERE request_key IS NOT NULL;
CREATE INDEX conversations_account_updated ON conversations(account_id,updated_at DESC);

-- An explicitly linked reading is copied at link time with its subject label,
-- so later turns never pull a different person's result implicitly.
CREATE TABLE conversation_links (
 id text PRIMARY KEY,
 conversation_id text NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 reading_id text NOT NULL,
 domain text NOT NULL,
 subject text NOT NULL CHECK (subject IN ('self','other','pair')),
 snapshot jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(conversation_id,reading_id)
);
CREATE TRIGGER active_account_conversation_link BEFORE INSERT OR UPDATE ON conversation_links FOR EACH ROW EXECUTE FUNCTION require_active_account();

-- through_seq records the last message the summary covers. A summary built
-- under an older memory generation is ignored until it is rebuilt.
CREATE TABLE conversation_summaries (
 conversation_id text PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 content text NOT NULL,
 through_seq bigint NOT NULL CHECK (through_seq > 0),
 memory_generation bigint NOT NULL,
 meta jsonb NOT NULL DEFAULT '{}',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER active_account_conversation_summary BEFORE INSERT OR UPDATE ON conversation_summaries FOR EACH ROW EXECUTE FUNCTION require_active_account();

-- Memory classes: a correctable fact (latest statement or correction wins) or an
-- episode (event, feeling or experience kept per time, subject and situation).
-- origin separates user-confirmed input, statements quoted from a user message,
-- and AI inferences. AI inferences stay unconfirmed and are never used as facts.
ALTER TABLE memories ADD COLUMN memory_class text NOT NULL DEFAULT 'fact' CHECK (memory_class IN ('fact','episode'));
ALTER TABLE memories ADD COLUMN origin text NOT NULL DEFAULT 'user_confirmed' CHECK (origin IN ('user_confirmed','user_stated','ai_inference'));
ALTER TABLE memories ADD COLUMN subject text;
ALTER TABLE memories ADD COLUMN situation text;

-- Deleting a memory taken from a message suppresses the same key from that
-- message, so retries or later summaries cannot restore it.
CREATE TABLE memory_suppressions (
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 source_message_id text NOT NULL,
 kind text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(account_id,source_message_id,kind)
);
CREATE TRIGGER active_account_memory_suppression BEFORE INSERT OR UPDATE ON memory_suppressions FOR EACH ROW EXECUTE FUNCTION require_active_account();
