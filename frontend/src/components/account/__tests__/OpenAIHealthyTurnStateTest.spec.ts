import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Proxy } from '@/types'

const { testHealthyTurnState } = vi.hoisted(() => ({ testHealthyTurnState: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { testHealthyTurnState, getHealthyTurnStateStats: vi.fn().mockResolvedValue({ available: 0, in_use: 0, captures: 0, attempts: 0, successes: 0, failures: 0, records: [], probes: [] }) } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
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
beforeEach(() => { testHealthyTurnState.mockReset() })
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
