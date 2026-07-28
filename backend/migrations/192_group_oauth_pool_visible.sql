-- 管理员可按分组决定是否向有权使用该分组的用户公开 OAuth 号池状态。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS oauth_pool_visible BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN groups.oauth_pool_visible IS '是否向有权访问该分组的用户公开 OAuth 账号名称与缓存额度快照';
