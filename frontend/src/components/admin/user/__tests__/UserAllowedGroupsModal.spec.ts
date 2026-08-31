import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { AdminUser, Group } from '@/types'
import UserAllowedGroupsModal from '../UserAllowedGroupsModal.vue'

const { getAllGroups, updateUser, showSuccess, showError } = vi.hoisted(() => ({
  getAllGroups: vi.fn(),
  updateUser: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: { getAllIncludingInactive: getAllGroups },
    users: { update: updateUser }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const createGroup = (
  id: number,
  options: Partial<Pick<Group, 'is_exclusive' | 'status' | 'subscription_type'>> = {}
): Group => ({
  id,
  name: `group-${id}`,
  platform: 'openai',
  rate_multiplier: 1,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  ...options
} as Group)

const createUser = (overrides: Partial<AdminUser> = {}): AdminUser => ({
  id: 42,
  username: 'blocked-user',
  email: 'blocked@example.com',
  role: 'user',
  balance: 10,
  concurrency: 2,
  status: 'active',
  allowed_groups: [4],
  blocked_groups: [2, 3, 4, 5, 999],
  balance_notify_enabled: false,
  balance_notify_threshold: null,
  balance_notify_extra_emails: [],
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T00:00:00Z',
  notes: '',
  ...overrides
})

const mountModal = (user = createUser()) => mount(UserAllowedGroupsModal, {
  props: {
    show: false,
    user
  },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      },
      PlatformIcon: { template: '<span />' }
    }
  }
})

describe('UserAllowedGroupsModal', () => {
  beforeEach(() => {
    getAllGroups.mockReset()
    updateUser.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    getAllGroups.mockResolvedValue([
      createGroup(1),
      createGroup(2),
      createGroup(3, { status: 'inactive' }),
      createGroup(4, { is_exclusive: true }),
      createGroup(5, { subscription_type: 'subscription' })
    ])
    updateUser.mockResolvedValue(createUser())
  })

  it('loads blacklist state and submits public group changes', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const availableCheckbox = wrapper.get('[data-test="public-group-checkbox-1"]')
    const blockedCheckbox = wrapper.get('[data-test="public-group-checkbox-2"]')
    expect((availableCheckbox.element as HTMLInputElement).checked).toBe(true)
    expect((blockedCheckbox.element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get('[data-test="public-group-2"]').text()).toContain('admin.users.blockedGroup')
    expect(wrapper.get('[data-test="public-group-2"] input[type="number"]').attributes('disabled')).toBeDefined()

    await availableCheckbox.trigger('change')
    await blockedCheckbox.trigger('change')
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(updateUser).toHaveBeenCalledWith(42, expect.objectContaining({
      allowed_groups: [4],
      blocked_groups: [3, 1]
    }))
    expect(wrapper.emitted('success')).toEqual([[]])
    expect(wrapper.emitted('close')).toEqual([[]])
  })

  it('combines public-group allow list and blacklist with blacklist priority', async () => {
    const wrapper = mountModal(createUser({
      allowed_groups: [1, 2, 4],
      blocked_groups: [2],
      restrict_public_groups: true
    }))
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.get('[data-test="public-group-checkbox-1"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="public-group-checkbox-2"]').element as HTMLInputElement).checked).toBe(false)

    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(updateUser).toHaveBeenCalledWith(42, expect.objectContaining({
      allowed_groups: [1, 4],
      blocked_groups: [2],
      restrict_public_groups: true
    }))
  })
})
