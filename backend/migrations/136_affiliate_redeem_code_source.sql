-- 邀请返利流水补充余额兑换码来源。
-- 支付订单返利继续使用 source_order_id；外部购买后站内兑换的余额兑换码使用 source_redeem_code_id。

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS source_redeem_code_id BIGINT NULL REFERENCES redeem_codes(id) ON DELETE SET NULL;

COMMENT ON COLUMN user_affiliate_ledger.source_redeem_code_id IS '产生该返利流水的余额兑换码；支付订单返利、转余额或无法可靠回填的历史数据为 NULL';

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_source_redeem_code_id
    ON user_affiliate_ledger(source_redeem_code_id)
    WHERE source_redeem_code_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_redeem_rebate_lookup
    ON user_affiliate_ledger(action, source_redeem_code_id, user_id, source_user_id, created_at)
    WHERE action = 'accrue' AND source_redeem_code_id IS NOT NULL;
