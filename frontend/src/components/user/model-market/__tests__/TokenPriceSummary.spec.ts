import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import TokenPriceSummary from '../TokenPriceSummary.vue'

const messages: Record<string, string> = {
  'modelMarket.inputPrice': '输入价格',
  'modelMarket.perMillionTokens': '每百万 Token',
  'modelMarket.priceDetails': '价格明细',
  'modelMarket.inputMeaning': '发送给模型的内容',
  'modelMarket.outputMeaning': '模型回复内容',
  'modelMarket.cacheReadMeaning': '重复内容命中缓存',
  'modelMarket.cacheWriteMeaning': '首次写入可复用缓存',
  'modelMarket.columns.input': '输入',
  'modelMarket.columns.output': '输出',
  'modelMarket.columns.cacheRead': '缓存读取',
  'modelMarket.columns.cacheWrite': '缓存写入',
  'modelMarket.officialReference': '官方输入参考',
  'modelMarket.discountCompared': '比官方参考低 {value}',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        let text = messages[key] ?? key
        for (const [paramKey, value] of Object.entries(params ?? {})) {
          text = text.replace(`{${paramKey}}`, String(value))
        }
        return text
      },
    }),
  }
})

describe('TokenPriceSummary', () => {
  it('主视图仅强调输入价格，并用可访问明细解释输出与缓存价格', () => {
    const wrapper = mount(TokenPriceSummary, {
      props: {
        inputValue: '1 灵石',
        outputValue: '2 灵石',
        cacheReadValue: '0.25 灵石',
        officialInputValue: '$1',
        discountValue: '-85.9%',
      },
      global: {
        stubs: {
          Icon: { template: '<span />' },
        },
      },
    })

    const trigger = wrapper.get('[data-testid="token-input-price"]')
    const tooltip = wrapper.get('[role="tooltip"]')

    expect(trigger.text()).toContain('输入价格')
    expect(trigger.text()).toContain('1 灵石')
    expect(wrapper.get('[data-testid="token-price-unit"]').text()).toBe('每百万 Token')
    expect(trigger.text()).not.toContain('2 灵石')
    expect(trigger.text()).not.toContain('0.25 灵石')
    expect(tooltip.text()).toContain('模型回复内容')
    expect(tooltip.text()).toContain('2 灵石')
    expect(tooltip.text()).toContain('重复内容命中缓存')
    expect(tooltip.text()).toContain('0.25 灵石')
    expect(tooltip.text()).toContain('官方输入参考')
    expect(tooltip.text()).toContain('$1')
    expect(tooltip.text()).toContain('比官方参考低 85.9%')
    expect(trigger.attributes('aria-describedby')).toBe(tooltip.attributes('id'))
  })

  it('仅在存在缓存写入价格时展示对应明细', async () => {
    const wrapper = mount(TokenPriceSummary, {
      props: {
        inputValue: '1 灵石',
        outputValue: '2 灵石',
        cacheReadValue: '0.25 灵石',
      },
      global: {
        stubs: {
          Icon: { template: '<span />' },
        },
      },
    })

    expect(wrapper.get('[role="tooltip"]').text()).not.toContain('缓存写入')

    await wrapper.setProps({ cacheWriteValue: '1.25 灵石' })

    expect(wrapper.get('[role="tooltip"]').text()).toContain('缓存写入')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('1.25 灵石')
  })
})
