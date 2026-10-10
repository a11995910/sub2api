import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import ExcelBPSAccountSection from '../ExcelBPSAccountSection.vue'
import { getBPSDefaults, getBPSSettings, initialBPSDefaults, recommendedBPSDefaults } from '@/api/admin/excelBPS'

vi.mock('@/api/admin/excelBPS', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/admin/excelBPS')>(),
  getBPSSettings: vi.fn(), getBPSDefaults: vi.fn()
}))
const optionsStub = defineComponent({
  props: ['modelValue'],
  template: '<button data-testid="nested" @click="modelValue.models.push(\'new-model\')">修改模型</button>'
})
function render(free = false) {
  return mount(ExcelBPSAccountSection, {
    props: { enabled: false, options: initialBPSDefaults(), free },
    global: { stubs: { RouterLink: true, ExcelBPSOptions: optionsStub } }
  })
}
beforeEach(() => {
  vi.mocked(getBPSSettings).mockResolvedValue({ excel_bps_enabled: true } as Awaited<ReturnType<typeof getBPSSettings>>)
  vi.mocked(getBPSDefaults).mockResolvedValue(recommendedBPSDefaults())
})
describe('Excel/BPS 账号配置', () => {
  it('总开关关闭时不展示账号配置', async () => {
    vi.mocked(getBPSSettings).mockResolvedValue({ excel_bps_enabled: false } as Awaited<ReturnType<typeof getBPSSettings>>)
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[data-testid="excel-bps-account"]').exists()).toBe(false)
  })
  it('套用模板只修改当前表单，并开启协议', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('套用默认配置'))!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:options')?.[0]).toEqual([recommendedBPSDefaults()])
    expect(wrapper.emitted('update:enabled')?.[0]).toEqual([true])
  })
  it('模板读取失败保留当前表单', async () => {
    vi.mocked(getBPSDefaults).mockRejectedValueOnce(new Error('不可用'))
    const wrapper = render()
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('套用默认配置'))!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:options')).toBeUndefined()
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    expect(wrapper.find('[role="alert"]').text()).toContain('表单未改变')
  })
  it('嵌套模型编辑也通知父表单保存', async () => {
    const wrapper = render()
    await wrapper.setProps({ enabled: true })
    await flushPromises()
    await wrapper.get('[data-testid="nested"]').trigger('click')
    expect(wrapper.emitted('change')).toBeTruthy()
  })
  it('免费账号不能从关闭状态开启', async () => {
    const wrapper = render(true)
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').find(b => b.text().includes('套用默认配置'))!.attributes('disabled')).toBeDefined()
  })
})
