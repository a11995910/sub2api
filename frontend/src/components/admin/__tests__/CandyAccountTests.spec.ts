import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CandyAccountTests from '../CandyAccountTests.vue'
import Pagination from '@/components/common/Pagination.vue'
import { DEFAULT_CANDY_PROMPT, type CandyTestResult, type CandyTestSettings, type CandyTestSettingsInput } from '@/api/admin/candyTests'

const { api, getGroups } = vi.hoisted(() => ({
  api: { getSettings: vi.fn(), updateSettings: vi.fn(), getAccounts: vi.fn(), run: vi.fn(), stop: vi.fn(), listResults: vi.fn(), getResult: vi.fn() },
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
  schedule_enabled: false, interval_hours: 24, auto_group_enabled: false,
  auto_group_excluded_account_ids: [],
  normal_add_group_ids: [], normal_remove_group_ids: [], degraded_add_group_ids: [], degraded_remove_group_ids: [],
})
const settings = (overrides: Partial<CandyTestSettings> = {}): CandyTestSettings => ({ ...input(), running_run_id: '', next_run_at: null, last_run_at: null, running_until: null, ...overrides })
const result = (overrides: Partial<CandyTestResult> = {}): CandyTestResult => ({
  id: 1, account_id: 18, account_name: 'GPT 账号', run_id: '批次一', status: 'normal', answer: '21', error_message: '',
  model_id: 'gpt-5.4', reasoning_effort: 'high', started_at: '2026-10-20T01:00:00Z', group_action: null, group_error: '', ...overrides,
})
const mountView = () => mount(CandyAccountTests, { global: { stubs: {
  Icon: true,
  Select: { props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'], template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', options.find(option => String(option.value) === $event.target.value)?.value ?? null)"><option value=""></option><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>' },
  GroupSelector: { props: ['modelValue', 'groups'], emits: ['update:modelValue'], template: '<div><input v-for="group in groups" :key="group.id" type="checkbox" :value="group.id" :checked="modelValue.includes(group.id)" @change="$emit(\'update:modelValue\', $event.target.checked ? [...modelValue, group.id] : modelValue.filter(id => id !== group.id))" /></div>' },
  BaseDialog: { props: ['show'], template: '<div v-if="show" class="detail-dialog"><slot /><slot name="footer" /></div>' },
  ConfirmDialog: { props: ['show'], emits: ['confirm'], template: '<div v-if="show"><button data-testid="stop-confirm" @click="$emit(\'confirm\')" /></div>' },
} } })

let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  localStorage.clear()
  for (const fn of Object.values(api)) fn.mockReset()
  api.getSettings.mockResolvedValue(settings())
  api.getAccounts.mockResolvedValue([{ id: 18, name: '保留正价账号' }, { id: 19, name: '普通账号' }])
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
  it('单账号测试只发送选中 ID，沿用保存配置且不修改定时范围', async () => {
    api.getSettings.mockResolvedValue(settings({ schedule_enabled: true }))
    api.run.mockResolvedValue({ run_id: '单账号批次', settings: input(), results: [result({ status: 'running' })] })
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-scope-single"]').setValue()
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="candy-single-account"]').setValue('18')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="candy-run"]').text()).toContain('candyTests.runSingle')
    await wrapper.get('[data-testid="candy-run"]').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenCalledTimes(1)
    expect(api.run).toHaveBeenCalledWith({ all: false, account_ids: [18] })
    expect(api.updateSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('candyTests.runStarted')
    await wrapper.get('[data-testid="candy-scope-all"]').setValue()
    await wrapper.get('[data-testid="candy-run"]').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenLastCalledWith({ all: true })
  })

  it('单账号测试保护未保存设置，已删除选择和列表失败不能误启动全量', async () => {
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-scope-single"]').setValue()
    await wrapper.get('[data-testid="candy-single-account"]').setValue('18')
    await wrapper.get('[data-testid="candy-prompt"]').setValue('未保存的题目')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="candy-run"]').trigger('click')
    expect(api.run).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="candy-prompt"]').setValue(DEFAULT_CANDY_PROMPT)
    api.getAccounts.mockResolvedValueOnce([{ id: 19, name: '其他账号' }])
    await wrapper.get('[data-testid="candy-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="candy-single-account"]').setValue('19')
    api.getAccounts.mockRejectedValueOnce(new Error('加载失败'))
    await wrapper.get('[data-testid="candy-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('candyTests.accountsLoadFailed')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    expect(api.run).not.toHaveBeenCalled()
  })

  it('单账号遇到并发批次显示冲突，不退回全部测试', async () => {
    api.run.mockRejectedValue(new Error('已有糖果测试正在运行'))
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-scope-single"]').setValue()
    await wrapper.get('[data-testid="candy-single-account"]').setValue('19')
    await wrapper.get('[data-testid="candy-run"]').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenCalledTimes(1)
    expect(api.run).toHaveBeenCalledWith({ all: false, account_ids: [19] })
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })

  it.each([undefined, null])('兼容旧配置排除名单 %s，默认折叠且全部参与', async excluded => {
    api.getSettings.mockResolvedValue({ ...settings({ auto_group_enabled: true }), auto_group_excluded_account_ids: excluded })
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="candy-accounts-toggle"]').attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('[data-testid="candy-accounts-panel"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="candy-accounts-summary"]').text()).toContain('{"participating":2,"excluded":0}')
    expect(wrapper.get('[data-testid="candy-save-state"]').text()).toBe('candyTests.savedState')
    await wrapper.get('[data-testid="candy-accounts-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="candy-accounts-toggle"]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-account-18"]').element.checked).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-account-19"]').element.checked).toBe(true)
  })

  it('取消参与后保存并重载保持排除，新增账号默认参与，测试仍运行全部账号', async () => {
    api.getSettings.mockResolvedValue(settings({ auto_group_enabled: true }))
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-accounts-toggle"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="candy-account-18"]').setValue(false)
    expect(wrapper.get('[data-testid="candy-accounts-summary"]').text()).toContain('{"participating":1,"excluded":1}')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ auto_group_excluded_account_ids: [18] }))
    const saved = settings({ auto_group_enabled: true, auto_group_excluded_account_ids: [18] })
    api.getSettings.mockResolvedValue(saved)
    api.getAccounts.mockResolvedValue([{ id: 18, name: '保留正价账号' }, { id: 19, name: '普通账号' }, { id: 20, name: '自动新增账号' }])
    wrapper.unmount()
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="candy-accounts-panel"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="candy-accounts-summary"]').text()).toContain('{"participating":2,"excluded":1}')
    await wrapper.get('[data-testid="candy-accounts-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-account-18"]').element.checked).toBe(false)
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-account-20"]').element.checked).toBe(true)
    api.run.mockResolvedValue({ run_id: '排除调组批次', settings: saved, results: [result({ status: 'running' })] })
    await wrapper.get('[data-testid="candy-run"]').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenCalledWith({ all: true })
  })

  it('搜索、刷新和账号加载失败均不清除排除项，关闭全局调组也保留名单', async () => {
    api.getSettings.mockResolvedValue(settings({ auto_group_enabled: true, auto_group_excluded_account_ids: [99] }))
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-accounts-toggle"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="candy-account-18"]').setValue(false)
    await wrapper.get('[data-testid="candy-accounts-search"]').setValue('普通')
    expect(wrapper.find('[data-testid="candy-account-18"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="candy-account-19"]').exists()).toBe(true)
    await wrapper.get('[data-testid="candy-accounts-search"]').setValue('#18')
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-account-18"]').element.checked).toBe(false)
    api.getAccounts.mockResolvedValueOnce([{ id: 19, name: '普通账号' }])
    await wrapper.get('[data-testid="candy-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('candyTests.accountsNoMatch')
    api.getAccounts.mockRejectedValueOnce(new Error('加载失败'))
    await wrapper.get('[data-testid="candy-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('candyTests.accountsLoadFailed')
    await wrapper.get('[data-testid="candy-auto-group-enabled"]').trigger('click')
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ auto_group_enabled: false, auto_group_excluded_account_ids: [18, 99] }))
  })

  it('轮询不覆盖未保存的参与选择，重新勾选只移除对应排除项', async () => {
    vi.useFakeTimers()
    api.getSettings.mockResolvedValue(settings({ auto_group_enabled: true, auto_group_excluded_account_ids: [18, 99] }))
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-accounts-toggle"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="candy-account-18"]').setValue(true)
    await wrapper.get('[data-testid="candy-account-19"]').setValue(false)
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-account-18"]').element.checked).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-account-19"]').element.checked).toBe(false)
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ auto_group_excluded_account_ids: [19, 99] }))
  })

  it('排除调组的结果仍显示测智结论，并在历史和详情显示跳过原因', async () => {
    const excluded = result({ status: 'degraded', answer: '30', group_action: { added_group_ids: [], removed_group_ids: [], skipped_reason: 'account_excluded' } })
    api.listResults.mockResolvedValue({ items: [excluded], total: 1 })
    api.getResult.mockResolvedValue(excluded)
    wrapper = mountView()
    await flushPromises()
    const row = wrapper.get('[data-testid="candy-result-1"]')
    expect(row.text()).toContain('candyTests.statuses.degraded')
    expect(row.text()).toContain('candyTests.groupExcluded')
    expect(row.text()).not.toContain('candyTests.groupUnchanged')
    await wrapper.get('[data-testid="candy-detail-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.detail-dialog').text()).toContain('candyTests.groupExcluded')
  })

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
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-interval-hours"]').element.value).toBe('24')
    await wrapper.get('[data-testid="candy-interval-hours"]').setValue('6')
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledWith({ ...input(), prompt: '自定义题目', expected_answer: '42', schedule_enabled: true, interval_hours: 6 })
    expect(api.updateSettings.mock.calls[0][0]).not.toHaveProperty('running_run_id')
    expect(api.updateSettings.mock.calls[0][0]).not.toHaveProperty('daily_time')
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="candy-reset"]').trigger('click')
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="candy-prompt"]').element.value).toBe(DEFAULT_CANDY_PROMPT)
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-answer"]').element.value).toBe('21')
  })

  it.each(['', '0', '-1', '1.5', '169'])('拒绝无效的小时测试间隔：%s', async value => {
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-schedule-enabled"]').trigger('click')
    await wrapper.get('[data-testid="candy-interval-hours"]').setValue(value)
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    expect(api.updateSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('candyTests.invalidInterval')
  })

  it.each([1, 168])('保存小时间隔边界 %s 并按北京时间显示下次运行', async hours => {
    api.getSettings.mockResolvedValue(settings({ interval_hours: hours }))
    api.updateSettings.mockImplementation(async (value: CandyTestSettingsInput) => settings({ ...value, next_run_at: '2026-10-20T01:00:00Z' }))
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="candy-schedule-enabled"]').trigger('click')
    expect(wrapper.get<HTMLInputElement>('[data-testid="candy-interval-hours"]').element.value).toBe(String(hours))
    expect(wrapper.text()).toContain(`candyTests.intervalHint {"hours":${hours}}`)
    await wrapper.get('[data-testid="candy-settings"]').trigger('submit')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ interval_hours: hours, schedule_enabled: true }))
    expect(wrapper.text()).toContain('candyTests.nextRun {"time":"2026/10/20 09:00"}')
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

it('跳过账号显示独立状态和原因，不显示智力下降或测试错误', async () => {
  const skipped = result({ status: 'skipped', answer: '', error_message: '账号处于 429 冷却' })
  api.listResults.mockResolvedValue({ items: [skipped], total: 1 })
  api.getResult.mockResolvedValue(skipped)
  wrapper = mountView()
  await flushPromises()
  const row = wrapper.get('[data-testid="candy-result-1"]')
  expect(row.text()).toContain('candyTests.statuses.skipped')
  expect(row.text()).toContain('429')
  expect(row.text()).not.toContain('candyTests.statuses.degraded')
  await wrapper.get('[data-testid="candy-detail-1"]').trigger('click')
  await flushPromises()
  expect(wrapper.get('.detail-dialog').text()).toContain('candyTests.skipReason')
  expect(wrapper.get('.detail-dialog').text()).not.toContain('candyTests.testError')
})
