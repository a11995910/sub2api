import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelMarketView from '../ModelMarketView.vue'

const getAvailableChannels = vi.hoisted(() => vi.fn())
const getAvailableGroups = vi.hoisted(() => vi.fn())
const getUserGroupRates = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const push = vi.hoisted(() => vi.fn())

const messages: Record<string, string> = {
  'availableChannels.exclusive': '专属',
  'availableChannels.pricing.billingModeToken': 'Token',
  'availableChannels.pricing.billingModePerRequest': '按次',
  'availableChannels.pricing.billingModeImage': '图片',
  'common.error': '错误',
  'common.refresh': '刷新',
  'modelMarket.title': '模型广场',
  'modelMarket.description': '查看当前可调用模型、可用分组和倍率后的灵石价格',
  'modelMarket.searchPlaceholder': '搜索模型、平台或分组...',
  'modelMarket.empty': '暂无可展示的模型',
  'modelMarket.noPricing': '未配置定价',
  'modelMarket.intervalCount': '阶梯 {count} 档',
  'modelMarket.subscriptionGroup': '订阅分组',
  'modelMarket.groupSummary': '{count} 个模型，当前倍率 x{rate}',
  'modelMarket.noModelsInGroup': '当前分组暂无匹配模型',
  'modelMarket.effectiveRate': '生效倍率',
  'modelMarket.test': '去测试',
  'modelMarket.columns.input': '输入',
  'modelMarket.columns.output': '输出',
  'modelMarket.columns.cacheRead': '缓存读取',
  'modelMarket.columns.perRequest': '按次',
  'modelMarket.columns.multiplier': '倍率',
}

vi.mock('@/api/channels', () => ({
  default: {
    getAvailable: getAvailableChannels,
  },
}))

vi.mock('@/api/groups', () => ({
  default: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push,
  }),
}))

vi.mock('@/i18n', () => ({
  i18n: {
    global: {
      t: (key: string) => (key === 'common.currencyName' ? '灵石' : key),
    },
  },
}))

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

function groupFixture(overrides: Partial<Record<string, unknown>>) {
  return {
    id: 1,
    name: '文本分组',
    description: null,
    platform: 'openai',
    rate_multiplier: 1,
    rpm_limit: 0,
    is_exclusive: false,
    status: 'active',
    subscription_type: 'standard',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    allow_image_generation: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    claude_code_only: false,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    require_oauth_only: false,
    require_privacy_set: false,
    created_at: '2026-05-26T00:00:00Z',
    updated_at: '2026-05-26T00:00:00Z',
    ...overrides,
  }
}

const AppLayoutStub = { template: '<div><slot /></div>' }
const IconStub = { template: '<span />' }
const PlatformIconStub = { template: '<span />' }

describe('ModelMarketView', () => {
  beforeEach(() => {
    getAvailableChannels.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    push.mockReset()
  })

  it('展示用户可用分组全集，并把图片分组排在最后', async () => {
    const textGroup = groupFixture({ id: 1, name: '文本分组', allow_image_generation: false })
    const emptyGroup = groupFixture({ id: 2, name: '暂无模型分组', platform: 'anthropic', allow_image_generation: false })
    const imageGroup = groupFixture({
      id: 3,
      name: '图片分组',
      allow_image_generation: true,
      image_rate_independent: true,
      image_rate_multiplier: 2,
      image_price_1k: 1,
      image_price_2k: 2,
      image_price_4k: 4,
    })

    getAvailableGroups.mockResolvedValue([textGroup, imageGroup, emptyGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([
      {
        name: 'OpenAI 渠道',
        description: '',
        platforms: [
          {
            platform: 'openai',
            groups: [textGroup, imageGroup],
            supported_models: [
              {
                name: 'image-2',
                platform: 'openai',
                kind: 'image',
                pricing: { billing_mode: 'image', intervals: [] },
              },
              {
                name: 'gpt-4.1',
                platform: 'openai',
                kind: 'token',
                pricing: { billing_mode: 'token', input_price: 0.000001, output_price: 0.000002, intervals: [] },
              },
            ],
          },
        ],
      },
    ])

    const wrapper = mount(ModelMarketView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          Icon: IconStub,
          PlatformIcon: PlatformIconStub,
        },
      },
    })

    await flushPromises()

    const groupButtons = wrapper.findAll('button').filter((button) =>
      ['文本分组', '暂无模型分组', '图片分组'].some((name) => button.text().includes(name)),
    )
    const groupButtonTexts = groupButtons.map((button) => button.text())

    expect(groupButtonTexts).toEqual([
      expect.stringContaining('暂无模型分组'),
      expect.stringContaining('文本分组'),
      expect.stringContaining('图片分组'),
    ])

    await groupButtons[1].trigger('click')
    expect(wrapper.text()).toContain('gpt-4.1')
    expect(wrapper.text()).not.toContain('image-2')

    await groupButtons[0].trigger('click')
    expect(wrapper.text()).toContain('当前分组暂无匹配模型')

    await groupButtons[2].trigger('click')
    expect(wrapper.text()).toContain('image-2')
  })
})
