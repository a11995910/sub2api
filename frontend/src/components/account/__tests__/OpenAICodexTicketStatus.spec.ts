import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import zh from '@/i18n/locales/zh/index'
import type { OpenAICodexTicketStatus } from '@/types'
import { formatDateTime } from '@/utils/format'
import OpenAICodexTicketStatusPanel from '../OpenAICodexTicketStatus.vue'

const { getById } = vi.hoisted(() => ({ getById: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getById } } }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({
  t: (key: string, params: Record<string, string | number> = {}) => {
    let value: unknown = zh
    for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
    return String(value ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  }
}) }))
const ticket: OpenAICodexTicketStatus = { model: 'gpt-5.5', ready: false, blocked: true, remaining_seconds: 0, harvest_status: 'waiting', retry_interval_seconds: 6 }
const wrappers: ReturnType<typeof mount>[] = []
function panel(props: { tickets?: OpenAICodexTicketStatus[]; accountId?: number; compact?: boolean; active?: boolean } = {}) {
  const wrapper = mount(OpenAICodexTicketStatusPanel, { props: { tickets: [ticket], ...props } })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  getById.mockReset()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('292/332 门票采集状态', () => {
  it('业务模型不一致后明确显示票据失效，并展示采集模型验证失败', () => {
    const wrapper = panel({ tickets: [{ ...ticket, invalid_reason: 'model_mismatch', last_result: 'model_mismatch' }] })
    expect(wrapper.get('[data-testid="codex-ticket-invalid-reason"]').text()).toContain('模型不一致，门票及代理绑定已失效')
    expect(wrapper.text()).toContain('响应模型不一致，未保存门票')
    expect(wrapper.text()).toContain('缺少门票，模型已暂停')
    expect(wrapper.find('.text-emerald-700').exists()).toBe(false)
  })

  it('展示安全结果、批次进度，采集中不显示旧重试时间', () => {
    const wrapper = panel({ tickets: [{ ...ticket, harvest_status: 'collecting', attempt_index: 2, attempt_total: 4, last_result: 'upstream_error', last_http_status: 403, next_attempt_at: '2026-09-20T10:00:00Z' }] })
    expect(wrapper.text()).toContain('缺少门票，模型已暂停')
    expect(wrapper.text()).toContain('正在采集')
    expect(wrapper.text()).toContain('本次已发起 2 / 4 个代理探测')
    expect(wrapper.text()).toContain('最近结果：上游请求失败')
    expect(wrapper.text()).toContain('HTTP 403')
    expect(wrapper.find('[data-testid="codex-ticket-next-attempt"]').exists()).toBe(false)
  })

  it('独立任务排队不伪造倒计时，定时器就绪后才展示预计时刻', async () => {
    const wrapper = panel()
    expect(wrapper.text()).toContain('等待独立采集任务或可用并发位；失败后约 6 秒重试')
    expect(wrapper.text()).not.toContain('预计下次尝试')
    const nextAt = '2026-09-20T10:00:00Z'
    await wrapper.setProps({ tickets: [{ ...ticket, next_attempt_at: nextAt }] })
    expect(wrapper.text()).toContain(`预计下次尝试：${formatDateTime(nextAt)}`)
    expect(wrapper.find('[data-testid="codex-ticket-waiting"]').exists()).toBe(false)
  })

  it('独立显示有效旧票与采集暂停原因，紧凑列表跟随新props但不单独发请求', async () => {
    const wrapper = panel({ compact: true, accountId: 7, tickets: [{ ...ticket, ready: true, blocked: false, remaining_seconds: 100, harvest_status: 'paused', pause_reason: 'proxy_not_configured' }] })
    expect(wrapper.text()).toContain('剩余 1:40')
    expect(wrapper.text()).toContain('采集已暂停：请先配置采集来源')
    expect(getById).not.toHaveBeenCalled()
    await wrapper.setProps({ tickets: [{ ...ticket, harvest_status: 'collecting' }] })
    expect(wrapper.text()).toContain('正在采集')
    expect(wrapper.text()).not.toContain('剩余 1:40')
  })

  it('编辑面板每5秒刷新，隐藏或关闭时停止，读取失败保留上次结果', async () => {
    vi.useFakeTimers()
    getById.mockResolvedValueOnce({ codex_turn_tickets: [{ ...ticket, harvest_status: 'collecting' }] }).mockRejectedValueOnce(new Error('测试失败')).mockResolvedValue({ codex_turn_tickets: [] })
    const wrapper = panel({ accountId: 7 })
    await flushPromises()
    expect(wrapper.text()).toContain('正在采集')
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.get('[role="alert"]').text()).toContain('状态刷新失败')
    expect(wrapper.text()).toContain('正在采集')
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    await vi.advanceTimersByTimeAsync(5000)
    expect(getById).toHaveBeenCalledTimes(2)
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    await wrapper.setProps({ active: false })
    await vi.advanceTimersByTimeAsync(10000)
    expect(getById).toHaveBeenCalledTimes(2)
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(3)
    expect(wrapper.text()).toContain('暂无门票状态')
  })

  it('切换账号取消旧读取，迟到结果不会污染新账号', async () => {
    let finish!: (value: unknown) => void
    getById.mockImplementationOnce(() => new Promise(resolve => { finish = resolve })).mockResolvedValueOnce({ codex_turn_tickets: [{ ...ticket, model: '新账号模型' }] })
    const wrapper = panel({ accountId: 7 })
    const oldSignal = getById.mock.calls[0]?.[1] as AbortSignal
    await wrapper.setProps({ accountId: 8 })
    expect(oldSignal.aborted).toBe(true)
    await flushPromises()
    finish({ codex_turn_tickets: [{ ...ticket, model: '旧账号模型' }] })
    await flushPromises()
    expect(wrapper.text()).toContain('新账号模型')
    expect(wrapper.text()).not.toContain('旧账号模型')
  })
})
