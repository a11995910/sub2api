-- 健康状态头按账号和模型独立保存；传输方式与代理仅保留为采集来源。
-- 旧共享池没有可靠的账号归属，原表完整保留，不把历史记录或累计数分配给任何账号。
CREATE TABLE IF NOT EXISTS openai_healthy_turn_state_account_model_pool (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    value_encrypted TEXT,
    value_hash TEXT NOT NULL,
    transport TEXT NOT NULL DEFAULT 'http',
    proxy_id BIGINT NOT NULL DEFAULT 0,
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
    UNIQUE (account_id, model, value_hash)
);
CREATE INDEX IF NOT EXISTS idx_healthy_turn_state_account_model_available
    ON openai_healthy_turn_state_account_model_pool (account_id, model, expires_at, lease_until);
CREATE INDEX IF NOT EXISTS idx_healthy_turn_state_account_model_recent
    ON openai_healthy_turn_state_account_model_pool (account_id, last_captured_at DESC NULLS LAST, id DESC);

CREATE TABLE IF NOT EXISTS openai_healthy_turn_state_account_model_rejections (
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    value_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_id, model, value_hash)
);
CREATE INDEX IF NOT EXISTS idx_healthy_turn_state_account_model_rejections_expiry
    ON openai_healthy_turn_state_account_model_rejections (expires_at);
