-- 分组限时活动折扣：活动期间最终计费倍率在（用户专属 ?? 分组默认）× 高峰因子的基础上
-- 再乘以 promo_discount_rate（如 0.95 表示 95 折）。到期后按请求时刻现算自动恢复原倍率，
-- 不依赖定时任务。字段语义与峰值倍率一致，适用于所有分组类型（不限于订阅分组）。
ALTER TABLE groups ADD COLUMN IF NOT EXISTS promo_discount_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS promo_discount_start TIMESTAMPTZ;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS promo_discount_end TIMESTAMPTZ;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS promo_discount_rate DECIMAL(10,4) NOT NULL DEFAULT 1.0;

-- 活动折扣参与 API-key 认证快照与计费口径，扩展 193 号迁移建立的持久失效触发器：
-- 直接 SQL 修改分组活动字段时也能强制失效该分组下所有密钥的缓存快照。
-- 函数体基于 193_group_profit_control_auth_cache_invalidation.sql 的最新版本。
CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_group_id BIGINT;
BEGIN
    target_group_id := OLD.id;
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.is_exclusive IS NOT DISTINCT FROM NEW.is_exclusive
       AND OLD.allow_image_generation IS NOT DISTINCT FROM NEW.allow_image_generation
       AND OLD.platform IS NOT DISTINCT FROM NEW.platform
       AND OLD.subscription_type IS NOT DISTINCT FROM NEW.subscription_type
       AND OLD.rate_multiplier IS NOT DISTINCT FROM NEW.rate_multiplier
       AND OLD.peak_rate_enabled IS NOT DISTINCT FROM NEW.peak_rate_enabled
       AND OLD.peak_start IS NOT DISTINCT FROM NEW.peak_start
       AND OLD.peak_end IS NOT DISTINCT FROM NEW.peak_end
       AND OLD.peak_rate_multiplier IS NOT DISTINCT FROM NEW.peak_rate_multiplier
       AND OLD.promo_discount_enabled IS NOT DISTINCT FROM NEW.promo_discount_enabled
       AND OLD.promo_discount_start IS NOT DISTINCT FROM NEW.promo_discount_start
       AND OLD.promo_discount_end IS NOT DISTINCT FROM NEW.promo_discount_end
       AND OLD.promo_discount_rate IS NOT DISTINCT FROM NEW.promo_discount_rate
       AND OLD.profit_control_enabled IS NOT DISTINCT FROM NEW.profit_control_enabled
       AND OLD.profit_min_margin IS NOT DISTINCT FROM NEW.profit_min_margin
       AND OLD.profit_safety_buffer IS NOT DISTINCT FROM NEW.profit_safety_buffer
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.group_id = target_group_id
      AND k.deleted_at IS NULL
      AND k.key <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
