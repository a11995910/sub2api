import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VideoTaskReviewsView from '../VideoTaskReviewsView.vue'

const { list, recheck, confirmFailed, confirmSucceeded } = vi.hoisted(() => ({
  list: vi.fn(),
  recheck: vi.fn(),
  confirmFailed: vi.fn(),
  confirmSucceeded: vi.fn(),
}))

vi.mock('@/api/admin/videoTaskReviews', () => ({
  videoTaskReviewsAPI: { list, recheck, confirmFailed, confirmSucceeded },
  default: { list, recheck, confirmFailed, confirmSucceeded },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }),
}))

vi.mock('@/utils/format', () => ({ formatCurrency: (value: number) => `${value} 灵石` }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const task = (overrides = {}) => ({
  id: 9,
  request_id: 'req-1',
  upstream_task_id: 'task-1',
  platform: 'openai',
  user_id: 7,
  user_email: 'user@example.com',
  username: 'tester',
  api_key_id: 11,
  api_key_name: 'canvas',
  account_id: 17,
  model: 'sora-2',
  upstream_model: 'sora-2',
  resolution: '720p',
  duration_seconds: 8,
  reference_image_count: 0,
  estimated_cost: 1.25,
  task_status: 'unknown',
  billing_status: 'reserved',
  poll_count: 3,
  last_poll_error: 'timeout',
  created_at: '2026-08-12T00:00:00Z',
  updated_at: '2026-08-12T00:01:00Z',
  ...overrides,
})

const mountView = () => mount(VideoTaskReviewsView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      Pagination: true,
      BaseDialog: { props: ['show', 'title'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      Icon: true,
    },
  },
})

describe('VideoTaskReviewsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    list.mockResolvedValue({ items: [task(), task({ id: 10, upstream_task_id: '' })], total: 2, page: 1, page_size: 20, pages: 1 })
    recheck.mockResolvedValue({ success: true })
    confirmFailed.mockResolvedValue({ success: true })
    confirmSucceeded.mockResolvedValue({ success: true })
  })

  it('按用户和任务信息筛选并仅对有上游任务号的记录显示重新核对', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="review-search"]').setValue('user@example.com')
    await wrapper.get('[data-testid="review-user-id"]').setValue('7')
    await wrapper.get('[data-testid="review-filter-submit"]').trigger('click')
    await flushPromises()

    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'user@example.com', user_id: 7 }))
    expect(wrapper.findAll('[data-testid="review-recheck"]')).toHaveLength(1)
  })

  it('人工确认失败必须填写核对依据', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('[data-testid="review-confirm-failed"]')[0].trigger('click')
    await wrapper.get('[data-testid="review-confirm-submit"]').trigger('click')
    expect(confirmFailed).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="review-reason"]').setValue('上游控制台确认无任务')
    await wrapper.get('[data-testid="review-confirm-submit"]').trigger('click')
    await flushPromises()

    expect(confirmFailed).toHaveBeenCalledWith(9, '上游控制台确认无任务')
  })

  it('重新核对只请求一次任务状态', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="review-recheck"]').trigger('click')
    await flushPromises()

    expect(recheck).toHaveBeenCalledTimes(1)
    expect(recheck).toHaveBeenCalledWith(9)
  })
})
