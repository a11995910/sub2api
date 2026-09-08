import { i18n } from '@/i18n'

function formatPriceNumber(value: number, minFractionDigits = 0): string {
  let s = value.toPrecision(10).replace(/\.?0+$/, '')
  if (minFractionDigits > 0 && !s.includes('e')) {
    const dot = s.indexOf('.')
    const digits = dot === -1 ? 0 : s.length - dot - 1
    if (digits < minFractionDigits) {
      s = (dot === -1 ? `${s}.` : s) + '0'.repeat(minFractionDigits - digits)
    }
  }
  return s
}

/**
 * 按比例格式化灵石价格，并追加当前语言配置的货币名称。
 * `minFractionDigits` 用于价格表保留最少的小数位数；更长的有效小数不会被截断。
 */
export function formatScaled(value: number | null, scale: number, minFractionDigits = 0): string {
  if (value == null) return '-'
  return `${formatPriceNumber(value * scale, minFractionDigits)} ${i18n.global.t('common.currencyName')}`
}

/** 按比例格式化美元价格，供需要固定展示 USD 的官方参考价使用。 */
export function formatUSDScaled(value: number | null, scale: number, minFractionDigits = 0): string {
  if (value == null) return '-'
  return `$${formatPriceNumber(value * scale, minFractionDigits)}`
}

/** 按渠道原价币种格式化；未知或缺失币种按 USD 兼容。 */
export function formatOriginalCurrencyScaled(
  value: number | null,
  scale: number,
  currency: 'USD' | 'CNY' | null | undefined,
  minFractionDigits = 0,
): string {
  if (value == null) return '-'
  const symbol = currency === 'CNY' ? '¥' : '$'
  return `${symbol}${formatPriceNumber(value * scale, minFractionDigits)}`
}

import type { UserPricingInterval } from '@/api/channels'

type TokenPrices = Pick<UserPricingInterval, 'input_price' | 'output_price' | 'cache_write_price' | 'cache_write_1h_price' | 'cache_read_price'>

export function resolveIntervalPrices(iv: UserPricingInterval, base: TokenPrices): UserPricingInterval {
  const price = (absolute: number | null | undefined, multiplier: number | null | undefined, fallback: number | null | undefined) =>
    absolute ?? (fallback == null ? null : fallback * (multiplier ?? 1))
  return {
    ...iv,
    input_price: price(iv.input_price, iv.input_multiplier, base.input_price),
    output_price: price(iv.output_price, iv.output_multiplier, base.output_price),
    cache_write_price: price(iv.cache_write_price, iv.cache_write_multiplier, base.cache_write_price),
    // Resolver uses an explicit cache-write price for both durations unless 1h is overridden.
    cache_write_1h_price: iv.cache_write_1h_price ?? iv.cache_write_price ?? price(null, iv.cache_write_multiplier, base.cache_write_1h_price),
    cache_read_price: price(iv.cache_read_price, iv.cache_read_multiplier, base.cache_read_price)
  }
}
