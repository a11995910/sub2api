import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CandyAccountTests from '../CandyAccountTests.vue'
import Pagination from '@/components/common/Pagination.vue'
import { DEFAULT_CANDY_PROMPT, type CandyTestResult, type CandyTestSettings, type CandyTestSettingsInput } from '@/api/admin/candyTests'

const { api, getGroups } = vi.hoisted(() => ({
  api: { getSettings: vi.fn(), updateSettings: vi.fn(), run: vi.fn(), stop: vi.fn(), listResults: vi.fn(), getResult: vi.fn() },
  getGroups: vi.fn(),
}))
vi.mock('@/api/admin/candyTests', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/admin/candyTests')>(), candyTestsAPI: api,
}))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: getGroups } } }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, named?: Record<string, unknown>) => named ? `${key} ${JSON.stringify(named)}` : key }),
}))

const input = (): CandyTestSettingsInput => ({
  prompt: DEFAULT_CANDY_PROMPT, expected_answer: '21', model_id: '', reasoning_effort: 'high',
  schedule_enabled: false, daily_time: '08:00', auto_group_enabled: false,
  normal_add_group_ids: [], normal_remove_group_ids: [], degraded_add_group_ids: [], degraded_remove_group_ids: [],
})
const settings = (overrides: Partial<CandyTestSettings> = {}): CandyTestSettings => ({ ...input(), running_run_id: '', next_run_at: null, last_run_at: null, running_until: null, ...overrides })
const result = (overrides: Partial<CandyTestResult> = {}): CandyTestResult => ({
  id: 1, account_id: 18, account_name: 'GPT 账号', run_id: '批次一', status: 'normal', answer: '21', error_message: '',
  model_id: 'gpt-5.4', reasoning_effort: 'high', started_at: '2026-10-20T01:00:00Z', group_action: null, group_error: '', ...overrides,
})
const mountView = () => mount(CandyAccountTests, { global: { stubs: {
  Icon: true,
  Select: { props: ['modelValue', 'options'], template: '<select :value="modelValue"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>' },
  GroupSelector: { props: ['modelValue', 'groups'], emits: ['update:modelValue'], template: '<div><input v-for="group in groups" :key="group.id" type="checkbox" :value="group.id" :checked="modelValue.includes(group.id)" @change="$emit(\'update:modelValue\', $event.target.checked ? [...modelValue, group.id] : modelValue.filter(id => id !== group.id))" /></div>' },
  BaseDialog: { props: ['show'], template: '<div v-if="show" class="detail-dialog"><slot /><slot name="footer" /></div>' },
  ConfirmDialog: { props: ['show'], emits: ['confirm'], template: '<div v-if="show"><button data-testid="stop-confirm" @click="$emit(\'confirm\')" /></div>' },
} } })

let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  localStorage.clear()
  for (const fn of Object.values(api)) fn.mockReset()
  api.getSettings.mockResolvedValue(settings())
  api.listResults.mockResolvedValue({ items: [], total: 0 })
  api.updateSettings.mockImplementation(async (value: CandyTestSettingsInput) => settings(value))
  getGroups.mockReset().mockResolvedValue([
    { id: 1, name: '正常池', platform: 'openai', status: 'active' },
    { id: 2, name: '下降池', platform: 'openai', status: 'active' },
    { id: 3, name: 'Claude', platform: 'anthropic', status: 'active' },
    { id: 4, name: '停用池', platform: 'openai', status: 'inactive' },
  ])
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })

describe('糖果账号测试', () => {
  it('默认关闭自动化，编辑后必须保存，提交排除运行字段并恢复默认题目答案', async () => {
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="candy-schedule-enabled"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[data-testid="candy-auto-group-enabled"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="candy-prompt"]').element.value).toBe(DEFAULT_CANDY_PROMPT)
    await wrapper.get('[data-testid="candy-prompt"]').setValue('自定义题目')
    await wrapper.get('[data-testid="candy-answer"]').setValue('42')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="candy-run"]').trigger('click')
    expect(api.run).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="candy-schedule-enabled"]').trigger('click')
    await wrapper.get('[data-testid="candy-daily-time"]').setValue('23:35')
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledWith({ ...input(), prompt: '自定义题目', expected_answer: '42', schedule_enabled: true, daily_time: '23:35' })
    expect(api.updateSettings.mock.calls[0][0]).not.toHaveProperty('running_run_id')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="candy-reset"]').trigger('click')
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="candy-prompt"]').element.value).toBe(DEFAULT_CANDY_PROMPT)
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-answer"]').element.value).toBe('21')
  })

  it('只提供可用GPT分组并阻止同一结果同时增减同一分组', async () => {
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-auto-group-enabled"]').trigger('click')
    const add = wrapper.get('[data-testid="candy-normal-add"]')
    expect(add.findAll('input')).toHaveLength(2)
    await add.get('input[value="1"]').setValue(true)
    await wrapper.get('[data-testid="candy-normal-remove"] input[value="1"]').setValue(true)
    expect(wrapper.text()).toContain('candyTests.conflict')
    expect(wrapper.get('[data-testid="candy-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    expect(api.updateSettings).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="candy-normal-remove"] input[value="1"]').setValue(false)
    await wrapper.get('[data-testid="candy-normal-remove"] input[value="2"]').setValue(true)
    await wrapper.get('[data-testid="candy-degraded-add"] input[value="2"]').setValue(true)
    await wrapper.get('[data-testid="candy-degraded-remove"] input[value="1"]').setValue(true)
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ auto_group_enabled: true, normal_add_group_ids: [1], normal_remove_group_ids: [2], degraded_add_group_ids: [2], degraded_remove_group_ids: [1] }))
  })

  it('轮询刷新运行状态但保留未保存题目，不把服务端元数据当配置修改', async () => {
    vi.useFakeTimers()
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-prompt"]').setValue('正在编辑的题目')
    api.getSettings.mockResolvedValue(settings({ running_run_id: '定时批次', last_run_at: '2026-10-20T01:00:00Z' }))
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="candy-prompt"]').element.value).toBe('正在编辑的题目')
    expect(wrapper.find('[data-testid="candy-stop"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="candy-save-state"]').text()).toBe('candyTests.unsaved')
    expect(wrapper.text()).toContain('2026/10/20 09:00')
  })

  it('使用真实批次响应契约启动全量测试并用当前运行ID停止', async () => {
    wrapper = mountView()
    await flushPromises()
    api.run.mockResolvedValue({ run_id: '手动批次', settings: input(), results: [result({ status: 'running' })] })
    api.getSettings.mockResolvedValue(settings({ running_run_id: '手动批次' }))
    await wrapper.get('[data-testid="candy-run"]').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenCalledWith({ all: true })
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="candy-save-state"]').text()).toBe('candyTests.savedState')
    await wrapper.get('[data-testid="candy-stop"]').trigger('click')
    await wrapper.get('[data-testid="stop-confirm"]').trigger('click')
    await flushPromises()
    expect(api.stop).toHaveBeenCalledWith('手动批次')
  })

  it('保存成功后忽略较早发起的设置读取，避免旧配置覆盖新题目', async () => {
    vi.useFakeTimers()
    wrapper = mountView()
    await flushPromises()
    let resolveOld: (value: CandyTestSettings) => void = () => {}
    api.getSettings.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    await vi.advanceTimersByTimeAsync(5000)
    await wrapper.get('[data-testid="candy-prompt"]').setValue('保存的新题目')
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    resolveOld(settings())
    await flushPromises()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="candy-prompt"]').element.value).toBe('保存的新题目')
    expect(wrapper.get('[data-testid="candy-save-state"]').text()).toBe('candyTests.savedState')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeUndefined()
  })

  it('独立显示失败、下降和分组执行失败，历史详情安全显示模型完整回复', async () => {
    api.listResults.mockResolvedValue({ items: [
      result({ id: 1, status: 'error', answer: '', error_message: '无法识别答案' }),
      result({ id: 2, status: 'degraded', answer: '30', group_error: '分组已停用' }),
      result({ id: 3, group_action: { added_group_ids: [1], removed_group_ids: [2] } }),
    ], total: 3 })
    api.getResult.mockResolvedValue(result({ id: 2, status: 'degraded', answer: '30', response_text: '<script>危险回复</script>\n答案：30', settings_snapshot: input(), group_error: '分组已停用' }))
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="candy-result-1"]').text()).toContain('candyTests.statuses.error')
    expect(wrapper.get('[data-testid="candy-result-1"]').text()).not.toContain('candyTests.statuses.degraded')
    expect(wrapper.get('[data-testid="candy-result-2"]').text()).toContain('分组已停用')
    expect(wrapper.get('[data-testid="candy-result-3"]').text()).toContain('正常池')
    await wrapper.get('[data-testid="candy-detail-2"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="candy-response"]').text()).toContain('<script>危险回复</script>')
    expect(wrapper.find('.detail-dialog script').exists()).toBe(false)
    expect(wrapper.get('.detail-dialog').text()).toContain(DEFAULT_CANDY_PROMPT)
  })

  it('分页时忽略旧请求，卸载时取消请求', async () => {
    api.listResults.mockResolvedValue({ items: [result()], total: 80 })
    wrapper = mountView()
    await flushPromises()
    let resolveOld: (value: { items: CandyTestResult[]; total: number }) => void = () => {}
    api.listResults.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    const oldSignal = api.listResults.mock.calls.at(-1)?.[2] as AbortSignal
    api.listResults.mockResolvedValueOnce({ items: [result({ id: 3 })], total: 80 })
    wrapper.getComponent(Pagination).vm.$emit('update:page', 3)
    await flushPromises()
    resolveOld({ items: [result({ id: 2 })], total: 80 })
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(wrapper.find('[data-testid="candy-result-3"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="candy-result-2"]').exists()).toBe(false)
    const signal = api.listResults.mock.calls.at(-1)?.[2] as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
})
