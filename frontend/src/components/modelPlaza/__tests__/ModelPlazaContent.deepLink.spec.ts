import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaFilterBar from '../PlazaFilterBar.vue'
import PlazaGroupSection from '../PlazaGroupSection.vue'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: true }) }))

function group(id: number, names: string[]): ModelPlazaGroup {
  return {
    id,
    name: `分组${id}`,
    platform: 'openai',
    rate_multiplier: 1,
    models: names.map((name) => ({ name }))
  } as ModelPlazaGroup
}
describe('模型广场榜单深链', () => {
  it('精确定位型号和分组，编辑搜索后恢复普通包含搜索', async () => {
    const wrapper = mount(ModelPlazaContent, {
      props: {
        loading: false,
        initialModel: 'gpt-6.1-sol',
        initialGroup: 2,
        response: {
          description: '',
          groups: [
            group(1, ['gpt-6.1-sol']),
            group(2, ['openai/gpt-6.1-sol', 'gpt-6.1-sol-high', 'gpt-6-sol'])
          ]
        }
      },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'zh',
            missingWarn: false,
            fallbackWarn: false,
            messages: { zh: {} }
          })
        ],
        stubs: { PlazaFilterBar: true, PlazaGroupSection: true, Icon: true }
      }
    })
    let sections = wrapper.findAllComponents(PlazaGroupSection)
    expect(sections).toHaveLength(1)
    expect(sections[0]!.props('group').id).toBe(2)
    expect(sections[0]!.props('group').models.map((model: { name: string }) => model.name)).toEqual(
      ['openai/gpt-6.1-sol']
    )
    wrapper.findComponent(PlazaFilterBar).vm.$emit('update:search', 'gpt-6')
    await wrapper.vm.$nextTick()
    sections = wrapper.findAllComponents(PlazaGroupSection)
    expect(sections[0]!.props('group').models).toHaveLength(3)
    await wrapper.setProps({ initialGroup: undefined })
    expect(wrapper.findAllComponents(PlazaGroupSection)).toHaveLength(2)
    wrapper.unmount()
  })
})
