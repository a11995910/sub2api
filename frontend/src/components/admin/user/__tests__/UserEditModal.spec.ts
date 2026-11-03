import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UserEditModal from '../UserEditModal.vue'

const { update, updateUserAttributeValues, showSuccess, showError } = vi.hoisted(() => ({
  update: vi.fn(),
  updateUserAttributeValues: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { update },
    userAttributes: { updateUserAttributeValues }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

// useStepUp pulls in the API client, which needs the real i18n instance.
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const mountModal = (concurrency: number) => mount(UserEditModal, {
  props: {
    show: true,
    user: { id: 7, email: 'user@example.test', username: 'user', notes: '', role: 'user', concurrency, rpm_limit: 0 } as never
  },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show', 'title'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      },
      Select: true,
      Icon: true,
      UserAttributeForm: true,
      UserPreferredAccountSelect: true,
      TotpStepUpDialog: true
    }
  }
})

describe('UserEditModal concurrency', () => {
  beforeEach(() => {
    update.mockReset()
    updateUserAttributeValues.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    update.mockResolvedValue({})
  })

  // Regression coverage for issue #5977: the gateway treats concurrency <= 0 as
  // unlimited (AcquireUserSlot) and both the batch limits endpoint and the bulk
  // edit modal accept 0, so this dialog must not be the only place that rejects
  // it — doing so blocked every other edit on such a user.
  it('saves an unlimited (0) concurrency instead of blocking the whole form', async () => {
    const wrapper = mountModal(0)

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({ concurrency: 0 }))
    expect(wrapper.emitted('success')).toBeTruthy()
  })

  it('still rejects a negative concurrency', async () => {
    const wrapper = mountModal(3)

    await wrapper.get('[data-test="concurrency-input"]').setValue('-1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.users.concurrencyNonNegative')
    expect(update).not.toHaveBeenCalled()
  })
})

describe('用户优先账号保存', () => {
  beforeEach(() => { update.mockReset().mockResolvedValue({}); showError.mockReset() })
  it('修改后提交账号 ID', async () => {
    const wrapper = mountModal(3)
    wrapper.findComponent({ name: 'UserPreferredAccountSelect' }).vm.$emit('update:modelValue', 2)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({ preferred_account_id: 2 }))
  })
  it('未修改时省略该字段，清除时提交 0', async () => {
    const wrapper = mountModal(3)
    await wrapper.setProps({ user: { id: 7, email: 'user@example.test', role: 'user', concurrency: 3, preferred_account_id: 99 } as never })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update.mock.calls[0][1]).not.toHaveProperty('preferred_account_id')
    wrapper.findComponent({ name: 'UserPreferredAccountSelect' }).vm.$emit('update:modelValue', 0)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenLastCalledWith(7, expect.objectContaining({ preferred_account_id: 0 }))
  })
  it('取消后重新打开恢复已保存设置', async () => {
    const wrapper = mountModal(3)
    wrapper.findComponent({ name: 'UserPreferredAccountSelect' }).vm.$emit('update:modelValue', 2)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update.mock.calls[0][1]).not.toHaveProperty('preferred_account_id')
  })
})
