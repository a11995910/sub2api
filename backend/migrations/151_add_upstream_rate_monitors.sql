-- 上游倍率监控：保存同类 Sub2API 上游站点配置，并缓存最近一次分组倍率快照。
--
-- 设计要点：
--   - password_encrypted 存放 AES-256-GCM 密文，前端永不回显明文密码。
--   - last_snapshot 仅保存上游分组只读快照，刷新失败时保留上一份可用数据。
--   - last_status 用于页面快速区分未刷新、刷新成功与刷新失败。

CREATE TABLE IF NOT EXISTS upstream_rate_monitors (
    id                 BIGSERIAL PRIMARY KEY,
    name               VARCHAR(100) NOT NULL,
    base_url           VARCHAR(500) NOT NULL,
    username           VARCHAR(255) NOT NULL,
    password_encrypted TEXT NOT NULL,
    enabled            BOOLEAN NOT NULL DEFAULT TRUE,
    last_checked_at    TIMESTAMPTZ,
    last_status        VARCHAR(20) NOT NULL DEFAULT 'unknown',
    last_error         TEXT NOT NULL DEFAULT '',
    last_group_count   INT NOT NULL DEFAULT 0,
    last_snapshot      JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by         BIGINT NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT upstream_rate_monitors_status_check
        CHECK (last_status IN ('unknown', 'success', 'failed')),
    CONSTRAINT upstream_rate_monitors_group_count_check
        CHECK (last_group_count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_upstream_rate_monitors_enabled
    ON upstream_rate_monitors (enabled);
CREATE INDEX IF NOT EXISTS idx_upstream_rate_monitors_updated_at
    ON upstream_rate_monitors (updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_upstream_rate_monitors_base_url
    ON upstream_rate_monitors (base_url);
