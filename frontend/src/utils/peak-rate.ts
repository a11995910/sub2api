/**
 * 高峰时段倍率的共享展示逻辑。
 *
 * 高峰窗口由后端按服务器全局时区判定（Group.PeakMultiplierAt），
 * 前端展示必须带上服务器时区标注（来自公共设置 server_utc_offset），
 * 避免用户按浏览器本地时间误读计费窗口。
 */

export interface PeakRateFields {
  peak_rate_enabled?: boolean
  peak_start?: string
  peak_end?: string
  peak_rate_multiplier?: number
}

export function hasPeakRate(fields?: PeakRateFields | null): boolean {
  return Boolean(fields?.peak_rate_enabled && fields.peak_start && fields.peak_end)
}

/** "+08:00" → "UTC+08:00"；旧缓存无该字段时返回空串，调用方降级为不带时区标注 */
export function serverTimezoneLabel(utcOffset?: string | null): string {
  return utcOffset ? `UTC${utcOffset}` : ''
}

/** "14:00-18:00 ×2 (UTC+08:00)"，tzLabel 为空时省略括号部分 */
export function formatPeakRateWindow(
  fields: PeakRateFields | null | undefined,
  tzLabel?: string
): string {
  if (!hasPeakRate(fields) || !fields) return ''
  const base = `${fields.peak_start}-${fields.peak_end} ×${fields.peak_rate_multiplier ?? 1}`
  return tzLabel ? `${base} (${tzLabel})` : base
}

/** 限时活动折扣的共享展示逻辑。 */
export interface PromoDiscountFields {
  promo_discount_enabled?: boolean
  promo_discount_start?: string
  promo_discount_end?: string
  promo_discount_rate?: number
  /** 服务端响应生成时刻是否处于活动窗口；缺省时视为不生效。 */
  promo_active?: boolean
}

/**
 * 折扣倍率（0.95）→ 折数数值（95，保留 1 位小数去除浮点噪声）；非法值返回 null。
 * zh 展示 "限时 95 折"，en 展示 "5% off"（100 - 折数）。
 */
export function promoDiscountZhe(rate?: number): number | null {
  if (!rate || rate <= 0 || rate > 1) return null
  const zhe = Math.round(rate * 1000) / 10
  return Number.isInteger(zhe) ? zhe : Number(zhe.toFixed(1))
}

/** 活动窗口展示："2026-09-01 00:00 ~ 2026-09-08 00:00"（站点时区） */
export function formatPromoDiscountWindow(fields?: PromoDiscountFields | null): string {
  if (!fields?.promo_discount_enabled || !fields.promo_discount_start || !fields.promo_discount_end) {
    return ''
  }
  return `${fields.promo_discount_start} ~ ${fields.promo_discount_end}`
}
