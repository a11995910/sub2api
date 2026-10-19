import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import PelicanAccountTests from '../PelicanAccountTests.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), checkMixedChannelRisk: vi.fn(), groups: vi.fn(), test: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: api, default: api }))
vi.mock('@/api/admin/groups', () => ({ groupsAPI: { getAllIncludingInactive: api.groups }, default: { getAllIncludingInactive: api.groups } }))
vi.mock('@/api/admin/pelicanAccounts', () => ({ testPelicanAccount: api.test }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const account = (id: number) => ({ id, name: `账号 ${id}`, platform: 'openai', type: 'oauth', group_ids: [1] })
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
  api.test.mockResolvedValue({ text: '<svg></svg>', model: '测试模型' })
  api.checkMixedChannelRisk.mockResolvedValue({ has_risk: false })
  api.update.mockResolvedValue({ ...account(1), group_ids: [2] })
})
afterEach(() => wrapper?.unmount())
describe('OAuth 鹈鹕测试', () => {
  it('遍历全部分页，测试范围不受预览分页限制', async () => {
    api.list.mockResolvedValueOnce({ items: Array.from({ length: 25 }, (_, i) => account(i + 1)), total: 26 })
      .mockResolvedValueOnce({ items: [account(26)], total: 26 })
    wrapper = create(); await flushPromises()
    expect(api.list).toHaveBeenCalledTimes(2)
    expect(api.list.mock.calls[0][2]).toMatchObject({ type: 'oauth', lite: '1' })
    expect(wrapper.findAll('article')).toHaveLength(25)
    await wrapper.get('[data-testid="oauth-run-all"]').trigger('click'); await flushPromises()
    expect(api.test).toHaveBeenCalledTimes(26)
    expect(wrapper.findAll('[data-testid="artwork"]')).toHaveLength(25)
  })
  it('限制并发并在停止后取消正在进行和等待中的测试', async () => {
    api.list.mockResolvedValue({ items: Array.from({ length: 6 }, (_, i) => account(i + 1)), total: 6 })
    api.test.mockImplementation((_id, _model, _effort, signal: AbortSignal) => new Promise((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(new DOMException('已取消', 'AbortError')))
    }))
    wrapper = create(); await flushPromises()
    await wrapper.get('[data-testid="oauth-run-all"]').trigger('click'); await flushPromises()
    expect(api.test).toHaveBeenCalledTimes(3)
    await wrapper.get('[data-testid="oauth-stop"]').trigger('click'); await flushPromises()
    expect(api.test).toHaveBeenCalledTimes(3)
    expect(wrapper.findAll('article').every(card => card.text().includes('accounts.stopped'))).toBe(true)
  })
  it('单账号失败不影响其余账号，非法作品不显示成功预览', async () => {
    api.test.mockResolvedValueOnce({ text: '不是作品', model: '模型' })
    wrapper = create(); await flushPromises()
    await wrapper.get('[data-testid="oauth-run-all"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="oauth-card-1"]').text()).toContain('accounts.invalid')
    expect(wrapper.get('[data-testid="oauth-card-2"]').find('[data-testid="artwork"]').exists()).toBe(true)
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
    expect(api.test).not.toHaveBeenCalled()
  })
})
