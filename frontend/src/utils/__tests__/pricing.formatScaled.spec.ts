import { describe, expect, it, vi } from 'vitest'
import { formatOriginalCurrencyScaled, formatScaled, formatUSDScaled } from '../pricing'

vi.mock('@/i18n', () => ({
  i18n: { global: { t: () => '灵石' } },
}))

describe('价格格式化', () => {
  it.each([
    [1e-10, 1, '$1e-10'],
    [1.25e-10, 1, '$1.25e-10'],
    [1e-20, 1, '$1e-20'],
    [1e-16, 1_000_000, '$1e-10'],
    [1e10, 1, '$10000000000'],
  ])('保留 %s 按 %s 缩放后的数量级', (value, scale, expected) => {
    expect(formatScaled(value, scale)).toBe(`${expected.slice(1)} 灵石`)
    expect(formatUSDScaled(value, scale)).toBe(expected)
    expect(formatOriginalCurrencyScaled(value, scale, 'CNY')).toBe(`¥${expected.slice(1)}`)
  })

  it.each([
    [0.000003, 1_000_000, 0, '$3'],
    [0.000003, 1_000_000, 2, '$3.00'],
    [1.25e-8, 1_000_000, 2, '$0.0125'],
    [5e-8, 1_000_000, 0, '$0.05'],
    [0, 1, 2, '$0.00'],
    [null, 1_000_000, 0, '-'],
  ])('保留 %s 的价格精度与小数位填充', (value, scale, digits, expected) => {
    expect(formatScaled(value, scale, digits)).toBe(value == null ? '-' : `${expected.slice(1)} 灵石`)
    expect(formatUSDScaled(value, scale, digits)).toBe(expected)
    expect(formatOriginalCurrencyScaled(value, scale, 'CNY', digits)).toBe(value == null ? '-' : `¥${expected.slice(1)}`)
  })
})
