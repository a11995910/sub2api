-- 糖果测智配置和批次租约独立保存；配置修改不覆盖已启动批次的快照。
CREATE TABLE IF NOT EXISTS candy_test_settings (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    config JSONB NOT NULL,
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    run_id TEXT NOT NULL DEFAULT '',
    running_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS candy_test_results (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    account_name TEXT NOT NULL,
    run_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'normal', 'degraded', 'error')),
    answer TEXT NOT NULL DEFAULT '',
    response_text TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL,
    reasoning_effort TEXT NOT NULL DEFAULT '',
    settings_snapshot JSONB NOT NULL,
    group_action JSONB,
    group_error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    running_until TIMESTAMPTZ NOT NULL,
    UNIQUE (run_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_candy_test_results_running
    ON candy_test_results (running_until) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS idx_candy_test_results_account
    ON candy_test_results (account_id, id DESC);
COMMENT ON TABLE candy_test_settings IS '账号测智的糖果测试配置、北京时间每日定时与跨实例批次租约';
COMMENT ON TABLE candy_test_results IS '糖果测试历史、配置快照与事务内增量调组结果';
