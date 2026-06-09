import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AccountBulkActionsBar from '../AccountBulkActionsBar.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('AccountBulkActionsBar', () => {
  it('选中账号时只展示已选账号批量编辑入口', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [1, 2]
      }
    })

    expect(wrapper.text()).toContain('admin.accounts.bulkActions.edit')
    expect(wrapper.text()).not.toContain('admin.accounts.bulkEdit.updateFiltered')

    const editSelectedButton = wrapper.findAll('button').find((button) => {
      return button.text() === 'admin.accounts.bulkActions.edit'
    })
    expect(editSelectedButton).toBeTruthy()

    await editSelectedButton!.trigger('click')

    expect(wrapper.emitted('edit-selected')).toHaveLength(1)
    expect(wrapper.emitted('edit-filtered')).toBeUndefined()
  })

  it('未选中账号时展示筛选结果批量更新入口', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: []
      }
    })

    expect(wrapper.text()).toContain('admin.accounts.bulkEdit.updateFiltered')
    expect(wrapper.text()).not.toContain('admin.accounts.bulkActions.edit')

    const editFilteredButton = wrapper.findAll('button').find((button) => {
      return button.text() === 'admin.accounts.bulkEdit.updateFiltered'
    })
    expect(editFilteredButton).toBeTruthy()

    await editFilteredButton!.trigger('click')

    expect(wrapper.emitted('edit-filtered')).toHaveLength(1)
    expect(wrapper.emitted('edit-selected')).toBeUndefined()
  })
})
