import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ExcelAutoAuthorization from '../ExcelAutoAuthorization.vue'
import { excelAutoAPI, type ExcelAutoStatus } from '@/api/admin/excelBps'
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn(), put: vi.fn() } }))
const initial = (): ExcelAutoStatus => ({ enabled: true, configured: true, email: 'test@example.invalid', totp_configured: true, state: 'queued', last_error: '', worker_ready: true })
beforeEach(() => { vi.useFakeTimers(); vi.spyOn(excelAutoAPI, 'status').mockResolvedValue(initial()); vi.spyOn(excelAutoAPI, 'save').mockResolvedValue() })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })
describe('自动 Excel 授权', () => {
  it('轮询不覆盖未保存的登录资料，保存后清空秘密', async () => {
    const wrapper = mount(ExcelAutoAuthorization, { props: { accountId: 1 } })
    await flushPromises()
    await wrapper.find('input[type="email"]').setValue('edited@example.invalid')
    await wrapper.findAll('input[type="password"]')[0]!.setValue('synthetic')
    await vi.advanceTimersByTimeAsync(10000); await flushPromises()
    expect((wrapper.find('input[type="email"]').element as HTMLInputElement).value).toBe('edited@example.invalid')
    expect((wrapper.findAll('input[type="password"]')[0]!.element as HTMLInputElement).value).toBe('synthetic')
    await wrapper.findAll('button').find(b => b.text().includes('保存自动'))!.trigger('click'); await flushPromises()
    expect(excelAutoAPI.save).toHaveBeenCalledWith(1, expect.objectContaining({ email: 'edited@example.invalid', password: 'synthetic' }))
    expect((wrapper.findAll('input[type="password"]')[0]!.element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })
  it('旧账号迟到的状态不覆盖切换后的账号', async () => {
    let resolveOld!: (value: ExcelAutoStatus) => void
    vi.mocked(excelAutoAPI.status).mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve })).mockResolvedValue({ ...initial(), email: 'new@example.invalid' })
    const wrapper = mount(ExcelAutoAuthorization, { props: { accountId: 1 } })
    await wrapper.setProps({ accountId: 2 }); await flushPromises()
    resolveOld(initial()); await flushPromises()
    expect((wrapper.find('input[type="email"]').element as HTMLInputElement).value).toBe('new@example.invalid')
    wrapper.unmount()
  })
  it('明确展示运行时缺失和安全验证失败', async () => {
    vi.mocked(excelAutoAPI.status).mockResolvedValue({ ...initial(), worker_ready: false, state: 'failed', last_error: '官方要求额外安全验证，请改用手动授权' })
    const wrapper = mount(ExcelAutoAuthorization, { props: { accountId: 1 } }); await flushPromises()
    expect(wrapper.text()).toContain('运行时尚未就绪'); expect(wrapper.text()).toContain('额外安全验证')
    wrapper.unmount()
  })
})
