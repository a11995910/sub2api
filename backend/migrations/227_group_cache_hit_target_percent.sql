-- 将固定“四分之一划入输入”升级为可配置的累计缓存命中率目标。
-- 旧开关字段继续保留，避免破坏已部署客户端与历史配置；其语义改为启用目标控制。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS cache_hit_target_percent DECIMAL(5,2) NOT NULL DEFAULT 90.00;

COMMENT ON COLUMN groups.cache_hit_quarter_to_input_enabled IS
    '是否启用按用户和分组累计控制缓存命中率；字段名为历史兼容保留。';

COMMENT ON COLUMN groups.cache_hit_target_percent IS
    '缓存命中率目标上限百分比，范围 0.01 至 100.00；启用累计控制后生效。';
