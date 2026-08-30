<template>
  <div
    data-testid="default-home"
    class="premium-home"
    :class="{ 'premium-home--dark': isDark }"
    @keydown.esc="mobileMenuOpen = false"
  >
    <header class="landing-header">
      <div class="landing-header__inner">
        <router-link
          to="/home"
          class="brand-link"
          :aria-label="`${siteName} ${t('home.experience.homeLabel')}`"
          @click="mobileMenuOpen = false"
        >
          <img :src="siteLogo || '/logo.svg'" alt="" class="brand-link__logo" />
          <span class="brand-link__name">{{ siteName }}</span>
        </router-link>

        <nav class="desktop-nav" :aria-label="t('home.experience.primaryNav')">
          <router-link to="/beginner-guide" class="nav-link">
            {{ t('home.experience.guide') }}
          </router-link>
          <router-link to="/model-market" class="nav-link">
            {{ t('home.experience.modelMarket') }}
          </router-link>
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="nav-link"
          >
            {{ t('home.experience.developerDocs') }}
          </a>
        </nav>

        <div class="header-actions">
          <div class="locale-action">
            <LocaleSwitcher />
          </div>
          <button
            type="button"
            class="icon-action"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="emit('toggleTheme')"
          >
            <span class="touch-target" aria-hidden="true"></span>
            <Icon v-if="isDark" name="sun" size="md" aria-hidden="true" />
            <Icon v-else name="moon" size="md" aria-hidden="true" />
          </button>
          <router-link :to="primaryDestination" class="header-entry">
            <span>{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</span>
            <Icon name="arrowRight" size="sm" aria-hidden="true" />
          </router-link>
          <button
            type="button"
            class="mobile-menu-button"
            :title="
              mobileMenuOpen
                ? t('home.experience.menuClose')
                : t('home.experience.menuOpen')
            "
            :aria-label="
              mobileMenuOpen
                ? t('home.experience.menuClose')
                : t('home.experience.menuOpen')
            "
            :aria-expanded="mobileMenuOpen"
            aria-controls="landing-mobile-nav"
            @click="mobileMenuOpen = !mobileMenuOpen"
          >
            <span class="touch-target" aria-hidden="true"></span>
            <Icon :name="mobileMenuOpen ? 'x' : 'menu'" size="md" aria-hidden="true" />
          </button>
        </div>
      </div>

      <Transition name="mobile-menu">
        <nav
          v-if="mobileMenuOpen"
          id="landing-mobile-nav"
          class="mobile-nav"
          :aria-label="t('home.experience.mobileNav')"
        >
          <router-link to="/beginner-guide" class="mobile-nav__link" @click="closeMobileMenu">
            {{ t('home.experience.guide') }}
          </router-link>
          <router-link to="/model-market" class="mobile-nav__link" @click="closeMobileMenu">
            {{ t('home.experience.modelMarket') }}
          </router-link>
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="mobile-nav__link"
            @click="closeMobileMenu"
          >
            {{ t('home.experience.developerDocs') }}
          </a>
          <div class="mobile-nav__locale">
            <LocaleSwitcher />
          </div>
        </nav>
      </Transition>
    </header>

    <main>
      <section class="hero-section" aria-labelledby="landing-hero-title">
        <div class="hero-copy">
          <p class="hero-eyebrow">{{ t('home.experience.eyebrow') }}</p>
          <h1 id="landing-hero-title" class="hero-title">
            <span>{{ t('home.experience.headlineLead') }}</span>
            <span>{{ t('home.experience.headlineTail') }}</span>
          </h1>
          <p class="hero-description">{{ siteSubtitle }}</p>

          <div class="hero-actions">
            <router-link :to="primaryDestination" class="primary-action">
              <span>
                {{
                  isAuthenticated
                    ? t('home.goToDashboard')
                    : t('home.experience.startRouting')
                }}
              </span>
              <Icon name="arrowRight" size="sm" aria-hidden="true" />
            </router-link>
            <router-link to="/beginner-guide" class="secondary-action">
              <span>{{ t('home.experience.viewGuide') }}</span>
              <Icon name="arrowRight" size="sm" aria-hidden="true" />
            </router-link>
          </div>

          <div class="routing-health" role="status">
            <span class="routing-health__dot" aria-hidden="true"></span>
            <span>{{ t('home.experience.routeStatus') }}</span>
          </div>
        </div>

        <div
          ref="heroVisual"
          class="routing-visual"
          role="img"
          :aria-label="t('home.experience.visualAlt')"
          @pointermove="handleVisualPointerMove"
          @pointerleave="resetVisualPointer"
        >
          <div class="routing-assembly" aria-hidden="true">
            <div
              class="orbit orbit--outer"
              style="transform: translate(-50%, -50%) rotate(-12deg)"
            >
              <div class="orbit-model orbit-model--outer" style="transform: rotate(12deg)">
                <span class="orbit-model__node"></span>
                <span>GPT</span>
              </div>
            </div>
            <div
              class="orbit orbit--middle"
              style="transform: translate(-50%, -50%) rotate(68deg)"
            >
              <div class="orbit-model orbit-model--middle" style="transform: rotate(-68deg)">
                <span>{{ t('home.providers.claude') }}</span>
                <span class="orbit-model__node"></span>
              </div>
            </div>
            <div
              class="orbit orbit--inner"
              style="transform: translate(-50%, -50%) rotate(18deg)"
            >
              <div class="orbit-model orbit-model--inner" style="transform: rotate(-18deg)">
                <span class="orbit-model__node"></span>
                <span>{{ t('home.providers.gemini') }}</span>
              </div>
            </div>

            <div class="glass-ring glass-ring--rear"></div>
            <div class="gateway-core">
              <div class="gateway-core__halo"></div>
              <img :src="siteLogo || '/logo.svg'" alt="" class="gateway-core__logo" />
            </div>
            <div class="glass-ring glass-ring--front"></div>

          </div>

          <div class="route-console" aria-hidden="true">
            <span class="route-console__method">POST</span>
            <code>/v1/messages</code>
            <span class="route-console__result">
              <span class="route-console__pulse"></span>
              {{ t('home.experience.routed') }}
            </span>
          </div>
        </div>
      </section>

      <section class="feature-band" :aria-labelledby="`${featureHeadingId}`">
        <h2 :id="featureHeadingId" class="sr-only">
          {{ t('home.experience.featureHeading') }}
        </h2>
        <dl class="feature-list">
          <div class="feature-item">
            <Icon name="key" size="md" class="feature-item__icon" aria-hidden="true" />
            <div class="feature-item__copy">
              <dt>{{ t('home.features.unifiedGateway') }}</dt>
              <dd>{{ t('home.features.unifiedGatewayDesc') }}</dd>
            </div>
          </div>
          <div class="feature-item">
            <Icon name="shield" size="md" class="feature-item__icon" aria-hidden="true" />
            <div class="feature-item__copy">
              <dt>{{ t('home.features.multiAccount') }}</dt>
              <dd>{{ t('home.features.multiAccountDesc') }}</dd>
            </div>
          </div>
          <div class="feature-item">
            <Icon name="chart" size="md" class="feature-item__icon" aria-hidden="true" />
            <div class="feature-item__copy">
              <dt>{{ t('home.features.balanceQuota') }}</dt>
              <dd>{{ t('home.features.balanceQuotaDesc') }}</dd>
            </div>
          </div>
        </dl>
      </section>

      <section class="model-network" aria-labelledby="model-network-title">
        <div class="model-network__heading">
          <p>{{ t('home.experience.modelEyebrow') }}</p>
          <h2 id="model-network-title">{{ t('home.experience.modelTitle') }}</h2>
          <p>{{ t('home.experience.modelDescription') }}</p>
        </div>

        <div class="model-network__content">
          <ul class="provider-list" role="list">
            <li>
              <span class="provider-index">01</span>
              <span>{{ t('home.providers.claude') }}</span>
            </li>
            <li>
              <span class="provider-index">02</span>
              <span>GPT</span>
            </li>
            <li>
              <span class="provider-index">03</span>
              <span>{{ t('home.providers.gemini') }}</span>
            </li>
            <li>
              <span class="provider-index">04</span>
              <span>{{ t('home.providers.antigravity') }}</span>
            </li>
          </ul>

          <router-link to="/model-market" class="model-network__link">
            <span>{{ t('home.experience.browseModels') }}</span>
            <Icon name="arrowRight" size="sm" aria-hidden="true" />
          </router-link>
        </div>
      </section>
    </main>

    <footer class="landing-footer">
      <div class="landing-footer__inner">
        <div class="footer-brand">
          <img :src="siteLogo || '/logo.svg'" alt="" />
          <span>{{ siteName }}</span>
        </div>
        <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
        <div class="footer-links">
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer">
            {{ t('home.docs') }}
          </a>
          <a href="https://github.com/Wei-Shaw/sub2api" target="_blank" rel="noopener noreferrer">
            GitHub
          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  siteName: string
  siteLogo: string
  siteSubtitle: string
  docUrl: string
  isDark: boolean
  isAuthenticated: boolean
  dashboardPath: string
}>()

const emit = defineEmits<{
  toggleTheme: []
}>()

const { t } = useI18n()
const mobileMenuOpen = ref(false)
const heroVisual = ref<HTMLElement | null>(null)
const currentYear = computed(() => new Date().getFullYear())
const primaryDestination = computed(() => (props.isAuthenticated ? props.dashboardPath : '/login'))
const featureHeadingId = 'landing-feature-heading'

function closeMobileMenu() {
  mobileMenuOpen.value = false
}

function handleVisualPointerMove(event: PointerEvent) {
  if (
    event.pointerType === 'touch' ||
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  ) {
    return
  }

  const element = heroVisual.value
  if (!element) return

  const bounds = element.getBoundingClientRect()
  const x = (event.clientX - bounds.left) / bounds.width - 0.5
  const y = (event.clientY - bounds.top) / bounds.height - 0.5
  element.style.setProperty('--visual-tilt-x', `${x * 5}deg`)
  element.style.setProperty('--visual-tilt-y', `${y * -4}deg`)
}

function resetVisualPointer() {
  heroVisual.value?.style.setProperty('--visual-tilt-x', '0deg')
  heroVisual.value?.style.setProperty('--visual-tilt-y', '0deg')
}
</script>

<style scoped>
.premium-home {
  --landing-bg: #f4f6fa;
  --landing-surface: rgba(255, 255, 255, 0.68);
  --landing-ink: #111318;
  --landing-muted: #626b78;
  --landing-faint: #929aa6;
  --landing-line: rgba(17, 19, 24, 0.1);
  --landing-line-strong: rgba(17, 19, 24, 0.18);
  --landing-accent: #5aaea4;
  --landing-accent-soft: rgba(90, 174, 164, 0.18);
  min-height: 100svh;
  overflow-x: hidden;
  background: var(--landing-bg);
  color: var(--landing-ink);
  font-family: 'Avenir Next', 'SF Pro Display', 'PingFang SC', 'Microsoft YaHei', sans-serif;
}

.premium-home--dark {
  --landing-bg: #101216;
  --landing-surface: rgba(25, 28, 34, 0.78);
  --landing-ink: #f5f7f9;
  --landing-muted: #aab1bb;
  --landing-faint: #737b87;
  --landing-line: rgba(255, 255, 255, 0.1);
  --landing-line-strong: rgba(255, 255, 255, 0.18);
  --landing-accent: #7ac7bd;
  --landing-accent-soft: rgba(122, 199, 189, 0.16);
}

.landing-header {
  position: relative;
  z-index: 50;
  border-bottom: 1px solid var(--landing-line);
  background: color-mix(in srgb, var(--landing-bg) 84%, transparent);
  backdrop-filter: blur(18px);
}

.landing-header__inner {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  width: min(100%, 90rem);
  min-height: 4.75rem;
  margin: 0 auto;
  padding: 0 2rem;
}

.brand-link {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  justify-self: start;
  gap: 0.75rem;
  color: var(--landing-ink);
  text-decoration: none;
}

.brand-link__logo {
  width: 2rem;
  height: 2rem;
  flex-shrink: 0;
  border-radius: 0.5rem;
  object-fit: contain;
}

.brand-link__name {
  overflow: hidden;
  font-size: 0.875rem;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.desktop-nav {
  display: flex;
  align-items: center;
  gap: 2.5rem;
}

.nav-link {
  position: relative;
  padding: 1.75rem 0;
  color: var(--landing-muted);
  font-size: 0.8125rem;
  font-weight: 500;
  text-decoration: none;
}

.nav-link::after {
  position: absolute;
  right: 0;
  bottom: 1.2rem;
  left: 0;
  height: 1px;
  background: var(--landing-ink);
  content: '';
  transform: scaleX(0);
  transform-origin: right;
  transition: transform 180ms ease;
}

.nav-link:hover,
.nav-link:focus-visible {
  color: var(--landing-ink);
}

.nav-link:hover::after,
.nav-link:focus-visible::after {
  transform: scaleX(1);
  transform-origin: left;
}

.header-actions {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: flex-end;
  gap: 0.75rem;
}

.icon-action,
.mobile-menu-button {
  position: relative;
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--landing-muted);
  cursor: pointer;
}

.icon-action:hover,
.icon-action:focus-visible,
.mobile-menu-button:hover,
.mobile-menu-button:focus-visible {
  background: var(--landing-line);
  color: var(--landing-ink);
}

.touch-target {
  position: absolute;
  top: 50%;
  left: 50%;
  width: max(100%, 3rem);
  height: max(100%, 3rem);
  transform: translate(-50%, -50%);
}

.header-entry {
  display: inline-flex;
  min-height: 2.5rem;
  align-items: center;
  gap: 0.5rem;
  padding: 0 0.25rem 0 0.75rem;
  color: var(--landing-ink);
  font-size: 0.8125rem;
  font-weight: 600;
  text-decoration: none;
}

.header-entry :deep(svg),
.primary-action :deep(svg),
.secondary-action :deep(svg),
.model-network__link :deep(svg) {
  flex-shrink: 0;
  transition: transform 180ms ease;
}

.header-entry:hover :deep(svg),
.primary-action:hover :deep(svg),
.secondary-action:hover :deep(svg),
.model-network__link:hover :deep(svg) {
  transform: translateX(0.2rem);
}

.mobile-menu-button,
.mobile-nav {
  display: none;
}

.hero-section {
  display: grid;
  grid-template-columns: minmax(0, 9fr) minmax(0, 11fr);
  align-items: center;
  width: min(100%, 90rem);
  min-height: calc(100svh - 14rem);
  margin: 0 auto;
  padding: 3.25rem 2rem 2.5rem;
  gap: 4rem;
}

.hero-copy {
  min-width: 0;
}

.hero-eyebrow {
  max-width: 48ch;
  color: var(--landing-faint);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.8125rem;
  line-height: 1.5rem;
  text-transform: uppercase;
  animation: copy-reveal 500ms 40ms both;
}

.hero-title {
  display: flex;
  max-width: 14ch;
  flex-direction: column;
  margin-top: 1.75rem;
  font-size: 4.5rem;
  font-weight: 500;
  line-height: 1.12;
  overflow-wrap: anywhere;
  text-wrap: balance;
  animation: copy-reveal 520ms 80ms both;
}

.hero-description {
  max-width: 42ch;
  margin-top: 1.75rem;
  color: var(--landing-muted);
  font-size: 1rem;
  line-height: 1.9rem;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  text-wrap: pretty;
  animation: copy-reveal 520ms 120ms both;
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  margin-top: 2.25rem;
  gap: 0.875rem 1.5rem;
  animation: copy-reveal 520ms 160ms both;
}

.primary-action,
.secondary-action,
.model-network__link {
  display: inline-flex;
  min-height: 3rem;
  align-items: center;
  justify-content: center;
  gap: 0.625rem;
  border-radius: 0.5rem;
  font-size: 0.875rem;
  font-weight: 600;
  text-decoration: none;
}

.primary-action {
  padding: 0 0.75rem 0 1.125rem;
  background: var(--landing-ink);
  color: var(--landing-bg);
  outline: 1px solid var(--landing-ink);
  outline-offset: -1px;
  transition: transform 180ms ease;
}

.primary-action:hover {
  transform: translateY(-0.125rem);
}

.secondary-action {
  padding: 0 0.25rem;
  color: var(--landing-muted);
}

.secondary-action:hover,
.secondary-action:focus-visible {
  color: var(--landing-ink);
}

.routing-health {
  display: flex;
  align-items: center;
  margin-top: 2rem;
  gap: 0.625rem;
  color: var(--landing-faint);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  line-height: 1.5rem;
  animation: copy-reveal 520ms 200ms both;
}

.routing-health__dot,
.route-console__pulse {
  width: 0.4rem;
  height: 0.4rem;
  flex-shrink: 0;
  border-radius: 999px;
  background: var(--landing-accent);
  box-shadow: 0 0 0 0.25rem var(--landing-accent-soft);
}

.routing-visual {
  --visual-tilt-x: 0deg;
  --visual-tilt-y: 0deg;
  position: relative;
  min-width: 0;
  min-height: 30rem;
  perspective: 75rem;
  animation: visual-reveal 650ms 80ms both;
}

.routing-assembly {
  position: absolute;
  top: 32%;
  left: 50%;
  width: min(100%, 34rem);
  aspect-ratio: 6 / 5;
  transform: translate(-50%, -50%) rotateX(var(--visual-tilt-y))
    rotateY(var(--visual-tilt-x));
  transform-style: preserve-3d;
  transition: transform 260ms ease-out;
}

.orbit {
  position: absolute;
  top: 50%;
  left: 50%;
  border: 1px solid var(--landing-line-strong);
  border-radius: 50%;
  transform-style: preserve-3d;
}

.orbit--outer {
  width: 92%;
  height: 66%;
  animation: orbit-outer 18s linear infinite;
}

.orbit--middle {
  width: 78%;
  height: 82%;
  animation: orbit-middle 22s linear infinite reverse;
}

.orbit--inner {
  width: 64%;
  height: 48%;
  animation: orbit-inner 14s linear infinite;
}

.orbit-model {
  position: absolute;
  z-index: 5;
  display: flex;
  align-items: center;
  gap: 0.6rem;
  color: var(--landing-muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  line-height: 1.5rem;
  white-space: nowrap;
}

.orbit-model__node {
  width: 0.45rem;
  height: 0.45rem;
  flex-shrink: 0;
  border: 0.12rem solid var(--landing-bg);
  border-radius: 50%;
  background: var(--landing-ink);
}

.orbit-model--outer {
  top: 3%;
  right: 16%;
  animation: orbit-label-outer 18s linear infinite;
}

.orbit-model--middle {
  top: 46%;
  left: -1%;
  animation: orbit-label-middle 22s linear infinite reverse;
}

.orbit-model--inner {
  right: 5%;
  bottom: 1%;
  animation: orbit-label-inner 14s linear infinite;
}

.glass-ring {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 72%;
  height: 38%;
  border: 1.75rem solid rgba(255, 255, 255, 0.42);
  border-radius: 50%;
  background: linear-gradient(
    110deg,
    rgba(191, 220, 233, 0.04),
    rgba(196, 188, 245, 0.24),
    rgba(160, 221, 213, 0.12),
    rgba(255, 255, 255, 0.42)
  );
  box-shadow:
    inset 0 0 1.4rem rgba(255, 255, 255, 0.72),
    inset 0 -0.5rem 1.2rem rgba(112, 125, 164, 0.18),
    0 1.5rem 2rem rgba(58, 70, 97, 0.1);
  backdrop-filter: blur(0.2rem);
}

.glass-ring--rear {
  z-index: 1;
  transform: translate(-50%, -50%) rotate(-10deg) skewX(-8deg);
}

.glass-ring--front {
  z-index: 4;
  clip-path: inset(50% -20% -20% -20%);
  transform: translate(-50%, -50%) rotate(-10deg) skewX(-8deg);
}

.gateway-core {
  position: absolute;
  z-index: 3;
  top: 50%;
  left: 50%;
  display: grid;
  width: 36%;
  aspect-ratio: 1;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: 50%;
  background: radial-gradient(circle at 34% 26%, #ffffff 0%, #edf1f7 38%, #c7ced9 100%);
  box-shadow:
    inset -1.25rem -1.5rem 2.5rem rgba(107, 119, 143, 0.2),
    inset 0.75rem 0.75rem 1.75rem rgba(255, 255, 255, 0.8),
    0 2rem 3.5rem rgba(47, 55, 74, 0.2);
  transform: translate(-50%, -55%);
  animation: core-float 7s ease-in-out infinite;
}

.gateway-core__halo {
  position: absolute;
  width: 72%;
  height: 72%;
  border: 1px solid rgba(255, 255, 255, 0.72);
  border-radius: 50%;
}

.gateway-core__logo {
  position: relative;
  z-index: 1;
  width: 28%;
  height: 28%;
  object-fit: contain;
}

.route-console {
  position: absolute;
  z-index: 8;
  right: 2rem;
  bottom: 1.25rem;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  width: min(100% - 4rem, 27rem);
  min-height: 3.5rem;
  align-items: center;
  gap: 0.75rem;
  padding: 0 1rem;
  border: 1px solid var(--landing-line);
  border-radius: 0.5rem;
  background: var(--landing-surface);
  box-shadow: 0 1rem 3rem rgba(44, 51, 66, 0.1);
  backdrop-filter: blur(20px);
  color: var(--landing-muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.6875rem;
}

.route-console code {
  overflow: hidden;
  color: var(--landing-ink);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.route-console__method {
  color: var(--landing-accent);
  font-weight: 700;
}

.route-console__result {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
}

.route-console__pulse {
  width: 0.3rem;
  height: 0.3rem;
  animation: status-pulse 2.4s ease-out infinite;
}

.feature-band {
  border-top: 1px solid var(--landing-line);
  border-bottom: 1px solid var(--landing-line);
}

.feature-list {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  width: min(100%, 90rem);
  margin: 0 auto;
  padding: 0 2rem;
}

.feature-item {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 1rem;
  padding: 2rem 2.5rem;
  border-left: 1px solid var(--landing-line);
}

.feature-item:first-child {
  padding-left: 0;
  border-left: 0;
}

.feature-item:last-child {
  padding-right: 0;
}

.feature-item__icon {
  flex-shrink: 0;
  color: var(--landing-ink);
}

.feature-item__copy {
  min-width: 0;
}

.feature-item dt {
  font-size: 0.875rem;
  font-weight: 600;
}

.feature-item dd {
  margin-top: 0.45rem;
  color: var(--landing-muted);
  font-size: 0.8125rem;
  line-height: 1.5rem;
  text-wrap: pretty;
}

.model-network {
  display: grid;
  grid-template-columns: minmax(0, 9fr) minmax(0, 11fr);
  width: min(100%, 90rem);
  margin: 0 auto;
  padding: 8rem 2rem;
  gap: 4rem;
}

.model-network__heading > p:first-child {
  max-width: 48ch;
  color: var(--landing-faint);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  line-height: 1.5rem;
  text-transform: uppercase;
}

.model-network__heading h2 {
  max-width: 18ch;
  margin-top: 1.25rem;
  font-size: 2.5rem;
  font-weight: 500;
  overflow-wrap: anywhere;
  text-wrap: balance;
}

.model-network__heading > p:last-child {
  max-width: 48ch;
  margin-top: 1.5rem;
  color: var(--landing-muted);
  font-size: 1rem;
  line-height: 1.8rem;
  text-wrap: pretty;
}

.model-network__content {
  min-width: 0;
}

.provider-list {
  margin: 0;
  padding: 0;
  border-top: 1px solid var(--landing-line-strong);
}

.provider-list li {
  display: grid;
  grid-template-columns: 3rem minmax(0, 1fr);
  min-height: 4.25rem;
  align-items: center;
  border-bottom: 1px solid var(--landing-line);
  font-size: 1rem;
  font-weight: 500;
}

.provider-index {
  color: var(--landing-faint);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.6875rem;
}

.model-network__link {
  margin-top: 1.75rem;
  padding: 0 0.25rem;
  color: var(--landing-ink);
}

.landing-footer {
  border-top: 1px solid var(--landing-line);
}

.landing-footer__inner {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  width: min(100%, 90rem);
  min-height: 6rem;
  margin: 0 auto;
  padding: 1.5rem 2rem;
  gap: 2rem;
  color: var(--landing-muted);
  font-size: 0.75rem;
}

.footer-brand {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 0.625rem;
  color: var(--landing-ink);
  font-weight: 600;
}

.footer-brand img {
  width: 1.5rem;
  height: 1.5rem;
  flex-shrink: 0;
  border-radius: 0.375rem;
  object-fit: contain;
}

.footer-brand span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.landing-footer__inner > p {
  text-align: center;
}

.footer-links {
  display: flex;
  justify-content: flex-end;
  gap: 1.5rem;
}

.footer-links a {
  color: var(--landing-muted);
  font-weight: 400;
  text-decoration: none;
}

.footer-links a:hover,
.footer-links a:focus-visible {
  color: var(--landing-ink);
}

.premium-home :where(a, button):focus-visible {
  outline: 2px solid var(--landing-accent);
  outline-offset: 3px;
}

.premium-home--dark .glass-ring,
.premium-home--dark .gateway-core,
.premium-home--dark .route-console,
.premium-home--dark .routing-health__dot,
.premium-home--dark .route-console__pulse {
  box-shadow: none;
}

.premium-home--dark .glass-ring {
  border-color: rgba(255, 255, 255, 0.14);
  background: linear-gradient(
    110deg,
    rgba(89, 116, 133, 0.08),
    rgba(124, 110, 171, 0.18),
    rgba(79, 147, 139, 0.12),
    rgba(255, 255, 255, 0.08)
  );
}

.premium-home--dark .gateway-core {
  border-color: rgba(255, 255, 255, 0.16);
  background: radial-gradient(circle at 34% 26%, #3a4049 0%, #252931 46%, #17191e 100%);
}

.premium-home--dark .gateway-core__halo {
  border-color: rgba(255, 255, 255, 0.12);
}

@keyframes copy-reveal {
  from {
    opacity: 0;
    transform: translateY(1rem);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes visual-reveal {
  from {
    opacity: 0;
    transform: translateY(1.5rem) scale(0.97);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

@keyframes core-float {
  0%,
  100% {
    transform: translate(-50%, -55%);
  }
  50% {
    transform: translate(-50%, -59%);
  }
}

@keyframes orbit-outer {
  from {
    transform: translate(-50%, -50%) rotate(-12deg);
  }
  to {
    transform: translate(-50%, -50%) rotate(348deg);
  }
}

@keyframes orbit-middle {
  from {
    transform: translate(-50%, -50%) rotate(68deg);
  }
  to {
    transform: translate(-50%, -50%) rotate(428deg);
  }
}

@keyframes orbit-inner {
  from {
    transform: translate(-50%, -50%) rotate(18deg);
  }
  to {
    transform: translate(-50%, -50%) rotate(378deg);
  }
}

@keyframes orbit-label-outer {
  from {
    transform: rotate(12deg);
  }
  to {
    transform: rotate(-348deg);
  }
}

@keyframes orbit-label-middle {
  from {
    transform: rotate(-68deg);
  }
  to {
    transform: rotate(-428deg);
  }
}

@keyframes orbit-label-inner {
  from {
    transform: rotate(-18deg);
  }
  to {
    transform: rotate(-378deg);
  }
}

@keyframes status-pulse {
  0% {
    opacity: 1;
    transform: scale(1);
  }
  70%,
  100% {
    opacity: 0.45;
    transform: scale(1.5);
  }
}

.mobile-menu-enter-active,
.mobile-menu-leave-active {
  transition:
    opacity 180ms ease,
    transform 180ms ease;
}

.mobile-menu-enter-from,
.mobile-menu-leave-to {
  opacity: 0;
  transform: translateY(-0.5rem);
}

@media (max-width: 64rem) {
  .landing-header__inner {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .desktop-nav {
    display: none;
  }

  .mobile-menu-button {
    display: inline-flex;
  }

  .mobile-nav {
    display: grid;
    width: min(100%, 90rem);
    margin: 0 auto;
    padding: 0.5rem 2rem 1.25rem;
    gap: 0.25rem;
  }

  .mobile-nav__link {
    display: flex;
    min-height: 3rem;
    align-items: center;
    border-bottom: 1px solid var(--landing-line);
    color: var(--landing-muted);
    font-size: 1rem;
    text-decoration: none;
  }

  .mobile-nav__link:hover,
  .mobile-nav__link:focus-visible {
    color: var(--landing-ink);
  }

  .hero-section,
  .model-network {
    grid-template-columns: minmax(0, 1fr);
    gap: 2rem;
  }

  .hero-section {
    min-height: auto;
    padding-top: 4rem;
  }

  .hero-copy {
    position: relative;
    z-index: 10;
  }

  .hero-title {
    font-size: 3.75rem;
  }

  .routing-visual {
    min-height: 26.5rem;
  }

  .routing-assembly {
    top: 32%;
    width: min(100%, 31rem);
  }

  .model-network {
    padding-top: 6rem;
    padding-bottom: 6rem;
  }
}

@media (max-width: 42rem) {
  .landing-header__inner {
    min-height: 4rem;
    padding: 0 1.25rem;
  }

  .brand-link__logo {
    width: 1.75rem;
    height: 1.75rem;
  }

  .brand-link__name {
    max-width: 8.5rem;
    font-size: 0.8125rem;
  }

  .header-actions {
    gap: 0.25rem;
  }

  .locale-action {
    display: none;
  }

  .mobile-nav__locale {
    padding-top: 0.75rem;
  }

  .header-entry {
    display: none;
  }

  .mobile-nav {
    padding: 0.5rem 1.25rem 1rem;
  }

  .hero-section {
    min-height: calc(100svh - 9rem);
    padding: 2.75rem 1.25rem 1.5rem;
    gap: 0.75rem;
  }

  .hero-eyebrow {
    font-size: 0.75rem;
  }

  .hero-title {
    max-width: 12ch;
    margin-top: 1.25rem;
    font-size: 3rem;
  }

  .hero-description {
    margin-top: 1.25rem;
    font-size: 1rem;
    line-height: 1.75rem;
  }

  .hero-actions {
    align-items: stretch;
    margin-top: 1.75rem;
    gap: 0.75rem;
  }

  .primary-action {
    min-height: 3.25rem;
  }

  .secondary-action {
    min-height: 3.25rem;
    justify-content: flex-start;
  }

  .routing-health {
    margin-top: 1.25rem;
  }

  .routing-visual {
    min-height: 20rem;
  }

  .routing-assembly {
    top: 32%;
    width: 20rem;
    max-width: 112%;
  }

  .gateway-core {
    width: 34%;
  }

  .glass-ring {
    border-width: 1.15rem;
  }

  .orbit-model--middle {
    left: 2%;
  }

  .orbit-model--inner {
    right: 8%;
  }

  .route-console {
    right: 0;
    bottom: 0;
    width: 100%;
    min-height: 3.25rem;
  }

  .feature-list {
    grid-template-columns: minmax(0, 1fr);
    padding: 0 1.25rem;
  }

  .feature-item,
  .feature-item:first-child,
  .feature-item:last-child {
    padding: 1.5rem 0;
    border-top: 1px solid var(--landing-line);
    border-left: 0;
  }

  .feature-item:first-child {
    border-top: 0;
  }

  .feature-item dd {
    font-size: 1rem;
    line-height: 1.65rem;
  }

  .model-network {
    padding: 5rem 1.25rem;
  }

  .model-network__heading h2 {
    font-size: 2.25rem;
  }

  .model-network__heading > p:last-child {
    font-size: 1rem;
  }

  .provider-list li {
    min-height: 4.5rem;
    font-size: 1.125rem;
  }

  .landing-footer__inner {
    grid-template-columns: minmax(0, 1fr);
    padding: 2rem 1.25rem;
    gap: 1rem;
  }

  .landing-footer__inner > p {
    text-align: left;
  }

  .footer-links {
    justify-content: flex-start;
  }
}

@media (hover: hover) and (pointer: fine) {
  .touch-target {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .hero-eyebrow,
  .hero-title,
  .hero-description,
  .hero-actions,
  .routing-health,
  .routing-visual,
  .gateway-core,
  .orbit--outer,
  .orbit--middle,
  .orbit--inner,
  .orbit-model--outer,
  .orbit-model--middle,
  .orbit-model--inner,
  .route-console__pulse {
    animation: none;
  }

  .routing-assembly,
  .nav-link::after,
  .header-entry :deep(svg),
  .primary-action,
  .primary-action :deep(svg),
  .secondary-action :deep(svg),
  .model-network__link :deep(svg) {
    transition: none;
  }
}
</style>
