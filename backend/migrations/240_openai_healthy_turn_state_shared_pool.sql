-- OpenAI 健康状态头由所有 OpenAI 账号共用，账号、模型、传输方式和代理只保留为来源统计。
-- 旧表保留用于兼容回滚；有效记录在首次迁移时导入共享池。
CREATE TABLE IF NOT EXISTS openai_healthy_turn_state_pool (
    id BIGSERIAL PRIMARY KEY,
    value_encrypted TEXT,
    value_hash TEXT NOT NULL UNIQUE,
    model TEXT NOT NULL DEFAULT '',
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
    last_http_status INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_openai_healthy_turn_state_pool_available
    ON openai_healthy_turn_state_pool (expires_at, lease_until);

CREATE TABLE IF NOT EXISTS openai_healthy_turn_state_pool_rejections (
    value_hash TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_openai_healthy_turn_state_pool_rejections_expiry
    ON openai_healthy_turn_state_pool_rejections (expires_at);

-- 同一个状态头可能曾经按不同账号、模型或代理各保存过一次，合并计数并只保留一份密文。
WITH source AS (
    SELECT s.*, ROW_NUMBER() OVER (
        PARTITION BY s.value_hash
        ORDER BY s.last_captured_at DESC NULLS LAST, s.account_id, s.scope_key
    ) AS row_number
    FROM openai_healthy_turn_states s
    WHERE s.value_encrypted IS NOT NULL
      AND s.value_hash <> ''
      AND s.expires_at > NOW()
), totals AS (
    SELECT value_hash,
           SUM(captures) AS captures,
           SUM(attempts) AS attempts,
           SUM(successes) AS successes,
           SUM(failures) AS failures,
           MAX(last_captured_at) AS last_captured_at,
           MAX(last_used_at) AS last_used_at,
           MAX(last_success_at) AS last_success_at,
           MAX(last_failure_at) AS last_failure_at
    FROM source
    GROUP BY value_hash
)
INSERT INTO openai_healthy_turn_state_pool (
    value_encrypted, value_hash, model, transport, proxy_id, expires_at,
    captures, attempts, successes, failures, last_captured_at, last_used_at,
    last_success_at, last_failure_at, last_outcome, last_http_status
)
SELECT s.value_encrypted, s.value_hash, s.model, s.transport, s.proxy_id, s.expires_at,
       t.captures, t.attempts, t.successes, t.failures, t.last_captured_at, t.last_used_at,
       t.last_success_at, t.last_failure_at, s.last_outcome, s.last_http_status
FROM source s
JOIN totals t ON t.value_hash = s.value_hash
WHERE s.row_number = 1
ON CONFLICT (value_hash) DO NOTHING;

INSERT INTO openai_healthy_turn_state_pool_rejections (value_hash, expires_at)
SELECT DISTINCT ON (value_hash) value_hash, expires_at
FROM openai_healthy_turn_state_rejections
WHERE value_hash <> '' AND expires_at > NOW()
ORDER BY value_hash, expires_at DESC
ON CONFLICT (value_hash) DO UPDATE SET expires_at = GREATEST(
    openai_healthy_turn_state_pool_rejections.expires_at,
    EXCLUDED.expires_at
);
