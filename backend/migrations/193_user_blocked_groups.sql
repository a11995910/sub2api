-- 用户可以单独屏蔽公开标准分组；未写入记录时继续保持公开分组默认可用。
CREATE TABLE IF NOT EXISTS user_blocked_groups (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id   BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_blocked_groups_group_id
    ON user_blocked_groups(group_id);

COMMENT ON TABLE user_blocked_groups IS '用户公开标准分组黑名单；命中后不可展示、绑定、调用或自动承接到该分组';
COMMENT ON COLUMN user_blocked_groups.user_id IS '被限制的用户 ID';
COMMENT ON COLUMN user_blocked_groups.group_id IS '被屏蔽的公开标准分组 ID';

-- 黑名单会影响初始绑定分组和自动承接目标，因此变更时失效该用户的全部 API Key 认证缓存。
CREATE OR REPLACE FUNCTION enqueue_blocked_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.user_id = CASE WHEN TG_OP = 'DELETE' THEN OLD.user_id ELSE NEW.user_id END
      AND k.deleted_at IS NULL
      AND k.key <> '';

    IF TG_OP = 'UPDATE' AND OLD.user_id IS DISTINCT FROM NEW.user_id THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
        FROM api_keys AS k
        WHERE k.user_id = OLD.user_id
          AND k.deleted_at IS NULL
          AND k.key <> '';
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_blocked_groups_auth_cache_invalidation ON user_blocked_groups;
CREATE TRIGGER trg_user_blocked_groups_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON user_blocked_groups
FOR EACH ROW EXECUTE FUNCTION enqueue_blocked_group_auth_cache_invalidation();
