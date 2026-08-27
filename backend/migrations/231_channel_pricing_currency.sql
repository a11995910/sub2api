SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS price_currency VARCHAR(3) NOT NULL DEFAULT 'USD';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'channel_model_pricing_price_currency_check'
          AND conrelid = 'channel_model_pricing'::regclass
    ) THEN
        ALTER TABLE channel_model_pricing
            ADD CONSTRAINT channel_model_pricing_price_currency_check
            CHECK (price_currency IN ('USD', 'CNY'));
    END IF;
END $$;

COMMENT ON COLUMN channel_model_pricing.price_currency IS
    '渠道原价币种：USD（美元）或 CNY（人民币）；仅定义原价展示和比较口径，不改变价格数值';
