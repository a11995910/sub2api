import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import OAuthAccountPoolView from '../OAuthAccountPoolView.vue'

const { getPool } = vi.hoisted(() => ({
  getPool: vi.fn(),
}))

vi.mock('@/api', () => ({
  oauthAccountPoolAPI: { get: getPool },
}))

const messages: Record<string, string> = {
  'oauthAccountPool.accountCount': '{count} 个账号',
  'oauthAccountPool.noUsageSnapshot': '暂无额度数据',
  'oauthAccountPool.emptyTitle': '暂无可见号池',
  'oauthAccountPool.emptyDescription': '当前没有向您公开的 OAuth 账号状态。',
  'oauthAccountPool.errorTitle': '号池状态加载失败',
  'oauthAccountPool.errorDescription': '暂时无法读取号池状态，请稍后重试。',
  'common.retry': '重试',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const message = messages[key] ?? key
        return params ? message.replace('{count}', String(params.count)) : message
      },
    }),
  }
})

const OAuthUsageWindowsStub = defineComponent({
  name: 'OAuthUsageWindows',
  props: ['usage', 'emptyText', 'showNowWhenIdle'],
  template: '<div data-testid="oauth-usage">{{ usage.five_hour?.utilization ?? emptyText }}</div>',
})

const mountView = () => mount(OAuthAccountPoolView, {
  global: {
    stubs: {
      AppLayout: { template: '<main><slot /></main>' },
      EmptyState: {
        props: ['title', 'description'],
        template: '<div data-testid="empty-state">{{ title }}|{{ description }}</div>',
      },
      Icon: true,
      OAuthUsageWindows: OAuthUsageWindowsStub,
    },
  },
})

describe('OAuthAccountPoolView', () => {
  beforeEach(() => {
    getPool.mockReset()
  })

  it('按分组展示账号名称和缓存额度，不渲染内部标识或操作入口', async () => {
    getPool.mockResolvedValue({
      groups: [{
        name: 'Pro 正价分组',
        accounts: [
          {
            name: 'Pro 正价',
            usage: {
              five_hour: { utilization: 24, resets_at: '2026-07-28T10:00:00Z' },
              seven_day: null,
            },
          },
        ],
      }],
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Pro 正价分组')
    expect(wrapper.text()).toContain('1 个账号')
    expect(wrapper.text()).toContain('Pro 正价')
    expect(wrapper.get('[data-testid="oauth-usage"]').text()).toBe('24')
    expect(wrapper.text()).not.toContain('account_id')
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('空响应展示统一空状态', async () => {
    getPool.mockResolvedValue({ groups: [] })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="empty-state"]').text()).toContain('暂无可见号池')
  })

  it('请求失败后允许重试并恢复分组内容', async () => {
    getPool
      .mockRejectedValueOnce(new Error('暂时不可用'))
      .mockResolvedValueOnce({
        groups: [{
          name: '可见分组',
          accounts: [{ name: 'OAuth 账号', usage: {} }],
        }],
      })

    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('号池状态加载失败')

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(getPool).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('可见分组')
    expect(wrapper.text()).not.toContain('号池状态加载失败')
  })
})
