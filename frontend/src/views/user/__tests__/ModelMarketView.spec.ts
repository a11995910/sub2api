import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelMarketView from '../ModelMarketView.vue'

const getAvailableChannels = vi.hoisted(() => vi.fn())
const getAvailableGroups = vi.hoisted(() => vi.fn())
const getUserGroupRates = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const fetchPublicSettings = vi.hoisted(() => vi.fn())
const cachedPublicSettings = vi.hoisted(() => ({ model_market_usd_to_cny_rate: 7.2 }))
const push = vi.hoisted(() => vi.fn())

const messages: Record<string, string> = {
  'availableChannels.exclusive': '专属',
  'availableChannels.pricing.billingModeToken': 'Token',
  'availableChannels.pricing.billingModePerRequest': '按次',
  'availableChannels.pricing.billingModeImage': '图片',
  'availableChannels.pricing.billingModeVideo': '视频',
  'common.error': '错误',
  'common.refresh': '刷新',
  'modelTest.perSecond': '秒',
  'modelMarket.title': '模型广场',
  'modelMarket.description': '查看当前可调用模型、可用分组和倍率后的灵石价格',
  'modelMarket.searchPlaceholder': '搜索模型、平台或分组...',
  'modelMarket.groupPicker': '选择分组',
  'modelMarket.availableGroupCount': '{count} 个可用分组',
  'modelMarket.modelCount': '{count} 个模型',
  'modelMarket.empty': '暂无可展示的模型',
  'modelMarket.noPricing': '未配置定价',
  'modelMarket.intervalCount': '阶梯 {count} 档',
  'modelMarket.subscriptionGroup': '订阅分组',
  'modelMarket.groupSummary': '{count} 个模型，当前倍率 x{rate}',
  'modelMarket.noModelsInGroup': '当前分组暂无匹配模型',
  'modelMarket.effectiveRate': '生效倍率',
  'modelMarket.currentPrice': '当前',
  'modelMarket.officialPrice': '官方',
  'modelMarket.discount': '优惠',
  'modelMarket.test': '去测试',
  'modelMarket.sort.label': '模型排序',
  'modelMarket.sort.recommended': '推荐排序（GPT 优先）',
  'modelMarket.sort.nameAsc': '名称 A-Z',
  'modelMarket.sort.nameDesc': '名称 Z-A',
  'modelMarket.inputPrice': '输入价格',
  'modelMarket.perMillionTokens': '每百万 Token',
  'modelMarket.priceDetails': '价格明细',
  'modelMarket.inputMeaning': '发送给模型的内容',
  'modelMarket.outputMeaning': '模型回复内容',
  'modelMarket.cacheReadMeaning': '重复内容命中缓存',
  'modelMarket.cacheWriteMeaning': '首次写入可复用缓存',
  'modelMarket.officialReference': '官方输入参考',
  'modelMarket.discountCompared': '比官方参考低 {value}',
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
    fetchPublicSettings,
    cachedPublicSettings,
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
    image_super_resolution_enabled: false,
    image_4k_enhancement_enabled: false,
    image_4k_enhancement_group_id: null,
    image_4k_enhancement_model: null,
    image_rate_independent: false,
    cache_hit_quarter_to_input_enabled: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    video_rate_independent: false,
    video_rate_multiplier: 1,
    video_price_480p: null,
    video_price_720p: null,
    video_price_1080p: null,
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

function videoPricingFixture(perRequestPrice: number) {
  return {
    billing_mode: 'video',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_output_price: null,
    per_request_price: perRequestPrice,
    intervals: [],
  }
}

function modelCard(wrapper: ReturnType<typeof mount>, modelName: string) {
  const card = wrapper.findAll('article').find((article) =>
    article.find('h3').text().trim() === modelName,
  )
  expect(card, `未找到模型卡片 ${modelName}`).toBeDefined()
  return card!
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
    fetchPublicSettings.mockReset()
    cachedPublicSettings.model_market_usd_to_cny_rate = 7.2
    fetchPublicSettings.mockResolvedValue(cachedPublicSettings)
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
      expect.stringContaining('文本分组'),
      expect.stringContaining('暂无模型分组'),
      expect.stringContaining('图片分组'),
    ])
    expect(wrapper.get('[data-testid="group-grid"]').classes()).toContain('grid')
    expect(wrapper.get('[data-testid="group-grid"]').classes()).not.toContain('overflow-x-auto')

    await groupButtons[0].trigger('click')
    expect(wrapper.text()).toContain('gpt-4.1')
    expect(modelCard(wrapper, 'gpt-4.1').get('[data-testid="token-price-unit"]').text()).toBe('每百万 Token')
    expect(wrapper.text()).toContain('$1')
    expect(wrapper.text()).toContain('比官方参考低 86.1%')
    expect(wrapper.text()).not.toContain('image-2')

    await groupButtons[1].trigger('click')
    expect(wrapper.text()).toContain('当前分组暂无匹配模型')

    await groupButtons[2].trigger('click')
    expect(wrapper.text()).toContain('image-2')
  })

  it('人民币渠道原价显示人民币符号并按一灵石等于一元计算优惠', async () => {
    const cnyGroup = groupFixture({
      id: 11,
      name: '国产模型分组',
      platform: 'zhipu',
      rate_multiplier: 0.5,
    })
    getAvailableGroups.mockResolvedValue([cnyGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: '国产模型渠道',
      description: '',
      platforms: [{
        platform: 'zhipu',
        groups: [cnyGroup],
        supported_models: [{
          name: 'glm-5',
          platform: 'zhipu',
          kind: 'token',
          pricing: {
            billing_mode: 'token',
            price_currency: 'CNY',
            input_price: 0.000002,
            output_price: 0.000008,
            cache_write_price: null,
            cache_read_price: null,
            image_output_price: null,
            per_request_price: null,
            intervals: [],
          },
        }],
      }],
    }])

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

    const card = modelCard(wrapper, 'glm-5')
    expect(card.text()).toContain('1 灵石')
    expect(card.get('[data-testid="token-official-price"]').text()).toBe('¥2')
    expect(card.get('[data-testid="token-price-discount"]').text()).toBe('优惠 50.0%')
    expect(card.text()).not.toContain('$2')
  })

  it('美元汇率未配置时保留美元原价但不显示优惠', async () => {
    cachedPublicSettings.model_market_usd_to_cny_rate = 0
    const usdGroup = groupFixture({ id: 12, rate_multiplier: 0.5 })
    getAvailableGroups.mockResolvedValue([usdGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: '美元渠道',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [usdGroup],
        supported_models: [{
          name: 'gpt-usd',
          platform: 'openai',
          kind: 'token',
          pricing: {
            billing_mode: 'token',
            price_currency: 'USD',
            input_price: 0.000002,
            output_price: 0.000008,
            intervals: [],
          },
        }],
      }],
    }])

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

    const card = modelCard(wrapper, 'gpt-usd')
    expect(card.get('[data-testid="token-official-price"]').text()).toBe('$2')
    expect(card.find('[data-testid="token-price-discount"]').exists()).toBe(false)
  })

  it('同平台模型只展示到持久号池支持的分组', async () => {
    const supportedGroup = groupFixture({ id: 74, name: 'pro正价分组' })
    const unsupportedGroup = groupFixture({ id: 75, name: '其他正价分组' })
    getAvailableGroups.mockResolvedValue([supportedGroup, unsupportedGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: 'OpenAI 渠道',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [supportedGroup, unsupportedGroup],
        supported_models: [{
          name: 'gpt-5.6',
          platform: 'openai',
          kind: 'token',
          group_ids: [74],
          pricing: { billing_mode: 'token', input_price: 0.000002, output_price: 0.000008, intervals: [] },
        }],
      }],
    }])

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

    const supportedButton = wrapper.findAll('[data-testid="group-option"]')
      .find((button) => button.text().includes('pro正价分组'))
    const unsupportedButton = wrapper.findAll('[data-testid="group-option"]')
      .find((button) => button.text().includes('其他正价分组'))
    expect(supportedButton).toBeDefined()
    expect(unsupportedButton).toBeDefined()

    await supportedButton!.trigger('click')
    expect(wrapper.text()).toContain('gpt-5.6')

    await unsupportedButton!.trigger('click')
    expect(wrapper.text()).not.toContain('gpt-5.6')
    expect(wrapper.text()).toContain('当前分组暂无匹配模型')
  })

  it('默认优先 GPT 分组和 GPT 模型，并支持按名称重新排序', async () => {
    const claudeGroup = groupFixture({
      id: 20,
      name: 'Claude 分组',
      platform: 'anthropic',
    })
    const gptGroup = groupFixture({
      id: 21,
      name: 'GPT 分组',
      platform: 'openai',
    })

    getAvailableGroups.mockResolvedValue([claudeGroup, gptGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([
      {
        name: 'Anthropic 渠道',
        description: '',
        platforms: [{
          platform: 'anthropic',
          groups: [claudeGroup],
          supported_models: [{
            name: 'claude-sonnet-4-6',
            platform: 'anthropic',
            kind: 'token',
            pricing: { billing_mode: 'token', input_price: 0.000003, output_price: 0.000015, intervals: [] },
          }],
        }],
      },
      {
        name: 'OpenAI 渠道',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [gptGroup],
          supported_models: [
            {
              name: 'alpha-chat',
              platform: 'openai',
              kind: 'token',
              pricing: { billing_mode: 'token', input_price: 0.000001, output_price: 0.000004, intervals: [] },
            },
            {
              name: 'gpt-5.6',
              platform: 'openai',
              kind: 'token',
              pricing: { billing_mode: 'token', input_price: 0.000002, output_price: 0.000008, intervals: [] },
            },
          ],
        }],
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

    const groupButtons = wrapper.findAll('[data-testid="group-option"]')
    expect(groupButtons.map((button) => button.text())).toEqual([
      expect.stringContaining('GPT 分组'),
      expect.stringContaining('Claude 分组'),
    ])
    expect(wrapper.get('[data-testid="selected-group-title"]').text()).toBe('GPT 分组')
    expect(wrapper.findAll('article').map((card) => card.get('h3').text())).toEqual(['gpt-5.6', 'alpha-chat'])

    await wrapper.get('select[name="model-sort"]').setValue('name-asc')
    expect(wrapper.findAll('article').map((card) => card.get('h3').text())).toEqual(['alpha-chat', 'gpt-5.6'])

    await wrapper.get('select[name="model-sort"]').setValue('name-desc')
    expect(wrapper.findAll('article').map((card) => card.get('h3').text())).toEqual(['gpt-5.6', 'alpha-chat'])
  })

  it.each([
    {
      name: '分组 720p 覆盖价',
      modelName: 'grok-imagine-video-1.5',
      groupOverrides: {
        video_rate_independent: true,
        video_rate_multiplier: 2,
        video_price_720p: 0.03,
      },
      billingMode: 'video',
      defaultPrice: 0.07,
      intervalPrice: 0.14,
      expectedPrice: '0.06 灵石',
      expectedUnit: '720p / 秒',
    },
    {
      name: '渠道 720p 层级价',
      modelName: 'grok-imagine-video',
      groupOverrides: {},
      billingMode: 'video',
      defaultPrice: 0.07,
      intervalPrice: 0.14,
      expectedPrice: '0.14 灵石',
      expectedUnit: '720p / 秒',
    },
    {
      name: '历史图片模式渠道默认价',
      modelName: 'grok-imagine-video',
      groupOverrides: {},
      billingMode: 'image',
      defaultPrice: 2.1,
      intervalPrice: undefined,
      expectedPrice: '2.1 灵石',
      expectedUnit: '720p / 按次',
    },
  ])('视频价格卡展示$name及正确单位', async ({
    groupOverrides,
    modelName,
    billingMode,
    defaultPrice,
    intervalPrice,
    expectedPrice,
    expectedUnit,
  }) => {
    const videoGroup = groupFixture({
      id: 8,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
      ...groupOverrides,
    })
    const intervals = intervalPrice === undefined
      ? []
      : [{ tier_label: '720p', per_request_price: intervalPrice }]

    getAvailableGroups.mockResolvedValue([videoGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: 'Grok 渠道',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [videoGroup],
        supported_models: [{
          name: modelName,
          platform: 'grok',
          kind: 'video',
          pricing: {
            billing_mode: billingMode,
            input_price: null,
            output_price: null,
            cache_write_price: null,
            cache_read_price: null,
            image_output_price: null,
            per_request_price: defaultPrice,
            intervals,
          },
        }],
      }],
    }])

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

    expect(wrapper.text()).toContain(modelName)
    expect(wrapper.text()).toContain(expectedUnit)
    expect(wrapper.text()).toContain(expectedPrice)
  })

  it('同名标准视频模型的不同定价卡分别使用自身价格', async () => {
    const videoGroup = groupFixture({
      id: 8,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    getAvailableGroups.mockResolvedValue([videoGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: 'Grok 渠道',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [videoGroup],
        supported_models: [
          {
            name: 'grok-imagine-video',
            platform: 'grok',
            kind: 'video',
            pricing: videoPricingFixture(0.07),
          },
          {
            name: 'grok-imagine-video',
            platform: 'grok',
            kind: 'video',
            pricing: videoPricingFixture(0.09),
          },
        ],
      }],
    }])

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

    const cards = wrapper.findAll('article').filter((article) =>
      article.find('h3').text().trim() === 'grok-imagine-video',
    )
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('720p / 秒当前0.07 灵石')
    expect(cards[1].text()).toContain('720p / 秒当前0.09 灵石')
  })

  it('video-1.5 默认无参考图时使用同平台标准模型渠道价', async () => {
    const videoGroup = groupFixture({
      id: 8,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    getAvailableGroups.mockResolvedValue([videoGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: 'Grok 渠道',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [videoGroup],
        supported_models: [
          {
            name: ' GROK-IMAGINE-VIDEO ',
            platform: 'grok',
            kind: 'video',
            pricing: videoPricingFixture(0.09),
          },
          {
            name: 'grok-imagine-video-1.5',
            platform: 'grok',
            kind: 'video',
            pricing: videoPricingFixture(0.14),
          },
        ],
      }],
    }])

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

    const cardText = modelCard(wrapper, 'grok-imagine-video-1.5').text()
    expect(cardText).toContain('720p / 秒当前0.14 灵石')
    expect(cardText).toContain('1080p / 秒当前0.14 灵石')
  })

  it('video-1.5 模型广场独立展示自身渠道价且不展示参考图费用', async () => {
    const videoGroup = groupFixture({
      id: 8,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    getAvailableGroups.mockResolvedValue([videoGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: 'Grok 渠道',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [videoGroup],
        supported_models: [{
          name: 'grok-imagine-video-1.5',
          platform: 'grok',
          kind: 'video',
          pricing: videoPricingFixture(0.22),
        }],
      }],
    }])

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

    const cardText = modelCard(wrapper, 'grok-imagine-video-1.5').text()
    expect(cardText).toContain('720p / 秒当前0.22 灵石')
    expect(cardText).toContain('1080p / 秒')
    expect(cardText).not.toContain('+0.01 灵石')
  })

  it('视频名称模型使用 token 定价时展示文本倍率', async () => {
    const videoGroup = groupFixture({
      id: 8,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
      rate_multiplier: 2,
      video_rate_independent: true,
      video_rate_multiplier: 3,
      video_price_720p: 0.03,
    })
    getAvailableGroups.mockResolvedValue([videoGroup])
    getUserGroupRates.mockResolvedValue({ 8: 1.25 })
    getAvailableChannels.mockResolvedValue([{
      name: 'Grok 渠道',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [videoGroup],
        supported_models: [{
          name: 'grok-imagine-video-token-preview',
          platform: 'grok',
          kind: 'video',
          pricing: {
            billing_mode: 'token',
            input_price: 0.000001,
            output_price: 0.000002,
            cache_write_price: null,
            cache_read_price: null,
            image_output_price: null,
            per_request_price: null,
            intervals: [],
          },
        }],
      }],
    }])

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

    expect(wrapper.text()).toContain('生效倍率x1.25')
    expect(wrapper.text()).not.toContain('x3.00')
  })

  it('历史视频渠道未命中当前分辨率且无默认价时显示无价格', async () => {
    const videoGroup = groupFixture({
      id: 8,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    getAvailableGroups.mockResolvedValue([videoGroup])
    getUserGroupRates.mockResolvedValue({})
    getAvailableChannels.mockResolvedValue([{
      name: 'Grok 渠道',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [videoGroup],
        supported_models: [{
          name: 'grok-imagine-video',
          platform: 'grok',
          kind: 'video',
          pricing: {
            billing_mode: 'image',
            input_price: null,
            output_price: null,
            cache_write_price: null,
            cache_read_price: null,
            image_output_price: null,
            per_request_price: null,
            intervals: [{ tier_label: '480p', per_request_price: 1.8 }],
          },
        }],
      }],
    }])

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

    expect(wrapper.text()).toContain('720p / 按次当前-')
  })
})
