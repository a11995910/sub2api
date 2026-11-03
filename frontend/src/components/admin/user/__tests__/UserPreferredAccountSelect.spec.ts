import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UserPreferredAccountSelect from '../UserPreferredAccountSelect.vue'

const { list, getById } = vi.hoisted(() => ({ list: vi.fn(), getById: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { list, getById } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))
const mountSelector = (id = 0) => mount(UserPreferredAccountSelect, {
  props: { modelValue: id }, global: { stubs: { Select: { name: 'Select', props: ['options', 'modelValue', 'loading'], template: '<div />' } } }
})
const account = (id: number) => ({ id, name: `账号 ${id}`, platform: 'openai' })

describe('用户优先账号选择', () => {
  beforeEach(() => {
    list.mockReset().mockResolvedValue({ items: [account(2)] })
    getById.mockReset().mockImplementation(async id => account(id))
  })
  it('搜索、选择和清除账号', async () => {
    const wrapper = mountSelector()
    await flushPromises()
    const select = wrapper.findComponent({ name: 'Select' })
    expect(list).toHaveBeenCalledWith(1, 50, { search: '', lite: 'true' })
    expect(select.props('options')).toContainEqual(expect.objectContaining({ value: 2 }))
    select.vm.$emit('search', 'test')
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(1, 50, { search: 'test', lite: 'true' })
    select.vm.$emit('update:modelValue', 2)
    select.vm.$emit('update:modelValue', 0)
    expect(wrapper.emitted('update:modelValue')).toEqual([[2], [0]])
  })
  it('已选账号不在首屏时补全名称', async () => {
    const wrapper = mountSelector(99)
    await flushPromises()
    expect(wrapper.findComponent({ name: 'Select' }).props('options')).toContainEqual({ value: 99, label: '账号 99 · openai · #99' })
  })
  it('列表加载失败和账号删除不清除已有配置', async () => {
    list.mockRejectedValue(new Error('断网'))
    getById.mockRejectedValue(new Error('已删除'))
    const wrapper = mountSelector(99)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('loadError')
    expect(wrapper.findComponent({ name: 'Select' }).props('options')).toContainEqual(expect.objectContaining({ value: 99 }))
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it('忽略晚返回的旧搜索结果', async () => {
    let finishOld!: (value: unknown) => void
    list.mockReturnValueOnce(new Promise(resolve => { finishOld = resolve }))
    const wrapper = mountSelector()
    list.mockResolvedValueOnce({ items: [account(3)] })
    wrapper.findComponent({ name: 'Select' }).vm.$emit('search', 'new')
    await flushPromises()
    finishOld({ items: [account(2)] })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'Select' }).props('options')).toContainEqual(expect.objectContaining({ value: 3 }))
    expect(wrapper.findComponent({ name: 'Select' }).props('options')).not.toContainEqual(expect.objectContaining({ value: 2 }))
  })
})
