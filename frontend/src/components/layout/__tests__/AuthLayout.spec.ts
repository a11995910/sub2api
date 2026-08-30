import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AuthLayout from '@/components/layout/AuthLayout.vue'

const fetchPublicSettingsMock = vi.fn()

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    siteName: 'Test Gateway',
    siteLogo: '/logo.svg',
    cachedPublicSettings: {
      site_subtitle: 'One gateway for every model.'
    },
    publicSettingsLoaded: true,
    fetchPublicSettings: fetchPublicSettingsMock
  })
}))

function mountLayout(variant?: 'default' | 'premium') {
  return mount(AuthLayout, {
    props: variant ? { variant } : {},
    slots: {
      default: '<div data-testid="auth-content">content</div>',
      footer: '<div data-testid="auth-footer">footer</div>'
    },
    global: {
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a><slot /></a>'
        },
        LocaleSwitcher: true,
        Icon: true
      }
    }
  })
}

describe('AuthLayout variants', () => {
  beforeEach(() => {
    fetchPublicSettingsMock.mockReset()
    document.documentElement.classList.remove('dark')
    localStorage.removeItem('theme')
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.removeItem('theme')
  })

  it('keeps the existing layout as the default variant', () => {
    const wrapper = mountLayout()

    expect(wrapper.get('[data-testid="auth-layout-default"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="auth-layout-premium"]').exists()).toBe(false)
    expect(wrapper.get('.card-glass').classes()).toContain('rounded-2xl')
    expect(wrapper.get('[data-testid="auth-content"]').text()).toBe('content')
    expect(wrapper.get('[data-testid="auth-footer"]').text()).toBe('footer')
    expect(fetchPublicSettingsMock).toHaveBeenCalledOnce()
  })

  it('renders the premium shell and keeps both slot contracts', async () => {
    const wrapper = mountLayout('premium')

    expect(wrapper.get('[data-testid="auth-layout-premium"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="auth-layout-default"]').exists()).toBe(false)
    expect(wrapper.get('.premium-panel [data-testid="auth-content"]').text()).toBe('content')
    expect(wrapper.get('.premium-panel__footer [data-testid="auth-footer"]').text()).toBe('footer')
    expect(wrapper.get('.premium-locale').exists()).toBe(true)
    expect(wrapper.get('.premium-home-link').attributes('aria-label')).toBe(
      'home.experience.homeLabel'
    )
    expect(wrapper.get('.premium-orbit--outer .premium-model--outer').text()).toContain('GPT')
    expect(wrapper.get('.premium-orbit--middle .premium-model--middle').text()).toContain(
      'home.providers.claude'
    )
    expect(wrapper.get('.premium-orbit--inner .premium-model--inner').text()).toContain(
      'home.providers.gemini'
    )

    await wrapper.get('.premium-icon-button').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
  })
})
