-- File fingerprints prevent the same legacy source being imported under a
-- different account map. Fingerprints contain no raw source content.
-- PostgreSQL timestamps have microsecond precision; keep the exact original
-- recording instant separately when importing JSON nanosecond timestamps.
ALTER TABLE memories ADD COLUMN legacy_recorded_at text;
CREATE TABLE legacy_import_sources (
 source_kind text NOT NULL CHECK (source_kind IN ('memory','readings')),
 source_sha256 text NOT NULL,
 mapping_sha256 text NOT NULL,
 imported_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source_kind,source_sha256)
);
