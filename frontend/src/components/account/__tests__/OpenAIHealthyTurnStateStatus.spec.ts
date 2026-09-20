import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import zh from '@/i18n/locales/zh/index'
import type { HealthyTurnStateStats } from '@/api/admin/accounts'
import { formatDateTime } from '@/utils/format'
import OpenAIHealthyTurnStateStatus from '../OpenAIHealthyTurnStateStatus.vue'

const { getStats } = vi.hoisted(() => ({ getStats: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getHealthyTurnStateStats: getStats } } }))

const empty: HealthyTurnStateStats = { available: 0, in_use: 0, captures: 0, attempts: 0, successes: 0, failures: 0, models: [], records: [], probes: [] }
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({
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
afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  vi.useRealTimers()
})

describe('健康状态头统计', () => {
  it('无记录时显示零库存和动态代理配置指引', async () => {
    getStats.mockResolvedValue(empty)
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('暂无记录')
    expect(wrapper.text()).toContain('全局动态 IP 接口已配置')
    expect(wrapper.findAll('dd').map(item => item.text())).toEqual(['0', '0'])
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
    expect(wrapper.findAll('dd').map(item => item.text())).toEqual(['2', '1474'])
    expect(wrapper.findAll('tbody tr').map(row => row.findAll('th, td').map(cell => cell.text()))).toEqual([
      ['gpt-6-astra', '0', '1400', '0', '0', '0'], ['gpt-5.6-sol', '1', '74', '10', '3', '1']
    ])
    expect(wrapper.text()).not.toContain('暂无记录')
    expect(wrapper.text()).not.toContain('替换成功率')
    expect(wrapper.text()).toContain('历史累计，不是当前模式的独立成功率')
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

  it('后台提取失败时显示脱敏原因和下次重试时间', async () => {
    const nextRetry = '2026-09-19T10:30:00Z'
    getStats.mockResolvedValue({ ...empty, maintenance: {
      status: 'backoff', message: '提取接口返回 HTTP 403，请检查供应商白名单', next_retry_at: nextRetry
    } })
    const wrapper = panel()
    await flushPromises()
    const status = wrapper.get('[role="status"]')
    expect(status.text()).toContain('后台采集 · 暂缓重试')
    expect(status.text()).toContain('提取接口返回 HTTP 403，请检查供应商白名单')
    expect(status.text()).toContain(`下次重试：${formatDateTime(nextRetry)}`)
    expect(wrapper.findAll('dd').map(item => item.text())).toEqual(['0', '0'])
  })

  it.each([
    ['disabled', '已关闭'], ['unconfigured', '待配置'], ['idle', '待命'], ['running', '正在补充']
  ])('后台状态 %s 显示中文说明', async (status, label) => {
    getStats.mockResolvedValue({ ...empty, maintenance: { status, message: '', next_retry_at: null } })
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain(`后台采集 · ${label}`)
    expect(wrapper.text()).not.toContain('下次重试')
  })

  it('打开期间每15秒刷新，慢请求期间不重叠发起轮询', async () => {
    vi.useFakeTimers()
    let finish!: (stats: HealthyTurnStateStats) => void
    getStats.mockResolvedValueOnce(empty)
      .mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
      .mockResolvedValue(empty)
    const wrapper = panel()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(14999)
    expect(getStats).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(getStats).toHaveBeenCalledTimes(2)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(45000)
    expect(getStats).toHaveBeenCalledTimes(2)
    expect(getStats.mock.calls[1]?.[1].aborted).toBe(false)
    finish({ ...empty, available: 3 })
    await flushPromises()
    expect(wrapper.findAll('dd')[0]?.text()).toBe('3')
    await vi.advanceTimersByTimeAsync(15000)
    expect(getStats).toHaveBeenCalledTimes(3)
  })

  it('关闭时停止轮询并取消在途请求，重开后恢复，卸载后不再发送请求', async () => {
    vi.useFakeTimers()
    let finish!: (stats: HealthyTurnStateStats) => void
    getStats.mockResolvedValueOnce(empty)
      .mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
      .mockResolvedValue(empty)
    const wrapper = panel()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(15000)
    expect(getStats).toHaveBeenCalledTimes(2)
    await wrapper.setProps({ active: false })
    expect(getStats.mock.calls[1]?.[1].aborted).toBe(true)
    finish({ ...empty, captures: 888 })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(45000)
    expect(getStats).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('888')
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(getStats).toHaveBeenCalledTimes(3)
    wrapper.unmount()
    expect(getStats.mock.calls[2]?.[1].aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(45000)
    expect(getStats).toHaveBeenCalledTimes(3)
  })

  it('轮询暂时失败后继续定期重试，不需要重新打开页面', async () => {
    vi.useFakeTimers()
    getStats.mockRejectedValueOnce(new Error('临时网络失败')).mockResolvedValue(empty)
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('统计读取失败')
    await vi.advanceTimersByTimeAsync(15000)
    expect(getStats).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('dl').exists()).toBe(true)
  })

})
