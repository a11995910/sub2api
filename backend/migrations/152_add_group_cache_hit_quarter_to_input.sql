-- Add group switch for billing cache-hit tokens as part of input.
-- Disabled by default to keep existing group billing behavior unchanged.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS cache_hit_quarter_to_input_enabled boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN groups.cache_hit_quarter_to_input_enabled IS
    '启用后将每次请求缓存读取 token 的四分之一划入输入 token 重新计费；历史用量不回填。';
