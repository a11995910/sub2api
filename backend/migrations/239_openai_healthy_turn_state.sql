-- 状态头与效果统计独立存储，不修改账号凭据或业务限制。
CREATE TABLE IF NOT EXISTS openai_healthy_turn_states (
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    scope_key TEXT NOT NULL,
    model TEXT NOT NULL,
    transport TEXT NOT NULL,
    proxy_id BIGINT NOT NULL DEFAULT 0,
    value_encrypted TEXT,
    value_hash TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    captures BIGINT NOT NULL DEFAULT 0,
    attempts BIGINT NOT NULL DEFAULT 0,
    successes BIGINT NOT NULL DEFAULT 0,
    failures BIGINT NOT NULL DEFAULT 0,
    last_captured_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    last_outcome TEXT NOT NULL DEFAULT '',
    last_http_status INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, scope_key)
);

-- 失败摘要阻止迟到响应重新写入已淘汰的头，不保存失败头原文。
CREATE TABLE IF NOT EXISTS openai_healthy_turn_state_rejections (
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    scope_key TEXT NOT NULL,
    value_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_id, scope_key, value_hash)
);
CREATE INDEX IF NOT EXISTS idx_healthy_turn_state_rejections_expiry
    ON openai_healthy_turn_state_rejections (expires_at);

-- 只保留测试结果和出口 ID，不保存代理凭据、对话或头内容。
CREATE TABLE IF NOT EXISTS openai_healthy_turn_state_probes (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    transport TEXT NOT NULL,
    proxy_id BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    http_status INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_healthy_turn_state_probes_account
    ON openai_healthy_turn_state_probes (account_id, id DESC);
