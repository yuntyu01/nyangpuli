-- All private data is scoped to an account; physical deletion cascades except
-- transaction audit records, which deliberately contain no counseling content.
CREATE TABLE accounts (
 id text PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now(),
 deleted_at timestamptz, deletion_generation bigint NOT NULL DEFAULT 0
);
CREATE TABLE account_identities (
 provider text NOT NULL, subject text NOT NULL, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 PRIMARY KEY(provider, subject)
);
CREATE TABLE auth_sessions (
 token_hash bytea PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_sessions_account ON auth_sessions(account_id);
CREATE TABLE profiles (
 id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 version integer NOT NULL DEFAULT 1, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE conversations (id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE messages (id text PRIMARY KEY, conversation_id text NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, role text NOT NULL, content text NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE memories (
 id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 kind text NOT NULL, value jsonb NOT NULL, source_message_id text REFERENCES messages(id) ON DELETE SET NULL,
 occurred_at timestamptz, recorded_at timestamptz NOT NULL DEFAULT now(), valid boolean NOT NULL DEFAULT true,
 confirmed boolean NOT NULL DEFAULT false
);
CREATE INDEX memories_current ON memories(account_id,kind,occurred_at DESC) WHERE valid;
CREATE TABLE memory_history (id bigserial PRIMARY KEY, memory_id text NOT NULL REFERENCES memories(id) ON DELETE CASCADE, value jsonb NOT NULL, changed_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE readings (
 id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 request_key text NOT NULL, request_hash text NOT NULL, status text NOT NULL, evidence jsonb NOT NULL,
 summary text, detail text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(account_id,request_key)
);
CREATE TABLE tarot_sessions (id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, question text NOT NULL, cards jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE products (id text PRIMARY KEY, active boolean NOT NULL DEFAULT true, configuration jsonb NOT NULL DEFAULT '{}');
CREATE TABLE purchase_transactions (
 provider text NOT NULL, transaction_id text NOT NULL, account_id text REFERENCES accounts(id) ON DELETE SET NULL,
 product_id text NOT NULL REFERENCES products(id), status text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(provider,transaction_id)
);
CREATE TABLE entitlements (account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, reading_id text NOT NULL REFERENCES readings(id) ON DELETE CASCADE, provider text NOT NULL, transaction_id text NOT NULL, active boolean NOT NULL DEFAULT true, PRIMARY KEY(account_id,reading_id), FOREIGN KEY(provider,transaction_id) REFERENCES purchase_transactions(provider,transaction_id));
CREATE TABLE generation_jobs (
 id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 reading_id text REFERENCES readings(id) ON DELETE CASCADE, request_key text NOT NULL, state text NOT NULL,
 model text, prompt_version text, source_version text, usage jsonb, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(account_id,request_key)
);
CREATE TABLE knowledge_sources (id text PRIMARY KEY, version text NOT NULL, license text NOT NULL, provenance jsonb NOT NULL, evaluation_only boolean NOT NULL DEFAULT false);
CREATE TABLE knowledge_chunks (id text PRIMARY KEY, source_id text NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE, content text NOT NULL, metadata jsonb NOT NULL DEFAULT '{}');
