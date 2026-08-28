import { describe, expect, it } from 'vitest'

import {
  activePromoDiscountMultiplier,
  applyActivePromoDiscount,
} from '@/utils/peak-rate'

describe('活动折扣倍率', () => {
  it('只接受服务端已判定生效且合法的折扣', () => {
    expect(activePromoDiscountMultiplier({
      promo_discount_enabled: true,
      promo_discount_rate: 0.8,
      promo_active: true,
    })).toBe(0.8)

    expect(activePromoDiscountMultiplier({
      promo_discount_enabled: true,
      promo_discount_rate: 0.8,
      promo_active: false,
    })).toBe(1)

    expect(activePromoDiscountMultiplier({
      promo_discount_enabled: true,
      promo_discount_rate: Number.NaN,
      promo_active: true,
    })).toBe(1)
  })

  it('在基础倍率后叠加折扣并消除浮点尾差', () => {
    expect(applyActivePromoDiscount(1.9, {
      promo_discount_enabled: true,
      promo_discount_rate: 0.95,
      promo_active: true,
    })).toBe(1.805)
  })
})
