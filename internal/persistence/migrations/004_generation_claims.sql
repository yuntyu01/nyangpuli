ALTER TABLE generation_jobs ADD COLUMN request_hash text;
ALTER TABLE generation_jobs ADD COLUMN deletion_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE generation_jobs ADD COLUMN evidence jsonb;
ALTER TABLE generation_jobs ADD COLUMN result jsonb;
