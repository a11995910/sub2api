import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import zh from '@/i18n/locales/zh/index'
import type { HealthyTurnStateStats } from '@/api/admin/accounts'
import OpenAIHealthyTurnStateStatus from '../OpenAIHealthyTurnStateStatus.vue'

const { getStats } = vi.hoisted(() => ({ getStats: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getHealthyTurnStateStats: getStats } } }))

const empty: HealthyTurnStateStats = { available: 0, in_use: 0, captures: 0, attempts: 0, successes: 0, failures: 0, models: [], records: [], probes: [] }
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, params: Record<string, string | number> = {}) => {
    let value: unknown = zh
    for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
    return String(value ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  }
}) }))
const mounted: ReturnType<typeof mount>[] = []
function panel() {
  const wrapper = mount(OpenAIHealthyTurnStateStatus, {
    props: { accountId: 7, active: true, revision: 0 }
  })
  mounted.push(wrapper)
  return wrapper
}
beforeEach(() => getStats.mockReset())
afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()))

describe('健康状态头统计', () => {
  it('无记录时显示当前账号零累计与默认自动记录指引', async () => {
    getStats.mockResolvedValue(empty)
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('暂无记录')
    expect(wrapper.text()).toContain('自动记录默认开启')
    expect(wrapper.findAll('dd').map(item => item.text())).toEqual(['0'])
    expect(wrapper.find('table').exists()).toBe(false)
  })

  it('显示账号全量累计与各模型累计，不依赖详情记录数或替换结果', async () => {
    getStats.mockResolvedValue({ ...empty, captures: 1474, attempts: 10, successes: 3, failures: 1, in_use: 2,
      models: [
        { model: 'gpt-6-astra', captures: 1400, available: 0, in_use: 0, attempts: 0, successes: 0, failures: 0 },
        { model: 'gpt-5.6-sol', captures: 74, available: 1, in_use: 0, attempts: 10, successes: 3, failures: 1 }
      ],
      probes: [{ model: 'gpt-test', transport: 'http', proxy_id: 0, status: 'failed', http_status: 429, created_at: '2026-09-17T10:00:00Z' }] })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('dd').text()).toBe('1474')
    expect(wrapper.findAll('tbody tr').map(row => row.findAll('th, td').map(cell => cell.text()))).toEqual([
      ['gpt-6-astra', '1400'], ['gpt-5.6-sol', '74']
    ])
    expect(wrapper.text()).not.toContain('暂无记录')
    expect(wrapper.text()).not.toContain('替换成功率')
    expect(wrapper.text()).not.toContain('gpt-test')
    expect(wrapper.find('details').exists()).toBe(false)
  })

  it('读取失败明确报错，刷新后显示真实统计', async () => {
    getStats.mockRejectedValueOnce(new Error('测试错误')).mockResolvedValueOnce(empty)
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('统计读取失败')
    expect(wrapper.find('dl').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('暂无记录')
  })

  it('切换账号会取消请求，旧账号的迟到结果不会覆盖新账号', async () => {
    let finish!: (stats: HealthyTurnStateStats) => void
    getStats.mockImplementationOnce(() => new Promise(resolve => { finish = resolve })).mockResolvedValueOnce(empty)
    const wrapper = panel()
    await wrapper.setProps({ accountId: 8 })
    expect(getStats.mock.calls[0]?.[1]?.aborted).toBe(true)
    await flushPromises()
    finish({ ...empty, captures: 999 })
    await flushPromises()
    expect(wrapper.get('dd').text()).toBe('0')
    await wrapper.setProps({ revision: 1 })
    await flushPromises()
    expect(getStats).toHaveBeenCalledTimes(3)
    await wrapper.setProps({ active: false })
    expect(getStats.mock.calls[2]?.[1]?.aborted).toBe(true)
    expect(wrapper.find('dl').exists()).toBe(false)
  })

  it('切换账号立即清除前一个账号已显示的模型和累计数', async () => {
    getStats.mockResolvedValueOnce({ ...empty, captures: 1474,
      models: [{ model: 'previous-account-model', captures: 1474, available: 0, in_use: 0, attempts: 0, successes: 0, failures: 0 }] })
      .mockReturnValueOnce(new Promise(() => {}))
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('previous-account-model')
    await wrapper.setProps({ accountId: 8 })
    expect(wrapper.text()).not.toContain('previous-account-model')
    expect(wrapper.text()).not.toContain('1474')
    expect(getStats.mock.calls[1]?.[0]).toBe(8)
  })
})
