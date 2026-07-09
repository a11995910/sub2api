import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type DOMWrapper, type VueWrapper } from '@vue/test-utils'

import ModelTestView from '../ModelTestView.vue'

const getAvailableChannels = vi.hoisted(() => vi.fn())
const getUserGroupRates = vi.hoisted(() => vi.fn())
const listKeys = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const showWarning = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())
const push = vi.hoisted(() => vi.fn())
const routeState = vi.hoisted(() => ({ query: {} as Record<string, unknown> }))

const messages: Record<string, string> = {
  'availableChannels.pricing.billingModeToken': 'Token',
  'common.cancel': '取消',
  'common.currencyName': '灵石',
  'common.error': '错误',
  'common.refresh': '刷新',
  'modelTest.empty': '暂无可测试模型',
  'modelTest.fields.apiKey': 'API Key',
  'modelTest.fields.group': '分组',
  'modelTest.fields.imageSize': '图片尺寸',
  'modelTest.fields.maxTokens': '最大输出',
  'modelTest.fields.model': '模型',
  'modelTest.fields.prompt': '提示词',
  'modelTest.fields.referenceImages': '参考图片',
  'modelTest.fields.type': '类型',
  'modelTest.goCreateKey': '去创建 Key',
  'modelTest.imageSizeAdaptivePreview': '自适应（{tier} 预估）',
  'modelTest.imageSizeOptions.adaptive': '自适应',
  'modelTest.loadKeysFailed': '加载 API Key 失败',
  'modelTest.modes.image': '图片',
  'modelTest.modes.text': '文本',
  'modelTest.perImage': '张',
  'modelTest.placeholders.apiKey': '请选择 API Key',
  'modelTest.placeholders.group': '请选择分组',
  'modelTest.placeholders.imagePrompt': '描述要生成的图片',
  'modelTest.placeholders.model': '请选择模型',
  'modelTest.placeholders.textPrompt': '输入要测试的文本提示词',
  'modelTest.realBillingNotice': '本测试会调用真实网关',
  'modelTest.referenceImageLimit': '最多上传 {count} 张参考图片',
  'modelTest.referenceImageSizeError': '单张图片不能超过 {size}',
  'modelTest.referenceImageTypeError': '请选择图片文件',
  'modelTest.referenceImagesHint': '上传后将调用图片编辑接口',
  'modelTest.removeReferenceImage': '移除参考图',
  'modelTest.result.empty': '运行一次测试后，这里会显示返回结果',
  'modelTest.result.raw': '原始响应',
  'modelTest.result.title': '测试结果',
  'modelTest.result.waiting': '正在请求真实网关...',
  'modelTest.run': '运行测试',
  'modelTest.running': '测试中...',
  'modelTest.runFailed': '测试失败',
  'modelTest.runSuccess': '测试完成',
  'modelTest.summary.endpoint': '请求端点',
  'modelTest.summary.groupRate': '当前倍率',
  'modelTest.summary.input': '输入',
  'modelTest.summary.output': '输出',
  'modelTest.summary.price': '价格预览',
  'modelTest.uploadReferenceImages': '上传图片',
  'modelTest.validation.missingSelection': '请先选择模型、分组和 API Key',
  'modelTest.validation.promptRequired': '请输入提示词',
}

vi.mock('@/api/channels', () => ({
  default: {
    getAvailable: getAvailableChannels,
  },
}))

vi.mock('@/api/groups', () => ({
  default: {
    getUserGroupRates,
  },
}))

vi.mock('@/api/keys', () => ({
  default: {
    list: listKeys,
  },
}))

vi.mock('@/api/modelTest', () => ({
  ModelTestError: class ModelTestError extends Error {
    status = 500
    payload: unknown = null
  },
  testChatCompletion: vi.fn(),
  testImageEdit: vi.fn(),
  testImageGeneration: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning,
  }),
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
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

const AppLayoutStub = { template: '<div><slot /></div>' }
const IconStub = { template: '<span />' }
const PlatformIconStub = { template: '<span />' }

function groupFixture(overrides: Record<string, unknown>) {
  return {
    id: 1,
    name: '文本分组',
    platform: 'openai',
    subscription_type: 'standard',
    rate_multiplier: 1,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    allow_image_generation: false,
    image_super_resolution_enabled: false,
    image_rate_independent: false,
    cache_hit_quarter_to_input_enabled: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    ...overrides,
  }
}

function apiKeyFixture(overrides: Record<string, unknown>) {
  return {
    id: 101,
    user_id: 1,
    key: 'sk-text-key-1234567890',
    name: '文本 Key',
    group_id: 1,
    status: 'active',
    openai_fast_mode_enabled: false,
    ip_whitelist: [],
    ip_blacklist: [],
    last_used_at: null,
    quota: 0,
    quota_used: 0,
    expires_at: null,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    rate_limit_5h: 0,
    rate_limit_1d: 0,
    rate_limit_7d: 0,
    usage_5h: 0,
    usage_1d: 0,
    usage_7d: 0,
    window_5h_start: null,
    window_1d_start: null,
    window_7d_start: null,
    reset_5h_at: null,
    reset_1d_at: null,
    reset_7d_at: null,
    ...overrides,
  }
}

function mountView() {
  return mount(ModelTestView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Icon: IconStub,
        PlatformIcon: PlatformIconStub,
      },
    },
  })
}

function selectByLabel(wrapper: VueWrapper, labelText: string): HTMLSelectElement {
  const label = wrapper.findAll('label').find((item) => item.text() === labelText)
  if (!label?.exists()) {
    throw new Error(`找不到字段标签：${labelText}`)
  }
  const select = label.element.parentElement?.querySelector('select')
  if (!select) {
    throw new Error(`字段没有下拉框：${labelText}`)
  }
  return select
}

function selectWrapperByLabel(wrapper: VueWrapper, labelText: string): DOMWrapper<HTMLSelectElement> {
  const select = selectByLabel(wrapper, labelText)
  const selectWrapper = wrapper.findAll('select').find((item) => item.element === select)
  if (!selectWrapper) {
    throw new Error(`找不到字段下拉组件：${labelText}`)
  }
  return selectWrapper as DOMWrapper<HTMLSelectElement>
}

function optionTexts(select: HTMLSelectElement): string[] {
  return Array.from(select.options).map((option) => option.textContent?.trim() || '')
}

describe('ModelTestView', () => {
  const textGroup = groupFixture({ id: 1, name: '文本分组', allow_image_generation: false })
  const imageGroup = groupFixture({
    id: 2,
    name: '图片分组',
    allow_image_generation: true,
    image_rate_independent: true,
    image_rate_multiplier: 2,
    image_price_1k: 1,
    image_price_2k: 2,
    image_price_4k: 4,
  })
  const textKey = apiKeyFixture({ id: 101, name: '文本 Key', key: 'sk-text-key-1234567890', group_id: 1 })
  const imageKey = apiKeyFixture({ id: 202, name: '图片 Key', key: 'sk-image-key-1234567890', group_id: 2 })

  beforeEach(() => {
    routeState.query = {}
    getAvailableChannels.mockReset()
    getUserGroupRates.mockReset()
    listKeys.mockReset()
    showError.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
    push.mockReset()

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
                name: 'gpt-4.1',
                platform: 'openai',
                kind: 'token',
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
              },
              {
                name: 'image-2',
                platform: 'openai',
                kind: 'image',
                pricing: {
                  billing_mode: 'image',
                  input_price: null,
                  output_price: null,
                  cache_write_price: null,
                  cache_read_price: null,
                  image_output_price: null,
                  per_request_price: null,
                  intervals: [],
                },
              },
            ],
          },
        ],
      },
    ])
  })

  it('默认优先选择用户已有 active Key，并带出该 Key 所属分组和可用模型', async () => {
    listKeys.mockResolvedValue({ items: [imageKey, textKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    const apiKeySelect = selectByLabel(wrapper, 'API Key')
    const groupSelect = selectByLabel(wrapper, '分组')
    const modelSelect = selectByLabel(wrapper, '模型')

    expect(apiKeySelect.value).toBe('202')
    expect(groupSelect.value).toBe('2')
    expect(optionTexts(modelSelect)).toEqual(['请选择模型', 'image-2 · OpenAI'])
    expect(modelSelect.value).not.toBe('')
  })

  it('从模型广场带 group_id 进入时，会选择同分组 Key 并限制模型范围', async () => {
    routeState.query = {
      kind: 'image',
      group_id: '2',
      model: 'image-2',
      platform: 'openai',
    }
    listKeys.mockResolvedValue({ items: [textKey, imageKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    const apiKeySelect = selectByLabel(wrapper, 'API Key')
    const groupSelect = selectByLabel(wrapper, '分组')
    const modelSelect = selectByLabel(wrapper, '模型')

    expect(apiKeySelect.value).toBe('202')
    expect(groupSelect.value).toBe('2')
    expect(optionTexts(modelSelect)).toEqual(['请选择模型', 'image-2 · OpenAI'])
    expect(modelSelect.value).not.toBe('')
  })

  it('用户切换 API Key 后，会改为该 Key 所属分组并刷新可选模型', async () => {
    listKeys.mockResolvedValue({ items: [textKey, imageKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    const apiKeySelect = selectWrapperByLabel(wrapper, 'API Key')
    expect(optionTexts(apiKeySelect.element)).toEqual([
      '请选择 API Key',
      '文本 Key · sk-text...7890',
      '图片 Key · sk-imag...7890',
    ])

    await apiKeySelect.setValue('202')
    await flushPromises()

    const groupSelect = selectByLabel(wrapper, '分组')
    const modelSelect = selectByLabel(wrapper, '模型')

    expect(groupSelect.value).toBe('2')
    expect(optionTexts(modelSelect)).toEqual(['请选择模型', 'image-2 · OpenAI'])
    expect(modelSelect.value).not.toBe('')
  })

  it('Grok 图片能力分组在文本模式下仍能选择文本模型', async () => {
    const grokGroup = groupFixture({
      id: 56,
      name: 'grok测试',
      platform: 'grok',
      allow_image_generation: true,
      image_rate_independent: true,
      image_rate_multiplier: 1,
      image_price_1k: 0.05,
      image_price_2k: 0.07,
      image_price_4k: 0.07,
    })
    const grokKey = apiKeyFixture({
      id: 1088,
      name: 'grok',
      key: 'sk-grok-key-1234567890',
      group_id: 56,
    })
    getAvailableChannels.mockResolvedValue([
      {
        name: 'grok',
        description: '',
        platforms: [
          {
            platform: 'grok',
            groups: [grokGroup],
            supported_models: [
              {
                name: 'grok-4.3',
                platform: 'grok',
                kind: 'token',
                pricing: {
                  billing_mode: 'token',
                  input_price: 0.00000125,
                  output_price: 0.0000025,
                  cache_write_price: null,
                  cache_read_price: 0.0000002,
                  image_output_price: null,
                  per_request_price: null,
                  intervals: [],
                },
              },
              {
                name: 'grok-imagine-image',
                platform: 'grok',
                kind: 'image',
                pricing: {
                  billing_mode: 'image',
                  input_price: null,
                  output_price: null,
                  cache_write_price: null,
                  cache_read_price: null,
                  image_output_price: null,
                  per_request_price: 0.02,
                  intervals: [],
                },
              },
            ],
          },
        ],
      },
    ])
    listKeys.mockResolvedValue({ items: [grokKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    const groupSelect = selectByLabel(wrapper, '分组')
    const modelSelect = selectByLabel(wrapper, '模型')

    expect(groupSelect.value).toBe('56')
    expect(optionTexts(modelSelect).some((text) => text.includes('grok-4.3'))).toBe(true)
    expect(modelSelect.value).not.toBe('')
  })
})
