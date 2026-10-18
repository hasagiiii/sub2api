-- Durable execution journal for synchronous providers behind the async model facade.
ALTER TABLE async_media_tasks ADD COLUMN IF NOT EXISTS execution_kind TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS inline_image_executions (
    task_id BIGINT PRIMARY KEY REFERENCES async_media_tasks(id) ON DELETE CASCADE,
    request_payload JSONB NOT NULL,
    request_hash TEXT NOT NULL,
    billing_type SMALLINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    result JSONB,
    failure_reason TEXT,
    failure_definitive BOOLEAN NOT NULL DEFAULT false,
    lease_token TEXT,
    lease_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (state IN ('pending','generating','result_ready','stored','failure_ready','settled','failed','unknown'))
);
