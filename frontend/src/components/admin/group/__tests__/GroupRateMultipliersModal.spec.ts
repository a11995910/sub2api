import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { AdminGroup } from '@/types'
import GroupRateMultipliersModal from '../GroupRateMultipliersModal.vue'

const {
  getGroupRateMultipliers,
  batchSetGroupRateMultipliers,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  getGroupRateMultipliers: vi.fn(),
  batchSetGroupRateMultipliers: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      getGroupRateMultipliers,
      batchSetGroupRateMultipliers
    },
    users: {
      list: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const group = {
  id: 73,
  name: 'pro稳定分组',
  platform: 'openai',
  rate_multiplier: 0.18,
  promo_discount_enabled: true,
  promo_discount_start: '2026-08-30 10:28',
  promo_discount_end: '2026-08-30 23:59',
  promo_discount_rate: 0.7,
  promo_active: true
} as AdminGroup

const entries = [
  {
    user_id: 101,
    user_name: 'user-one',
    user_email: 'one@example.com',
    user_notes: '',
    user_status: 'active',
    rate_multiplier: 0.15,
    rpm_override: null
  },
  {
    user_id: 202,
    user_name: 'user-two',
    user_email: 'two@example.com',
    user_notes: '',
    user_status: 'active',
    rate_multiplier: 0.16,
    rpm_override: null
  }
]

const mountModal = async (targetGroup: AdminGroup = group) => {
  const wrapper = mount(GroupRateMultipliersModal, {
    props: {
      show: false,
      group: targetGroup
    },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show', 'title'],
          emits: ['close'],
          template: '<section v-if="show"><h2>{{ title }}</h2><slot /></section>'
        },
        Pagination: true,
        Icon: true,
        PlatformIcon: true
      }
    }
  })

  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('GroupRateMultipliersModal', () => {
  beforeEach(() => {
    getGroupRateMultipliers.mockReset()
    batchSetGroupRateMultipliers.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    getGroupRateMultipliers.mockResolvedValue(entries.map((entry) => ({ ...entry })))
    batchSetGroupRateMultipliers.mockResolvedValue(undefined)
  })

  it('shows promotional effective rates based on each user base rate', async () => {
    const wrapper = await mountModal()

    expect(wrapper.get('[data-testid="group-base-rate"]').text()).toBe('0.18x')
    expect(wrapper.get('[data-testid="promo-group-rate"]').text()).toBe('0.126x')
    expect(wrapper.get('[data-testid="promo-effective-rate-101"]').text()).toBe('0.105x')
    expect(wrapper.get('[data-testid="promo-effective-rate-202"]').text()).toBe('0.112x')
    expect(wrapper.get('[data-testid="promo-summary"]').text()).toContain(
      'admin.groups.promoRateAppliedHint'
    )
  })

  it('updates the preview immediately but saves the undiscounted base rate', async () => {
    const wrapper = await mountModal()

    await wrapper.get('[data-testid="rate-input-101"]').setValue('0.13')

    expect(wrapper.get('[data-testid="promo-effective-rate-101"]').text()).toBe('0.091x')
    await wrapper.get('[data-testid="save-rate-multipliers"]').trigger('click')
    await flushPromises()

    expect(batchSetGroupRateMultipliers).toHaveBeenCalledWith(73, [
      { user_id: 101, rate_multiplier: 0.13 },
      { user_id: 202, rate_multiplier: 0.16 }
    ])
  })

  it('does not show promotional values when the server marks the promotion inactive', async () => {
    const wrapper = await mountModal({ ...group, promo_active: false })

    expect(wrapper.find('[data-testid="promo-summary"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="promo-effective-rate-heading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="promo-effective-rate-101"]').exists()).toBe(false)
  })
})
