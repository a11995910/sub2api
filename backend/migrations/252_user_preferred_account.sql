-- 用户优先上游账号仅影响组内调度；0 表示未设置。
-- 保留软删除账号的配置值，运行时校验可用性并自动回退。
ALTER TABLE users ADD COLUMN IF NOT EXISTS preferred_account_id BIGINT NOT NULL DEFAULT 0
    CHECK (preferred_account_id >= 0);

-- 同步失效失败时，沿用 outbox 异步补偿，避免继续使用旧偏好。
CREATE OR REPLACE FUNCTION enqueue_user_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_user_id BIGINT;
BEGIN
    target_user_id := OLD.id;
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.role IS NOT DISTINCT FROM NEW.role
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at
       AND OLD.preferred_account_id IS NOT DISTINCT FROM NEW.preferred_account_id THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.user_id = target_user_id
      AND k.deleted_at IS NULL
      AND k.key <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

