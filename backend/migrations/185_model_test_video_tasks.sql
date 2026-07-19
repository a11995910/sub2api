CREATE TABLE IF NOT EXISTS model_test_video_tasks (
    id                    UUID PRIMARY KEY,
    user_id               BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id            BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id              BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    account_id            BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    upstream_task_id      TEXT NOT NULL CHECK (BTRIM(upstream_task_id) <> ''),
    platform              TEXT NOT NULL CHECK (platform IN ('openai', 'grok')),
    model                 TEXT NOT NULL CHECK (BTRIM(model) <> ''),
    prompt                TEXT NOT NULL DEFAULT '',
    resolution            TEXT NOT NULL DEFAULT '',
    duration_seconds      INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),
    reference_image_count INTEGER NOT NULL DEFAULT 0 CHECK (reference_image_count >= 0),
    status                TEXT NOT NULL CHECK (status IN ('queued', 'in_progress', 'completed', 'failed')),
    progress              DOUBLE PRECISION CHECK (progress IS NULL OR (progress >= 0 AND progress <= 100)),
    response_json         JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(response_json) = 'object'),
    error_message         TEXT NOT NULL DEFAULT '',
    last_poll_error       TEXT NOT NULL DEFAULT '',
    last_polled_at        TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    failed_at             TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, api_key_id, upstream_task_id)
);

CREATE INDEX IF NOT EXISTS idx_model_test_video_tasks_user_created
    ON model_test_video_tasks (user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_model_test_video_tasks_pending
    ON model_test_video_tasks (user_id, updated_at DESC)
    WHERE status IN ('queued', 'in_progress');

CREATE INDEX IF NOT EXISTS idx_model_test_video_tasks_terminal_cleanup
    ON model_test_video_tasks (COALESCE(completed_at, failed_at, updated_at))
    WHERE status IN ('completed', 'failed');
