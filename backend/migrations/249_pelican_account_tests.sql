-- 每个账号只保存最新一轮测试；运行标识防止取消或过期的任务覆盖新结果。
CREATE TABLE IF NOT EXISTS pelican_account_test_results (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'success', 'failed', 'stopped')),
    model_id TEXT NOT NULL DEFAULT '',
    reasoning_effort TEXT NOT NULL DEFAULT '',
    response_text TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    running_until TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pelican_account_tests_running
    ON pelican_account_test_results (running_until) WHERE status = 'running';
COMMENT ON TABLE pelican_account_test_results IS 'OAuth 账号鹈鹕测试最新结果，不进入用户展示；重测原位覆盖';
