-- 创意绘图任务持久化。
-- 目的：让生图任务由后端继续执行和回写，避免页面刷新或浏览器中断后丢失状态。

CREATE TABLE IF NOT EXISTS creative_drawing_tasks (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    conversation_id TEXT NOT NULL DEFAULT '',
    turn_id TEXT NOT NULL DEFAULT '',
    mode VARCHAR(16) NOT NULL,
    model VARCHAR(128) NOT NULL,
    prompt TEXT NOT NULL,
    size VARCHAR(64) NOT NULL DEFAULT '',
    image_count INTEGER NOT NULL DEFAULT 1,
    output_format VARCHAR(16) NOT NULL DEFAULT 'png',
    reference_images JSONB NOT NULL DEFAULT '[]'::jsonb,
    request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    error TEXT NOT NULL DEFAULT '',
    result_images JSONB NOT NULL DEFAULT '[]'::jsonb,
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT creative_drawing_tasks_mode_check CHECK (mode IN ('generate', 'edit')),
    CONSTRAINT creative_drawing_tasks_status_check CHECK (status IN ('queued', 'running', 'success', 'error')),
    CONSTRAINT creative_drawing_tasks_image_count_check CHECK (image_count BETWEEN 1 AND 4)
);

CREATE INDEX IF NOT EXISTS idx_creative_drawing_tasks_user_created
    ON creative_drawing_tasks (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_creative_drawing_tasks_status_created
    ON creative_drawing_tasks (status, created_at ASC);
