import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TimeRotationView from '../TimeRotationView.vue'
import timeRotation from '@/i18n/locales/zh/timeRotation'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params: Record<string, unknown> = {}) => {
    const text = timeRotation[key.replace('timeRotation.', '') as keyof typeof timeRotation] || key
    return text.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  } })
}))

const { get, save, list, showSuccess } = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), list: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin/timeRotation', () => ({ timeRotationAPI: { get, save } }))
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
})
