import { describe, expect, it } from 'vitest'
import {
  pricingInputToAPI,
  pricingInputToForm,
  transitionPricingBillingMode,
  validateIntervals,
  type IntervalFormEntry,
} from '../types'
import type { PricingFormEntry } from '../types'

function makeInterval(over: Partial<IntervalFormEntry>): IntervalFormEntry {
  return {
    min_tokens: 0,
    max_tokens: null,
    tier_label: '',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    per_request_price: null,
    sort_order: 0,
    ...over,
  }
}

function t(key: string, params?: Record<string, unknown>): string {
  return `${key}${params ? ` ${JSON.stringify(params)}` : ''}`
}

function makePricingEntry(overrides: Partial<PricingFormEntry> = {}): PricingFormEntry {
  return {
    models: ['grok-imagine-video'],
    billing_mode: 'image',
    input_price: 1,
    output_price: 2,
    cache_write_price: 3,
    cache_read_price: 4,
    image_output_price: 5,
    per_request_price: 2.1,
    intervals: [makeInterval({ tier_label: '720p', per_request_price: 2.3 })],
    ...overrides,
  }
}

describe('transitionPricingBillingMode', () => {
  it.each(['image', 'per_request'] as const)('历史 %s 切到 video 时清空按次默认价和层级', (mode) => {
    const result = transitionPricingBillingMode(makePricingEntry({ billing_mode: mode }), 'video')

    expect(result.billing_mode).toBe('video')
    expect(result.per_request_price).toBeNull()
    expect(result.intervals).toEqual([])
  })

  it('显式零价跨入 video 时仍清空，因为价格单位已经变化', () => {
    const result = transitionPricingBillingMode(makePricingEntry({ per_request_price: 0 }), 'video')

    expect(result.per_request_price).toBeNull()
  })

  it.each(['token', 'per_request', 'image'] as const)('video 切到 %s 时清空每秒默认价和层级', (mode) => {
    const result = transitionPricingBillingMode(
      makePricingEntry({ billing_mode: 'video', per_request_price: 0.14 }),
      mode,
    )

    expect(result.billing_mode).toBe(mode)
    expect(result.per_request_price).toBeNull()
    expect(result.intervals).toEqual([])
  })

  it.each([
    ['image', 'per_request'],
    ['per_request', 'image'],
  ] as const)('%s 与 %s 切换时保留同单位默认按次价', (from, to) => {
    const result = transitionPricingBillingMode(makePricingEntry({ billing_mode: from }), to)

    expect(result.per_request_price).toBe(2.1)
    expect(result.intervals).toEqual([])
  })

  it('模式未变化时完整保留原对象和区间', () => {
    const entry = makePricingEntry()

    expect(transitionPricingBillingMode(entry, 'image')).toBe(entry)
  })

  it('切换到 video 时清空单位冲突的输入价并保留其他 token 字段', () => {
    const result = transitionPricingBillingMode(makePricingEntry(), 'video')

    expect(result).toMatchObject({
      input_price: null,
      output_price: 2,
      cache_write_price: 3,
      cache_read_price: 4,
      image_output_price: 5,
    })
  })
})

describe('视频历史参考图价格兼容', () => {
  it('video 模式历史输入价在表单和保存时统一清空', () => {
    expect(pricingInputToForm('video', 0.01)).toBeNull()
    expect(pricingInputToAPI('video', 0.01)).toBeNull()
  })

  it('token 模式继续执行每百万 token 换算', () => {
    expect(pricingInputToForm('token', 0.000001)).toBe(1)
    expect(pricingInputToAPI('token', 1)).toBe(0.000001)
  })
})

describe('validateIntervals', () => {
  describe('token mode', () => {
    it('rejects unbounded interval that is not last', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: null, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 200000, max_tokens: 500000, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toContain('unboundedLast')
    })

    it('accepts unbounded interval at the end', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: 200000, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 200000, max_tokens: null, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toBeNull()
    })

    it('rejects overlapping intervals', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: 250000, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 200000, max_tokens: 500000, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toContain('overlap')
    })

    it('rejects unbounded interval in token mode', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: null, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 100, max_tokens: 200, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toContain('unboundedLast')
    })
  })

  describe('image / video / per_request mode', () => {
    it('allows multiple unbounded tiers identified by label', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ tier_label: '1K', per_request_price: 0.04 }),
        makeInterval({ tier_label: '2K', per_request_price: 0.06 }),
        makeInterval({ tier_label: '4K', per_request_price: 0.08 }),
      ]
      expect(validateIntervals(intervals, 'image', t)).toBeNull()
      expect(validateIntervals(intervals, 'video', t)).toBeNull()
      expect(validateIntervals(intervals, 'per_request', t)).toBeNull()
    })

    it('still rejects negative prices', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ tier_label: '1K', per_request_price: -1 }),
      ]
      expect(validateIntervals(intervals, 'image', t)).toContain('negativePrice')
    })

    it('still rejects max <= min on a single tier', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ tier_label: '1K', min_tokens: 100, max_tokens: 50, per_request_price: 0.04 }),
      ]
      expect(validateIntervals(intervals, 'image', t)).toContain('maxGreaterThanMin')
    })
  })
})
