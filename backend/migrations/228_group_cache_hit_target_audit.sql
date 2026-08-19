-- 为累计缓存命中率控制增加容差带、状态审计和可追溯原始 Token。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS cache_hit_target_tolerance_percent DECIMAL(5,2) NOT NULL DEFAULT 0.50;

COMMENT ON COLUMN groups.cache_hit_target_tolerance_percent IS
    '缓存命中率目标容差百分比；累计值超过目标加容差时回调到目标。';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS cache_hit_original_input_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_hit_original_cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_hit_shifted_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_hit_target_percent DECIMAL(5,2),
    ADD COLUMN IF NOT EXISTS cache_hit_target_tolerance_percent DECIMAL(5,2),
    ADD COLUMN IF NOT EXISTS cache_hit_cumulative_prompt_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_hit_cumulative_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_hit_cumulative_percent DECIMAL(7,4),
    ADD COLUMN IF NOT EXISTS cache_hit_state_version BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN usage_logs.cache_hit_original_input_tokens IS
    '缓存命中率控制前的普通输入 token；未启用控制时为 0。';
COMMENT ON COLUMN usage_logs.cache_hit_original_cache_read_tokens IS
    '缓存命中率控制前的缓存读取 token；未启用控制时为 0。';
COMMENT ON COLUMN usage_logs.cache_hit_shifted_tokens IS
    '本次从缓存读取划入普通输入的 token。';
COMMENT ON COLUMN usage_logs.cache_hit_target_percent IS
    '本次请求使用的缓存命中率目标百分比。';
COMMENT ON COLUMN usage_logs.cache_hit_target_tolerance_percent IS
    '本次请求使用的缓存命中率容差百分比。';
COMMENT ON COLUMN usage_logs.cache_hit_cumulative_prompt_tokens IS
    '本次调整后的状态累计提示词 token。';
COMMENT ON COLUMN usage_logs.cache_hit_cumulative_cache_read_tokens IS
    '本次调整后的状态累计缓存读取 token。';
COMMENT ON COLUMN usage_logs.cache_hit_cumulative_percent IS
    '本次调整后的累计缓存命中率百分比。';
COMMENT ON COLUMN usage_logs.cache_hit_state_version IS
    '缓存命中率累计状态代次，来自分组更新时间。';
