import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { DOMWrapper, VueWrapper } from '@vue/test-utils'

import RiskControlView from '../RiskControlView.vue'
import type { ContentModerationConfig, ContentModerationLog, UpdateContentModerationConfig } from '@/api/admin/riskControl'

enableAutoUnmount(afterEach)

const {
  getConfig,
  updateConfig,
  getStatus,
  listLogs,
  getLogDetail,
  copyToClipboard,
  getGroups,
  getProxies,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  getStatus: vi.fn(),
  listLogs: vi.fn(),
  getLogDetail: vi.fn(),
  copyToClipboard: vi.fn(),
  getGroups: vi.fn(),
  getProxies: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    riskControl: {
      getConfig,
      updateConfig,
      getStatus,
      listLogs,
      getLogDetail,
      testAPIKeys: vi.fn(),
      deleteFlaggedHash: vi.fn(),
      clearFlaggedHashes: vi.fn(),
      unbanUser: vi.fn(),
    },
    groups: {
      getAll: getGroups,
    },
    proxies: {
      getAll: getProxies,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_err: unknown, fallback: string) => fallback,
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.riskControl.preBlockAPIKeyLoadSummary') {
          return `同步并发 ${params?.active} / 可用 Key ${params?.available}，累计 ${params?.total} 次，worker：${params?.workerActive} / ${params?.workerTotal}`
        }
        return key.replace(/\{(\w+)\}/g, (_, token) => String(params?.[token] ?? `{${token}}`))
      },
    }),
  }
})

const baseConfig = (): ContentModerationConfig => ({
  enabled: true,
  mode: 'pre_block',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  proxy_id: null,
  api_key_configured: false,
  api_key_masked: '',
  api_key_count: 0,
  api_key_masks: [],
  api_key_statuses: [],
  timeout_ms: 3000,
  sample_rate: 100,
  all_groups: true,
  group_ids: [],
  record_non_hits: false,
  worker_count: 4,
  queue_size: 32768,
  block_status: 403,
  block_message: '内容审计命中风险规则，请调整输入后重试',
  email_on_hit: true,
  auto_ban_enabled: true,
  ban_threshold: 10,
  violation_window_hours: 720,
  retry_count: 2,
  hit_retention_days: 180,
  non_hit_retention_days: 3,
  pre_hash_check_enabled: false,
  blocked_keywords: [],
  keyword_blocking_mode: 'keyword_and_api',
  thresholds: {
    harassment: 0.98,
    sexual: 0.65,
  },
  model_filter: {
    type: 'all',
    models: [],
  },
})

const runtimeStatus = () => ({
  enabled: true,
  risk_control_enabled: true,
  mode: 'pre_block',
  worker_count: 4,
  max_workers: 32,
  active_workers: 0,
  idle_workers: 4,
  queue_size: 32768,
  queue_length: 0,
  queue_usage_percent: 0,
  enqueued: 0,
  dropped: 0,
  processed: 0,
  errors: 0,
  pre_block_active: 0,
  pre_block_checked: 0,
  pre_block_allowed: 0,
  pre_block_blocked: 0,
  pre_block_errors: 0,
  pre_block_avg_latency_ms: 0,
  pre_block_api_key_active: 0,
  pre_block_api_key_available_count: 0,
  pre_block_api_key_total_calls: 0,
  pre_block_api_key_loads: [],
  api_key_statuses: [],
  flagged_hash_count: 0,
  last_cleanup_deleted_hit: 0,
  last_cleanup_deleted_non_hit: 0,
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const BaseDialogStub = defineComponent({
  props: {
    show: {
      type: Boolean,
      default: false,
    },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})
const ModelWhitelistSelectorStub = defineComponent({
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const onInput = (event: Event) => {
      const value = (event.target as HTMLInputElement).value
      emit(
        'update:modelValue',
        value
          .split(/[,\n]/)
          .map((item) => item.trim())
          .filter(Boolean)
      )
    }
    return () =>
      h('input', {
        'data-test': 'model-filter-input',
        value: (props.modelValue as string[]).join('\n'),
        onInput,
      })
  },
})

function findButtonByText(wrapper: VueWrapper, text: string): DOMWrapper<HTMLButtonElement> {
  const button = wrapper.findAll<HTMLButtonElement>('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`button not found: ${text}`)
  }
  return button
}

function moderationLog(overrides: Partial<ContentModerationLog> = {}): ContentModerationLog {
  return {
    id: 1,
    request_id: 'audit-request-1',
    user_id: 1,
    user_email: 'user@example.com',
    api_key_id: 1,
    api_key_name: '测试密钥',
    group_id: 1,
    group_name: '测试分组',
    endpoint: '/v1/responses',
    provider: 'openai',
    model: 'gpt-5.6-luna',
    mode: 'pre_block',
    action: 'keyword_blocked',
    flagged: true,
    highest_category: 'keyword',
    highest_score: 1,
    matched_keyword: '测试关键词',
    category_scores: {},
    threshold_snapshot: {},
    input_excerpt: '旧的输入摘要',
    upstream_latency_ms: 0,
    error: '',
    violation_count: 1,
    auto_banned: false,
    email_sent: false,
    user_status: 'active',
    queue_delay_ms: 0,
    created_at: '2026-09-20T12:00:00Z',
    ...overrides,
  }
}

async function mountWithLogs(items: ContentModerationLog[]) {
  listLogs.mockResolvedValue({ items, total: items.length, page: 1, page_size: 20, pages: 1 })
  const wrapper = mount(RiskControlView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        BaseDialog: BaseDialogStub,
        Icon: true,
        Select: true,
        Toggle: true,
        Pagination: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        ProxySelector: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('admin RiskControlView', () => {
  beforeEach(() => {
    getConfig.mockReset()
    updateConfig.mockReset()
    getStatus.mockReset()
    listLogs.mockReset()
    getLogDetail.mockReset()
    copyToClipboard.mockReset()
    getGroups.mockReset()
    showError.mockReset()
    showSuccess.mockReset()

    getConfig.mockResolvedValue(baseConfig())
    getStatus.mockResolvedValue(runtimeStatus())
    listLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    getGroups.mockResolvedValue([])
    getProxies.mockResolvedValue([])
    updateConfig.mockImplementation(async (payload: UpdateContentModerationConfig) => ({
      ...baseConfig(),
      ...payload,
      model_filter: payload.model_filter ?? baseConfig().model_filter,
      api_key_configured: false,
      api_key_masked: '',
      api_key_count: 0,
      api_key_masks: [],
      api_key_statuses: [],
    }))
  })

  it('点击记录后获取原始输入并保留换行，复制详情全文而非列表摘要', async () => {
    const row = moderationLog()
    const fullText = `第一行原始输入\n${'完整内容'.repeat(100)}\n末尾内容`
    let resolveDetail!: (detail: ContentModerationLog) => void
    getLogDetail.mockReturnValue(new Promise<ContentModerationLog>((resolve) => { resolveDetail = resolve }))
    const wrapper = await mountWithLogs([row])

    expect(getLogDetail).not.toHaveBeenCalled()
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    expect(getLogDetail).toHaveBeenCalledWith(1)
    expect(wrapper.find('[data-test="input-detail-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="input-detail-text"]').exists()).toBe(false)

    resolveDetail({ ...row, input_content: { text: fullText, text_truncated: false, text_runes: fullText.length, image_count: 0 } })
    await flushPromises()

    expect(wrapper.get('[data-test="input-detail-text"]').element.textContent).toBe(fullText)
    expect(wrapper.get('[data-test="input-detail-notice"]').text()).toBe('admin.riskControl.inputDetailRedactedHint')
    await wrapper.get('[data-test="input-detail-copy"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(fullText)
  })

  it('旧记录明确提示仅有摘要，审核错误不会冒充输入内容', async () => {
    const row = moderationLog({ input_excerpt: '', error: '上游审核超时' })
    getLogDetail.mockResolvedValue(row)
    const wrapper = await mountWithLogs([row])

    expect(wrapper.get('[data-test="input-detail-open-1"]').text()).toBe('-')
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="input-detail-notice"]').text()).toBe('admin.riskControl.inputDetailUnavailableHint')
    expect(wrapper.find('[data-test="input-detail-text"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="input-detail-copy"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="input-detail-audit-error"]').text()).toBe('上游审核超时')

    getLogDetail.mockResolvedValue({ ...row, input_excerpt: '已保存的历史摘要' })
    await findButtonByText(wrapper, 'common.close').trigger('click')
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="input-detail-text"]').text()).toBe('已保存的历史摘要')
    expect(wrapper.get('[data-test="input-detail-notice"]').text()).toBe('admin.riskControl.inputDetailUnavailableHint')
  })

  it('截断记录和图片输入显示保存范围，纯图片输入不回退成摘要', async () => {
    const row = moderationLog()
    getLogDetail.mockResolvedValue({ ...row, input_content: { text: '保存的前缀', text_truncated: true, text_runes: 70000, image_count: 2 } })
    const wrapper = await mountWithLogs([row])
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="input-detail-truncated"]').text()).toBe('admin.riskControl.inputDetailTruncatedHint')
    expect(wrapper.get('[data-test="input-detail-images"]').text()).toBe('admin.riskControl.inputDetailImagesHint')
    expect(wrapper.get('[data-test="input-detail-text"]').text()).toBe('保存的前缀')

    getLogDetail.mockResolvedValue({ ...row, input_content: { text: '', text_truncated: false, text_runes: 0, image_count: 1 } })
    await findButtonByText(wrapper, 'common.close').trigger('click')
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="input-detail-text"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="input-detail-images"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="input-detail-empty"]').text()).toBe('admin.riskControl.inputDetailNoText')
  })

  it('加载详情失败显示错误并支持重试，失败时不会把摘要当成已加载的原文', async () => {
    const row = moderationLog()
    getLogDetail.mockRejectedValueOnce(new Error('请求失败')).mockResolvedValueOnce({
      ...row,
      input_content: { text: '重试后的原始输入', text_truncated: false, text_runes: 9, image_count: 0 },
    })
    const wrapper = await mountWithLogs([row])
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="input-detail-error"]').text()).toContain('admin.riskControl.inputDetailLoadFailed')
    expect(wrapper.find('[data-test="input-detail-text"]').exists()).toBe(false)
    await wrapper.get('[data-test="input-detail-retry"]').trigger('click')
    await flushPromises()
    expect(getLogDetail).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-test="input-detail-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="input-detail-text"]').text()).toBe('重试后的原始输入')
  })

  it('切换记录时忽略前一条延迟返回的详情', async () => {
    const first = moderationLog()
    const second = moderationLog({ id: 2, input_excerpt: '第二条摘要' })
    let resolveFirst!: (detail: ContentModerationLog) => void
    getLogDetail.mockReturnValueOnce(new Promise<ContentModerationLog>((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ ...second, input_content: { text: '第二条原始输入', text_truncated: false, text_runes: 7, image_count: 0 } })
    const wrapper = await mountWithLogs([first, second])
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    await wrapper.get('[data-test="input-detail-open-2"]').trigger('click')
    await flushPromises()
    resolveFirst({ ...first, input_content: { text: '过期的第一条原始输入', text_truncated: false, text_runes: 10, image_count: 0 } })
    await flushPromises()

    expect(wrapper.get('[data-test="input-detail-text"]').text()).toBe('第二条原始输入')
  })

  it('关闭弹窗后返回的旧详情不会重新打开弹窗', async () => {
    const row = moderationLog()
    let resolveDetail!: (detail: ContentModerationLog) => void
    getLogDetail.mockReturnValue(new Promise<ContentModerationLog>((resolve) => { resolveDetail = resolve }))
    const wrapper = await mountWithLogs([row])
    await wrapper.get('[data-test="input-detail-open-1"]').trigger('click')
    await findButtonByText(wrapper, 'common.close').trigger('click')
    resolveDetail({ ...row, input_content: { text: '已关闭记录的内容', text_truncated: false, text_runes: 8, image_count: 0 } })
    await flushPromises()

    expect(wrapper.find('[data-test="input-detail-text"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="input-detail-loading"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('已关闭记录的内容')
  })

  it('saves the selected model filter mode and models', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.modelFilterInclude').trigger('click')
    await wrapper.get('[data-test="model-filter-input"]').setValue('gpt-5.5, gpt-5.4')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      model_filter: {
        type: 'include',
        models: ['gpt-5.5', 'gpt-5.4'],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('submits edited risk control thresholds when saving moderation config', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.riskThresholds').trigger('click')
    await wrapper.get('[data-test="risk-threshold-sexual"]').setValue('72')
    await wrapper.get('[data-test="risk-threshold-harassment"]').setValue('99')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      thresholds: expect.objectContaining({
        sexual: 0.72,
        harassment: 0.99,
      }),
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('describes worker runtime as async audit and pre-block record processing', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      mode: 'observe',
      processed: 12,
      queue_length: 2,
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.workerStatusHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('2 / 32,768')
  })

  it('shows pre-block synchronous moderation metrics separately from worker queue', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      pre_block_active: 2,
      pre_block_checked: 128,
      pre_block_allowed: 120,
      pre_block_blocked: 8,
      pre_block_errors: 1,
      pre_block_avg_latency_ms: 86,
      pre_block_api_key_active: 2,
      pre_block_api_key_available_count: 2,
      pre_block_api_key_total_calls: 128,
      active_workers: 3,
      worker_count: 7,
      pre_block_api_key_loads: [
        {
          index: 0,
          key_hash: 'hash-one',
          masked: 'sk-...one',
          status: 'ok',
          active: 1,
          total: 72,
          success: 70,
          errors: 2,
          avg_latency_ms: 84,
          last_latency_ms: 80,
          last_http_status: 200,
        },
        {
          index: 1,
          key_hash: 'hash-two',
          masked: 'sk-...two',
          status: 'ok',
          active: 1,
          total: 56,
          success: 56,
          errors: 0,
          avg_latency_ms: 90,
          last_latency_ms: 92,
          last_http_status: 200,
        },
      ],
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.workerStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('128')
    expect(wrapper.text()).toContain('120')
    expect(wrapper.text()).toContain('8')
    expect(wrapper.text()).toContain('86 ms')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockAPIKeyLoad')
    expect(wrapper.text()).toContain('sk-...one')
    expect(wrapper.text()).toContain('sk-...two')
    expect(wrapper.text()).toContain('72')
    expect(wrapper.text()).toContain('56')
    expect(wrapper.text()).toContain('同步并发 2 / 可用 Key 2，累计 128 次，worker：3 / 7')

    const runtimeCards = wrapper.get('[data-test="pre-block-runtime-cards"]')
    const syncCard = wrapper.get('[data-test="pre-block-sync-card"]')
    const apiKeyLoadCard = wrapper.get('[data-test="pre-block-api-key-load-card"]')

    expect(runtimeCards.classes()).toEqual(expect.arrayContaining([
      'grid',
      'grid-cols-1',
      'xl:grid-cols-[minmax(0,520px)_minmax(0,1fr)]',
    ]))
    expect(syncCard.element.parentElement).toBe(runtimeCards.element)
    expect(apiKeyLoadCard.element.parentElement).toBe(runtimeCards.element)
    expect(syncCard.classes()).toContain('card')
    expect(apiKeyLoadCard.classes()).toContain('card')
    expect(syncCard.get('h2').text()).toBe('admin.riskControl.preBlockSyncStatus')
    expect(syncCard.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(apiKeyLoadCard.get('h2').text()).toBe('admin.riskControl.preBlockAPIKeyLoad')
    expect(apiKeyLoadCard.text()).toContain('admin.riskControl.preBlockAPIKeyLoadHint')
    expect(wrapper.get('[data-test="pre-block-api-key-load-list"]').classes()).toEqual(expect.arrayContaining([
      'max-h-[280px]',
      'overflow-y-auto',
    ]))
  })
})
