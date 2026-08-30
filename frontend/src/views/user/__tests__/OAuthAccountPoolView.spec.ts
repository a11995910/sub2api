import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import OAuthAccountPoolView from '../OAuthAccountPoolView.vue'

const { getPool, authStore } = vi.hoisted(() => ({
  getPool: vi.fn(),
  authStore: { isAdmin: false },
}))

vi.mock('@/api', () => ({
  oauthAccountPoolAPI: { get: getPool },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

const messages: Record<string, string> = {
  'oauthAccountPool.overviewLabel': '号池概览',
  'oauthAccountPool.overviewDescription': '展示套餐与实时运行状态，方便快速了解当前服务可用性。',
  'oauthAccountPool.groupCount': '{count} 个分组',
  'oauthAccountPool.accountCount': '{count} 个账号',
  'oauthAccountPool.visibleAccounts': '可见账号',
  'oauthAccountPool.totalRequests': '总请求次数',
  'oauthAccountPool.totalTokens': '总 Token 用量',
  'oauthAccountPool.requests': '请求次数',
  'oauthAccountPool.tokens': 'Token 用量',
  'oauthAccountPool.period5h': '5 小时',
  'oauthAccountPool.period7d': '7 天',
  'oauthAccountPool.cumulative': '累计',
  'oauthAccountPool.quotaStatus': '额度状态',
  'oauthAccountPool.accountStatus': '运行状态',
  'oauthAccountPool.connectionsShort': '实时连接',
  'oauthAccountPool.status.available': '可用',
  'oauthAccountPool.status.active': '使用中',
  'oauthAccountPool.status.busy': '繁忙',
  'oauthAccountPool.connections': '当前连接数 / 并发总数',
  'oauthAccountPool.unknownIdentifier': '账号信息不可用',
  'oauthAccountPool.unknownPlan': '未知套餐',
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

const buildPoolResponse = () => ({
  summary: { account_count: 1, requests: 120, tokens: 12000 },
  groups: [{
    name: '公开分组',
    summary: { account_count: 1, requests: 120, tokens: 12000 },
    accounts: [
      {
        identifier: '1072******@qq.com',
        plan_type: 'Pro 20x',
        current_concurrency: 6,
        concurrency: 15,
        usage: {
          five_hour: { utilization: 24, resets_at: '2026-07-28T10:00:00Z' },
          seven_day: null,
        },
        stats: {
          five_hour: { requests: 5, tokens: 500 },
          seven_day: { requests: 70, tokens: 7000 },
          total: { requests: 120, tokens: 12000 },
        },
      },
    ],
  }],
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
    authStore.isAdmin = false
  })

  it('普通用户只展示脱敏账号、套餐、运行状态与并发，不展示任何用量', async () => {
    getPool.mockResolvedValue(buildPoolResponse())

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('公开分组')
    expect(wrapper.text()).toContain('1 个账号')
    expect(wrapper.text()).toContain('Pro 20x')
    expect(wrapper.text()).toContain('1072******@qq.com')
    expect(wrapper.text()).not.toContain('1072688154@qq.com')
    expect(wrapper.text()).not.toContain('总请求次数')
    expect(wrapper.text()).not.toContain('总 Token 用量')
    expect(wrapper.text()).not.toContain('请求次数')
    expect(wrapper.text()).not.toContain('Token 用量')
    expect(wrapper.text()).not.toContain('5 小时')
    expect(wrapper.text()).not.toContain('7 天')
    expect(wrapper.text()).not.toContain('12.0K')
    expect(wrapper.text()).not.toContain('Pro 正价')
    expect(wrapper.text()).toContain('运行状态')
    expect(wrapper.text()).toContain('使用中')
    expect(wrapper.text()).toContain('实时连接')
    expect(wrapper.text()).not.toContain('用量')
    expect(wrapper.get('[data-testid="account-concurrency"]').text()).toContain('6/15')
    expect(wrapper.find('[data-testid="oauth-usage"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="pool-summary"]').text()).toContain('可见账号')
    expect(wrapper.get('[data-testid="pool-summary"]').text()).toContain('1')
    expect(wrapper.text()).not.toContain('account_id')
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('仅根据实时连接负载显示运行状态', async () => {
    const response = buildPoolResponse()
    response.groups[0].accounts[0].current_concurrency = 15
    getPool.mockResolvedValue(response)

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('繁忙')
    expect(wrapper.text()).not.toContain('24%')
    expect(wrapper.find('[data-testid="oauth-usage"]').exists()).toBe(false)
  })

  it('管理员继续展示请求和 Token 统计', async () => {
    authStore.isAdmin = true
    getPool.mockResolvedValue(buildPoolResponse())

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="pool-summary"]').text()).toContain('总请求次数')
    expect(wrapper.get('[data-testid="pool-summary"]').text()).toContain('120')
    expect(wrapper.text()).toContain('总 Token 用量')
    expect(wrapper.text()).toContain('请求次数')
    expect(wrapper.text()).toContain('Token 用量')
    expect(wrapper.text()).toContain('5 小时')
    expect(wrapper.text()).toContain('7 天')
    expect(wrapper.text()).toContain('12.0K')
  })

  it('空响应展示统一空状态', async () => {
    getPool.mockResolvedValue({ groups: [], summary: { account_count: 0, requests: 0, tokens: 0 } })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="empty-state"]').text()).toContain('暂无可见号池')
  })

  it('请求失败后允许重试并恢复分组内容', async () => {
    getPool
      .mockRejectedValueOnce(new Error('暂时不可用'))
      .mockResolvedValueOnce({
        summary: { account_count: 1, requests: 0, tokens: 0 },
        groups: [{
          name: '可见分组',
          summary: { account_count: 1, requests: 0, tokens: 0 },
          accounts: [{
            identifier: 'user@example.com',
            plan_type: 'Free',
            current_concurrency: 0,
            concurrency: 10,
            usage: {},
            stats: {
              five_hour: { requests: 0, tokens: 0 },
              seven_day: { requests: 0, tokens: 0 },
              total: { requests: 0, tokens: 0 },
            },
          }],
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
