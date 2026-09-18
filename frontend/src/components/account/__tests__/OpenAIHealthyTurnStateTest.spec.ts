import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Proxy } from '@/types'
import type { HealthyTurnStateDynamicRun } from '@/api/admin/accounts'

const { testHealthyTurnState, getDynamicConfig, updateDynamicConfig, startDynamicRun, stepDynamicRun, stopDynamicRun } = vi.hoisted(() => ({ testHealthyTurnState: vi.fn(), getDynamicConfig: vi.fn(), updateDynamicConfig: vi.fn(), startDynamicRun: vi.fn(), stepDynamicRun: vi.fn(), stopDynamicRun: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { testHealthyTurnState, getHealthyTurnStateDynamicConfig: getDynamicConfig, updateHealthyTurnStateDynamicConfig: updateDynamicConfig, startHealthyTurnStateDynamicRun: startDynamicRun, stepHealthyTurnStateDynamicRun: stepDynamicRun, stopHealthyTurnStateDynamicRun: stopDynamicRun, getHealthyTurnStateStats: vi.fn().mockResolvedValue({ available: 0, in_use: 0, captures: 0, attempts: 0, successes: 0, failures: 0, models: [], records: [], probes: [] }) } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: unknown) => params ? key + JSON.stringify(params) : key }) }))
import OpenAIHealthyTurnStateTest from '../OpenAIHealthyTurnStateTest.vue'

const proxies = [
  { id: 11, name: '代理甲', host: 'proxy-a.test', port: 8080, status: 'active', expires_at: null },
  { id: 12, name: '代理乙', host: 'proxy-b.test', port: 8080, status: 'active', expires_at: null },
  { id: 13, name: '已停用', host: 'disabled.test', port: 8080, status: 'inactive', expires_at: null }
] as Proxy[]
const mounted: ReturnType<typeof mount>[] = []
async function openPanel() {
  const wrapper = mount(OpenAIHealthyTurnStateTest, { props: { accountId: 7, proxies, active: true } })
  mounted.push(wrapper)
  await wrapper.get('button[aria-controls="healthy-state-test-controls"]').trigger('click')
  return wrapper
}
function button(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper.findAll('button').find(item => item.text().includes(text))!
}
const savedConfig = { configured: true, api_url_masked: 'https://provider.example/extract?key=***', protocol: 'http', target_count: 5, max_attempts: 100 }
const run = (values: Partial<HealthyTurnStateDynamicRun> = {}): HealthyTurnStateDynamicRun => ({ id: 'run-7', status: 'running', model: 'gpt-6-astra', transport: 'http', attempts: 0, recorded: 0, target_count: 5, max_attempts: 100, fetched_batches: 0, ...values })
beforeEach(() => {
  testHealthyTurnState.mockReset()
  getDynamicConfig.mockReset().mockResolvedValue({ ...savedConfig })
  updateDynamicConfig.mockReset().mockImplementation((_id, input) => Promise.resolve({ ...savedConfig, ...input, api_url_masked: savedConfig.api_url_masked }))
  startDynamicRun.mockReset().mockResolvedValue(run())
  stepDynamicRun.mockReset().mockResolvedValue(run({ status: 'completed' }))
  stopDynamicRun.mockReset().mockResolvedValue(run({ status: 'stopped' }))
})
afterEach(() => { for (const wrapper of mounted.splice(0)) wrapper.unmount() })

describe('OpenAIHealthyTurnStateTest', () => {
  it('使用所选模型和代理逐个测试，成功与 503 结果分别显示', async () => {
    let completeFirst!: (value: unknown) => void
    testHealthyTurnState.mockImplementationOnce(() => new Promise(resolve => { completeFirst = resolve }))
      .mockResolvedValueOnce({ status: 'failed', model: 'gpt-5.4', transport: 'websocket', http_status: 503 })
    const wrapper = await openPanel()
    expect(wrapper.find('input[value="13"]').exists()).toBe(false)
    await wrapper.get('input[value="account"]').setValue(false)
    await wrapper.get('input[value="11"]').setValue(true)
    await wrapper.get('input[value="12"]').setValue(true)
    await wrapper.get('#healthy-state-test-model').setValue('gpt-5.4')
    await wrapper.get('#healthy-state-test-transport').setValue('websocket')
    await button(wrapper, 'testStart').trigger('click')
    expect(testHealthyTurnState).toHaveBeenCalledTimes(1)
    expect(testHealthyTurnState.mock.calls[0]?.slice(0, 2)).toEqual([7, { model: 'gpt-5.4', transport: 'websocket', proxy_id: 11 }])
    expect(button(wrapper, 'testRunning').attributes('disabled')).toBeDefined()
    completeFirst({ status: 'recorded', model: 'gpt-5.4', transport: 'websocket', http_status: 101 })
    await flushPromises()
    expect(testHealthyTurnState).toHaveBeenCalledTimes(2)
    expect(testHealthyTurnState.mock.calls[1]?.[1]?.proxy_id).toBe(12)
    expect(wrapper.text()).toContain('testStatus.recorded')
    expect(wrapper.text()).toContain('testStatus.failed')
    expect(wrapper.text()).toContain('503')
  })

  it('取消会终止当前请求并停止后续代理测试', async () => {
    testHealthyTurnState.mockImplementation((_id, _input, signal: AbortSignal) => new Promise((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(new Error('已取消')), { once: true })
    }))
    const wrapper = await openPanel()
    await wrapper.get('input[value="11"]').setValue(true)
    await button(wrapper, 'testStart').trigger('click')
    await button(wrapper, 'common.cancel').trigger('click')
    await flushPromises()
    expect(testHealthyTurnState.mock.calls[0]?.[2]?.aborted).toBe(true)
    expect(testHealthyTurnState).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('testStatus.cancelled')
  })

  it('切换账号时取消旧请求，旧结果不能进入新账号界面', async () => {
    let complete!: (value: unknown) => void
    testHealthyTurnState.mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    const wrapper = await openPanel()
    await button(wrapper, 'testStart').trigger('click')
    await wrapper.setProps({ accountId: 8 })
    expect(testHealthyTurnState.mock.calls[0]?.[2]?.aborted).toBe(true)
    complete({ status: 'recorded', model: 'gpt-5.4', transport: 'http' })
    await flushPromises()
    expect(wrapper.find('ul').exists()).toBe(false)
  })

  it('无所选出口不能开始；缺少状态头会显示未记录', async () => {
    testHealthyTurnState.mockResolvedValue({ status: 'no_header', model: 'gpt-5.4', transport: 'http', http_status: 200 })
    const wrapper = await openPanel()
    await wrapper.get('input[value="account"]').setValue(false)
    expect(button(wrapper, 'testStart').attributes('disabled')).toBeDefined()
    await wrapper.get('input[value="direct"]').setValue(true)
    await button(wrapper, 'testStart').trigger('click')
    await flushPromises()
    expect(testHealthyTurnState.mock.calls[0]?.[1]?.proxy_id).toBe(0)
    expect(testHealthyTurnState.mock.calls[0]?.[1]?.model).toBe('gpt-6-astra')
    expect(wrapper.text()).toContain('testStatus.no_header')
  })
})

async function openDynamicPanel() {
  const wrapper = await openPanel()
  await wrapper.get('#healthy-state-test-mode').setValue('dynamic')
  await flushPromises()
  return wrapper
}

describe('动态 IP 采集', () => {
  it('保留已保存 URL，并按模型协议及当前数量保存后再开始，只有新增计入目标', async () => {
    startDynamicRun.mockResolvedValue(run({ target_count: 2 }))
    stepDynamicRun.mockResolvedValueOnce(run({ target_count: 2, attempts: 1, fetched_batches: 1, last_result: { status: 'already_recorded', model: 'gpt-6-astra', transport: 'http' } }))
      .mockResolvedValueOnce(run({ target_count: 2, attempts: 2, recorded: 1, fetched_batches: 1, last_result: { status: 'recorded', model: 'gpt-6-astra', transport: 'http' } }))
      .mockResolvedValueOnce(run({ status: 'completed', target_count: 2, attempts: 3, recorded: 2, fetched_batches: 1, last_result: { status: 'recorded', model: 'gpt-6-astra', transport: 'http' } }))
    const wrapper = await openDynamicPanel()
    expect(wrapper.get('#healthy-state-dynamic-url').attributes('type')).toBe('password')
    expect((wrapper.get('#healthy-state-dynamic-url').element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).toContain('key=***')
    await wrapper.get('#healthy-state-dynamic-target').setValue(2)
    await wrapper.get('#healthy-state-dynamic-protocol').setValue('socks5h')
    await wrapper.get('#healthy-state-test-model').setValue('gpt-5.4')
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    expect(updateDynamicConfig.mock.calls[0]?.slice(0, 2)).toEqual([7, { api_url: '', protocol: 'socks5h', target_count: 2, max_attempts: 100 }])
    expect(startDynamicRun.mock.calls[0]).toEqual([7, { model: 'gpt-5.4', transport: 'http' }])
    expect(stepDynamicRun).toHaveBeenCalledTimes(3)
    expect(wrapper.text()).toContain('"recorded":2')
    expect(wrapper.text()).toContain('dynamic.status.completed')
    expect(wrapper.findAll('ul li')).toHaveLength(3)
  })

  it('串行等待每次结果，到达服务端尝试上限后停止并保留未达目标的真实数量', async () => {
    let finish!: (value: HealthyTurnStateDynamicRun) => void
    stepDynamicRun.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = await openDynamicPanel()
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    expect(stepDynamicRun).toHaveBeenCalledTimes(1)
    finish(run({ status: 'completed', attempts: 100, recorded: 1, fetched_batches: 10, message: '已达到最多尝试入口数' }))
    await flushPromises()
    expect(stepDynamicRun).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('"recorded":1')
    expect(wrapper.text()).toContain('已达到最多尝试入口数')
  })

  it('取消中止 step 并停止正确账号的任务，不再发送后续探测', async () => {
    stepDynamicRun.mockImplementation((_accountId, _runId, signal: AbortSignal) => new Promise((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(new Error('已取消')), { once: true })
    }))
    const wrapper = await openDynamicPanel()
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    await button(wrapper, 'common.cancel').trigger('click')
    await flushPromises()
    expect(stepDynamicRun.mock.calls[0]?.[2]?.aborted).toBe(true)
    expect(stepDynamicRun).toHaveBeenCalledTimes(1)
    expect(stopDynamicRun).toHaveBeenCalledTimes(1)
    expect(stopDynamicRun).toHaveBeenCalledWith(7, 'run-7')
    expect(wrapper.text()).toContain('dynamic.status.stopped')
  })

  it('启动在途切换账号时，收到旧 run ID 后停止旧任务，不能污染新账号或开始 step', async () => {
    let started!: (value: HealthyTurnStateDynamicRun) => void
    startDynamicRun.mockImplementationOnce(() => new Promise(resolve => { started = resolve }))
    const wrapper = await openDynamicPanel()
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    await wrapper.setProps({ accountId: 8 })
    expect(updateDynamicConfig.mock.calls[0]?.[2]?.aborted).toBe(true)
    started(run())
    await flushPromises()
    expect(stopDynamicRun).toHaveBeenCalledTimes(1)
    expect(stopDynamicRun).toHaveBeenCalledWith(7, 'run-7')
    expect(stepDynamicRun).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('dynamic.status.stopped')
    expect(wrapper.find('ul').exists()).toBe(false)
  })

  it('卸载或关闭编辑页时中止当前 step 并停止任务，迟到响应不触发下一步', async () => {
    for (const close of ['inactive', 'unmount']) {
      let finish!: (value: HealthyTurnStateDynamicRun) => void
      stepDynamicRun.mockClear().mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
      stopDynamicRun.mockClear()
      const wrapper = await openDynamicPanel()
      await button(wrapper, 'dynamic.start').trigger('click')
      await flushPromises()
      if (close === 'inactive') await wrapper.setProps({ active: false })
      else wrapper.unmount()
      finish(run({ attempts: 1, recorded: 1 }))
      await flushPromises()
      expect(stepDynamicRun.mock.calls[0]?.[2]?.aborted).toBe(true)
      expect(stepDynamicRun).toHaveBeenCalledTimes(1)
      expect(stopDynamicRun).toHaveBeenCalledTimes(1)
      expect(stopDynamicRun).toHaveBeenCalledWith(7, 'run-7')
    }
  })

  it('供应商失败明确展示错误且停止循环，保存失败不会启动任务', async () => {
    stepDynamicRun.mockResolvedValueOnce(run({ status: 'failed', message: '提取接口返回 HTTP 403，请检查供应商白名单' }))
    const wrapper = await openDynamicPanel()
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('提取接口返回 HTTP 403，请检查供应商白名单')
    expect(stepDynamicRun).toHaveBeenCalledTimes(1)
    updateDynamicConfig.mockRejectedValueOnce(new Error('禁止显示含凭据的原始错误'))
    startDynamicRun.mockClear()
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    expect(startDynamicRun).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('dynamic.saveFailed')
    expect(wrapper.text()).not.toContain('禁止显示含凭据的原始错误')
  })

  it('保留管理接口返回的认证、模型和已有任务错误说明', async () => {
    getDynamicConfig.mockRejectedValueOnce({ status: 401, message: '登录已过期，请重新登录' })
    const wrapper = await openDynamicPanel()
    expect(wrapper.get('[role="alert"]').text()).toContain('登录已过期，请重新登录')
    await button(wrapper, 'dynamic.reload').trigger('click')
    await flushPromises()
    updateDynamicConfig.mockRejectedValueOnce({ response: { data: { message: 'HTTP 403：当前用户无权修改账号配置' } } })
    await button(wrapper, 'dynamic.save').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('HTTP 403：当前用户无权修改账号配置')
    startDynamicRun.mockRejectedValueOnce({ status: 400, message: '账号不支持该模型' })
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('账号不支持该模型')
    startDynamicRun.mockRejectedValueOnce({ status: 409, message: '当前账号已有正在运行的采集任务' })
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('当前账号已有正在运行的采集任务')
    expect(stepDynamicRun).not.toHaveBeenCalled()
  })

  it('仅保留最近 50 次结果，提取失败时不重复显示上次采集结果', async () => {
    const lastResult = { status: 'no_header' as const, model: 'gpt-6-astra', transport: 'http' as const }
    for (let attempts = 1; attempts <= 51; attempts++) {
      stepDynamicRun.mockResolvedValueOnce(run({ attempts, fetched_batches: 1, last_result: lastResult }))
    }
    stepDynamicRun.mockResolvedValueOnce(run({ status: 'failed', attempts: 51, fetched_batches: 2, last_result: lastResult, message: '下一批提取失败' }))
    const wrapper = await openDynamicPanel()
    await button(wrapper, 'dynamic.start').trigger('click')
    await flushPromises()
    expect(stepDynamicRun).toHaveBeenCalledTimes(52)
    expect(wrapper.findAll('ul li')).toHaveLength(50)
    expect(wrapper.findAll('ul li')[0]?.text()).toContain('"count":2')
    expect(wrapper.findAll('ul li').at(-1)?.text()).toContain('"count":51')
    expect(wrapper.get('[role="alert"]').text()).toContain('下一批提取失败')
  })

  it('按账号加载配置，旧账号的迟到配置不覆盖新账号；无URL或数量不合法不能开始', async () => {
    let finish!: (value: unknown) => void
    getDynamicConfig.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
      .mockResolvedValueOnce({ ...savedConfig, configured: false, api_url_masked: '' })
    const wrapper = await openPanel()
    await wrapper.get('#healthy-state-test-mode').setValue('dynamic')
    await wrapper.setProps({ accountId: 8 })
    await wrapper.get('button[aria-controls="healthy-state-test-controls"]').trigger('click')
    await wrapper.get('#healthy-state-test-mode').setValue('dynamic')
    await flushPromises()
    finish({ ...savedConfig, api_url_masked: '旧账号地址' })
    await flushPromises()
    expect(getDynamicConfig.mock.calls[0]?.[1]?.aborted).toBe(true)
    expect(wrapper.text()).not.toContain('旧账号地址')
    expect(button(wrapper, 'dynamic.start').attributes('disabled')).toBeDefined()
    await wrapper.get('#healthy-state-dynamic-url').setValue('https://provider.example/extract?num=5&time=1')
    await wrapper.get('#healthy-state-dynamic-target').setValue(101)
    expect(button(wrapper, 'dynamic.start').attributes('disabled')).toBeDefined()
    await wrapper.get('#healthy-state-dynamic-target').setValue(5)
    await wrapper.get('#healthy-state-dynamic-attempts').setValue(4)
    expect(button(wrapper, 'dynamic.start').attributes('disabled')).toBeDefined()
    await wrapper.get('#healthy-state-dynamic-attempts').setValue(100)
    await button(wrapper, 'dynamic.save').trigger('click')
    await flushPromises()
    expect(updateDynamicConfig.mock.calls[0]?.[0]).toBe(8)
    expect(updateDynamicConfig.mock.calls[0]?.[1]?.api_url).toBe('https://provider.example/extract?num=5&time=1')
    expect((wrapper.get('#healthy-state-dynamic-url').element as HTMLInputElement).value).toBe('')
  })
})
