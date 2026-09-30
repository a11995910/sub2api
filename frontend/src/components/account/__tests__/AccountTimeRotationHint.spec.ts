import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTimeRotationHint from '../AccountTimeRotationHint.vue'
import timeRotation from '@/i18n/locales/zh/timeRotation'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params: Record<string, unknown> = {}) => {
    const text = timeRotation[key.replace('timeRotation.', '') as keyof typeof timeRotation] || key
    return text.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  } })
}))

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/admin/timeRotation', () => ({ timeRotationAPI: { get } }))
const config = (enabled = true, id = 7) => ({
  enabled, revision: 1,
  slots: [{ start: '22:00', end: '06:00', account_ids: [id], active_priority: 2, inactive_priority: 80 }]
})
const smartConfig = (enabled = true, id = 7) => ({
  enabled, revision: 2, mode: 'smart' as const, slots: [],
  smart: { account_ids: [id], periods: [], rotation_minutes: 60, quota_reserve_percent: 10 }
})
const render = () => mount(AccountTimeRotationHint, {
  props: { accountId: 7 },
  global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } }
})

beforeEach(() => { get.mockReset() })
describe('账号编辑时段轮候提示', () => {
  it('显示关联时段和优先级并锁定手工编辑', async () => {
    get.mockResolvedValue(config())
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('已经设置时段轮候')
    expect(wrapper.text()).toContain('22:00–06:00')
    expect(wrapper.text()).toContain('时段内优先级 2，时段外优先级 80')
    expect(wrapper.emitted('managed')?.at(-1)).toEqual([true])
    wrapper.unmount()
  })
  it('停用后保留关联提示并释放手工优先级', async () => {
    get.mockResolvedValue(config(false))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('已停用')
    expect(wrapper.emitted('managed')?.at(-1)).toEqual([false])
    wrapper.unmount()
  })
  it('账号切换时忽略旧请求，未选账号无提示', async () => {
    let resolveOld!: (value: ReturnType<typeof config>) => void
    get.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const wrapper = render()
    get.mockResolvedValue(config(true, 9))
    await wrapper.setProps({ accountId: 8 })
    await flushPromises()
    resolveOld(config())
    await flushPromises()
    expect(wrapper.find('[data-testid="time-rotation-hint"]').exists()).toBe(false)
    expect(wrapper.emitted('managed')?.at(-1)).toEqual([false])
    wrapper.unmount()
  })
  it('读取失败时明确提示并避免回写旧优先级', async () => {
    get.mockRejectedValue(new Error('断开连接'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('无法确认时段轮候状态')
    expect(wrapper.emitted('managed')?.at(-1)).toEqual([true])
    wrapper.unmount()
  })
  it('智能模式显示受管提示但不锁定 priority', async () => {
    get.mockResolvedValue(smartConfig())
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('智能轮候')
    expect(wrapper.text()).toContain('可正常编辑账号优先级')
    expect(wrapper.emitted('managed')?.at(-1)).toEqual([false])
    wrapper.unmount()
  })
})
