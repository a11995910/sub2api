-- 邀请充值奖励分组按「邀请人 + 被邀请人」只发放一次。
-- 历史支付订单奖励已有审计 detail，可以回填为新的业务幂等 claim；历史兑换码奖励缺少稳定审计源，无法可靠回填。

INSERT INTO payment_audit_logs (order_id, action, detail, operator, created_at)
SELECT
    'agr:' || parsed.inviter_user_id || ':' || parsed.invitee_user_id,
    'AFFILIATE_GROUP_ACCESS_REWARD_CLAIMED',
    '{"source_type":"historical_payment_audit","source_order_id":' || pal.order_id ||
        ',"inviter_user_id":' || parsed.inviter_user_id ||
        ',"invitee_user_id":' || parsed.invitee_user_id ||
        ',"group_id":' || parsed.group_id || '}',
    'system',
    pal.created_at
FROM payment_audit_logs pal
CROSS JOIN LATERAL (
    SELECT
        substring(pal.detail FROM '"inviter_user_id"[[:space:]]*:[[:space:]]*([0-9]+)') AS inviter_user_id,
        substring(pal.detail FROM '"invitee_user_id"[[:space:]]*:[[:space:]]*([0-9]+)') AS invitee_user_id,
        substring(pal.detail FROM '"group_id"[[:space:]]*:[[:space:]]*([0-9]+)') AS group_id
) parsed
WHERE pal.action IN ('AFFILIATE_GROUP_ACCESS_REWARD_APPLIED', 'AFFILIATE_SUBSCRIPTION_REWARD_APPLIED')
  AND parsed.inviter_user_id IS NOT NULL
  AND parsed.invitee_user_id IS NOT NULL
  AND parsed.group_id IS NOT NULL
ON CONFLICT (order_id, action) DO NOTHING;
