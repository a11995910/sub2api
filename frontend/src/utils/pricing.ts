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
