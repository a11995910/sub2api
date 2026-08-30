-- 内置抽奖功能：按每日 Token 用量累计抽奖机会，并以余额作为奖品。

CREATE TABLE IF NOT EXISTS sub2api_lottery_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    usage_threshold_tokens BIGINT NOT NULL DEFAULT 1000000 CHECK (usage_threshold_tokens > 0),
    award_mode VARCHAR(32) NOT NULL DEFAULT 'daily_once' CHECK (award_mode IN ('daily_once', 'per_threshold')),
    prizes JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(prizes) = 'array'),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO sub2api_lottery_settings (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- 门槛按自然日版本化，避免后续调整门槛时重算已经结束的日期。
CREATE TABLE IF NOT EXISTS sub2api_lottery_rule_versions (
    id BIGSERIAL PRIMARY KEY,
    effective_date DATE NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL,
    usage_threshold_tokens BIGINT NOT NULL CHECK (usage_threshold_tokens > 0),
    award_mode VARCHAR(32) NOT NULL CHECK (award_mode IN ('daily_once', 'per_threshold')),
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sub2api_lottery_daily_awards (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    usage_date DATE NOT NULL,
    usage_tokens BIGINT NOT NULL CHECK (usage_tokens >= 0),
    threshold_tokens BIGINT NOT NULL CHECK (threshold_tokens > 0),
    award_mode VARCHAR(32) NOT NULL CHECK (award_mode IN ('daily_once', 'per_threshold')),
    awarded_chances BIGINT NOT NULL CHECK (awarded_chances > 0),
    awarded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, usage_date)
);

CREATE TABLE IF NOT EXISTS sub2api_lottery_user_states (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    available_chances BIGINT NOT NULL DEFAULT 0 CHECK (available_chances >= 0),
    total_earned BIGINT NOT NULL DEFAULT 0 CHECK (total_earned >= 0),
    total_drawn BIGINT NOT NULL DEFAULT 0 CHECK (total_drawn >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sub2api_lottery_draws (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prize_id VARCHAR(80) NOT NULL,
    prize_name VARCHAR(160) NOT NULL,
    reward_amount DECIMAL(20,8) NOT NULL DEFAULT 0 CHECK (reward_amount >= 0),
    probability_basis_points INTEGER NOT NULL CHECK (probability_basis_points BETWEEN 0 AND 10000),
    random_roll INTEGER NOT NULL CHECK (random_roll BETWEEN 0 AND 9999),
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    chance_before BIGINT NOT NULL CHECK (chance_before > 0),
    chance_after BIGINT NOT NULL CHECK (chance_after >= 0),
    balance_after DECIMAL(20,8) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sub2api_lottery_draws_user_created
    ON sub2api_lottery_draws(user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_sub2api_lottery_draws_created
    ON sub2api_lottery_draws(created_at DESC, id DESC);

-- 总开关属于系统功能开关；抽奖设置表只保留一份事务内同步的运行快照。
INSERT INTO settings (key, value, updated_at)
VALUES ('lottery_enabled', 'false', NOW())
ON CONFLICT (key) DO NOTHING;

UPDATE sub2api_lottery_settings
SET enabled = COALESCE((
        SELECT LOWER(BTRIM(value)) = 'true'
        FROM settings
        WHERE key = 'lottery_enabled'
    ), FALSE)
WHERE id = 1;

-- 从迁移当天开始记录关闭区间，防止首次启用时回溯迁移前或关闭期间的用量。
INSERT INTO sub2api_lottery_rule_versions (
    effective_date, enabled, usage_threshold_tokens, award_mode
)
SELECT
    (NOW() AT TIME ZONE current_setting('TimeZone'))::date,
    enabled,
    usage_threshold_tokens,
    award_mode
FROM sub2api_lottery_settings
WHERE id = 1
ON CONFLICT (effective_date) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    usage_threshold_tokens = EXCLUDED.usage_threshold_tokens,
    award_mode = EXCLUDED.award_mode,
    updated_at = NOW();

CREATE OR REPLACE FUNCTION sync_sub2api_lottery_enabled()
RETURNS TRIGGER AS $$
DECLARE
    new_enabled BOOLEAN := LOWER(BTRIM(NEW.value)) = 'true';
    config_threshold BIGINT;
    config_award_mode VARCHAR(32);
BEGIN
    UPDATE sub2api_lottery_settings
    SET enabled = new_enabled,
        version = version + CASE WHEN enabled IS DISTINCT FROM new_enabled THEN 1 ELSE 0 END,
        updated_at = CASE WHEN enabled IS DISTINCT FROM new_enabled THEN NOW() ELSE updated_at END
    WHERE id = 1
    RETURNING usage_threshold_tokens, award_mode
    INTO config_threshold, config_award_mode;

    IF TG_OP = 'INSERT' OR OLD.value IS DISTINCT FROM NEW.value THEN
        INSERT INTO sub2api_lottery_rule_versions (
            effective_date, enabled, usage_threshold_tokens, award_mode
        ) VALUES (
            (NOW() AT TIME ZONE current_setting('TimeZone'))::date,
            new_enabled,
            config_threshold,
            config_award_mode
        )
        ON CONFLICT (effective_date) DO UPDATE SET
            enabled = EXCLUDED.enabled,
            usage_threshold_tokens = EXCLUDED.usage_threshold_tokens,
            award_mode = EXCLUDED.award_mode,
            updated_at = NOW();
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sync_sub2api_lottery_enabled ON settings;
CREATE TRIGGER trg_sync_sub2api_lottery_enabled
AFTER INSERT OR UPDATE OF value ON settings
FOR EACH ROW
WHEN (NEW.key = 'lottery_enabled')
EXECUTE FUNCTION sync_sub2api_lottery_enabled();
