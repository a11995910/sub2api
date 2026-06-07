-- 为用户专属分组授权增加限时能力。
-- 既有 user_allowed_groups 数据一律视为管理员手动永久授权。

ALTER TABLE user_allowed_groups
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NULL;

ALTER TABLE user_allowed_groups
    ADD COLUMN IF NOT EXISTS source VARCHAR(50) NOT NULL DEFAULT 'manual';

ALTER TABLE user_allowed_groups
    ADD COLUMN IF NOT EXISTS source_order_id BIGINT NULL;

ALTER TABLE user_allowed_groups
    ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT '';

ALTER TABLE user_allowed_groups
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_user_allowed_groups_expires_at
    ON user_allowed_groups(expires_at)
    WHERE expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_allowed_groups_source_expires_at
    ON user_allowed_groups(source, expires_at)
    WHERE expires_at IS NOT NULL;

COMMENT ON COLUMN user_allowed_groups.expires_at IS '专属分组授权过期时间；NULL 表示永久授权';
COMMENT ON COLUMN user_allowed_groups.source IS '授权来源：manual 为管理员手动授权，affiliate_payment_reward 为邀请支付奖励';
COMMENT ON COLUMN user_allowed_groups.source_order_id IS '产生该限时授权的支付订单 ID';
COMMENT ON COLUMN user_allowed_groups.notes IS '授权备注';
COMMENT ON COLUMN user_allowed_groups.updated_at IS '授权更新时间';
