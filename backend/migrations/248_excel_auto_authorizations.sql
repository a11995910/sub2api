-- 登录资料仅以密文保存；任务租约和修订号防止旧任务覆盖新的配置或授权。
CREATE TABLE IF NOT EXISTS account_excel_auto_authorizations (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    ciphertext TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1,
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','authorized','failed','disabled')),
    run_id TEXT NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_excel_auto_authorizations_due
    ON account_excel_auto_authorizations(next_attempt_at) WHERE enabled;
