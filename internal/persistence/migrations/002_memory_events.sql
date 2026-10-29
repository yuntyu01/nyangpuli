ALTER TABLE memories ADD COLUMN source_event_id text;
ALTER TABLE memories ADD COLUMN supersedes text;
CREATE UNIQUE INDEX memories_event_unique ON memories(account_id,kind,source_event_id) WHERE source_event_id IS NOT NULL;
