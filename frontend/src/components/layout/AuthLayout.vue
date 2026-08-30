<template>
  <div
    v-if="variant === 'premium'"
    data-testid="auth-layout-premium"
    class="auth-layout--premium"
  >
    <header class="premium-header">
      <router-link
        to="/home"
        class="premium-brand"
        :aria-label="`${siteName} ${t('home.experience.homeLabel')}`"
      >
        <img :src="siteLogo || '/logo.svg'" alt="" class="premium-brand__logo" />
        <span class="premium-brand__name">{{ siteName }}</span>
      </router-link>

      <div class="premium-header__actions">
        <div class="premium-locale">
          <LocaleSwitcher />
        </div>
        <button
          type="button"
          class="premium-icon-button"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="sm" aria-hidden="true" />
          <Icon v-else name="moon" size="sm" aria-hidden="true" />
        </button>
        <router-link
          to="/home"
          class="premium-home-link"
          :title="t('home.experience.homeLabel')"
          :aria-label="t('home.experience.homeLabel')"
        >
          <Icon name="arrowLeft" size="sm" aria-hidden="true" />
          <span>{{ t('home.experience.homeLabel') }}</span>
        </router-link>
      </div>
    </header>

    <main class="premium-main">
      <section class="premium-visual" aria-labelledby="premium-visual-title">
        <div class="premium-visual__copy">
          <p class="premium-visual__eyebrow">{{ t('home.experience.eyebrow') }}</p>
          <h1 id="premium-visual-title">{{ siteName }}</h1>
          <p class="premium-visual__subtitle">{{ siteSubtitle }}</p>
        </div>

        <div
          class="premium-orbit-scene"
          role="img"
          :aria-label="t('home.experience.visualAlt')"
        >
          <div class="premium-orbit premium-orbit--outer">
            <div class="premium-model premium-model--outer">
              <span class="premium-model__node" aria-hidden="true"></span>
              <span>GPT</span>
            </div>
          </div>
          <div class="premium-orbit premium-orbit--middle">
            <div class="premium-model premium-model--middle">
              <span>{{ t('home.providers.claude') }}</span>
              <span class="premium-model__node" aria-hidden="true"></span>
            </div>
          </div>
          <div class="premium-orbit premium-orbit--inner">
            <div class="premium-model premium-model--inner">
              <span class="premium-model__node" aria-hidden="true"></span>
              <span>{{ t('home.providers.gemini') }}</span>
            </div>
          </div>

          <div class="premium-glass-ring premium-glass-ring--rear" aria-hidden="true"></div>
          <div class="premium-core" aria-hidden="true">
            <span class="premium-core__halo"></span>
            <img :src="siteLogo || '/logo.svg'" alt="" class="premium-core__logo" />
          </div>
          <div class="premium-glass-ring premium-glass-ring--front" aria-hidden="true"></div>
        </div>

        <div class="premium-route" aria-hidden="true">
          <span class="premium-route__method">POST</span>
          <code>/v1/messages</code>
          <span class="premium-route__status">
            <span class="premium-route__pulse"></span>
            {{ t('home.experience.routed') }}
          </span>
        </div>
      </section>

      <section class="premium-panel">
        <div class="premium-panel__content">
          <slot />

          <div class="premium-panel__footer">
            <slot name="footer" />
          </div>
        </div>

        <p class="premium-copyright">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </p>
      </section>
    </main>
  </div>

  <div
    v-else
    data-testid="auth-layout-default"
    class="relative flex min-h-screen items-center justify-center overflow-hidden p-4"
  >
    <!-- Background -->
    <div
      class="absolute inset-0 bg-gradient-to-br from-gray-50 via-primary-50/30 to-gray-100 dark:from-dark-950 dark:via-dark-900 dark:to-dark-950"
    ></div>

    <!-- Decorative Elements -->
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <!-- Gradient Orbs -->
      <div
        class="absolute -right-40 -top-40 h-80 w-80 rounded-full bg-primary-400/20 blur-3xl"
      ></div>
      <div
        class="absolute -bottom-40 -left-40 h-80 w-80 rounded-full bg-primary-500/15 blur-3xl"
      ></div>
      <div
        class="absolute left-1/2 top-1/2 h-96 w-96 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary-300/10 blur-3xl"
      ></div>

      <!-- Grid Pattern -->
      <div
        class="absolute inset-0 bg-[linear-gradient(rgba(20,184,166,0.03)_1px,transparent_1px),linear-gradient(90deg,rgba(20,184,166,0.03)_1px,transparent_1px)] bg-[size:64px_64px]"
      ></div>
    </div>

    <!-- Content Container -->
    <div class="relative z-10 w-full max-w-md">
      <!-- Logo/Brand -->
      <div class="mb-8 text-center">
        <!-- Custom Logo or Default Logo -->
        <template v-if="settingsLoaded">
          <div
            class="mb-4 inline-flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl shadow-lg shadow-primary-500/30"
          >
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <h1 class="text-gradient mb-2 text-3xl font-bold">
            {{ siteName }}
          </h1>
          <p class="text-sm text-gray-500 dark:text-dark-400">
            {{ siteSubtitle }}
          </p>
        </template>
      </div>

      <!-- Card Container -->
      <div class="card-glass rounded-2xl p-8 shadow-glass">
        <slot />
      </div>

      <!-- Footer Links -->
      <div class="mt-6 text-center text-sm">
        <slot name="footer" />
      </div>

      <!-- Copyright -->
      <div class="mt-8 text-center text-xs text-gray-400 dark:text-dark-500">
        &copy; {{ currentYear }} {{ siteName }}. All rights reserved.
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'

type AuthLayoutVariant = 'default' | 'premium'

withDefaults(
  defineProps<{
    variant?: AuthLayoutVariant
  }>(),
  {
    variant: 'default'
  }
)

const { t } = useI18n()
const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())
const isDark = ref(document.documentElement.classList.contains('dark'))

function toggleTheme(): void {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>

<style scoped>
.text-gradient {
  @apply bg-gradient-to-r from-primary-600 to-primary-500 bg-clip-text text-transparent;
}

.auth-layout--premium {
  --auth-canvas: #ffffff;
  --auth-panel: #ffffff;
  --auth-visual: #f2f4f7;
  --auth-ink: #121419;
  --auth-muted: #747b88;
  --auth-faint: #9ca2ad;
  --auth-line: rgba(18, 20, 25, 0.1);
  --auth-line-strong: rgba(18, 20, 25, 0.16);
  --auth-surface: rgba(255, 255, 255, 0.72);
  --auth-accent: #20a995;
  min-height: 100dvh;
  background: var(--auth-canvas);
  color: var(--auth-ink);
}

.premium-header {
  position: sticky;
  z-index: 30;
  top: 0;
  display: flex;
  min-width: 0;
  min-height: 4.5rem;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0 2rem;
  border-bottom: 1px solid var(--auth-line);
  background: color-mix(in srgb, var(--auth-canvas) 88%, transparent);
  backdrop-filter: blur(1rem);
}

.premium-brand,
.premium-home-link,
.premium-icon-button {
  color: var(--auth-ink);
  text-decoration: none;
}

.premium-brand {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.75rem;
}

.premium-brand__logo {
  width: 2rem;
  height: 2rem;
  flex-shrink: 0;
  border-radius: 0.375rem;
  object-fit: contain;
}

.premium-brand__name {
  overflow: hidden;
  max-width: 20rem;
  font-size: 0.875rem;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.premium-header__actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 0.375rem;
}

.premium-locale :deep(button),
.premium-icon-button,
.premium-home-link {
  min-height: 2.5rem;
  border-radius: 0.5rem;
}

.premium-locale :deep(button) {
  color: var(--auth-muted);
}

.premium-locale :deep(button:hover) {
  background: var(--auth-visual);
  color: var(--auth-ink);
}

.premium-icon-button {
  display: grid;
  width: 2.5rem;
  place-items: center;
  border: 0;
  background: transparent;
  color: var(--auth-muted);
  cursor: pointer;
}

.premium-icon-button:hover,
.premium-icon-button:focus-visible {
  background: var(--auth-visual);
  color: var(--auth-ink);
}

.premium-icon-button:focus-visible,
.premium-home-link:focus-visible,
.premium-brand:focus-visible {
  outline: 2px solid var(--auth-accent);
  outline-offset: 2px;
}

.premium-home-link {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0 0.75rem 0 0.625rem;
  color: var(--auth-muted);
  font-size: 0.8125rem;
  font-weight: 600;
}

.premium-home-link:hover,
.premium-home-link:focus-visible {
  color: var(--auth-ink);
}

.premium-main {
  display: grid;
  min-height: calc(100dvh - 4.5rem);
  grid-template-columns: minmax(0, 6fr) minmax(28rem, 4fr);
}

.premium-visual {
  position: sticky;
  top: 4.5rem;
  min-width: 0;
  height: calc(100dvh - 4.5rem);
  min-height: 38rem;
  overflow: hidden;
  border-right: 1px solid var(--auth-line);
  background: var(--auth-visual);
}

.premium-visual::after {
  position: absolute;
  content: '';
  pointer-events: none;
}

.premium-visual::after {
  right: -8rem;
  bottom: -10rem;
  width: 25rem;
  height: 25rem;
  border: 1px solid var(--auth-line);
  border-radius: 50%;
}

.premium-visual__copy {
  position: absolute;
  z-index: 6;
  top: clamp(3rem, 8vh, 6rem);
  left: clamp(2rem, 5vw, 5rem);
  max-width: min(28rem, calc(100% - 4rem));
}

.premium-visual__eyebrow {
  color: var(--auth-faint);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.6875rem;
  letter-spacing: 0;
  text-transform: uppercase;
}

.premium-visual__copy h1 {
  max-width: 18ch;
  margin-top: 1.125rem;
  font-size: 4rem;
  font-weight: 500;
  overflow-wrap: anywhere;
}

.premium-visual__subtitle {
  max-width: 44ch;
  margin-top: 1.25rem;
  color: var(--auth-muted);
  font-size: 0.9375rem;
  line-height: 1.7;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.premium-orbit-scene {
  position: absolute;
  z-index: 3;
  right: clamp(-4rem, 2vw, 2rem);
  bottom: clamp(4rem, 8vh, 7rem);
  width: min(76%, 42rem);
  aspect-ratio: 6 / 5;
}

.premium-orbit {
  position: absolute;
  top: 50%;
  left: 50%;
  border: 1px solid var(--auth-line-strong);
  border-radius: 50%;
  transform-style: preserve-3d;
}

.premium-orbit--outer {
  width: 94%;
  height: 64%;
  transform: translate(-50%, -50%) rotate(-12deg);
  animation: premium-orbit-outer 21s linear infinite;
}

.premium-orbit--middle {
  width: 76%;
  height: 84%;
  transform: translate(-50%, -50%) rotate(68deg);
  animation: premium-orbit-middle 25s linear infinite reverse;
}

.premium-orbit--inner {
  width: 62%;
  height: 46%;
  transform: translate(-50%, -50%) rotate(18deg);
  animation: premium-orbit-inner 16s linear infinite;
}

.premium-model {
  position: absolute;
  z-index: 6;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--auth-muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.6875rem;
  line-height: 1.5rem;
  white-space: nowrap;
}

.premium-model__node {
  width: 0.45rem;
  height: 0.45rem;
  flex-shrink: 0;
  border: 0.1rem solid var(--auth-visual);
  border-radius: 50%;
  background: var(--auth-ink);
}

.premium-model--outer {
  top: 4%;
  right: 16%;
  transform: rotate(12deg);
  animation: premium-label-outer 21s linear infinite;
}

.premium-model--middle {
  top: 46%;
  left: -2%;
  transform: rotate(-68deg);
  animation: premium-label-middle 25s linear infinite reverse;
}

.premium-model--inner {
  right: 5%;
  bottom: 1%;
  transform: rotate(-18deg);
  animation: premium-label-inner 16s linear infinite;
}

.premium-glass-ring {
  position: absolute;
  z-index: 2;
  top: 50%;
  left: 50%;
  width: 72%;
  height: 36%;
  border: clamp(0.8rem, 2vw, 1.5rem) solid rgba(255, 255, 255, 0.48);
  border-radius: 50%;
  background: linear-gradient(
    112deg,
    rgba(215, 230, 238, 0.04),
    rgba(192, 184, 240, 0.2),
    rgba(154, 222, 211, 0.12),
    rgba(255, 255, 255, 0.4)
  );
  box-shadow:
    inset 0 0 1.25rem rgba(255, 255, 255, 0.78),
    inset 0 -0.5rem 1rem rgba(105, 117, 151, 0.17),
    0 1.5rem 2rem rgba(46, 54, 73, 0.1);
  transform: translate(-50%, -50%) rotate(-10deg) skewX(-8deg);
  backdrop-filter: blur(0.2rem);
}

.premium-glass-ring--front {
  z-index: 5;
  clip-path: inset(50% -20% -20% -20%);
}

.premium-core {
  position: absolute;
  z-index: 4;
  top: 50%;
  left: 50%;
  display: grid;
  width: 35%;
  aspect-ratio: 1;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.78);
  border-radius: 50%;
  background: radial-gradient(circle at 34% 26%, #ffffff 0%, #edf1f7 40%, #c7ced9 100%);
  box-shadow:
    inset -1rem -1.25rem 2.25rem rgba(107, 119, 143, 0.2),
    inset 0.75rem 0.75rem 1.5rem rgba(255, 255, 255, 0.82),
    0 2rem 3.5rem rgba(47, 55, 74, 0.2);
  transform: translate(-50%, -55%);
  animation: premium-core-float 7s ease-in-out infinite;
}

.premium-core__halo {
  position: absolute;
  width: 72%;
  height: 72%;
  border: 1px solid rgba(255, 255, 255, 0.76);
  border-radius: 50%;
}

.premium-core__logo {
  position: relative;
  z-index: 1;
  width: 28%;
  height: 28%;
  border-radius: 50%;
  object-fit: cover;
}

.premium-route {
  position: absolute;
  z-index: 8;
  right: clamp(1.5rem, 4vw, 4rem);
  bottom: clamp(1.25rem, 3vh, 2.5rem);
  display: grid;
  width: min(25rem, calc(100% - 3rem));
  min-height: 3.25rem;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 0.75rem;
  padding: 0 0.875rem;
  border: 1px solid var(--auth-line);
  border-radius: 0.5rem;
  background: var(--auth-surface);
  backdrop-filter: blur(1.25rem);
  color: var(--auth-muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.6875rem;
}

.premium-route code {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.premium-route__method {
  color: var(--auth-ink);
  font-weight: 700;
}

.premium-route__status {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--auth-ink);
}

.premium-route__pulse {
  width: 0.4rem;
  height: 0.4rem;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--auth-accent);
  box-shadow: 0 0 0 0.25rem color-mix(in srgb, var(--auth-accent) 18%, transparent);
  animation: premium-status-pulse 2.4s ease-out infinite;
}

.premium-panel {
  display: flex;
  min-width: 0;
  min-height: calc(100dvh - 4.5rem);
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2rem;
  padding: clamp(2rem, 6vh, 4.5rem) clamp(1.25rem, 4vw, 4rem) 1.5rem;
  background: var(--auth-panel);
}

.premium-panel__content {
  width: min(100%, 26rem);
}

.premium-panel__footer {
  margin-top: 1.5rem;
  text-align: center;
}

.premium-copyright {
  width: 100%;
  margin-top: auto;
  color: var(--auth-faint);
  font-size: 0.6875rem;
  line-height: 1.5;
  text-align: center;
}

@keyframes premium-orbit-outer {
  from {
    transform: translate(-50%, -50%) rotate(-12deg);
  }
  to {
    transform: translate(-50%, -50%) rotate(348deg);
  }
}

@keyframes premium-orbit-middle {
  from {
    transform: translate(-50%, -50%) rotate(68deg);
  }
  to {
    transform: translate(-50%, -50%) rotate(428deg);
  }
}

@keyframes premium-orbit-inner {
  from {
    transform: translate(-50%, -50%) rotate(18deg);
  }
  to {
    transform: translate(-50%, -50%) rotate(378deg);
  }
}

@keyframes premium-label-outer {
  from {
    transform: rotate(12deg);
  }
  to {
    transform: rotate(-348deg);
  }
}

@keyframes premium-label-middle {
  from {
    transform: rotate(-68deg);
  }
  to {
    transform: rotate(-428deg);
  }
}

@keyframes premium-label-inner {
  from {
    transform: rotate(-18deg);
  }
  to {
    transform: rotate(-378deg);
  }
}

@keyframes premium-core-float {
  0%,
  100% {
    transform: translate(-50%, -55%);
  }
  50% {
    transform: translate(-50%, -59%);
  }
}

@keyframes premium-status-pulse {
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

@media (max-width: 64rem) {
  .premium-main {
    grid-template-columns: minmax(0, 1fr) minmax(26rem, 1fr);
  }

  .premium-visual__copy {
    top: 3.5rem;
    left: 2.5rem;
  }

  .premium-visual__copy h1 {
    font-size: 3rem;
  }

  .premium-orbit-scene {
    right: auto;
    left: 50%;
    width: min(86%, 36rem);
    transform: translateX(-50%);
  }
}

@media (max-width: 56rem) {
  .premium-header {
    min-height: 4rem;
    padding: 0 1rem;
  }

  .premium-brand__name {
    max-width: 9rem;
  }

  .premium-main {
    min-height: calc(100dvh - 4rem);
    grid-template-columns: minmax(0, 1fr);
  }

  .premium-visual {
    position: relative;
    top: auto;
    height: 13rem;
    min-height: 13rem;
    border-right: 0;
    border-bottom: 1px solid var(--auth-line);
  }

  .premium-visual__copy,
  .premium-route {
    display: none;
  }

  .premium-orbit-scene {
    right: auto;
    bottom: auto;
    left: 50%;
    width: min(20rem, 92vw);
    transform: translate(-50%, -7%);
  }

  .premium-panel {
    min-height: auto;
    justify-content: flex-start;
    gap: 2.5rem;
    padding: 2.5rem 1.25rem 1.5rem;
  }

  .premium-copyright {
    margin-top: 1rem;
  }
}

@media (max-width: 24rem) {
  .premium-brand__name,
  .premium-home-link span {
    display: none;
  }

  .premium-home-link {
    width: 2.5rem;
    justify-content: center;
    padding: 0;
  }
}

@media (hover: none) and (pointer: coarse) {
  .premium-locale :deep(button),
  .premium-icon-button,
  .premium-home-link {
    min-height: 3rem;
  }

  .premium-icon-button {
    width: 3rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .premium-orbit,
  .premium-model,
  .premium-core,
  .premium-route__pulse {
    animation: none;
  }
}
</style>

<style>
/* Vue's scoped CSS compiler drops `:global(.dark) ...` in production. */
.dark .auth-layout--premium {
  --auth-canvas: #090a0d;
  --auth-panel: #0c0d11;
  --auth-visual: #111319;
  --auth-ink: #f5f6f8;
  --auth-muted: #9aa1ad;
  --auth-faint: #737a86;
  --auth-line: rgba(255, 255, 255, 0.09);
  --auth-line-strong: rgba(255, 255, 255, 0.15);
  --auth-surface: rgba(16, 18, 23, 0.76);
  --auth-accent: #5bd5c1;
}

.dark .premium-glass-ring,
.dark .premium-core {
  box-shadow: none;
}

.dark .premium-glass-ring {
  border-color: rgba(255, 255, 255, 0.16);
  background: linear-gradient(
    112deg,
    rgba(255, 255, 255, 0.02),
    rgba(161, 151, 222, 0.16),
    rgba(83, 190, 171, 0.1),
    rgba(255, 255, 255, 0.08)
  );
}

.dark .premium-core {
  border-color: rgba(255, 255, 255, 0.12);
  background: radial-gradient(circle at 34% 26%, #333943 0%, #191c22 42%, #0e1014 100%);
}
</style>
