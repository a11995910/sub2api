import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TimeRotationView from '../TimeRotationView.vue'
import timeRotation from '@/i18n/locales/zh/timeRotation'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params: Record<string, unknown> = {}) => {
    const value = key.replace('timeRotation.', '').split('.').reduce<unknown>((result, part) => result != null && typeof result === 'object' ? (result as Record<string, unknown>)[part] : undefined, timeRotation)
    const text = typeof value === 'string' ? value : key
    return text.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  } })
}))

const { get, save, status, list, showSuccess } = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), status: vi.fn(), list: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin/timeRotation', () => ({ timeRotationAPI: { get, save, status } }))
vi.mock('@/api/admin/accounts', () => ({ list }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess }) }))
const defaults = () => ({ enabled: false, revision: 4, slots: [
  { start: '00:00', end: '08:00', account_ids: [] as number[], active_priority: 1, inactive_priority: 50 },
  { start: '08:00', end: '16:00', account_ids: [] as number[], active_priority: 1, inactive_priority: 50 },
  { start: '16:00', end: '24:00', account_ids: [] as number[], active_priority: 1, inactive_priority: 50 }
] })
const render = () => mount(TimeRotationView, { global: {
  stubs: { AppLayout: { template: '<div><slot /></div>' } }
} })
beforeEach(() => {
  vi.clearAllMocks()
  get.mockResolvedValue(defaults())
  save.mockImplementation(async value => ({ ...value, revision: 5 }))
  status.mockResolvedValue(null)
  list.mockResolvedValue({ items: [{ id: 1, name: '账号甲', priority: 70 }, { id: 2, name: '影子账号', parent_account_id: 1, priority: 70 }], pages: 1 })
})
describe('时段轮候配置', () => {
  it('加载三个时段、排除影子账号，并防止跨时段重复选择', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('section')).toHaveLength(3)
    expect(wrapper.text()).not.toContain('影子账号')
    await wrapper.find('[data-testid="rotation-slot-0"] input[data-account-id="1"]').setValue(true)
    expect(wrapper.find('[data-testid="rotation-slot-1"] input[data-account-id="1"]').attributes('disabled')).toBeDefined()
    await wrapper.find('[data-testid="rotation-enabled"]').setValue(true)
    await wrapper.find('[data-testid="rotation-save"]').trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, revision: 4, slots: expect.arrayContaining([expect.objectContaining({ account_ids: [1] })]) }))
    expect(showSuccess).toHaveBeenCalled()
    wrapper.unmount()
  })
  it('读取所有分页，后续页账号也能选择', async () => {
    list.mockResolvedValueOnce({ items: [{ id: 1, name: '第一页', priority: 10 }], pages: 2 })
    list.mockResolvedValueOnce({ items: [{ id: 3, name: '第二页', priority: 30 }], pages: 2 })
    const wrapper = render()
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('第二页')
    wrapper.unmount()
  })
  it('非法时间拒绝保存，接口冲突时保留编辑内容并显示错误', async () => {
    const wrapper = render()
    await flushPromises()
    const end = wrapper.find('[data-testid="rotation-slot-0"] input[type="text"]')
    await end.setValue('00:00')
    await wrapper.find('[data-testid="rotation-save"]').trigger('click')
    expect(save).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').text()).toContain('请检查时间')
    await end.setValue('07:30')
    save.mockRejectedValue({ response: { data: { message: '配置已更新，请重新加载' } } })
    await wrapper.find('[data-testid="rotation-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('配置已更新')
    expect((end.element as HTMLInputElement).value).toBe('07:30')
    wrapper.unmount()
  })
  it('加载失败时禁止保存，避免以空配置覆盖已有设置', async () => {
    get.mockRejectedValue(new Error('断开连接'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[data-testid="rotation-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('智能模式加载默认六段、可选择独立账号池，并在预览失败时仍允许保存', async () => {
    get.mockResolvedValue({ enabled: true, revision: 8, mode: 'smart', slots: [], smart: { account_ids: [], periods: [
      { start: '00:00', end: '08:00', primary_count: 1 }, { start: '08:00', end: '10:00', primary_count: 2 }, { start: '10:00', end: '14:00', primary_count: 4 }, { start: '14:00', end: '18:00', primary_count: 4 }, { start: '18:00', end: '22:00', primary_count: 3 }, { start: '22:00', end: '24:00', primary_count: 2 }
    ], rotation_minutes: 60, quota_reserve_percent: 10 } })
    status.mockRejectedValue(new Error('预览不可用'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('[data-testid^="smart-period-"]')).toHaveLength(6)
    await wrapper.find('[data-smart-account-id="1"]').setValue(true)
    await wrapper.find('[data-testid="rotation-save"]').trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ mode: 'smart', smart: expect.objectContaining({ account_ids: [1] }) }))
    expect(wrapper.text()).toContain('暂时无法读取计划预览')
    wrapper.unmount()
  })
})


const smartDefaults = () => ({
  ...defaults(), enabled: true, mode: 'smart',
  smart: {
    account_ids: [1, 999],
    periods: [{ start: '00:00', end: '24:00', primary_count: 1 }],
    rotation_minutes: 60, quota_reserve_percent: 10
  }
})
const runningStatus = (revision = 4) => ({
  mode: 'smart', enabled: true, ready: true, revision,
  updated_at: '2026-09-30T01:00:00Z',
  period: { start: '00:00', end: '24:00', primary_count: 1 },
  next_rotation_at: '2026-09-30T02:00:00Z', scope: 'all',
  accounts: [{ account_id: 1, name: '账号甲', role: 'primary', reason: '主力', quota_7d_remaining: 0 }]
})

describe('智能轮候状态与编辑边界', () => {
  it('允许保存非六段配置，缺失账号可以移除', async () => {
    get.mockResolvedValue(smartDefaults())
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('[data-testid^="smart-period-"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('账号 #999（已删除或不再符合条件）')
    await wrapper.find('[data-smart-remove-id="999"]').trigger('click')
    await wrapper.find('[data-testid="rotation-save"]').trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      smart: expect.objectContaining({ account_ids: [1], periods: [{ start: '00:00', end: '24:00', primary_count: 1 }] })
    }))
    wrapper.unmount()
  })

  it('显示已保存状态、北京时间、未知额度，零额度不能显示为未知', async () => {
    get.mockResolvedValue(smartDefaults())
    status.mockResolvedValue(runningStatus())
    const wrapper = render()
    await flushPromises()
    const preview = wrapper.find('[data-testid="rotation-status"]')
    expect(preview.text()).toContain('已保存版本 4')
    expect(preview.text()).toContain('09:00:00')
    expect(preview.text()).toContain('10:00:00')
    expect(preview.text()).toContain('未知')
    expect(preview.text()).toContain('0%')
    await wrapper.find('[data-testid="rotation-enabled"]').setValue(false)
    expect(wrapper.find('[data-testid="status-state"]').text()).toContain('智能轮候已生效')
    wrapper.unmount()
  })

  it.each([
    { enabled: false, ready: true, message: '已保存配置处于停用状态' },
    { enabled: true, ready: false, message: '等待配置同步或状态已过期' }
  ])('明确展示未运行状态 $message，并隐藏过期角色', async ({ enabled, ready, message }) => {
    get.mockResolvedValue(smartDefaults())
    status.mockResolvedValue({ ...runningStatus(), enabled, ready })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[data-testid="status-state"]').text()).toContain(message)
    expect(wrapper.find('[data-testid="rotation-status"] table').exists()).toBe(false)
    wrapper.unmount()
  })

  it('保存后新状态不被更早发出的慢请求覆盖', async () => {
    get.mockResolvedValue(smartDefaults())
    status.mockResolvedValueOnce(runningStatus())
    const wrapper = render()
    await flushPromises()
    let resolveOld: (value: ReturnType<typeof runningStatus>) => void = () => {}
    status.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    await wrapper.find('[data-testid="refresh-status"]').trigger('click')
    status.mockResolvedValueOnce(runningStatus(5))
    await wrapper.find('[data-testid="rotation-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="rotation-status"]').text()).toContain('已保存版本 5')
    resolveOld(runningStatus(4))
    await flushPromises()
    expect(wrapper.find('[data-testid="rotation-status"]').text()).toContain('已保存版本 5')
    wrapper.unmount()
  })
})
