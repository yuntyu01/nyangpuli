ALTER TABLE accounts ADD COLUMN memory_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE generation_jobs ADD COLUMN memory_generation bigint;
-- Older counseling records did not record memory dependencies; conservatively
-- treat them as memory-dependent so the next edit invalidates their snapshot.
UPDATE generation_jobs SET memory_generation=0 WHERE evidence->>'domain'='counsel';
