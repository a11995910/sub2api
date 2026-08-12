-- Balance reservations move users.balance into users.frozen_balance atomically with the task row.
CREATE TABLE IF NOT EXISTS video_task_billings (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    upstream_task_id TEXT,
    platform VARCHAR(32) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
    group_id BIGINT,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    model TEXT NOT NULL,
    upstream_model TEXT,
    resolution VARCHAR(16) NOT NULL DEFAULT '',
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    reference_image_count INTEGER NOT NULL DEFAULT 0,
    usage_context_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    estimated_cost DECIMAL(20,8) NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20,8),
    task_status VARCHAR(32) NOT NULL,
    billing_status VARCHAR(32) NOT NULL,
    response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    poll_count INTEGER NOT NULL DEFAULT 0,
    last_polled_at TIMESTAMPTZ,
    next_poll_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_poll_error TEXT,
    submission_deadline TIMESTAMPTZ,
    terminal_at TIMESTAMPTZ,
    claimed_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_video_task_billings_platform_task
    ON video_task_billings (platform, upstream_task_id)
    WHERE upstream_task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_video_task_billings_due
    ON video_task_billings (billing_status, task_status, next_poll_at);
CREATE INDEX IF NOT EXISTS idx_video_task_billings_user_created
    ON video_task_billings (user_id, created_at DESC, id DESC);
