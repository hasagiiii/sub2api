-- Persist the project-facing error code for terminal async media tasks.
ALTER TABLE async_media_tasks
    ADD COLUMN IF NOT EXISTS error_code VARCHAR(128);
