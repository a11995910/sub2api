import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import ExcelBPSAccountSettings from '../ExcelBPSAccountSettings.vue'
import { excelBpsAPI, initialExcelBPSDefaults, readExcelBPSAccount, writeExcelBPSAccount } from '@/api/admin/excelBps'
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn(), put: vi.fn() } }))
const config = () => ({ enabled: true, defaults: { ...initialExcelBPSDefaults(), models: ['gpt-6-astra'], ignore_encrypted_content: true }, images: {} })
beforeEach(() => vi.restoreAllMocks())
describe('Excel/BPS 账号设置', () => {
  it('应用默认设置只复制当前表单，不改变服务器模板或无关字段', async () => {
    const saved = config()
    vi.spyOn(excelBpsAPI, 'get').mockResolvedValue(saved as any)
    const form = reactive({ ...readExcelBPSAccount(), enabled: true })
    const wrapper = mount(ExcelBPSAccountSettings, { props: { modelValue: form }, global: { stubs: { RouterLink: true, ExcelBPSOptions: true } } })
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('应用智能运维'))!.trigger('click')
    expect(form.ignore_encrypted_content).toBe(true)
    expect(form.models).toEqual(['gpt-6-astra'])
    form.models.push('local')
    expect(saved.defaults.models).toEqual(['gpt-6-astra'])
    const payload = writeExcelBPSAccount({ existing: '保留' }, form)
    expect(payload.existing).toBe('保留')
    expect(payload.openai_excel_bps).toBe(true)
  })
  it('全局关闭时隐藏账号选项，保留已保存值', async () => {
    vi.spyOn(excelBpsAPI, 'get').mockResolvedValue({ ...config(), enabled: false } as any)
    const form = { ...readExcelBPSAccount(), enabled: true }
    const wrapper = mount(ExcelBPSAccountSettings, { props: { modelValue: form }, global: { stubs: { RouterLink: true } } })
    await flushPromises()
    expect(wrapper.find('[data-testid="excel-bps-account-settings"]').exists()).toBe(false)
    expect(form.enabled).toBe(true)
  })
  it('模型全选删除模型字段，明确空数组不被当成全选', () => {
    expect(readExcelBPSAccount({ openai_excel_bps: true, openai_excel_bps_models: [] }).all_models).toBe(false)
    const form = { ...readExcelBPSAccount(), enabled: true, all_models: true }
    expect(writeExcelBPSAccount({ openai_excel_bps_models: ['old'] }, form)).not.toHaveProperty('openai_excel_bps_models')
  })
  it('配置读取失败不覆盖当前表单', async () => {
    vi.spyOn(excelBpsAPI, 'get').mockRejectedValue(new Error('离线'))
    const form = readExcelBPSAccount({ openai_excel_bps: true, openai_excel_bps_models: ['custom'] })
    const wrapper = mount(ExcelBPSAccountSettings, { props: { modelValue: form }, global: { stubs: { RouterLink: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('读取失败')
    expect(form.models).toEqual(['custom'])
  })
})
