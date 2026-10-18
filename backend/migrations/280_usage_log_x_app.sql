-- Preserve the submitting application's HTTP header across synchronous and async usage writes.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS x_app TEXT;
ALTER TABLE async_media_tasks ADD COLUMN IF NOT EXISTS x_app TEXT;
ALTER TABLE async_video_tasks ADD COLUMN IF NOT EXISTS x_app TEXT;
COMMENT ON COLUMN usage_logs.x_app IS 'Application identifier from the inbound x-app HTTP header';
