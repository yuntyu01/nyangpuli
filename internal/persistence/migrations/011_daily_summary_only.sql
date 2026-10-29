-- Existing cached views may have been captured after detail unlock. Preserve
-- their summary and identifiers while removing paid content and its metadata.
UPDATE daily_cache SET result = result - 'detail' - 'detail_model' - 'detail_prompt_sha256' - 'detail_usage';
ALTER TABLE daily_cache ADD CONSTRAINT daily_cache_no_paid_detail CHECK (
 NOT (result ?| ARRAY['detail','detail_model','detail_prompt_sha256'])
);
