import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import OAuthUsageWindows from '../OAuthUsageWindows.vue'

const UsageProgressBarStub = defineComponent({
  name: 'UsageProgressBar',
  props: {
    label: String,
    utilization: Number,
    resetsAt: { type: String, default: null },
    windowStats: { type: Object, default: null },
    showNowWhenIdle: Boolean,
    color: String,
  },
  template: '<div data-testid="usage-window">{{ label }}|{{ utilization }}|{{ color }}</div>',
})

const mountWindows = (props: Record<string, unknown>) => mount(OAuthUsageWindows, {
  props: props as never,
  global: {
    stubs: {
      UsageProgressBar: UsageProgressBarStub,
    },
  },
})

describe('OAuthUsageWindows', () => {
  it('默认只展示 5 小时和 7 天窗口，不透出窗口统计', () => {
    const wrapper = mountWindows({
      usage: {
        five_hour: {
          utilization: 25,
          resets_at: '2026-07-28T10:00:00Z',
          window_stats: { requests: 3, tokens: 30, cost: 0.3 },
        },
        seven_day: { utilization: 60, resets_at: '2026-08-03T10:00:00Z' },
        seven_day_sonnet: { utilization: 70, resets_at: null },
      },
      showNowWhenIdle: true,
    })

    const windows = wrapper.findAllComponents(UsageProgressBarStub)
    expect(windows).toHaveLength(2)
    expect(windows.map((window) => window.props('label'))).toEqual(['5h', '7d'])
    expect(windows[0].props('windowStats')).toBeNull()
    expect(windows[0].props('showNowWhenIdle')).toBe(true)
  })

  it('管理端可显式展示扩展窗口和窗口统计', () => {
    const stats = { requests: 3, tokens: 30, cost: 0.3 }
    const wrapper = mountWindows({
      usage: {
        five_hour: { utilization: 25, resets_at: null, window_stats: stats },
        seven_day_sonnet: { utilization: 70, resets_at: null },
        seven_day_fable: { utilization: 80, resets_at: null },
      },
      showWindowStats: true,
      showExtendedWindows: true,
    })

    const windows = wrapper.findAllComponents(UsageProgressBarStub)
    expect(windows.map((window) => window.props('label'))).toEqual(['5h', '7d S', '7d F'])
    expect(windows[0].props('windowStats')).toEqual(stats)
  })

  it('没有可展示额度时显示调用方提供的空状态文本', () => {
    const wrapper = mountWindows({ usage: {}, emptyText: '暂无额度数据' })

    expect(wrapper.text()).toBe('暂无额度数据')
    expect(wrapper.findAllComponents(UsageProgressBarStub)).toHaveLength(0)
  })
})
