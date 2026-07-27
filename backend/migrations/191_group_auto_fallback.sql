-- 分组同模型账号耗尽时可按管理员配置进入承接链；用户可在 API Key 上独立关闭。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS auto_fallback_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_groups_auto_fallback_group_id
    ON groups(auto_fallback_group_id)
    WHERE deleted_at IS NULL AND auto_fallback_group_id IS NOT NULL;

COMMENT ON COLUMN groups.auto_fallback_group_id IS '当前分组支持的模型无可用账号时自动承接的分组 ID';

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS auto_group_fallback_enabled BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN api_keys.auto_group_fallback_enabled IS '是否允许在当前分组支持的模型无可用账号时自动切换到承接分组';
