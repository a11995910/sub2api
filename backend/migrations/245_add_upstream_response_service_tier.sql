ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS upstream_response_service_tier TEXT;

COMMENT ON COLUMN usage_logs.upstream_response_service_tier IS
    '上游响应声明的原始档位，与最终计费档位独立；历史或未声明时为空';
