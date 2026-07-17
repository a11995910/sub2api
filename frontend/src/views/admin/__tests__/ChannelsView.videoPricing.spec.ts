import { describe, expect, it } from 'vitest'
import type { AccountStatsPricingRule, ChannelModelPricing } from '@/api/admin/channels'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
  BILLING_MODE_VIDEO,
} from '@/constants/channel'
import {
  mapChannelPricingToForm,
  preserveChannelBillingMode,
} from '../channelPricingCompatibility'

function createPricing(overrides: Partial<ChannelModelPricing> = {}): ChannelModelPricing {
  return {
    platform: 'grok',
    models: ['grok-imagine-video-1.5'],
    billing_mode: BILLING_MODE_IMAGE,
    input_price: 0.000001,
    output_price: 0.000002,
    cache_write_price: 0.000003,
    cache_read_price: 0.000004,
    image_output_price: 0.000005,
    per_request_price: 2.1,
    intervals: [{
      min_tokens: 0,
      max_tokens: null,
      tier_label: '720p',
      input_price: 0.000006,
      output_price: 0.000007,
      cache_write_price: 0.000008,
      cache_read_price: 0.000009,
      per_request_price: 2.3,
      sort_order: 1,
    }],
    ...overrides,
  }
}

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

  it.each([undefined, null])('计费模式为 %s 时回退为 token', (billingMode) => {
    expect(preserveChannelBillingMode({ billing_mode: billingMode })).toBe(BILLING_MODE_TOKEN)
  })

  it('主渠道价格映射保留历史 image 模式及全部关键字段', () => {
    const result = mapChannelPricingToForm(createPricing())

    expect(result).toEqual({
      models: ['grok-imagine-video-1.5'],
      billing_mode: BILLING_MODE_IMAGE,
      input_price: 1,
      output_price: 2,
      cache_write_price: 3,
      cache_read_price: 4,
      image_input_price: null,
      image_output_price: 5,
      per_request_price: 2.1,
      intervals: [{
        min_tokens: 0,
        max_tokens: null,
        tier_label: '720p',
        input_price: 6,
        output_price: 7,
        cache_write_price: 8,
        cache_read_price: 9,
        per_request_price: 2.3,
        sort_order: 1,
      }],
    })
  })

  it('video 模式历史参考图价格不再回填', () => {
    const result = mapChannelPricingToForm(createPricing({
      billing_mode: BILLING_MODE_VIDEO,
      input_price: 0.01,
      per_request_price: null,
    }))

    expect(result.billing_mode).toBe(BILLING_MODE_VIDEO)
    expect(result.input_price).toBeNull()
  })

  it('账号统计价格映射保留历史 per_request 模式及层级价格', () => {
    const rule: AccountStatsPricingRule = {
      name: '历史视频按次规则',
      group_ids: [1],
      account_ids: [2],
      pricing: [createPricing({
        billing_mode: BILLING_MODE_PER_REQUEST,
        per_request_price: 3.1,
        intervals: [{
          min_tokens: 0,
          max_tokens: null,
          tier_label: '1080p',
          input_price: null,
          output_price: null,
          cache_write_price: null,
          cache_read_price: null,
          per_request_price: 3.3,
          sort_order: 0,
        }],
      })],
    }

    const result = mapChannelPricingToForm(rule.pricing[0])

    expect(result.billing_mode).toBe(BILLING_MODE_PER_REQUEST)
    expect(result.per_request_price).toBe(3.1)
    expect(result.intervals).toEqual([{
      min_tokens: 0,
      max_tokens: null,
      tier_label: '1080p',
      input_price: null,
      output_price: null,
      cache_write_price: null,
      cache_read_price: null,
      per_request_price: 3.3,
      sort_order: 0,
    }])
  })
})
