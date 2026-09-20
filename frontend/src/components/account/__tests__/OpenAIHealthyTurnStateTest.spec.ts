import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { HealthyTurnStateDynamicConfig } from '@/api/admin/accounts'

const { getDynamicConfig, getModels, updateDynamicConfig } = vi.hoisted(() => ({ getDynamicConfig: vi.fn(), getModels: vi.fn(), updateDynamicConfig: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: {
  getHealthyTurnStateDynamicConfig: getDynamicConfig,
  getHealthyTurnStateModels: getModels,
  updateHealthyTurnStateDynamicConfig: updateDynamicConfig
} } }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string, params?: unknown) => params ? key + JSON.stringify(params) : key }) }))
import OpenAIHealthyTurnStateTest from '../OpenAIHealthyTurnStateTest.vue'

const savedConfig: HealthyTurnStateDynamicConfig = {
  configured: true, api_url_masked: 'https://provider.example/extract?key=***',
  protocol: 'http', target_count: 3, max_attempts: 100, models: ['gpt-5.4'], transport: 'http'
}
const supportedModels = [
  { id: 'gpt-5.4', display_name: 'GPT-5.4' },
  { id: 'gpt-5.5', display_name: 'GPT-5.5' }
]
const mounted: ReturnType<typeof mount>[] = []
function panel() {
  const wrapper = mount(OpenAIHealthyTurnStateTest, {
    props: { accountId: 7, active: true },
    global: { stubs: { OpenAIHealthyTurnStateStatus: true } }
  })
  mounted.push(wrapper)
  return wrapper
}
function save(wrapper: ReturnType<typeof mount>, allowUnchanged = false) {
  return (wrapper.vm as unknown as { saveConfig: (allowUnchanged: boolean) => Promise<boolean> }).saveConfig(allowUnchanged)
}
beforeEach(() => {
  getDynamicConfig.mockReset().mockResolvedValue({ ...savedConfig, models: [...savedConfig.models] })
  getModels.mockReset().mockResolvedValue(supportedModels)
  updateDynamicConfig.mockReset().mockImplementation((_id, input) => Promise.resolve({ ...savedConfig, ...input, api_url_masked: savedConfig.api_url_masked }))
})
afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()))

describe('健康状态头自动采集配置', () => {
  it('仅使用动态代理和上游支持模型复选框，保留已选模型与已保存的每模型三条', async () => {
    const wrapper = panel()
    await flushPromises()
    expect(getModels).toHaveBeenCalledWith(7, expect.any(AbortSignal))
    expect(wrapper.find('#healthy-state-test-mode').exists()).toBe(false)
    expect(wrapper.find('#healthy-state-test-model').exists()).toBe(false)
    expect(wrapper.find('input[value="direct"]').exists()).toBe(false)
    expect(wrapper.findAll('input[type="checkbox"]').map(input => input.attributes('value'))).toEqual(['gpt-5.4', 'gpt-5.5'])
    expect((wrapper.get('input[value="gpt-5.4"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('input[value="gpt-5.5"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('#healthy-state-dynamic-target').element as HTMLInputElement).value).toBe('3')
    expect(wrapper.text()).toContain('dynamic.saveWithAccount')
    expect(updateDynamicConfig).not.toHaveBeenCalled()
  })

  it('快捷增加至每模型十个仅修改表单，经账号统一保存时写入', async () => {
    const wrapper = panel()
    await flushPromises()
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('button').attributes('type')).toBe('button')
    expect((wrapper.get('#healthy-state-dynamic-target').element as HTMLInputElement).value).toBe('10')
    expect(wrapper.get('#healthy-state-dynamic-target-summary').text()).toContain('"models":1,"total":10')
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig).toHaveBeenCalledTimes(1)
    expect(updateDynamicConfig.mock.calls[0]?.[1]).toMatchObject({ target_count: 10, models: ['gpt-5.4'], update_shared_proxy: false, update_default_models: false })
  })

  it('目标库存按所选项和数量更新，非法数量时不显示误导总数', async () => {
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('#healthy-state-dynamic-target-summary').text()).toContain('"models":1,"total":3')
    await wrapper.get('input[value="gpt-5.5"]').setValue(true)
    await wrapper.get('#healthy-state-dynamic-target').setValue(8)
    expect(wrapper.get('#healthy-state-dynamic-target-summary').text()).toContain('"models":2,"total":16')
    await wrapper.get('#healthy-state-dynamic-target').setValue('')
    expect(wrapper.find('#healthy-state-dynamic-target-summary').exists()).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
  })

  it('快捷增加库存时同步满足最低尝试数量，仍等待统一保存', async () => {
    getDynamicConfig.mockResolvedValueOnce({ ...savedConfig, max_attempts: 3 })
    const wrapper = panel()
    await flushPromises()
    await wrapper.get('button').trigger('click')
    expect((wrapper.get('#healthy-state-dynamic-attempts').element as HTMLInputElement).value).toBe('10')
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1]).toMatchObject({ target_count: 10, max_attempts: 10 })
  })

  it('统一保存所选模型、传输、库存和代理配置，URL留空保留已保存值', async () => {
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('#healthy-state-dynamic-url').attributes('type')).toBe('password')
    expect((wrapper.get('#healthy-state-dynamic-url').element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).toContain('key=***')
    await wrapper.get('input[value="gpt-5.5"]').setValue(true)
    await wrapper.get('#healthy-state-test-transport').setValue('websocket')
    await wrapper.get('#healthy-state-dynamic-protocol').setValue('socks5h')
    await wrapper.get('#healthy-state-dynamic-target').setValue(5)
    expect(await save(wrapper)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.slice(0, 2)).toEqual([7, {
      api_url: '', protocol: 'socks5h', update_shared_proxy: true, update_default_models: true, target_count: 5, max_attempts: 100,
      models: ['gpt-5.4', 'gpt-5.5'], transport: 'websocket'
    }])
  })

  it('首次配置无默认选择时不自动全选，缺少URL、模型或数量非法均阻止保存', async () => {
    getDynamicConfig.mockResolvedValueOnce({ ...savedConfig, configured: false, api_url_masked: '', models: [] })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.findAll('input[type="checkbox"]').every(input => !(input.element as HTMLInputElement).checked)).toBe(true)
    expect(await save(wrapper)).toBe(false)
    await wrapper.get('#healthy-state-dynamic-url').setValue('https://provider.example/extract?num=3&time=1')
    expect(await save(wrapper)).toBe(false)
    await wrapper.get('input[value="gpt-5.4"]').setValue(true)
    await wrapper.get('#healthy-state-dynamic-target').setValue(101)
    expect(await save(wrapper)).toBe(false)
    await wrapper.get('#healthy-state-dynamic-target').setValue(3)
    await wrapper.get('#healthy-state-dynamic-attempts').setValue(2)
    expect(await save(wrapper)).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    await wrapper.get('#healthy-state-dynamic-attempts').setValue(100)
    expect(await save(wrapper)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1].api_url).toBe('https://provider.example/extract?num=3&time=1')
    expect(updateDynamicConfig.mock.calls[0]?.[1].update_shared_proxy).toBe(true)
    expect((wrapper.get('#healthy-state-dynamic-url').element as HTMLInputElement).value).toBe('')
  })

  it('新账号预勾继承的支持模型，无需点击模型或填写共享URL即可保存', async () => {
    getDynamicConfig.mockResolvedValueOnce({ ...savedConfig, models: ['gpt-5.5'], models_inherited: true })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('dynamic.modelsInherited')
    expect((wrapper.get('input[value="gpt-5.5"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('input[value="gpt-5.4"]').element as HTMLInputElement).checked).toBe(false)
    expect(await save(wrapper)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1]).toMatchObject({
      api_url: '', update_shared_proxy: false, update_default_models: false, models: ['gpt-5.5'], target_count: 3
    })
    expect(updateDynamicConfig.mock.calls[0]?.[1]).not.toHaveProperty('models_inherited')
    expect(wrapper.text()).not.toContain('dynamic.modelsInherited')
  })

  it('没有可继承的支持交集时显示原因，不自动全选，手动选择后可保存', async () => {
    const message = '上次选择的模型均不受当前账号支持，请重新勾选。'
    getDynamicConfig.mockResolvedValueOnce({ ...savedConfig, models: [], models_inherited: true, models_inheritance_message: message })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain(message)
    expect(wrapper.text()).not.toContain('dynamic.modelsInherited')
    expect(wrapper.findAll('input[type="checkbox"]').every(input => !(input.element as HTMLInputElement).checked)).toBe(true)
    expect(await save(wrapper)).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    await wrapper.get('input[value="gpt-5.4"]').setValue(true)
    expect(wrapper.text()).not.toContain(message)
    expect(await save(wrapper)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1].models).toEqual(['gpt-5.4'])
  })

  it('其他账号复用全局接口，只需勾选自己的模型即可留空URL保存', async () => {
    getDynamicConfig.mockResolvedValueOnce({ ...savedConfig, models: [], protocol: 'socks5h' })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('dynamic.savedUrl')
    expect(wrapper.text()).not.toContain('dynamic.urlRequired')
    expect((wrapper.get('#healthy-state-dynamic-url').element as HTMLInputElement).value).toBe('')
    expect(wrapper.findAll('input[type="checkbox"]').every(input => !(input.element as HTMLInputElement).checked)).toBe(true)
    expect(await save(wrapper)).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    await wrapper.get('input[value="gpt-5.5"]').setValue(true)
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.slice(0, 2)).toEqual([7, {
      api_url: '', protocol: 'socks5h', update_shared_proxy: false, update_default_models: true, target_count: 3, max_attempts: 100,
      models: ['gpt-5.5'], transport: 'http'
    }])
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig).toHaveBeenCalledTimes(1)
  })

  it('协议改回原值后只保存账号配置，保存成功后以最新全局协议为基准', async () => {
    const wrapper = panel()
    await flushPromises()
    await wrapper.get('#healthy-state-dynamic-protocol').setValue('https')
    await wrapper.get('#healthy-state-dynamic-protocol').setValue('http')
    updateDynamicConfig.mockResolvedValueOnce({ ...savedConfig, protocol: 'socks5h' })
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1].update_shared_proxy).toBe(false)
    expect((wrapper.get('#healthy-state-dynamic-protocol').element as HTMLSelectElement).value).toBe('socks5h')
    await wrapper.get('#healthy-state-dynamic-target').setValue(4)
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[1]?.[1]).toMatchObject({ protocol: 'socks5h', update_shared_proxy: false, target_count: 4 })
  })

  it('模型选择改回原集合不覆盖全局默认，实际修改保存后更新比较基准', async () => {
    getDynamicConfig.mockResolvedValueOnce({ ...savedConfig, models: ['gpt-5.4'], models_inherited: true })
    const wrapper = panel()
    await flushPromises()
    await wrapper.get('input[value="gpt-5.5"]').setValue(true)
    await wrapper.get('input[value="gpt-5.5"]').setValue(false)
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1].update_default_models).toBe(false)
    await wrapper.get('input[value="gpt-5.5"]').setValue(true)
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[1]?.[1]).toMatchObject({ models: ['gpt-5.4', 'gpt-5.5'], update_default_models: true })
    await wrapper.get('#healthy-state-dynamic-target').setValue(4)
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[2]?.[1]).toMatchObject({ models: ['gpt-5.4', 'gpt-5.5'], update_default_models: false })
  })

  it('旧账号接口冲突时保留模型和数量，填写统一URL即可修复并保存', async () => {
    getDynamicConfig.mockResolvedValueOnce({
      ...savedConfig, configured: false, api_url_masked: '', shared_proxy_conflict: true,
      models: ['gpt-5.5'], target_count: 5
    })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('dynamic.sharedProxyConflict')
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect((wrapper.get('input[value="gpt-5.5"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('#healthy-state-dynamic-target').element as HTMLInputElement).value).toBe('5')
    expect(await save(wrapper)).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    await wrapper.get('#healthy-state-dynamic-url').setValue('https://provider.example/unified')
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1]).toMatchObject({
      api_url: 'https://provider.example/unified', update_shared_proxy: true,
      models: ['gpt-5.5'], target_count: 5
    })
    expect(wrapper.text()).not.toContain('dynamic.sharedProxyConflict')
    expect(wrapper.text()).toContain('dynamic.savedUrl')
  })

  it('模型已不受支持时明确提示并阻止保存，取消旧模型后可保存', async () => {
    getDynamicConfig.mockResolvedValueOnce({ ...savedConfig, models: ['retired-model', 'gpt-5.4'] })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('dynamic.unsupportedModel')
    expect(await save(wrapper)).toBe(false)
    await wrapper.get('input[value="retired-model"]').setValue(false)
    expect(await save(wrapper)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1].models).toEqual(['gpt-5.4'])
  })

  it('模型加载失败不会静默回退或启用，重新加载成功后可保存', async () => {
    getModels.mockRejectedValueOnce(new Error('上游读取失败'))
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('dynamic.modelsLoadFailed')
    expect(await save(wrapper)).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(await save(wrapper)).toBe(true)
  })

  it('保留脱敏业务错误，原始网络错误不显示代理接口凭据', async () => {
    const wrapper = panel()
    await flushPromises()
    updateDynamicConfig.mockRejectedValueOnce(new Error('https://provider.example?secret=敏感信息'))
    expect(await save(wrapper)).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('dynamic.saveFailed')
    expect(wrapper.text()).not.toContain('敏感信息')
    updateDynamicConfig.mockRejectedValueOnce({ status: 401, message: '登录已过期，请重新登录' })
    expect(await save(wrapper)).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('登录已过期，请重新登录')
  })

  it('切换账号会中止旧加载，迟到的模型和配置不覆盖新账号', async () => {
    let finishConfig!: (value: HealthyTurnStateDynamicConfig) => void
    let finishModels!: (value: typeof supportedModels) => void
    getDynamicConfig.mockImplementationOnce(() => new Promise(resolve => { finishConfig = resolve }))
      .mockResolvedValueOnce({ ...savedConfig, models: ['gpt-5.5'], target_count: 4 })
    getModels.mockImplementationOnce(() => new Promise(resolve => { finishModels = resolve }))
    const wrapper = panel()
    await wrapper.setProps({ accountId: 8 })
    expect(getDynamicConfig.mock.calls[0]?.[1].aborted).toBe(true)
    expect(getModels.mock.calls[0]?.[1].aborted).toBe(true)
    await flushPromises()
    finishConfig({ ...savedConfig, target_count: 99 })
    finishModels([{ id: 'old-account-model', display_name: '旧账号模型' }])
    await flushPromises()
    expect(wrapper.text()).not.toContain('旧账号模型')
    expect((wrapper.get('#healthy-state-dynamic-target').element as HTMLInputElement).value).toBe('4')
    expect(await save(wrapper)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[0]).toBe(8)
  })

  it('关闭编辑页后中止界面保存请求，不发起或停止后台采集任务', async () => {
    let finish!: (value: HealthyTurnStateDynamicConfig) => void
    updateDynamicConfig.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = panel()
    await flushPromises()
    const pending = save(wrapper)
    await wrapper.setProps({ active: false })
    expect(updateDynamicConfig.mock.calls[0]?.[2].aborted).toBe(true)
    finish(savedConfig)
    expect(await pending).toBe(false)
    expect(await save(wrapper)).toBe(false)
    expect(updateDynamicConfig).toHaveBeenCalledTimes(1)
  })

  it('原已启用且未改健康配置时，上游模型加载失败不阻止保存其他账号字段', async () => {
    getModels.mockRejectedValueOnce(new Error('账号原代理不可达'))
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('dynamic.modelsLoadFailed')
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    expect(await save(wrapper, false)).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
  })

  it('原已启用但健康配置被改时仍校验，非法改动阻止保存，修正后写入配置', async () => {
    const wrapper = panel()
    await flushPromises()
    await wrapper.get('input[value="gpt-5.4"]').setValue(false)
    expect(await save(wrapper, true)).toBe(false)
    expect(updateDynamicConfig).not.toHaveBeenCalled()
    await wrapper.get('input[value="gpt-5.5"]').setValue(true)
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig.mock.calls[0]?.[1].models).toEqual(['gpt-5.5'])
    expect(await save(wrapper, true)).toBe(true)
    expect(updateDynamicConfig).toHaveBeenCalledTimes(1)
  })

})
