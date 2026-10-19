import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import PelicanAccountTests from '../PelicanAccountTests.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), checkMixedChannelRisk: vi.fn(), groups: vi.fn(), start: vi.fn(), results: vi.fn(), get: vi.fn(), stop: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: api, default: api }))
vi.mock('@/api/admin/groups', () => ({ groupsAPI: { getAllIncludingInactive: api.groups }, default: { getAllIncludingInactive: api.groups } }))
vi.mock('@/api/admin/pelicanAccounts', () => ({ pelicanAccountsAPI: { start: api.start, list: api.results, get: api.get, stop: api.stop } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const account = (id: number) => ({ id, name: `账号 ${id}`, platform: 'openai', type: 'oauth', group_ids: [1] })
const result = (id: number, run = '第一轮', status = 'success') => ({ account_id: id, run_id: run, status, model_id: '测试模型', error_message: '', started_at: '', finished_at: null, running_until: '', reasoning_effort: '' })
const create = () => mount(PelicanAccountTests, { global: { stubs: {
  PelicanArtworkPreview: { props: ['html'], template: '<div data-testid="artwork">{{ html }}</div>' },
  Pagination: true,
  GroupSelector: { props: ['modelValue'], emits: ['update:modelValue'], template: '<button data-testid="select-groups" @click="$emit(\'update:modelValue\', [2])">分组</button>' },
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  ConfirmDialog: { props: ['show'], emits: ['confirm'], template: '<button v-if="show" data-testid="confirm-risk" @click="$emit(\'confirm\')">确认</button>' },
} } })
let wrapper: ReturnType<typeof create>
beforeEach(() => {
  vi.resetAllMocks()
  api.list.mockResolvedValue({ items: [account(1), account(2)], total: 2 })
  api.groups.mockResolvedValue([{ id: 1, name: '分组一' }, { id: 2, name: '分组二' }])
  api.results.mockResolvedValue([])
  api.start.mockResolvedValue([])
  api.get.mockImplementation(async id => ({ ...result(id), response_text: '<svg>上次作品</svg>' }))
  api.stop.mockResolvedValue(undefined)
  api.checkMixedChannelRisk.mockResolvedValue({ has_risk: false })
  api.update.mockResolvedValue({ ...account(1), group_ids: [2] })
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })
describe('OAuth 鹈鹕测试', () => {
  it('全部账号只提交一个后台批次，不受展示分页和浏览器连接数量限制', async () => {
    api.list.mockResolvedValueOnce({ items: Array.from({ length: 25 }, (_, i) => account(i + 1)), total: 26 })
      .mockResolvedValueOnce({ items: [account(26)], total: 26 })
    wrapper = create(); await flushPromises()
    expect(wrapper.findAll('article')).toHaveLength(25)
    await wrapper.get('[data-testid="oauth-run-all"]').trigger('click'); await flushPromises()
    expect(api.start).toHaveBeenCalledTimes(1)
    expect(api.start).toHaveBeenCalledWith({ all: true, models: {}, reasoning_effort: '' })
  })
  it('重新打开页面恢复服务端作品，不重新发起测试', async () => {
    api.results.mockResolvedValue([result(1)])
    wrapper = create(); await flushPromises()
    expect(wrapper.get('[data-testid="artwork"]').text()).toContain('上次作品')
    wrapper.unmount()
    wrapper = create(); await flushPromises()
    expect(wrapper.get('[data-testid="artwork"]').text()).toContain('上次作品')
    expect(api.start).not.toHaveBeenCalled()
  })
  it('刷新后仍显示运行中，离开页面不会停止服务端测试', async () => {
    api.results.mockResolvedValue([result(1, '第一轮', 'running')])
    wrapper = create(); await flushPromises()
    expect(wrapper.find('[data-testid="oauth-stop"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="oauth-run-all"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
    expect(api.stop).not.toHaveBeenCalled()
  })
  it('轮询新一轮结果替换旧作品', async () => {
    vi.useFakeTimers()
    api.results.mockResolvedValue([result(1)])
    wrapper = create(); await flushPromises()
    api.results.mockResolvedValue([result(1, '第二轮')])
    api.get.mockResolvedValue({ ...result(1, '第二轮'), response_text: '<svg>新作品</svg>' })
    await vi.advanceTimersByTimeAsync(3000); await flushPromises()
    expect(wrapper.get('[data-testid="artwork"]').text()).toContain('新作品')
    expect(wrapper.text()).not.toContain('上次作品')
  })
  it('过期的作品响应不会覆盖重测结果', async () => {
    vi.useFakeTimers()
    api.results.mockResolvedValue([result(1)])
    let resolveOld!: (value: unknown) => void
    api.get.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    wrapper = create(); await flushPromises()
    api.results.mockResolvedValue([result(1, '第二轮')])
    api.get.mockResolvedValue({ ...result(1, '第二轮'), response_text: '<svg>新作品</svg>' })
    // 手动刷新可在旧正文请求未完成时读取最新一轮。
    await (wrapper.vm as unknown as { refreshResults(): Promise<void> }).refreshResults()
    resolveOld({ ...result(1), response_text: '<svg>旧作品</svg>' })
    await flushPromises()
    expect(wrapper.get('[data-testid="artwork"]').text()).toContain('新作品')
  })
  it('停止请求绑定运行标识，避免误停随后启动的新测试', async () => {
    api.results.mockResolvedValue([result(1, '第一轮', 'running')])
    wrapper = create(); await flushPromises()
    api.results.mockResolvedValue([result(1, '第一轮', 'stopped')])
    await wrapper.get('[data-testid="oauth-stop"]').trigger('click'); await flushPromises()
    expect(api.stop).toHaveBeenCalledWith([{ account_id: 1, run_id: '第一轮' }])
    expect(wrapper.text()).toContain('accounts.stopped')
  })
  it('修改分组仅提交分组字段，并在风险确认后保存', async () => {
    api.checkMixedChannelRisk.mockResolvedValue({ has_risk: true, message: '混合风险' })
    wrapper = create(); await flushPromises()
    await wrapper.get('[data-testid="oauth-groups-1"]').trigger('click')
    await wrapper.get('[data-testid="select-groups"]').trigger('click')
    await wrapper.get('[data-testid="oauth-save-groups"]').trigger('click'); await flushPromises()
    expect(api.update).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-risk"]').trigger('click'); await flushPromises()
    expect(api.update).toHaveBeenCalledWith(1, { group_ids: [2], confirm_mixed_channel_risk: true })
    expect(wrapper.get('[data-testid="oauth-card-1"]').text()).toContain('分组二')
  })
  it('列表加载不完整时禁止启动批量测试', async () => {
    api.list.mockResolvedValueOnce({ items: [account(1)], total: 2 }).mockRejectedValueOnce(new Error('加载失败'))
    wrapper = create(); await flushPromises()
    expect(wrapper.get('[data-testid="oauth-run-all"]').attributes('disabled')).toBeDefined()
    expect(api.start).not.toHaveBeenCalled()
  })
})
