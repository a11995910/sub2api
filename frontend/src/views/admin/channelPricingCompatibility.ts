import type { ChannelModelPricing } from '@/api/admin/channels'
import { BILLING_MODE_TOKEN, type BillingMode } from '@/constants/channel'

/** 编辑历史定价时保留后端模式，避免把按次价格隐式解释为视频每秒价格。 */
export function preserveChannelBillingMode(
  entry: Pick<ChannelModelPricing, 'billing_mode'>,
): BillingMode {
  return entry.billing_mode || BILLING_MODE_TOKEN
}
