import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ExcelOAuthAuthorization from '../ExcelOAuthAuthorization.vue'
import { excelOAuthAPI } from '@/api/admin/excelBps'
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn(), put: vi.fn(), post: vi.fn() } }))
beforeEach(() => vi.restoreAllMocks())
describe('Excel 独立授权', () => {
  it('核对 state 后保存授权，不在表单或日志中接收令牌', async () => {
    vi.spyOn(excelOAuthAPI, 'status').mockResolvedValue({ authorized: false, primary: false })
    vi.spyOn(excelOAuthAPI, 'generate').mockResolvedValue({ auth_url: 'https://auth.openai.com/oauth/authorize?state=test-state', session_id: 'test-session' })
    const exchange = vi.spyOn(excelOAuthAPI, 'exchange').mockResolvedValue()
    const wrapper = mount(ExcelOAuthAuthorization, { props: { accountId: 12 } })
    await flushPromises()
    await wrapper.get('button').trigger('click'); await flushPromises()
    await wrapper.get('textarea').setValue('https://bps.openai.com/callback?code=test-code&state=wrong')
    await wrapper.findAll('button').at(-1)!.trigger('click'); await flushPromises()
    expect(exchange).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('不匹配')
    await wrapper.get('textarea').setValue('https://bps.openai.com/callback?code=test-code&state=test-state')
    await wrapper.findAll('button').at(-1)!.trigger('click'); await flushPromises()
    expect(exchange).toHaveBeenCalledWith(12, { session_id: 'test-session', code: 'test-code', state: 'test-state' })
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.text()).toContain('Excel 授权已保存')
  })
  it('切换编辑账号后清除旧授权链接，忽略迟到状态', async () => {
    let resolve!: (value: { authorized: boolean; primary: boolean }) => void
    vi.spyOn(excelOAuthAPI, 'status').mockImplementation(id => id === 1 ? new Promise(r => { resolve = r }) : Promise.resolve({ authorized: false, primary: false }))
    const wrapper = mount(ExcelOAuthAuthorization, { props: { accountId: 1 } })
    await wrapper.setProps({ accountId: 2 }); await flushPromises()
    resolve({ authorized: true, primary: true }); await flushPromises()
    expect(wrapper.text()).toContain('生成授权链接')
    expect(wrapper.text()).not.toContain('此账号已使用 Excel OAuth')
  })
})
