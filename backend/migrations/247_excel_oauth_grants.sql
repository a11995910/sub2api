-- Excel 授权与主账号凭据独立存储，避免主凭据刷新或账号编辑覆盖轮换后的令牌。
CREATE TABLE IF NOT EXISTS account_excel_oauth_grants (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    ciphertext TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
