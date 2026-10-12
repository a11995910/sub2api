import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ExcelBPSSettings from '../ExcelBPSSettings.vue'
import { excelBpsAPI, initialExcelBPSDefaults } from '@/api/admin/excelBps'
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn(), put: vi.fn() } }))
describe('Excel/BPS 默认设置', () => {
  it('只保存默认字段，不能用旧快照重新打开全局协议', async () => {
    const config = { enabled: true, defaults: initialExcelBPSDefaults(), images: {} } as any
    vi.spyOn(excelBpsAPI, 'get').mockResolvedValue(config)
    const save = vi.spyOn(excelBpsAPI, 'save').mockResolvedValue(config)
    const wrapper = mount(ExcelBPSSettings, { props: { defaultsOnly: true }, global: { stubs: { RouterLink: true, ExcelBPSOptions: true } } })
    await flushPromises()
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith({ defaults: config.defaults })
    expect(wrapper.text()).toContain('已保存')
  })
})
