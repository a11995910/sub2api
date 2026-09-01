-- 补齐账号统计自定义定价与 ChannelModelPricing 共用字段的持久化。
-- image_input_price 为 NULL 时继续回退文本输入价；区间显式价优先于倍率，
-- 倍率仅在对应显式价为 NULL 时作用于基础价。

ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price NUMERIC(20,12),
    ADD COLUMN IF NOT EXISTS price_currency VARCHAR(3) NOT NULL DEFAULT 'USD';

ALTER TABLE channel_account_stats_pricing_intervals
    ADD COLUMN IF NOT EXISTS input_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS output_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS cache_write_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS cache_read_multiplier NUMERIC(12,6);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_account_stats_model_pricing_price_currency_check' AND conrelid = 'channel_account_stats_model_pricing'::regclass) THEN
        ALTER TABLE channel_account_stats_model_pricing
            ADD CONSTRAINT channel_account_stats_model_pricing_price_currency_check
            CHECK (price_currency IN ('USD', 'CNY'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'account_stats_pricing_intervals_input_multiplier_positive' AND conrelid = 'channel_account_stats_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_account_stats_pricing_intervals
            ADD CONSTRAINT account_stats_pricing_intervals_input_multiplier_positive
            CHECK (input_multiplier IS NULL OR input_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'account_stats_pricing_intervals_output_multiplier_positive' AND conrelid = 'channel_account_stats_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_account_stats_pricing_intervals
            ADD CONSTRAINT account_stats_pricing_intervals_output_multiplier_positive
            CHECK (output_multiplier IS NULL OR output_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'account_stats_pricing_intervals_cache_write_multiplier_positive' AND conrelid = 'channel_account_stats_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_account_stats_pricing_intervals
            ADD CONSTRAINT account_stats_pricing_intervals_cache_write_multiplier_positive
            CHECK (cache_write_multiplier IS NULL OR cache_write_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'account_stats_pricing_intervals_cache_read_multiplier_positive' AND conrelid = 'channel_account_stats_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_account_stats_pricing_intervals
            ADD CONSTRAINT account_stats_pricing_intervals_cache_read_multiplier_positive
            CHECK (cache_read_multiplier IS NULL OR cache_read_multiplier > 0);
    END IF;
END $$;

COMMENT ON COLUMN channel_account_stats_model_pricing.price_currency IS
    '账号统计自定义原价币种：USD（美元）或 CNY（人民币）；仅定义原价展示和比较口径，不改变价格数值';
