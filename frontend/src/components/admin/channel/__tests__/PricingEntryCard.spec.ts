import { defineComponent, nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import PricingEntryCard from '../PricingEntryCard.vue'
import type { PricingFormEntry } from '../types'
import type { BillingMode } from '@/constants/channel'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: { type: [String, Number, Boolean], default: null },
    options: { type: Array, default: () => [] },
  },
  emits: ['update:modelValue'],
  template: '<button type="button" class="select-stub">{{ modelValue }}</button>',
})

function makeEntry(overrides: Partial<PricingFormEntry> = {}): PricingFormEntry {
  return {
    models: ['grok-imagine-video'],
    billing_mode: 'image',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_output_price: null,
    per_request_price: 2.1,
    intervals: [{
      min_tokens: 0,
      max_tokens: null,
      tier_label: '720p',
      input_price: null,
      output_price: null,
      cache_write_price: null,
      cache_read_price: null,
      per_request_price: 2.3,
      sort_order: 0,
    }],
    ...overrides,
  }
}

function mountCard(
  entry = makeEntry(),
  allowedBillingModes?: BillingMode[],
  useRealSelect = false,
  enablePriceCurrency = false,
) {
  return mount(PricingEntryCard, {
    props: {
      entry,
      ...(allowedBillingModes ? { allowedBillingModes } : {}),
      enablePriceCurrency,
    },
    global: {
      stubs: {
        ...(!useRealSelect ? { Select: SelectStub } : {}),
        Icon: true,
        ModelTagInput: true,
        IntervalRow: true,
      },
    },
  })
}

describe('PricingEntryCard', () => {
  it('将下拉模式变化交给统一切换规则并发出安全的新条目', async () => {
    const wrapper = mountCard()

    wrapper.findComponent(SelectStub).vm.$emit('update:modelValue', 'video')
    await nextTick()

    expect(wrapper.emitted<[{ billing_mode: string; per_request_price: number | null; intervals: unknown[] }]>('update')?.[0]?.[0]).toMatchObject({
      billing_mode: 'video',
      per_request_price: null,
      intervals: [],
    })
  })

  it('默认允许主渠道选择全部计费模式', () => {
    const wrapper = mountCard()
    const options = wrapper.findComponent(SelectStub).props('options') as Array<{ value: string }>

    expect(options.map((option) => option.value)).toEqual(['token', 'per_request', 'image', 'video'])
  })

  it('账号统计规则可通过 allowedBillingModes 隐藏 video 选项', () => {
    const wrapper = mountCard(makeEntry(), ['token', 'per_request', 'image'])
    const options = wrapper.findComponent(SelectStub).props('options') as Array<{ value: string }>

    expect(options.map((option) => option.value)).toEqual(['token', 'per_request', 'image'])
  })

  it('历史 video 值不在允许列表时保留原始值且挂载时不主动转换', () => {
    const wrapper = mountCard(
      makeEntry({ billing_mode: 'video', per_request_price: 0.14 }),
      ['token', 'per_request', 'image'],
    )

    expect(wrapper.findComponent(SelectStub).props('modelValue')).toBe('video')
    expect(wrapper.findComponent(SelectStub).props('options')).toContainEqual({
      value: 'video',
      label: 'admin.channels.billingMode.video',
      disabled: true,
    })
    expect(wrapper.emitted('update')).toBeUndefined()
  })

  it('真实下拉框显示禁用的历史 video 值而不是占位文案', () => {
    const wrapper = mountCard(
      makeEntry({ billing_mode: 'video', per_request_price: 0.14 }),
      ['token', 'per_request', 'image'],
      true,
    )

    expect(wrapper.find('.select-value').text()).toBe('admin.channels.billingMode.video')
  })

  it('主渠道可选择人民币原价币种并显示对应价格单位', async () => {
    const wrapper = mountCard(
      makeEntry({ billing_mode: 'token', price_currency: 'USD' }),
      undefined,
      false,
      true,
    )
    const selects = wrapper.findAllComponents(SelectStub)

    expect(wrapper.get('[data-testid="pricing-price-currency"]').text()).toContain(
      'admin.channels.form.priceCurrency',
    )
    expect(selects).toHaveLength(2)
    expect(selects[1].props('modelValue')).toBe('USD')
    expect(selects[1].props('options')).toEqual([
      { value: 'USD', label: 'admin.channels.form.priceCurrencyUSD' },
      { value: 'CNY', label: 'admin.channels.form.priceCurrencyCNY' },
    ])
    expect(wrapper.text()).toContain('$/MTok')

    selects[1].vm.$emit('update:modelValue', 'CNY')
    await nextTick()

    expect(wrapper.emitted<[{ price_currency: string }]>('update')?.[0]?.[0]).toMatchObject({
      price_currency: 'CNY',
    })
  })

  it('未由渠道管理开启时不显示原价币种入口', () => {
    const wrapper = mountCard(makeEntry({ price_currency: 'CNY' }))

    expect(wrapper.find('[data-testid="pricing-price-currency"]').exists()).toBe(false)
    expect(wrapper.findAllComponents(SelectStub)).toHaveLength(1)
  })
})
