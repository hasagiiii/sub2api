-- Preserve the native provider result for the final async image result endpoint.
ALTER TABLE async_media_tasks
    ADD COLUMN IF NOT EXISTS result_payload JSONB;
