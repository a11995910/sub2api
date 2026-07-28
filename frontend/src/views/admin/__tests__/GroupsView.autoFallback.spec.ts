import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'

const {
  listGroups,
  getAllGroups,
  getModelsListCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  createGroupAPI,
  updateGroup,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getAllGroups: vi.fn(),
  getModelsListCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  createGroupAPI: vi.fn(),
  updateGroup: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: getAllGroups,
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: createGroupAPI,
      update: updateGroup,
      delete: vi.fn(),
      updateSortOrder: vi.fn(),
    },
    accounts: {
      list: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const createGroup = (overrides: Partial<AdminGroup> = {}): AdminGroup => ({
  id: 72,
  name: 'Plus',
  description: null,
  platform: 'openai',
  rate_multiplier: 0.12,
  rpm_limit: 0,
  is_exclusive: false,
  oauth_pool_visible: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  auto_fallback_group_id: null,
  allow_messages_dispatch: false,
  allow_live: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  sort_order: 10,
  ...overrides,
})

const mountView = async () => {
  const wrapper = mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        TablePageLayout: {
          template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
        },
        DataTable: true,
        Pagination: true,
        BaseDialog: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('GroupsView 自动承接分组', () => {
  beforeEach(() => {
    listGroups.mockReset()
    getAllGroups.mockReset()
    getModelsListCandidates.mockReset()
    getUsageSummary.mockReset()
    getCapacitySummary.mockReset()
    getLiveCapability.mockReset()
    createGroupAPI.mockReset()
    updateGroup.mockReset()

    const plus = createGroup()
    const allGroups = [
      plus,
      createGroup({ id: 73, name: 'Pro', rate_multiplier: 0.18 }),
      createGroup({ id: 74, name: 'Full Price', rate_multiplier: 0.26 }),
      createGroup({ id: 75, name: 'Anthropic', platform: 'anthropic' }),
      createGroup({ id: 76, name: 'Subscription', subscription_type: 'subscription' }),
      createGroup({ id: 77, name: 'Inactive', status: 'inactive' }),
    ]
    listGroups.mockResolvedValue({
      items: [plus],
      total: allGroups.length,
      page: 1,
      page_size: 1,
      pages: allGroups.length,
    })
    getAllGroups.mockImplementation((platform?: string) =>
      Promise.resolve(platform ? allGroups.filter((group) => group.platform === platform) : allGroups),
    )
    getModelsListCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
    createGroupAPI.mockImplementation((payload: object) => Promise.resolve({ ...plus, ...payload }))
    updateGroup.mockImplementation((_id: number, payload: object) =>
      Promise.resolve({ ...plus, ...payload }),
    )
  })

  it('从全部分组中过滤候选，并在编辑时排除当前分组', async () => {
    const wrapper = await mountView()
    const vm = wrapper.vm as any

    vm.createForm.platform = 'openai'
    await nextTick()
    expect(vm.autoFallbackGroupOptions.map((option: { value: number | null }) => option.value)).toEqual([
      null,
      72,
      73,
      74,
    ])

    await vm.handleEdit(createGroup())
    await flushPromises()
    expect(vm.autoFallbackGroupOptionsForEdit.map((option: { value: number | null }) => option.value)).toEqual([
      null,
      73,
      74,
    ])

    wrapper.unmount()
  })

  it('更新分组时提交选中的承接分组', async () => {
    const wrapper = await mountView()
    const vm = wrapper.vm as any

    await vm.handleEdit(createGroup())
    await flushPromises()
    vm.editForm.auto_fallback_group_id = 73
    await vm.handleUpdateGroup()

    expect(updateGroup).toHaveBeenCalledWith(
      72,
      expect.objectContaining({ auto_fallback_group_id: 73 }),
    )
    wrapper.unmount()
  })

  it('创建分组时提交号池可见开关', async () => {
    const wrapper = await mountView()
    const vm = wrapper.vm as any

    vm.createForm.name = '公开号池分组'
    vm.createForm.oauth_pool_visible = true
    await vm.handleCreateGroup()

    expect(createGroupAPI).toHaveBeenCalledWith(
      expect.objectContaining({ oauth_pool_visible: true }),
    )
    wrapper.unmount()
  })

  it('编辑分组时回显并提交号池可见开关', async () => {
    const wrapper = await mountView()
    const vm = wrapper.vm as any

    await vm.handleEdit(createGroup({ oauth_pool_visible: true }))
    await flushPromises()
    expect(vm.editForm.oauth_pool_visible).toBe(true)

    vm.editForm.oauth_pool_visible = false
    await vm.handleUpdateGroup()

    expect(updateGroup).toHaveBeenCalledWith(
      72,
      expect.objectContaining({ oauth_pool_visible: false }),
    )
    wrapper.unmount()
  })
})
