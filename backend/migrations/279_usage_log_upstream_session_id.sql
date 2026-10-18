-- Actual outbound conversation identity; historical rows remain unknown.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS upstream_session_id VARCHAR(255);
COMMENT ON COLUMN usage_logs.upstream_session_id IS 'Final conversation/session identifier sent to the upstream account; NULL when unavailable';
