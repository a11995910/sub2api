import { describe, expect, it } from 'vitest'

import type { UserAvailableGroup, UserSupportedModelPricing } from '@/api/channels'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
  BILLING_MODE_VIDEO,
} from '@/constants/channel'
import {
  normalizeVideoBillingModelName,
  resolveVideoPriceQuote,
  videoResolutionsForModel,
} from '@/utils/videoPricing'

function groupFixture(overrides: Partial<UserAvailableGroup> = {}): UserAvailableGroup {
  return {
    id: 1,
    name: '视频分组',
    platform: 'grok',
    subscription_type: 'standard',
    rate_multiplier: 1,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    allow_image_generation: true,
    image_rate_independent: false,
    cache_hit_quarter_to_input_enabled: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    video_rate_independent: false,
    video_rate_multiplier: 1,
    video_price_480p: null,
    video_price_720p: null,
    video_price_1080p: null,
    ...overrides,
  }
}

function pricingFixture(
  billingMode: UserSupportedModelPricing['billing_mode'],
  defaultPrice: number | null,
  tierPrice?: number | null,
): UserSupportedModelPricing {
  return {
    billing_mode: billingMode,
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_output_price: null,
    per_request_price: defaultPrice,
    intervals: tierPrice === undefined
      ? []
      : [{
          min_tokens: 0,
          max_tokens: null,
          tier_label: '720p',
          input_price: null,
          output_price: null,
          cache_write_price: null,
          cache_read_price: null,
          per_request_price: tierPrice,
        }],
  }
}

describe('resolveVideoPriceQuote', () => {
  it.each([
    [
      '分组分辨率覆盖价',
      groupFixture({ video_price_720p: 0.03 }),
      pricingFixture(BILLING_MODE_VIDEO, 0.07, 0.14),
      0.03,
      'second',
      'group',
    ],
    [
      '渠道分辨率层级价',
      groupFixture(),
      pricingFixture(BILLING_MODE_VIDEO, 0.07, 0.14),
      0.14,
      'second',
      'channel_interval',
    ],
    [
      '渠道默认价',
      groupFixture(),
      pricingFixture(BILLING_MODE_VIDEO, 0.07),
      0.07,
      'second',
      'channel_default',
    ],
    [
      '历史图片模式默认价',
      groupFixture(),
      pricingFixture(BILLING_MODE_IMAGE, 2.1),
      2.1,
      'request',
      'channel_default',
    ],
    [
      '系统默认价',
      groupFixture(),
      null,
      0.14,
      'second',
      'system_default',
    ],
  ] as const)('%s', (_name, group, pricing, price, billingUnit, source) => {
    expect(resolveVideoPriceQuote({
      group,
      pricing,
      modelName: 'grok-imagine-video-1.5',
      resolution: '720p',
    })).toMatchObject({ basePrice: price, effectivePrice: price, billingUnit, source })
  })

  it('视频独立倍率优先于用户专属分组倍率', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture({
        rate_multiplier: 2,
        video_rate_independent: true,
        video_rate_multiplier: 1.5,
      }),
      pricing: pricingFixture(BILLING_MODE_VIDEO, 0.08),
      modelName: 'grok-imagine-video-1.5',
      resolution: '480p',
      userGroupRate: 3,
    })).toMatchObject({ basePrice: 0.08, effectivePrice: 0.12 })
  })

  it('未启用视频独立倍率时优先使用用户专属分组倍率', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture({ rate_multiplier: 2 }),
      pricing: pricingFixture(BILLING_MODE_VIDEO, 0.08),
      modelName: 'grok-imagine-video-1.5',
      resolution: '480p',
      userGroupRate: 1.25,
    })).toMatchObject({ basePrice: 0.08, effectivePrice: 0.1 })
  })

  it('用户专属倍率缺失时使用分组通用倍率', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture({ rate_multiplier: 2 }),
      pricing: pricingFixture(BILLING_MODE_VIDEO, 0.08),
      modelName: 'grok-imagine-video-1.5',
      resolution: '480p',
    })).toMatchObject({ basePrice: 0.08, effectivePrice: 0.16 })
  })

  it('空层级价格不是零价并回退渠道默认价', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: pricingFixture(BILLING_MODE_VIDEO, 0.07, null),
      modelName: 'grok-imagine-video',
      resolution: '720p',
    })).toMatchObject({ basePrice: 0.07, source: 'channel_default' })
  })

  it.each([
    ['分组覆盖价', groupFixture({ video_price_720p: 0 }), pricingFixture(BILLING_MODE_VIDEO, 0.07, 0.14), 'group'],
    ['渠道层级价', groupFixture(), pricingFixture(BILLING_MODE_VIDEO, 0.07, 0), 'channel_interval'],
    ['渠道默认价', groupFixture(), pricingFixture(BILLING_MODE_VIDEO, 0), 'channel_default'],
  ] as const)('显式零价保留来源：%s', (_name, group, pricing, source) => {
    expect(resolveVideoPriceQuote({
      group,
      pricing,
      modelName: 'grok-imagine-video',
      resolution: '720p',
    })).toMatchObject({ basePrice: 0, effectivePrice: 0, source })
  })

  it.each([
    [BILLING_MODE_IMAGE, 'request'],
    [BILLING_MODE_PER_REQUEST, 'request'],
  ] as const)('历史 %s 分辨率层级价格保持按次单位', (billingMode, billingUnit) => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: pricingFixture(billingMode, 2.1, 1.8),
      modelName: 'grok-imagine-video',
      resolution: '720p',
    })).toMatchObject({ basePrice: 1.8, billingUnit, source: 'channel_interval' })
  })

  it('token 定价不能伪装成视频单价', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: pricingFixture(BILLING_MODE_TOKEN, 2.1, 1.8),
      modelName: 'grok-imagine-video-1.5',
      resolution: '720p',
    })).toBeNull()
  })

  it('token 定价即使分组配置了视频价格也必须短路', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture({ video_price_720p: 0.03 }),
      pricing: pricingFixture(BILLING_MODE_TOKEN, 2.1, 1.8),
      modelName: 'grok-imagine-video-1.5',
      resolution: '720p',
    })).toBeNull()
  })

  it.each([
    BILLING_MODE_IMAGE,
    BILLING_MODE_PER_REQUEST,
  ] as const)('历史 %s 未命中当前分辨率且无默认价时不回退系统每秒价', (billingMode) => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: pricingFixture(billingMode, null, 1.8),
      modelName: 'grok-imagine-video-1.5',
      resolution: '480p',
    })).toBeNull()
  })

  it('显式视频模式未命中当前分辨率且无默认价时仍回退系统每秒价', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: pricingFixture(BILLING_MODE_VIDEO, null, 1.8),
      modelName: 'grok-imagine-video',
      resolution: '480p',
    })).toMatchObject({
      basePrice: 0.05,
      billingUnit: 'second',
      source: 'system_default',
    })
  })

  it.each([
    ['grok-imagine-video', '480p', 0.05],
    ['grok-imagine-video', '720p', 0.07],
    ['grok-imagine-video-1.5', '480p', 0.08],
    ['grok-imagine-video-1.5', '720p', 0.14],
    ['grok-imagine-video-1.5', '1080p', 0.25],
    ['dreamina-seedance-2-0-ep', '480p', 0.07],
    ['dreamina-seedance-2-0-ep', '720p', 0.15],
    ['dreamina-seedance-2-0-ep', '1080p', 0.37],
    ['dreamina-seedance-2-0-fast-ep', '480p', 0.06],
    ['dreamina-seedance-2-0-fast-ep', '720p', 0.12],
    ['dreamina-seedance-2-0-mini-ep', '480p', 0.04],
    ['dreamina-seedance-2-0-mini-hc', '720p', 0.08],
  ] as const)('%s 的 %s 使用与后端一致的系统默认价', (modelName, resolution, basePrice) => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: null,
      modelName,
      resolution,
    })).toMatchObject({ basePrice, billingUnit: 'second', source: 'system_default' })
  })

  it('标准视频模型不提供 1080p 报价', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: null,
      modelName: 'grok-imagine-video',
      resolution: '1080p',
    })).toBeNull()
  })

  it('未知视频模型没有任何价格来源时返回空', () => {
    expect(resolveVideoPriceQuote({
      group: groupFixture(),
      pricing: null,
      modelName: 'unknown-video-model',
      resolution: '720p',
    })).toBeNull()
  })
})

describe('videoResolutionsForModel', () => {
  it('标准版仅包含官方支持的 480p 和 720p', () => {
    expect(videoResolutionsForModel('grok-imagine-video')).toEqual(['480p', '720p'])
  })

  it('1.5 包含 1080p', () => {
    expect(videoResolutionsForModel('grok-imagine-video-1.5')).toEqual(['480p', '720p', '1080p'])
  })

  it('Seedance 完整版包含 1080p', () => {
    expect(videoResolutionsForModel('dreamina-seedance-2-0-ep')).toEqual(['480p', '720p', '1080p'])
  })

  it.each([
    'dreamina-seedance-2-0-fast-ep',
    'dreamina-seedance-2-0-mini-ep',
    'dreamina-seedance-2-0-mini-hc',
  ])('%s 仅提供 480p 和 720p', (modelName) => {
    expect(videoResolutionsForModel(modelName)).toEqual(['480p', '720p'])
  })
})

describe('normalizeVideoBillingModelName', () => {
  it('video-1.5 无参考图时按后端规则使用标准视频模型计费', () => {
    expect(normalizeVideoBillingModelName('grok-imagine-video-1.5', false)).toBe('grok-imagine-video')
    expect(normalizeVideoBillingModelName('Grok-Imagine-Video-1.5-Preview', false)).toBe('grok-imagine-video')
  })

  it('video-1.5 有参考图时保留原计费模型', () => {
    expect(normalizeVideoBillingModelName('grok-imagine-video-1.5', true)).toBe('grok-imagine-video-1.5')
  })

  it('其他视频模型只清理首尾空白', () => {
    expect(normalizeVideoBillingModelName('  grok-imagine-video  ', false)).toBe('grok-imagine-video')
  })
})
