-- 为缓存命中率累计控制增加可配置的时间半衰期，避免早期流量永久影响后续控制。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS cache_hit_half_life_days DECIMAL(8,2) NOT NULL DEFAULT 1.00;

COMMENT ON COLUMN groups.cache_hit_half_life_days IS
    '缓存命中率累计状态的时间半衰期（天）；历史提示词与缓存读取权重按时间指数衰减。';
