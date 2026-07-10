import { describe, expect, it } from 'vitest'
import type { ChannelModelPricing } from '@/api/admin/channels'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
} from '@/constants/channel'
import { preserveChannelBillingMode } from '../channelPricingCompatibility'

describe('渠道视频定价兼容', () => {
  it.each([
    BILLING_MODE_IMAGE,
    BILLING_MODE_PER_REQUEST,
  ])('保留后端返回的历史 %s 计费模式', (billingMode) => {
    const entry = {
      billing_mode: billingMode,
      models: ['grok-imagine-video-1.5'],
    }

    expect(preserveChannelBillingMode(entry)).toBe(billingMode)
  })

  it('缺少计费模式时回退为 token', () => {
    const entry = { billing_mode: undefined } as unknown as Pick<ChannelModelPricing, 'billing_mode'>

    expect(preserveChannelBillingMode(entry)).toBe(BILLING_MODE_TOKEN)
  })
})
