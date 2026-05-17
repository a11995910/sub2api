import { i18n } from '@/i18n'

/**
 * formatScaled formats a per-token (or per-request) 灵石 price scaled by `scale`.
 *
 *   formatScaled(0.000003, 1_000_000) → "3 灵石"     // per 1M tokens
 *   formatScaled(0.5,        1)        → "0.5 灵石"   // per request
 *   formatScaled(null,       1_000_000) → "-"
 *
 * Uses toPrecision(10) then strips trailing zeros to avoid IEEE 754 display noise.
 */
export function formatScaled(value: number | null, scale: number): string {
  if (value == null) return '-'
  return `${(value * scale).toPrecision(10).replace(/\.?0+$/, '')} ${i18n.global.t('common.currencyName')}`
}
