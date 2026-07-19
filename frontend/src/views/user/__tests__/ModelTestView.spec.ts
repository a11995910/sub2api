import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
const testVideoGeneration = vi.hoisted(() => vi.fn())
const listGatewayModels = vi.hoisted(() => vi.fn())
const listVideoTestTasks = vi.hoisted(() => vi.fn())
const refreshVideoTestTask = vi.hoisted(() => vi.fn())
const deleteVideoTestTask = vi.hoisted(() => vi.fn())
const fetchVideoTestTaskContent = vi.hoisted(() => vi.fn())

const messages: Record<string, string> = {
  'availableChannels.pricing.billingModeToken': 'Token',
  'availableChannels.pricing.billingModePerRequest': '按次',
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
  'modelTest.fields.videoReferenceImage': '起始参考图（可选）',
  'modelTest.fields.videoReferenceImages': '视频参考图（可选）',
  'modelTest.fields.videoDuration': '视频时长（秒）',
  'modelTest.fields.videoResolution': '视频分辨率',
  'modelTest.fields.type': '类型',
  'modelTest.goCreateKey': '去创建 Key',
  'modelTest.imageSizeAdaptivePreview': '自适应（{tier} 预估）',
  'modelTest.imageSizeOptions.adaptive': '自适应',
  'modelTest.loadKeysFailed': '加载 API Key 失败',
  'modelTest.modes.image': '图片',
  'modelTest.modes.text': '文本',
  'modelTest.modes.video': '视频',
  'modelTest.perImage': '张',
  'modelTest.perSecond': '秒',
  'modelTest.placeholders.apiKey': '请选择 API Key',
  'modelTest.placeholders.group': '请选择分组',
  'modelTest.placeholders.imagePrompt': '描述要生成的图片',
  'modelTest.placeholders.model': '请选择模型',
  'modelTest.placeholders.textPrompt': '输入要测试的文本提示词',
  'modelTest.placeholders.videoPrompt': '描述要生成的视频',
  'modelTest.realBillingNotice': '本测试会调用真实网关',
  'modelTest.referenceImageLimit': '最多上传 {count} 张参考图片',
  'modelTest.referenceImageSizeError': '单张图片不能超过 {size}',
  'modelTest.referenceImageTypeError': '请选择图片文件',
  'modelTest.referenceImagesHint': '上传后将调用图片编辑接口',
  'modelTest.videoReferenceImageHint': '可上传 1 张起始参考图',
  'modelTest.videoReferenceImagesHint': '支持上传最多 {count} 张参考图；超过 {size} 时会自动压缩',
  'modelTest.videoReferenceImageUnsupported': '当前模型 {model} 不支持视频参考图，请选择支持参考图的视频模型。',
  'modelTest.videoReferenceImageCompressing': '图片大小为 {original}，正在压缩到 {target}',
  'modelTest.videoReferenceImageCompressed': '图片已从 {original} 压缩到 {compressed}',
  'modelTest.videoReferenceImageCompressedSize': '{original} -> {compressed}',
  'modelTest.videoReferenceImageCompressFailed': '图片无法压缩到 {size}',
  'modelTest.compressingVideoReferenceImage': '正在压缩',
  'modelTest.removeReferenceImage': '移除参考图',
  'modelTest.result.empty': '运行一次测试后，这里会显示返回结果',
  'modelTest.result.raw': '原始响应',
  'modelTest.result.title': '测试结果',
  'modelTest.result.waiting': '正在请求真实网关...',
  'modelTest.run': '运行测试',
  'modelTest.running': '测试中...',
  'modelTest.runFailed': '测试失败',
  'modelTest.runSuccess': '测试完成',
  'modelTest.videoTasks.submitSuccess': '视频任务已提交',
  'modelTest.videoTasks.title': '视频任务记录',
  'modelTest.videoTasks.empty': '暂无视频任务',
  'modelTest.videoTasks.pollError': '暂时无法查询，仍在等待',
  'modelTest.summary.endpoint': '请求端点',
  'modelTest.summary.groupRate': '当前倍率',
  'modelTest.summary.input': '输入',
  'modelTest.summary.output': '输出',
  'modelTest.summary.price': '价格预览',
  'modelTest.uploadReferenceImages': '上传图片',
  'modelTest.uploadVideoReferenceImage': '添加参考图',
  'modelTest.uploadVideoReferenceImages': '上传参考图',
  'modelTest.validation.missingSelection': '请先选择模型、分组和 API Key',
  'modelTest.validation.promptRequired': '请输入提示词',
  'modelTest.validation.videoReferenceImageUnsupported': '当前模型不支持起始参考图',
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
  testVideoGeneration,
  listGatewayModels,
  extractVideoURL: (payload: any) => String(payload?.video?.url || ''),
}))

vi.mock('@/api/videoTestTasks', () => ({
  listVideoTestTasks,
  refreshVideoTestTask,
  deleteVideoTestTask,
  fetchVideoTestTaskContent,
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
    video_rate_independent: false,
    video_rate_multiplier: 1,
    video_price_480p: null,
    video_price_720p: null,
    video_price_1080p: null,
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

function summaryValue(wrapper: VueWrapper, labelText: string): string {
  const label = wrapper.findAll('p').find((item) => item.text() === labelText)
  const value = label?.element.parentElement?.querySelectorAll('p')[1]?.textContent?.trim()
  if (!value) {
    throw new Error(`找不到摘要值：${labelText}`)
  }
  return value
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
    testVideoGeneration.mockReset()
    listVideoTestTasks.mockReset()
    refreshVideoTestTask.mockReset()
    deleteVideoTestTask.mockReset()
    fetchVideoTestTaskContent.mockReset()
    listGatewayModels.mockReset()
    listGatewayModels.mockResolvedValue([])
    listVideoTestTasks.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    window.URL.createObjectURL = vi.fn(() => 'blob:model-test-reference') as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL

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

  afterEach(() => {
    vi.useRealTimers()
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

  it('无渠道分组从当前 API Key 的网关模型列表读取 Seedance', async () => {
    const seedanceGroup = groupFixture({
      id: 62,
      name: '视频测试',
      platform: 'openai',
      allow_image_generation: true,
    })
    const seedanceKey = apiKeyFixture({
      id: 9960,
      name: '视频 Key',
      key: 'sk-seedance-key-1234567890',
      group_id: 62,
      group: seedanceGroup,
    })
    getAvailableChannels.mockResolvedValue([])
    listKeys.mockResolvedValue({ items: [seedanceKey], pages: 1 })
    listGatewayModels.mockResolvedValue([
      'dreamina-seedance-2-0-ep',
      'dreamina-seedance-2-0-mini-ep',
    ])

    const wrapper = mountView()
    await flushPromises()

    expect(listGatewayModels).toHaveBeenCalledWith(seedanceKey.key)
    expect(selectByLabel(wrapper, '分组').value).toBe('62')
    expect(optionTexts(selectByLabel(wrapper, '模型'))).toEqual([
      '请选择模型',
      'dreamina-seedance-2-0-ep · OpenAI',
      'dreamina-seedance-2-0-mini-ep · OpenAI',
    ])
    expect(summaryValue(wrapper, '价格预览')).toBe('720p 1.2 灵石 / 8秒')
  })

  it('Seedance 视频提交后立即刷新任务记录且不等待内容下载', async () => {
    const seedanceGroup = groupFixture({
      id: 62,
      name: '视频测试',
      platform: 'openai',
      allow_image_generation: true,
    })
    const seedanceKey = apiKeyFixture({
      id: 9960,
      name: '视频 Key',
      key: 'sk-seedance-key-1234567890',
      group_id: 62,
      group: seedanceGroup,
    })
    getAvailableChannels.mockResolvedValue([])
    listKeys.mockResolvedValue({ items: [seedanceKey], pages: 1 })
    listGatewayModels.mockResolvedValue(['dreamina-seedance-2-0-mini-ep'])
    testVideoGeneration.mockResolvedValue({
      payload: {
        id: 'chatcmpl_seedance',
        status: 'queued',
      },
      requestID: 'chatcmpl_seedance',
    })
    listVideoTestTasks
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 })
      .mockResolvedValueOnce({
        items: [{
          id: 'local-seedance',
          api_key_id: 9960,
          group_id: 62,
          upstream_task_id: 'chatcmpl_seedance',
          platform: 'openai',
          model: 'dreamina-seedance-2-0-mini-ep',
          prompt: '生成纯黑背景视频',
          reference_image_count: 0,
          status: 'queued',
          created_at: '2026-07-19T12:00:00Z',
          updated_at: '2026-07-19T12:00:00Z',
        }],
        total: 1,
        page: 1,
        page_size: 20,
      })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('textarea').setValue('生成纯黑背景视频')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchVideoTestTaskContent).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('视频任务记录')
    expect(wrapper.text()).toContain('生成纯黑背景视频')
    expect(showSuccess).toHaveBeenCalledWith('视频任务已提交')
  })

  it('未知模型名可按视频意图选择且价格未知不阻止测试', async () => {
    const videoGroup = groupFixture({
      id: 63,
      name: '未来视频',
      platform: 'openai',
      allow_image_generation: true,
    })
    const videoKey = apiKeyFixture({
      id: 9961,
      name: '未来视频 Key',
      key: 'sk-future-video-1234567890',
      group_id: 63,
      group: videoGroup,
    })
    getAvailableChannels.mockResolvedValue([])
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })
    listGatewayModels.mockResolvedValue(['future-motion-pro'])
    testVideoGeneration.mockResolvedValue({
      payload: { id: 'future-task-1', status: 'completed' },
      requestID: 'future-task-1',
    })

    const wrapper = mountView()
    await flushPromises()
    const videoButton = wrapper.findAll('button').find((button) => button.text().trim() === '视频')
    expect(videoButton).toBeDefined()
    await videoButton!.trigger('click')
    await flushPromises()

    expect(optionTexts(selectByLabel(wrapper, '模型'))).toEqual([
      '请选择模型',
      'future-motion-pro · OpenAI',
    ])
    expect(summaryValue(wrapper, '价格预览')).toBe('-')
    expect(wrapper.find('input[type="file"]').attributes('disabled')).toBeDefined()

    await wrapper.find('textarea').setValue('生成未来城市镜头')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(testVideoGeneration).toHaveBeenCalledWith(expect.objectContaining({
      model: 'future-motion-pro',
      prompt: '生成未来城市镜头',
    }))
    expect(fetchVideoTestTaskContent).not.toHaveBeenCalled()
  })

  it('加载历史任务并仅在页面可见时轮询未完成任务', async () => {
    vi.useFakeTimers()
    const pendingTask = {
      id: 'local-pending',
      api_key_id: 101,
      group_id: 1,
      upstream_task_id: 'upstream-pending',
      platform: 'openai',
      model: 'future-motion-pro',
      prompt: '等待中的任务',
      reference_image_count: 0,
      status: 'in_progress' as const,
      created_at: '2026-07-19T12:00:00Z',
      updated_at: '2026-07-19T12:00:00Z',
    }
    listKeys.mockResolvedValue({ items: [textKey], pages: 1 })
    listVideoTestTasks.mockResolvedValue({ items: [pendingTask], total: 1, page: 1, page_size: 20 })
    refreshVideoTestTask.mockResolvedValue(pendingTask)

    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('等待中的任务')

    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(5000)
    expect(refreshVideoTestTask).not.toHaveBeenCalled()

    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(refreshVideoTestTask).toHaveBeenCalledWith('local-pending')
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
              {
                name: 'grok-imagine-edit',
                platform: 'grok',
                kind: 'image',
                pricing: {
                  billing_mode: 'image',
                  input_price: null,
                  output_price: null,
                  cache_write_price: null,
                  cache_read_price: null,
                  image_output_price: null,
                  per_request_price: 0.08,
                  intervals: [],
                },
              },
              {
                name: 'grok-imagine-video',
                platform: 'grok',
                kind: 'image',
                pricing: {
                  billing_mode: 'image',
                  input_price: null,
                  output_price: null,
                  cache_write_price: null,
                  cache_read_price: null,
                  image_output_price: null,
                  per_request_price: 2.1,
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

    const imageModeButton = wrapper.findAll('button').find((button) => button.text() === '图片')
    if (!imageModeButton) {
      throw new Error('找不到图片模式按钮')
    }
    await imageModeButton.trigger('click')
    await flushPromises()

    const imageModelOptions = optionTexts(selectByLabel(wrapper, '模型'))
    expect(imageModelOptions.some((text) => text.includes('grok-imagine-image'))).toBe(true)
    expect(imageModelOptions.some((text) => text.includes('grok-imagine-edit'))).toBe(false)
    expect(imageModelOptions.some((text) => text.includes('grok-imagine-video'))).toBe(false)

    const fileInput = wrapper.find('input[type="file"]')
    Object.defineProperty(fileInput.element, 'files', {
      value: [new File(['reference image'], 'reference.png', { type: 'image/png' })],
      configurable: true,
    })
    await fileInput.trigger('change')
    await flushPromises()

    const editModelOptions = optionTexts(selectByLabel(wrapper, '模型'))
    expect(editModelOptions.some((text) => text.includes('grok-imagine-image'))).toBe(true)
    expect(editModelOptions.some((text) => text.includes('grok-imagine-edit'))).toBe(true)
    expect(editModelOptions.some((text) => text.includes('grok-imagine-video'))).toBe(false)
  })

  it.each([
    {
      name: '渠道 720p 每秒层级价乘视频时长',
      groupOverrides: {},
      billingMode: 'video',
      defaultPrice: 0.07,
      intervalPrice: 0.14,
      expectedPreview: '720p 1.12 灵石 / 8秒',
    },
    {
      name: '分组 720p 每秒覆盖价乘独立视频倍率和时长',
      groupOverrides: {
        video_rate_independent: true,
        video_rate_multiplier: 2,
        video_price_720p: 0.03,
      },
      billingMode: 'video',
      defaultPrice: 0.07,
      intervalPrice: 0.14,
      expectedPreview: '720p 0.48 灵石 / 8秒',
    },
    {
      name: '历史图片模式渠道默认价只按次乘倍率',
      groupOverrides: { rate_multiplier: 2 },
      billingMode: 'image',
      defaultPrice: 2.1,
      intervalPrice: undefined,
      expectedPreview: '720p 4.2 灵石 / 按次',
    },
  ])('$name', async ({
    groupOverrides,
    billingMode,
    defaultPrice,
    intervalPrice,
    expectedPreview,
  }) => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
      ...groupOverrides,
    })
    const videoKey = apiKeyFixture({
      id: 909,
      name: '视频 Key',
      key: 'sk-video-key-1234567890',
      group_id: 9,
    })
    const intervals = intervalPrice === undefined
      ? []
      : [{ tier_label: '720p', per_request_price: intervalPrice }]

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
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    expect(selectByLabel(wrapper, '分组').value).toBe('9')
    expect(optionTexts(selectByLabel(wrapper, '模型'))).toEqual([
      '请选择模型',
      'grok-imagine-video · Grok',
    ])
    expect(summaryValue(wrapper, '价格预览')).toBe(expectedPreview)
  })

  it('视频名称模型使用 token 定价时展示文本倍率', async () => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
      rate_multiplier: 2,
      video_rate_independent: true,
      video_rate_multiplier: 3,
      video_price_720p: 0.03,
    })
    const videoKey = apiKeyFixture({ id: 909, group_id: 9 })
    getUserGroupRates.mockResolvedValue({ 9: 1.25 })
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
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    expect(summaryValue(wrapper, '当前倍率')).toBe('1.25x')
    expect(summaryValue(wrapper, '价格预览')).toBe('-')
  })

  it('历史视频渠道未命中当前分辨率且无默认价时显示无价格', async () => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    const videoKey = apiKeyFixture({ id: 909, group_id: 9 })
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
            billing_mode: 'per_request',
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
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    expect(summaryValue(wrapper, '价格预览')).toBe('-')
  })

  it('video-1.5 根据参考图状态忽略模型名大小写并使用实际计费模型的渠道价格', async () => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    const videoKey = apiKeyFixture({ id: 909, group_id: 9 })
    routeState.query = {
      kind: 'video',
      group_id: '9',
      model: 'grok-imagine-video-1.5',
      platform: 'grok',
    }
    getAvailableChannels.mockResolvedValue([{
      name: 'Grok 渠道',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [videoGroup],
        supported_models: [
          {
            name: 'Grok-Imagine-Video',
            platform: 'grok',
            kind: 'video',
            pricing: {
              billing_mode: 'video',
              input_price: null,
              output_price: null,
              cache_write_price: null,
              cache_read_price: null,
              image_output_price: null,
              per_request_price: 0.09,
              intervals: [],
            },
          },
          {
            name: 'grok-imagine-video-1.5',
            platform: 'grok',
            kind: 'video',
            pricing: {
              billing_mode: 'video',
              input_price: null,
              output_price: null,
              cache_write_price: null,
              cache_read_price: null,
              image_output_price: null,
              per_request_price: 0.14,
              intervals: [],
            },
          },
        ],
      }],
    }])
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    expect((selectByLabel(wrapper, '模型').selectedOptions[0]?.textContent ?? '').trim()).toBe('grok-imagine-video-1.5 · Grok')
    expect(summaryValue(wrapper, '价格预览')).toBe('720p 0.72 灵石 / 8秒')

    const fileInput = wrapper.find('input[type="file"]')
    Object.defineProperty(fileInput.element, 'files', {
      value: [new File(['reference image'], 'reference.png', { type: 'image/png' })],
      configurable: true,
    })
    await fileInput.trigger('change')
    await flushPromises()

    expect(summaryValue(wrapper, '价格预览')).toBe('720p 1.12 灵石 / 8秒')
  })

  it('video-1.5 无参考图且标准模型条目缺失时使用标准模型系统价', async () => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    const videoKey = apiKeyFixture({ id: 909, group_id: 9 })
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
          pricing: {
            billing_mode: 'video',
            input_price: null,
            output_price: null,
            cache_write_price: null,
            cache_read_price: null,
            image_output_price: null,
            per_request_price: 0.22,
            intervals: [],
          },
        }],
      }],
    }])
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    expect(summaryValue(wrapper, '价格预览')).toBe('720p 0.56 灵石 / 8秒')
  })

  it('标准视频模型支持多张参考图并显示对应提示', async () => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    const videoKey = apiKeyFixture({ id: 909, group_id: 9 })
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
          pricing: null,
        }],
      }],
    }])
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('input[type="file"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('input[type="file"]').attributes('multiple')).toBeDefined()
    expect(wrapper.text()).toContain('支持上传最多 4 张参考图；超过 1.0 MB 时会自动压缩')
  })

  it('标准视频模型提交时会保留全部参考图', async () => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    const videoKey = apiKeyFixture({
      id: 909,
      key: 'sk-video-key-1234567890',
      group_id: 9,
    })
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
          pricing: null,
        }],
      }],
    }])
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })
    testVideoGeneration.mockResolvedValue({
      payload: { status: 'done' },
      requestID: 'video-request-multiple-images',
    })

    const wrapper = mountView()
    await flushPromises()
    const fileInput = wrapper.find('input[type="file"]')
    Object.defineProperty(fileInput.element, 'files', {
      value: [
        new File(['reference image one'], 'one.png', { type: 'image/png' }),
        new File(['reference image two'], 'two.jpg', { type: 'image/jpeg' }),
      ],
      configurable: true,
    })
    await fileInput.trigger('change')
    await flushPromises()
    await wrapper.find('textarea').setValue('展示产品细节')
    await wrapper.find('form').trigger('submit')

    await vi.waitFor(() => expect(testVideoGeneration).toHaveBeenCalledTimes(1))
    const request = testVideoGeneration.mock.calls[0][0] as Record<string, unknown>
    expect(request.startingImageDataUrl).toBeUndefined()
    expect(request.referenceImageDataUrls).toEqual([
      'data:image/png;base64,cmVmZXJlbmNlIGltYWdlIG9uZQ==',
      'data:image/jpeg;base64,cmVmZXJlbmNlIGltYWdlIHR3bw==',
    ])
  })

  it('历史完成任务通过登录态内容接口创建 Blob 播放地址并在卸载时释放', async () => {
    const videoGroup = groupFixture({
      id: 9,
      name: '视频分组',
      platform: 'grok',
      allow_image_generation: true,
    })
    const videoKey = apiKeyFixture({
      id: 909,
      key: 'sk-video-key-1234567890',
      group_id: 9,
    })
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
          pricing: null,
        }],
      }],
    }])
    listKeys.mockResolvedValue({ items: [videoKey], pages: 1 })
    listVideoTestTasks.mockResolvedValue({
      items: [{
        id: 'local-completed',
        api_key_id: 909,
        group_id: 9,
        upstream_task_id: 'video-request-123',
        platform: 'grok',
        model: 'grok-imagine-video',
        prompt: '生成海浪视频',
        reference_image_count: 0,
        status: 'completed',
        response: { status: 'done' },
        created_at: '2026-07-19T12:00:00Z',
        updated_at: '2026-07-19T12:10:00Z',
      }],
      total: 1,
      page: 1,
      page_size: 20,
    })
    const videoBlob = new Blob(['video-content'], { type: 'video/mp4' })
    fetchVideoTestTaskContent.mockResolvedValue(videoBlob)

    const wrapper = mountView()
    await flushPromises()

    expect(fetchVideoTestTaskContent).toHaveBeenCalledWith('local-completed')
    expect(window.URL.createObjectURL).toHaveBeenCalledWith(videoBlob)
    expect(wrapper.find('video').attributes('src')).toBe('blob:model-test-reference')

    wrapper.unmount()
    expect(window.URL.revokeObjectURL).toHaveBeenCalledWith('blob:model-test-reference')
  })
})
