import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import messages from '@/i18n/locales/zh/modelLeaderboard'
import ModelLeaderboardView from '../ModelLeaderboardView.vue'

const mocks = vi.hoisted(() => ({
  leaderboard: vi.fn(),
  plaza: vi.fn(),
  auth: { isAuthenticated: true, isAdmin: false },
  app: {
    fetchPublicSettings: vi.fn(),
    publicSettingsLoaded: true,
    cachedPublicSettings: { model_plaza_enabled: true, model_plaza_require_auth: false }
  }
}))
vi.mock('@/api/modelLeaderboard', () => ({ getModelLeaderboard: mocks.leaderboard }))
vi.mock('@/api/modelPlaza', () => ({ getModelPlaza: mocks.plaza }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'zh' },
    t: (key: string, params?: Record<string, unknown>) => {
      const value =
        messages.modelLeaderboard[
          key.replace('modelLeaderboard.', '') as keyof typeof messages.modelLeaderboard
        ] ?? key
      return value.replace(/\{(\w+)\}/g, (_, name: string) => String(params?.[name] ?? ''))
    }
  })
}))

const snapshot = {
  source: 'Artificial Analysis',
  source_url: 'https://artificialanalysis.ai',
  methodology_url: 'https://artificialanalysis.ai/methodology',
  data_url: 'https://artificialanalysis.ai/documentation',
  sync_enabled: true,
  synced_at: '2026-11-04T03:00:00Z',
  refresh_interval_hours: 24,
  stale: false,
  refreshing: false,
  last_attempt_failed: false,
  categories: [
    { key: 'overall', name: 'Intelligence Index', unit: 'index', ascending: false },
    { key: 'coding', name: 'Terminal-Bench 4.0', unit: 'percent', ascending: false },
    { key: 'reasoning', name: 'GPQA', unit: 'percent', ascending: false },
    { key: 'latency', name: 'Latency', unit: 's', ascending: true }
  ],
  models: [
    {
      id: 'gpt-6.1-sol',
      overall: 90,
      name: 'GPT-6.1 Sol (Max)',
      slug: 'gpt-6.1-sol',
      creator: 'OpenAI',
      input_price: 1,
      output_price: 4,
      scores: { coding: 80, reasoning: 100 }
    },
    {
      id: 'claude-opus-5.5',
      overall: 85,
      name: 'Claude Opus 5.5 (Max, Default Fallback)',
      slug: 'claude-opus-5-5',
      creator: 'Anthropic',
      input_price: 1,
      output_price: 4,
      scores: { coding: 95, reasoning: 70 }
    },
    {
      id: 'gpt-6-sol',
      overall: 80,
      name: 'GPT-6 Sol (High)',
      slug: 'gpt-6-sol',
      creator: 'OpenAI',
      input_price: 1,
      output_price: 4,
      scores: { coding: 75, reasoning: 85 }
    }
  ]
}
function render() {
  return mount(ModelLeaderboardView, {
    global: { stubs: { Icon: true, ModelIcon: true, RouterLink: RouterLinkStub } }
  })
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.auth.isAuthenticated = true
  mocks.app.cachedPublicSettings = { model_plaza_enabled: true, model_plaza_require_auth: false }
  mocks.leaderboard.mockResolvedValue(snapshot)
  mocks.plaza.mockResolvedValue({
    groups: [{ id: 7, name: '可见分组', models: [{ name: 'openai/gpt-6.1-sol' }] }]
  })
})

describe('全球模型榜页面', () => {
  it('展示来源、更新机制，并精确跳转广场与分组', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('2026-11-04')
    expect(wrapper.text()).toContain('每 24 小时检查一次')
    expect(wrapper.find('a[href="https://artificialanalysis.ai/documentation"]').exists()).toBe(
      true
    )
    const links = wrapper.findAllComponents(RouterLinkStub)
    expect(
      links.some(
        (link) =>
          JSON.stringify(link.props('to')) ===
          JSON.stringify({ path: '/model-plaza', query: { model: 'openai/gpt-6.1-sol' } })
      )
    ).toBe(true)
    expect(wrapper.findAll('.plaza-button')).toHaveLength(1)
    await wrapper.find('.model-name-button').trigger('click')
    expect(wrapper.find('.group-links').text()).toContain('可见分组')
    const groupLink = wrapper.find('.group-links').findComponent(RouterLinkStub)
    expect(groupLink.props('to')).toEqual({
      path: '/model-plaza',
      query: { model: 'openai/gpt-6.1-sol', group: '7' }
    })
    wrapper.unmount()
  })
  it('切换维度、搜索和本站收录筛选都保留真实排名', async () => {
    const wrapper = render()
    await flushPromises()
    const codingTab = wrapper
      .findAll('.metric-tabs button')
      .find((button) => button.text() === '编程')!
    await codingTab.trigger('click')
    expect(wrapper.find('tbody tr .model-identity small').text()).toContain('claude-opus-5-5')
    await wrapper.find('input[type="checkbox"]').setValue(true)
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('.position').text()).toBe('02')
    await wrapper.find('.rank-search input').setValue('不存在的模型')
    expect(wrapper.text()).toContain('没有找到符合条件的模型')
    wrapper.unmount()
  })
  it('广场要求登录时不请求分组，榜单仍可正常浏览', async () => {
    mocks.auth.isAuthenticated = false
    mocks.app.cachedPublicSettings.model_plaza_require_auth = true
    const wrapper = render()
    await flushPromises()
    expect(mocks.plaza).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('登录后查看可用分组')
    expect(wrapper.find('input[type="checkbox"]').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('.plaza-button')).toHaveLength(0)
    wrapper.unmount()
  })
  it('数据失败可重试，源站过期状态明确显示', async () => {
    mocks.leaderboard.mockRejectedValueOnce(new Error('网络错误'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('榜单暂时无法加载')
    mocks.leaderboard.mockResolvedValueOnce({ ...snapshot, stale: true, last_attempt_failed: true })
    await wrapper.find('.gold-button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('最近同步失败，当前展示上次成功数据')
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    wrapper.unmount()
  })
})
