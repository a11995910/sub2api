import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'

import HomeView from '../HomeView.vue'

const { appStore, authStore } = vi.hoisted(() => ({
  appStore: {
    cachedPublicSettings: {} as Record<string, unknown>,
    siteName: 'Fallback site',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    user: null as { email?: string } | null,
    checkAuth: vi.fn(),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountHome(settings: Record<string, unknown> = {}) {
  appStore.cachedPublicSettings = {
    site_name: 'Test site',
    site_subtitle: 'Test subtitle',
    ...settings,
  }

  return mount(HomeView, {
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        LocaleSwitcher: { template: '<div data-testid="locale-switcher" />' },
        Icon: { template: '<span data-testid="icon" />' },
      },
    },
  })
}

function compactDestination(wrapper: ReturnType<typeof mountHome>) {
  return wrapper.get('[data-testid="compact-home"] main').findComponent(RouterLinkStub).props('to')
}

function modelMarketDestinations(wrapper: ReturnType<typeof mountHome>) {
  return wrapper
    .findAllComponents(RouterLinkStub)
    .map((link) => link.props('to'))
    .filter((destination) => destination === '/model-market')
}

describe('HomeView compact mode', () => {
  beforeEach(() => {
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.user = null
    authStore.checkAuth.mockClear()
    appStore.fetchPublicSettings.mockClear()
    localStorage.clear()
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
  })

  it('renders custom HTML ahead of compact mode', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: '<section id="custom-home">Custom home</section>',
    })

    expect(wrapper.get('#custom-home').text()).toBe('Custom home')
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
  })

  it('renders custom URL content ahead of compact mode', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: ' https://example.com/home ',
    })

    expect(wrapper.get('iframe').attributes('src')).toBe('https://example.com/home')
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
  })

  it('treats whitespace-only custom content as empty and selects compact mode', () => {
    const wrapper = mountHome({ compact_home_enabled: true, home_content: ' \n\t ' })

    expect(wrapper.get('[data-testid="compact-home"]').text()).toContain('Test site')
  })

  it.each([undefined, false])('selects the default home when compact mode is %s', (enabled) => {
    const settings = enabled === undefined ? {} : { compact_home_enabled: enabled }
    const wrapper = mountHome(settings)

    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="default-home"]').text()).toContain('Test site')
    expect(wrapper.get('[data-testid="default-home"]').text()).toContain('Test subtitle')
  })

  it('keeps provider labels attached to the animated orbit elements', () => {
    const wrapper = mountHome()
    const orbitLabels = wrapper.findAll('.orbit > .orbit-model')

    expect(orbitLabels).toHaveLength(3)
    expect(orbitLabels.map((label) => label.text())).toEqual(['GPT', 'home.providers.claude', 'home.providers.gemini'])
  })

  it('keeps the orbits centered when reduced motion disables their animations', () => {
    const wrapper = mountHome()
    const restingTransforms = [
      ['.orbit--outer', 'translate(-50%, -50%) rotate(-12deg)', 'rotate(12deg)'],
      ['.orbit--middle', 'translate(-50%, -50%) rotate(68deg)', 'rotate(-68deg)'],
      ['.orbit--inner', 'translate(-50%, -50%) rotate(18deg)', 'rotate(-18deg)'],
    ] as const

    restingTransforms.forEach(([selector, orbitTransform, labelTransform]) => {
      const orbit = wrapper.get(selector)

      expect(getComputedStyle(orbit.element).transform).toBe(orbitTransform)
      expect(getComputedStyle(orbit.get('.orbit-model').element).transform).toBe(labelTransform)
    })
  })

  it('applies the landing dark theme class when the theme toggle is used', async () => {
    const wrapper = mountHome()

    await wrapper.get('button[aria-label="home.switchToDark"]').trigger('click')

    expect(wrapper.get('[data-testid="default-home"]').classes()).toContain('premium-home--dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('links unauthenticated visitors to login', () => {
    expect(compactDestination(mountHome({ compact_home_enabled: true }))).toBe('/login')
  })

  it('links authenticated users to their dashboard', () => {
    authStore.isAuthenticated = true

    expect(compactDestination(mountHome({ compact_home_enabled: true }))).toBe('/dashboard')
  })

  it('links administrators to the admin dashboard', () => {
    authStore.isAuthenticated = true
    authStore.isAdmin = true

    const wrapper = mountHome({ compact_home_enabled: true })
    expect(compactDestination(wrapper)).toBe('/admin/dashboard')
    expect(authStore.checkAuth).toHaveBeenCalledOnce()
    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
  })

  it('links the compact home to the custom model market even when the public plaza is disabled', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      model_plaza_enabled: false,
    })

    expect(modelMarketDestinations(wrapper)).toEqual(['/model-market'])
    expect(wrapper.findAllComponents(RouterLinkStub).some((link) => link.props('to') === '/model-plaza')).toBe(false)
  })

  it('links the default home to the custom model market independently of public plaza settings', () => {
    const wrapper = mountHome({
      model_plaza_enabled: false,
      model_plaza_require_auth: true,
    })

    expect(modelMarketDestinations(wrapper)).toEqual(['/model-market', '/model-market'])
    expect(wrapper.findAllComponents(RouterLinkStub).some((link) => link.props('to') === '/model-plaza')).toBe(false)
  })
})
