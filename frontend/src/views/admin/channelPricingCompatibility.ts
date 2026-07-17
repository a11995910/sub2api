import type { ChannelModelPricing } from '@/api/admin/channels'
import type { PricingFormEntry } from '@/components/admin/channel/types'
import { apiIntervalsToForm, perTokenToMTok, pricingInputToForm } from '@/components/admin/channel/types'
import { BILLING_MODE_TOKEN, type BillingMode } from '@/constants/channel'

/** 编辑历史定价时保留后端模式，避免把按次价格隐式解释为视频每秒价格。 */
export function preserveChannelBillingMode(
  entry: { billing_mode?: BillingMode | null },
): BillingMode {
  return entry.billing_mode ?? BILLING_MODE_TOKEN
}

/** 将管理接口价格条目完整转换为编辑表单结构。 */
export function mapChannelPricingToForm(entry: ChannelModelPricing): PricingFormEntry {
  const billingMode = preserveChannelBillingMode(entry)
  return {
    models: [...(entry.models || [])],
    billing_mode: billingMode,
    input_price: pricingInputToForm(billingMode, entry.input_price),
    output_price: perTokenToMTok(entry.output_price),
    cache_write_price: perTokenToMTok(entry.cache_write_price),
    cache_read_price: perTokenToMTok(entry.cache_read_price),
    image_input_price: perTokenToMTok(entry.image_input_price),
    image_output_price: perTokenToMTok(entry.image_output_price),
    per_request_price: entry.per_request_price,
    intervals: apiIntervalsToForm(entry.intervals || []),
  }
}
