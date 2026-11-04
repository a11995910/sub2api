<template>
  <div class="model-leaderboard">
    <div class="ambient ambient-purple" aria-hidden="true"></div>
    <div class="ambient ambient-gold" aria-hidden="true"></div>
    <header class="rank-nav">
      <RouterLink to="/home" class="rank-brand"
        ><span class="brand-mark"><Icon name="trophy" size="md" /></span
        >{{ t('modelLeaderboard.title') }}<span class="edition">01 / GLOBAL</span></RouterLink
      >
      <div class="nav-actions">
        <a href="#sources" class="nav-source"
          >{{ t('modelLeaderboard.sourceName') }} <Icon name="arrowDown" size="xs"
        /></a>
        <RouterLink :to="backTarget" class="back-link"
          ><Icon name="arrowLeft" size="sm" /><span>{{
            t(authStore.isAuthenticated ? 'modelLeaderboard.back' : 'modelLeaderboard.home')
          }}</span></RouterLink
        >
      </div>
    </header>

    <main class="rank-main">
      <section class="rank-hero">
        <div class="hero-copy">
          <h1>
            <span>{{ t('modelLeaderboard.title') }}</span>
            <span class="hero-spark" aria-hidden="true">✦</span>
          </h1>
          <p class="hero-subtitle">{{ t('modelLeaderboard.subtitle') }}</p>
          <div class="hero-source">
            <span class="source-dot"></span><span>Artificial Analysis</span
            ><span class="source-divider">/</span>{{ t('modelLeaderboard.sourceTag')
            }}<a
              href="https://artificialanalysis.ai/leaderboards/models"
              target="_blank"
              rel="noopener noreferrer"
              :aria-label="t('modelLeaderboard.sourceLink')"
              ><Icon name="externalLink" size="xs"
            /></a>
          </div>
        </div>
        <div class="hero-art" aria-hidden="true">
          <div class="orbit-glow"></div>
          <svg class="orbit-globe" viewBox="0 0 360 310" fill="none">
            <defs>
              <linearGradient
                id="rank-orbit"
                x1="45"
                y1="30"
                x2="310"
                y2="285"
                gradientUnits="userSpaceOnUse"
              >
                <stop stop-color="#ecd29a" />
                <stop offset=".48" stop-color="#a998cf" />
                <stop offset="1" stop-color="#6e3eb8" />
              </linearGradient>
              <radialGradient id="rank-sphere">
                <stop stop-color="#332546" />
                <stop offset="1" stop-color="#11101b" />
              </radialGradient>
            </defs>
            <circle
              cx="180"
              cy="151"
              r="104"
              fill="url(#rank-sphere)"
              stroke="url(#rank-orbit)"
              stroke-opacity=".7"
            />
            <g stroke="url(#rank-orbit)" stroke-opacity=".35">
              <ellipse cx="180" cy="151" rx="104" ry="35" />
              <ellipse cx="180" cy="151" rx="104" ry="74" />
              <ellipse cx="180" cy="151" rx="36" ry="104" />
              <ellipse cx="180" cy="151" rx="76" ry="104" />
              <path d="M76 151h208M180 47v208" />
            </g>
            <ellipse
              cx="180"
              cy="151"
              rx="165"
              ry="59"
              transform="rotate(-30 180 151)"
              stroke="url(#rank-orbit)"
              stroke-opacity=".8"
            />
            <ellipse
              cx="180"
              cy="151"
              rx="148"
              ry="133"
              transform="rotate(-30 180 151)"
              stroke="#aa8bca"
              stroke-dasharray="2 9"
              stroke-opacity=".25"
            />
            <circle cx="310" cy="85" r="5" fill="#e5c786" />
            <circle cx="51" cy="216" r="3" fill="#bfa1ec" />
            <path d="m180 111 12 27 29 13-29 12-12 28-13-28-28-12 28-13Z" fill="url(#rank-orbit)" />
            <path d="m297 220 3 7 8 3-8 3-3 7-3-7-7-3 7-3Z" fill="#c5a2ee" />
          </svg>
        </div>
      </section>

      <div v-if="loading && !data" class="rank-state" role="status">
        <span class="loading-orbit"></span>{{ t('modelLeaderboard.loading') }}
      </div>
      <div v-else-if="!data" class="rank-state" role="alert">
        <Icon name="infoCircle" size="lg" />
        <p>{{ t('modelLeaderboard.loadFailed') }}</p>
        <button class="gold-button" @click="loadLeaderboard">
          {{ t('modelLeaderboard.retry') }}
        </button>
      </div>
      <template v-if="data">
        <div class="rank-stats">
          <div>
            <strong>{{ intelligenceCount }}</strong
            ><span>{{ t('modelLeaderboard.models') }}</span>
          </div>
          <div>
            <strong>{{ data.categories.length }}<span> / </span></strong
            ><span>{{ t('modelLeaderboard.dimensions') }}</span>
          </div>
          <div>
            <strong>{{ data.refresh_interval_hours }}<small>h</small></strong
            ><span>{{ t('modelLeaderboard.refresh') }}</span>
          </div>
          <div class="release-stat">
            <span>{{ t('modelLeaderboard.snapshotDate') }}</span
            ><strong>{{ snapshotDate }}</strong
            ><span>ARTIFICIAL ANALYSIS</span>
          </div>
        </div>

        <div
          v-if="
            !data.sync_enabled ||
            data.stale ||
            data.refreshing ||
            data.last_attempt_failed ||
            loadFailed
          "
          class="sync-notice"
          role="status"
        >
          <Icon name="clock" size="sm" />{{
            t(
              !data.sync_enabled
                ? 'modelLeaderboard.syncDisabled'
                : data.refreshing
                  ? 'modelLeaderboard.refreshing'
                  : data.last_attempt_failed || loadFailed
                    ? 'modelLeaderboard.syncFailed'
                    : 'modelLeaderboard.stale'
            )
          }}
        </div>

        <section class="ranking-section">
          <div class="section-heading">
            <div>
              <p class="eyebrow muted">THE INTELLIGENCE INDEX</p>
              <h2>
                {{ t('modelLeaderboard.ranking')
                }}<span class="heading-star" aria-hidden="true">✧</span>
              </h2>
            </div>
            <span class="section-note">{{ t('modelLeaderboard.rankingHint') }}</span>
          </div>
          <div class="metric-tabs" role="group" :aria-label="t('modelLeaderboard.dimensions')">
            <button
              v-for="item in metrics"
              :key="item.key"
              :class="{ active: metric === item.key }"
              :aria-pressed="metric === item.key"
              @click="metric = item.key"
            >
              <Icon :name="item.icon" size="sm" />{{ t(`modelLeaderboard.${item.key}`) }}
            </button>
          </div>
          <p class="metric-explanation">
            <span class="tiny-line"></span>{{ t(`modelLeaderboard.${metric}Hint`) }}
          </p>

          <div class="podium" :aria-label="t('modelLeaderboard.podium')">
            <article
              v-for="(model, index) in ranked.slice(0, 3)"
              :key="model.id"
              class="podium-card"
              :class="`podium-${index + 1}`"
            >
              <div class="podium-top">
                <span class="podium-place">{{ String(model.rank).padStart(2, '0') }}</span
                ><span class="podium-tag"
                  ><Icon v-if="index === 0" name="trophy" size="xs" />{{
                    index === 0 ? t('modelLeaderboard.leader') : 'AA DATA'
                  }}</span
                >
              </div>
              <div class="podium-model">
                <span class="model-logo"><ModelIcon :model="model.slug" size="26px" /></span
                ><span>{{ model.creator }}</span>
              </div>
              <h3>{{ model.name }}</h3>
              <div class="podium-bottom">
                <div>
                  <strong>{{ formatScore(model.score) }}</strong
                  ><small>{{ scoreUnit(metric) }}</small>
                </div>
                <RouterLink
                  v-if="matches(model.id).length"
                  :to="plazaTarget(model.id)"
                  :aria-label="`${model.name} · ${t('modelLeaderboard.plaza')}`"
                  class="podium-link"
                  ><Icon name="arrowRight" size="md" /></RouterLink
                ><span v-else class="podium-caption">{{ t('modelLeaderboard.score') }}</span>
              </div>
              <div class="podium-track">
                <span :style="{ width: barWidth(model.score, metric) }"></span>
              </div>
            </article>
          </div>

          <div class="ranking-board">
            <div class="board-toolbar">
              <label class="rank-search"
                ><Icon name="search" size="sm" /><input
                  v-model="search"
                  :aria-label="t('modelLeaderboard.search')"
                  :placeholder="t('modelLeaderboard.search')"
              /></label>
              <div class="board-filters">
                <select v-model="provider" :aria-label="t('modelLeaderboard.allProviders')">
                  <option value="all">{{ t('modelLeaderboard.allProviders') }}</option>
                  <option v-for="item in providers" :key="item" :value="item">
                    {{ item }}
                  </option></select
                ><label class="available-filter"
                  ><input
                    v-model="availableOnly"
                    type="checkbox"
                    :disabled="plazaState !== 'ready'"
                  /><span>{{ t('modelLeaderboard.availableOnly') }}</span></label
                >
              </div>
            </div>
            <p v-if="plazaState !== 'ready'" class="plaza-status">
              <RouterLink
                v-if="plazaState === 'login'"
                :to="{ path: '/login', query: { redirect: '/model-leaderboard' } }"
                >{{ t('modelLeaderboard.loginToMatch') }} ↗</RouterLink
              ><template v-else>{{ plazaStateText }}</template
              ><button v-if="plazaState === 'failed'" @click="loadPlaza">
                {{ t('modelLeaderboard.retry') }}
              </button>
            </p>
            <div class="board-scroll">
              <table class="rank-table">
                <thead>
                  <tr>
                    <th scope="col">{{ t('modelLeaderboard.rank') }}</th>
                    <th scope="col">{{ t('modelLeaderboard.model') }}</th>
                    <th scope="col" class="score-column">
                      {{ t(`modelLeaderboard.${metric}`) }}
                      {{ currentCategory?.ascending ? '↑' : '↓' }}
                    </th>
                    <th scope="col" class="profile-column">{{ t('modelLeaderboard.profile') }}</th>
                    <th scope="col">{{ t('modelLeaderboard.access') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <template v-for="model in visibleRows" :key="model.id">
                    <tr :class="{ expanded: expanded === model.id }">
                      <td class="position" :class="{ 'top-position': model.rank <= 3 }">
                        {{ String(model.rank).padStart(2, '0') }}
                      </td>
                      <td>
                        <button
                          class="model-name-button"
                          :aria-expanded="expanded === model.id"
                          :aria-label="`${model.name} · ${t(expanded === model.id ? 'modelLeaderboard.collapse' : 'modelLeaderboard.detail')}`"
                          @click="expanded = expanded === model.id ? '' : model.id"
                        >
                          <span class="table-logo"
                            ><ModelIcon :model="model.slug" size="21px" /></span
                          ><span class="model-identity"
                            ><strong>{{ model.name }}</strong
                            ><small
                              >{{ model.slug
                              }}<span class="provider-name"> · {{ model.creator }}</span></small
                            ></span
                          ><Icon
                            :name="expanded === model.id ? 'chevronUp' : 'chevronDown'"
                            size="xs"
                          />
                        </button>
                      </td>
                      <td class="score-cell">
                        <strong>{{ formatScore(model.score) }}</strong>
                        <div class="score-track">
                          <span :style="{ width: barWidth(model.score, metric) }"></span>
                        </div>
                        <small v-if="model.overall == null">{{
                          t('modelLeaderboard.partial')
                        }}</small>
                      </td>
                      <td class="profile-column">
                        <div class="mini-profile">
                          <div
                            v-for="key in profileKeys"
                            :key="key"
                            :title="`${t(`modelLeaderboard.${key}`)}: ${formatScore(model.scores[key], key)}`"
                          >
                            <span>{{ t(`modelLeaderboard.${key}`) }}</span>
                            <div><i :style="{ width: barWidth(model.scores[key], key) }"></i></div>
                            <b>{{ formatScore(model.scores[key], key) }}</b>
                          </div>
                        </div>
                      </td>
                      <td>
                        <RouterLink
                          v-if="matches(model.id).length"
                          :to="plazaTarget(model.id)"
                          class="plaza-button"
                          >{{ t('modelLeaderboard.plaza')
                          }}<Icon name="arrowRight" size="xs" /><small>{{
                            t('modelLeaderboard.groups', { count: matches(model.id).length })
                          }}</small></RouterLink
                        ><span v-else class="not-listed">{{
                          t(
                            plazaState === 'ready'
                              ? 'modelLeaderboard.unavailable'
                              : 'modelLeaderboard.missing'
                          )
                        }}</span>
                      </td>
                    </tr>
                    <tr v-if="expanded === model.id" class="detail-row">
                      <td colspan="5">
                        <div class="detail-inner">
                          <div class="detail-scores">
                            <div v-for="category in data.categories" :key="category.key">
                              <span
                                >{{ t(`modelLeaderboard.${category.key}`)
                                }}<b
                                  >{{ formatScore(model.scores[category.key], category.key) }}
                                  {{ scoreUnit(category.key) }}</b
                                ></span
                              >
                              <div class="detail-track">
                                <i
                                  :style="{
                                    width: barWidth(model.scores[category.key], category.key)
                                  }"
                                ></i>
                              </div>
                            </div>
                          </div>
                          <p class="detail-prices">
                            {{ t('modelLeaderboard.referencePrice') }} ·
                            {{ t('modelLeaderboard.inputPrice') }}
                            {{ formatPrice(model.input_price) }} / 1M ·
                            {{ t('modelLeaderboard.outputPrice') }}
                            {{ formatPrice(model.output_price) }} / 1M
                          </p>
                          <div v-if="matches(model.id).length" class="group-links">
                            <RouterLink
                              v-for="group in matches(model.id)"
                              :key="group.id"
                              :to="{
                                path: '/model-plaza',
                                query: { model: group.model, group: String(group.id) }
                              }"
                              >{{ group.name }}<Icon name="arrowRight" size="xs"
                            /></RouterLink>
                          </div>
                          <p>{{ t('modelLeaderboard.matchNote') }}</p>
                        </div>
                      </td>
                    </tr>
                  </template>
                </tbody>
              </table>
              <div v-if="filtered.length === 0" class="empty-ranking">
                <Icon name="search" size="lg" />
                <p>{{ t('modelLeaderboard.noResults') }}</p>
                <button @click="resetFilters">{{ t('modelLeaderboard.reset') }}</button>
              </div>
            </div>
            <button
              v-if="filtered.length > visibleRows.length"
              class="load-more"
              @click="displayLimit += 25"
            >
              {{ t('modelLeaderboard.loadMore') }}
            </button>
            <div class="board-footer">
              <span>{{
                t('modelLeaderboard.showing', { count: visibleRows.length, total: ranked.length })
              }}</span
              ><span>{{ t('modelLeaderboard.matchHint') }}</span>
            </div>
          </div>
        </section>

        <section id="sources" class="source-section">
          <div class="source-heading">
            <span class="source-symbol" aria-hidden="true">✳</span>
            <div>
              <h2>{{ t('modelLeaderboard.sourceTitle') }}</h2>
              <p>{{ t('modelLeaderboard.sourceSubtitle') }}</p>
            </div>
          </div>
          <div class="source-grid">
            <div>
              <h3><Icon name="globe" size="sm" />{{ t('modelLeaderboard.sourceName') }}</h3>
              <a
                :href="data.source_url"
                target="_blank"
                rel="noopener noreferrer"
                class="source-title"
                >{{ data.source }}<Icon name="externalLink" size="sm"
              /></a>
              <p>{{ t('modelLeaderboard.sourceDescription') }}</p>
              <div class="source-links">
                <a :href="data.methodology_url" target="_blank" rel="noopener noreferrer"
                  >{{ t('modelLeaderboard.methodology') }} ↗</a
                ><a :href="data.data_url" target="_blank" rel="noopener noreferrer"
                  >{{ t('modelLeaderboard.rawData') }} ↗</a
                ><a href="https://artificialanalysis.ai/" target="_blank" rel="noopener noreferrer"
                  >{{ t('modelLeaderboard.attribution') }} ↗</a
                >
              </div>
            </div>
            <div>
              <h3><Icon name="clock" size="sm" />{{ t('modelLeaderboard.refresh') }}</h3>
              <dl>
                <div>
                  <dt>{{ t('modelLeaderboard.snapshotDate') }}</dt>
                  <dd>{{ snapshotDate }}</dd>
                </div>
                <div>
                  <dt>{{ t('modelLeaderboard.synced') }}</dt>
                  <dd>{{ syncedTime }}</dd>
                </div>
              </dl>
              <p>
                {{
                  t(
                    data.sync_enabled
                      ? 'modelLeaderboard.schedule'
                      : 'modelLeaderboard.scheduleDisabled',
                    { hours: data.refresh_interval_hours }
                  )
                }}
              </p>
              <p>{{ t('modelLeaderboard.scheduleFailure') }}</p>
            </div>
          </div>
          <details class="methodology-details">
            <summary>
              {{ t('modelLeaderboard.metricsTitle') }}<Icon name="chevronDown" size="sm" />
            </summary>
            <div class="weight-tags">
              <span v-for="category in data.categories" :key="category.key"
                >{{ t(`modelLeaderboard.${category.key}`) }} <b>{{ category.name }}</b></span
              >
            </div>
            <p>{{ t('modelLeaderboard.methodologyNote') }}</p>
            <p>{{ t('modelLeaderboard.rankNote') }}</p>
          </details>
          <p class="scope-note">
            <Icon name="infoCircle" size="sm" />{{ t('modelLeaderboard.scopeNote') }}
          </p>
        </section>
      </template>
      <footer class="rank-footer">
        <span>GLOBAL MODEL INDEX<span class="footer-star">✦</span></span
        ><span>{{ t('modelLeaderboard.footer') }}</span>
      </footer>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import { getModelLeaderboard, type ModelLeaderboardResponse } from '@/api/modelLeaderboard'
import { getModelPlaza, type ModelPlazaGroup } from '@/api/modelPlaza'
import { matchLeaderboardGroups, rankLeaderboard } from '@/utils/modelLeaderboard'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'

const { t, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const data = ref<ModelLeaderboardResponse | null>(null)
const loading = ref(true)
const loadFailed = ref(false)
const groups = ref<ModelPlazaGroup[]>([])
const plazaState = ref<'loading' | 'ready' | 'disabled' | 'login' | 'failed'>('loading')
const search = ref('')
const provider = ref('all')
const availableOnly = ref(false)
const displayLimit = ref(25)
const expanded = ref('')
const metric = ref('overall')
const controller = new AbortController()
let refreshTimer: ReturnType<typeof setTimeout> | undefined
let refreshPolls = 0
const profileKeys = ['coding', 'long_context', 'knowledge']
const metrics = [
  { key: 'overall', icon: 'sparkles' },
  { key: 'coding', icon: 'terminal' },
  { key: 'reasoning', icon: 'lightbulb' },
  { key: 'knowledge', icon: 'book' },
  { key: 'long_context', icon: 'globe' },
  { key: 'speed', icon: 'bolt' },
  { key: 'latency', icon: 'clock' }
] as const
const currentCategory = computed(() =>
  data.value?.categories.find((item) => item.key === metric.value)
)
const intelligenceCount = computed(
  () => data.value?.models.filter((model) => model.overall != null).length ?? 0
)
const snapshotDate = computed(() => data.value?.synced_at.slice(0, 10) ?? '—')
const backTarget = computed(() =>
  authStore.isAuthenticated ? (authStore.isAdmin ? '/admin/dashboard' : '/dashboard') : '/home'
)
const ranked = computed(() =>
  rankLeaderboard(data.value?.models ?? [], metric.value, currentCategory.value?.ascending)
)
const providers = computed(() =>
  [...new Set((data.value?.models ?? []).map((model) => model.creator))].sort()
)
const matchedGroups = computed(
  () =>
    new Map(
      (data.value?.models ?? []).map((model) => [
        model.id,
        matchLeaderboardGroups(model, groups.value)
      ])
    )
)
const matches = (id: string) => matchedGroups.value.get(id) ?? []
const plazaTarget = (id: string) => ({
  path: '/model-plaza',
  query: { model: matches(id)[0]?.model ?? '' }
})
const filtered = computed(() => {
  const query = search.value.trim().toLowerCase()
  return ranked.value.filter(
    (model) =>
      (!query || `${model.slug} ${model.name} ${model.creator}`.toLowerCase().includes(query)) &&
      (provider.value === 'all' || model.creator === provider.value) &&
      (!availableOnly.value || matches(model.id).length > 0)
  )
})
const visibleRows = computed(() => filtered.value.slice(0, displayLimit.value))
watch([search, provider, availableOnly, metric], () => {
  displayLimit.value = 25
  expanded.value = ''
})
const metricMaximums = computed(() =>
  Object.fromEntries(
    (data.value?.categories ?? []).map((category) => [
      category.key,
      Math.max(1, ...(data.value?.models.map((model) => model.scores[category.key] ?? 0) ?? []))
    ])
  )
)
const plazaStateText = computed(() =>
  t(
    {
      loading: 'modelLeaderboard.plazaLoading',
      ready: 'modelLeaderboard.matchHint',
      disabled: 'modelLeaderboard.plazaDisabled',
      login: 'modelLeaderboard.loginToMatch',
      failed: 'modelLeaderboard.plazaFailed'
    }[plazaState.value]
  )
)
const syncedTime = computed(() =>
  data.value
    ? new Intl.DateTimeFormat(locale.value === 'zh' ? 'zh-CN' : 'en-US', {
        dateStyle: 'medium',
        timeStyle: 'short',
        timeZone: 'Asia/Shanghai'
      }).format(new Date(data.value.synced_at)) + ' (UTC+8)'
    : '—'
)
function scoreUnit(key: string) {
  const unit = data.value?.categories.find((item) => item.key === key)?.unit
  return unit === 'percent'
    ? '%'
    : unit === 'index'
      ? t('modelLeaderboard.indexUnit')
      : (unit ?? '')
}
function formatScore(score: number | null | undefined, key = metric.value) {
  return score == null ? '—' : score.toFixed(key === 'latency' ? 2 : key === 'speed' ? 0 : 1)
}
function barWidth(score: number | null | undefined, key: string) {
  if (score == null) return '0%'
  const category = data.value?.categories.find((item) => item.key === key)
  const maximum = category?.unit === 'percent' ? 100 : (metricMaximums.value[key] ?? 1)
  return `${Math.min(100, (score / maximum) * 100)}%`
}
function formatPrice(value: number | null) {
  return value == null ? '—' : `$${value.toFixed(2)}`
}
function resetFilters() {
  search.value = ''
  provider.value = 'all'
  availableOnly.value = false
}

async function loadLeaderboard() {
  clearTimeout(refreshTimer)
  loading.value = true
  loadFailed.value = false
  try {
    data.value = await getModelLeaderboard(controller.signal)
    // 仅在当前页面确有后台同步时短暂轮询，离开页面立即取消。
    if (data.value.refreshing && refreshPolls++ < 12 && !controller.signal.aborted) {
      refreshTimer = setTimeout(loadLeaderboard, 5000)
    }
  } catch {
    if (!controller.signal.aborted) loadFailed.value = true
  } finally {
    loading.value = false
  }
}

async function loadPlaza() {
  plazaState.value = 'loading'
  groups.value = []
  availableOnly.value = false
  try {
    await appStore.fetchPublicSettings()
    const settings = appStore.cachedPublicSettings
    if (settings?.model_plaza_enabled !== true) {
      plazaState.value = appStore.publicSettingsLoaded ? 'disabled' : 'failed'
      return
    }
    if (settings.model_plaza_require_auth && !authStore.isAuthenticated) {
      plazaState.value = 'login'
      return
    }
    groups.value = (await getModelPlaza({ signal: controller.signal })).groups
    plazaState.value = 'ready'
  } catch {
    if (!controller.signal.aborted) plazaState.value = 'failed'
  }
}
onMounted(() => {
  void loadLeaderboard()
  void loadPlaza()
})
onBeforeUnmount(() => {
  controller.abort()
  clearTimeout(refreshTimer)
})
</script>

<style scoped>
.model-leaderboard {
  --rank-gold: #e6c98b;
  --rank-purple: #b59ae8;
  --rank-muted: #9a96a8;
  position: relative;
  isolation: isolate;
  min-height: 100vh;
  overflow: clip;
  background: #0b0a10;
  color: #eeedf3;
  font-family:
    Inter,
    -apple-system,
    BlinkMacSystemFont,
    'Segoe UI',
    sans-serif;
}
.model-leaderboard button,
.model-leaderboard a,
.model-leaderboard input,
.model-leaderboard select {
  -webkit-tap-highlight-color: transparent;
}
.model-leaderboard :is(a, button, input, select, summary):focus-visible {
  outline: 2px solid #c6a4fa;
  outline-offset: 4px;
}
.ambient {
  position: absolute;
  z-index: -1;
  pointer-events: none;
  border-radius: 50%;
  filter: blur(100px);
}
.ambient-purple {
  width: 700px;
  height: 600px;
  right: -260px;
  top: -180px;
  background: #6131ad24;
}
.ambient-gold {
  width: 500px;
  height: 300px;
  left: -350px;
  top: 470px;
  background: #a6812720;
}
.rank-nav {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  max-width: 1320px;
  margin: auto;
  padding: 16px 40px;
  border-bottom: 1px solid #ffffff0c;
}
.rank-brand {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  color: #f2e5c9;
  font-size: 18px;
  font-weight: 650;
  letter-spacing: 0.04em;
}
.brand-mark {
  display: grid;
  place-items: center;
  width: 33px;
  height: 33px;
  color: var(--rank-gold);
  background: #d8b66b0c;
  border: 1px solid #d8b66b40;
  border-radius: 9px;
}
.edition {
  margin-left: 12px;
  padding-left: 20px;
  border-left: 1px solid #ffffff20;
  font:
    10px ui-monospace,
    monospace;
  letter-spacing: 0.12em;
  color: #858091;
}
.nav-actions {
  display: flex;
  align-items: center;
  gap: 30px;
  font-size: 12px;
}
.nav-source,
.back-link {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: #b8b2c6;
}
.back-link {
  padding: 9px 13px;
  border: 1px solid #ffffff16;
  border-radius: 8px;
}
.rank-main {
  max-width: 1240px;
  margin: auto;
  padding: 0 40px;
}
.rank-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 24px 0;
}
.hero-copy {
  min-width: 0;
}
.eyebrow {
  display: flex;
  align-items: center;
  gap: 9px;
  color: #c1a977;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.18em;
}
h1 {
  margin: 0 0 12px;
  font-size: clamp(36px, 4.2vw, 52px);
  line-height: 1.2;
  letter-spacing: -0.025em;
  font-weight: 750;
}
h1 > span:first-child {
  background: linear-gradient(100deg, #f3dfac, #d8b776 50%, #b893df);
  background-clip: text;
  -webkit-text-fill-color: transparent;
}
.hero-spark {
  display: inline-block;
  margin-left: 12px;
  vertical-align: middle;
  color: #cfb0ef;
  font-size: 22px;
  font-weight: 400;
  animation: float-star 7s ease-in-out infinite;
}
.hero-subtitle {
  max-width: 680px;
  color: #a09aaf;
  font-size: 13px;
  line-height: 1.7;
}
.hero-source {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
  color: #9e98aa;
  font-size: 11px;
}
.hero-source > span:nth-child(2) {
  color: #d6cfdf;
  font-weight: 600;
}
.source-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #b5c89e;
}
.source-divider {
  color: #4d4659;
}
.hero-art {
  position: relative;
  width: 176px;
  flex-shrink: 0;
}
.orbit-glow {
  position: absolute;
  inset: 20%;
  background: #aa73e225;
  filter: blur(45px);
  border-radius: 50%;
}
.orbit-globe {
  position: relative;
  width: 100%;
  animation: float-star 9s ease-in-out infinite;
}
.rank-stats {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr 1.25fr;
  padding: 12px 0;
  border-top: 1px solid #ffffff12;
  border-bottom: 1px solid #ffffff12;
}
.rank-stats > div {
  display: flex;
  align-items: baseline;
  gap: 12px;
  border-right: 1px solid #ffffff0d;
  padding-left: 34px;
}
.rank-stats > div:first-child {
  padding-left: 0;
}
.rank-stats strong {
  color: #e1dce9;
  font-size: 26px;
  font-weight: 450;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.04em;
}
.rank-stats strong span,
.rank-stats strong small {
  color: #b59966;
  font-size: 15px;
  margin-left: 4px;
}
.rank-stats > div > span {
  color: #8f899c;
  font-size: 11px;
}
.rank-stats > .release-stat {
  border: 0;
  flex-direction: column;
  gap: 3px;
  align-items: flex-end;
}
.release-stat strong {
  font:
    15px ui-monospace,
    monospace;
  letter-spacing: 0.05em;
}
.release-stat > span:last-child {
  font-size: 8px;
  letter-spacing: 0.2em;
}
.sync-notice {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  margin-top: 18px;
  border: 1px solid #d8b66b30;
  border-radius: 8px;
  color: #d4bc89;
  background: #d8b66b08;
  font-size: 12px;
}
.ranking-section {
  margin-top: 24px;
}
.section-heading {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 20px;
  margin-bottom: 16px;
}
.muted {
  color: #827392;
  font-size: 9px;
  letter-spacing: 0.2em;
}
h2 {
  margin-top: 9px;
  font-size: 25px;
  font-weight: 600;
  letter-spacing: -0.025em;
}
.heading-star {
  margin-left: 13px;
  color: #b297d0;
  font-size: 24px;
  font-weight: 400;
}
.section-note {
  color: #8b8498;
  font-size: 11px;
  padding-bottom: 4px;
}
.metric-tabs {
  display: flex;
  gap: 5px;
  overflow-x: auto;
  padding: 5px;
  border: 1px solid #ffffff0b;
  border-radius: 12px;
  background: #ffffff03;
  scrollbar-width: thin;
}
.metric-tabs button {
  display: flex;
  flex: 1 0 auto;
  justify-content: center;
  align-items: center;
  gap: 7px;
  padding: 11px 12px;
  border: 1px solid transparent;
  border-radius: 8px;
  color: #aaa2b7;
  font-size: 12px;
  transition:
    background 0.2s,
    color 0.2s;
}
.metric-tabs button:hover {
  background: #ffffff08;
  color: #ebe4f3;
}
.metric-tabs button.active {
  color: #ead1a0;
  background: linear-gradient(120deg, #b99a5820, #b99a580c);
  border-color: #c0a36c3a;
  box-shadow: 0 2px 6px #0002;
}
.metric-explanation {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-height: 40px;
  padding: 12px 3px;
  color: #9790a4;
  font-size: 11px;
  line-height: 1.7;
}
.tiny-line {
  width: 12px;
  height: 1px;
  background: #ac9363;
  flex-shrink: 0;
}
.podium {
  display: grid;
  grid-template-columns: 1.08fr 1fr 1fr;
  gap: 16px;
  margin: 10px 0 30px;
}
.podium-card {
  display: flex;
  flex-direction: column;
  position: relative;
  min-width: 0;
  overflow: hidden;
  padding: 21px 24px 0;
  border: 1px solid #a780d42c;
  border-radius: 14px;
  background: radial-gradient(ellipse at 100% 0%, #7a4aa51b, transparent 70%), #14111c;
  transition:
    transform 0.25s,
    border-color 0.25s;
}
.podium-card:hover {
  transform: translateY(-4px);
  border-color: #a780d473;
}
.podium-1 {
  border-color: #c9a8624a;
  background:
    radial-gradient(ellipse at 90% 0%, #bca05b19, transparent 65%),
    linear-gradient(130deg, #1c181c, #15101d);
}
.podium-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.podium-place {
  color: #9885b5;
  font:
    italic 26px Georgia,
    serif;
}
.podium-1 .podium-place {
  color: var(--rank-gold);
}
.podium-tag {
  display: flex;
  align-items: center;
  gap: 5px;
  color: #9d87b7;
  font-size: 8px;
  letter-spacing: 0.16em;
}
.podium-1 .podium-tag {
  color: #ddbc7e;
}
.podium-model {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 24px;
  font-size: 11px;
  color: #b1a7be;
}
.model-logo {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  background: #eee9e3;
  border-radius: 11px;
  box-shadow: 0 4px 20px #0003;
}
.podium h3 {
  margin-top: 13px;
  font-size: clamp(15px, 1.6vw, 20px);
  font-weight: 600;
  overflow-wrap: anywhere;
}
.podium-bottom {
  margin-top: auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 19px 0 23px;
}
.podium-bottom strong {
  color: #d2bceb;
  font-size: 36px;
  font-weight: 450;
  letter-spacing: -0.05em;
  font-variant-numeric: tabular-nums;
}
.podium-1 .podium-bottom strong {
  color: #f0d69f;
}
.podium-bottom small {
  color: #82738f;
  font-size: 10px;
  margin-left: 7px;
}
.podium-caption {
  color: #8e809c;
  font-size: 10px;
}
.podium-link {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border: 1px solid #cfb37340;
  border-radius: 50%;
  color: #e6c98b;
}
.podium-link:hover {
  background: #d6b06620;
}
.podium-track {
  height: 2px;
  margin: 0 -24px;
  background: #ffffff05;
}
.podium-track > span {
  display: block;
  height: 100%;
  background: linear-gradient(90deg, #69469b, #c3a4e8);
}
.podium-1 .podium-track > span {
  background: linear-gradient(90deg, #755931, #e0c085);
}
.ranking-board {
  border: 1px solid #ffffff10;
  border-radius: 14px;
  background: #101016;
  overflow: hidden;
}
.board-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 20px;
}
.rank-search {
  display: flex;
  align-items: center;
  gap: 9px;
  color: #898194;
}
.rank-search input {
  width: 225px;
  padding: 4px 0;
  border: 0;
  background: transparent;
  color: #e5ddef;
  font-size: 12px;
  box-shadow: none;
}
.rank-search input::placeholder {
  color: #898194;
}
.board-filters {
  display: flex;
  align-items: center;
  gap: 20px;
}
.board-filters select {
  color-scheme: dark;
  max-width: 160px;
  padding: 6px 28px 6px 10px;
  border: 1px solid #ffffff13;
  border-radius: 7px;
  background-color: #17131f;
  color: #bfb4d0;
  font-size: 11px;
}
.available-filter {
  display: flex;
  align-items: center;
  gap: 7px;
  color: #b1a6bf;
  font-size: 11px;
  white-space: nowrap;
}
.available-filter input {
  width: 13px;
  height: 13px;
  border-radius: 3px;
  accent-color: #b99a6a;
  color-scheme: dark;
}
.available-filter:has(input:disabled) {
  opacity: 0.5;
}
.plaza-status {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 20px 14px;
  color: #a395b6;
  font-size: 11px;
}
.plaza-status a,
.plaza-status button {
  color: #d0b47a;
}
.board-scroll {
  overflow-x: auto;
  scrollbar-width: thin;
}
.rank-table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
}
.rank-table th {
  padding: 13px 20px;
  border-top: 1px solid #ffffff06;
  background: #ffffff04;
  color: #8d829d;
  font-size: 10px;
  font-weight: 500;
  white-space: nowrap;
}
.rank-table td {
  padding: 18px 20px;
  border-top: 1px solid #ffffff07;
}
.rank-table tbody > tr:hover {
  background: #b69adc04;
}
.rank-table th:first-child,
.rank-table td:first-child {
  width: 70px;
}
.position {
  font:
    14px ui-monospace,
    monospace;
  color: #7f738f;
}
.position.top-position {
  color: #d5b980;
}
.model-name-button {
  display: flex;
  align-items: center;
  gap: 12px;
  text-align: left;
  width: 100%;
}
.table-logo {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 33px;
  height: 33px;
  border-radius: 9px;
  background: #eee9e3;
}
.model-identity {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}
.model-identity strong {
  color: #e4deed;
  font-size: 12px;
  font-weight: 550;
  overflow-wrap: anywhere;
}
.model-identity small {
  font:
    9px ui-monospace,
    monospace;
  color: #8e829d;
  overflow-wrap: anywhere;
}
.model-name-button > svg {
  margin-left: auto;
  flex-shrink: 0;
  color: #756385;
}
.score-column {
  width: 115px;
}
.score-cell strong {
  color: #decca5;
  font:
    18px ui-monospace,
    monospace;
  letter-spacing: -0.04em;
}
.score-cell > small {
  display: block;
  color: #b19b77;
  font-size: 8px;
  padding-top: 5px;
}
.score-track {
  height: 3px;
  max-width: 72px;
  margin-top: 7px;
  background: #ffffff08;
  border-radius: 4px;
  overflow: hidden;
}
.score-track > span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #94723d, #d6b879);
}
.profile-column {
  width: 175px;
}
.mini-profile {
  display: grid;
  gap: 5px;
}
.mini-profile > div {
  display: flex;
  align-items: center;
  gap: 7px;
}
.mini-profile span {
  color: #9e91ab;
  font-size: 8px;
  min-width: 34px;
}
.mini-profile > div > div {
  height: 3px;
  width: 55px;
  background: #ffffff06;
  border-radius: 3px;
}
.mini-profile i {
  display: block;
  height: 100%;
  background: #9271b2;
  border-radius: 3px;
}
.mini-profile b {
  color: #b2a3c2;
  font:
    8px ui-monospace,
    monospace;
  font-weight: 400;
}
.plaza-button {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 5px;
  color: #e1c68c;
  font-size: 11px;
  white-space: nowrap;
}
.plaza-button small {
  display: block;
  flex-basis: 100%;
  color: #a599b0;
  font-size: 9px;
}
.plaza-button:hover {
  color: #ffe4ac;
}
.not-listed {
  font-size: 11px;
  color: #82758f;
  white-space: nowrap;
}
.load-more {
  display: block;
  margin: 18px auto;
  padding: 9px 24px;
  border: 1px solid #ac8ac447;
  border-radius: 7px;
  color: #c4a8df;
  font-size: 12px;
}
.board-footer {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  padding: 15px 20px;
  border-top: 1px solid #ffffff0a;
  font-size: 9px;
  line-height: 1.7;
  color: #93889f;
}
.expanded {
  background: #ad84d508;
}
.detail-row > td {
  padding: 0 !important;
  background: #0e0c14;
}
.detail-inner {
  padding: 22px 24px 22px 90px;
}
.detail-scores {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 18px 26px;
}
.detail-scores > div > span {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 10px;
  color: #b6a6c6;
}
.detail-scores b {
  font-weight: 400;
  color: #d6bddf;
}
.detail-track {
  height: 4px;
  margin-top: 9px;
  background: #ffffff07;
  border-radius: 4px;
}
.detail-track i {
  display: block;
  height: 100%;
  background: linear-gradient(90deg, #6c4d8f, #bb97d9);
  border-radius: inherit;
}
.detail-inner > p {
  margin-top: 18px;
  color: #91829f;
  font-size: 10px;
  line-height: 1.7;
}
.group-links {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 22px;
}
.group-links a {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border: 1px solid #c3a16333;
  border-radius: 6px;
  color: #d9be88;
  font-size: 11px;
}
.empty-ranking {
  display: grid;
  justify-items: center;
  gap: 15px;
  padding: 65px 20px;
  font-size: 13px;
  color: #a99ab9;
}
.empty-ranking button {
  color: #e0bd7d;
}
.source-section {
  margin-top: 58px;
  padding: 30px;
  border: 1px solid #ffffff0d;
  border-radius: 14px;
  background: linear-gradient(120deg, #b08dd807, #d3b37303);
  scroll-margin-top: 24px;
}
.source-heading {
  display: flex;
  gap: 16px;
  align-items: center;
  margin-bottom: 30px;
}
.source-symbol {
  font-size: 39px;
  color: #b9a1d4;
  font-weight: 300;
}
.source-heading h2 {
  margin: 0;
  font-size: 19px;
  color: #e2d9eb;
}
.source-heading p {
  font-size: 11px;
  color: #94849f;
  margin-top: 6px;
}
.source-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 50px;
}
.source-grid h3 {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  color: #a89ab6;
  margin-bottom: 14px;
}
.source-title {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  color: #e4c78f;
  font-size: 16px;
}
.source-grid p,
.methodology-details p {
  margin-top: 10px;
  font-size: 11px;
  line-height: 1.9;
  color: #a496b0;
}
.source-links {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 16px;
  font-size: 10px;
  color: #c7acd8;
}
.source-grid dl > div {
  display: flex;
  justify-content: space-between;
  gap: 15px;
  margin-bottom: 10px;
  font-size: 11px;
}
.source-grid dt {
  color: #9b8ba9;
}
.source-grid dd {
  color: #dbd1e4;
  font-variant-numeric: tabular-nums;
}
.methodology-details {
  margin-top: 28px;
  border-top: 1px solid #ffffff0c;
  padding-top: 18px;
}
.methodology-details summary {
  display: flex;
  justify-content: space-between;
  align-items: center;
  color: #bcadca;
  font-size: 11px;
  cursor: pointer;
}
.weight-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 18px;
}
.weight-tags > span {
  padding: 6px 10px;
  background: #ffffff04;
  border: 1px solid #ffffff0a;
  border-radius: 5px;
  font-size: 10px;
  color: #b2a1c1;
}
.weight-tags b {
  margin-left: 6px;
  color: #d9bd89;
  font-weight: 400;
}
.scope-note {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  padding-top: 20px;
  margin-top: 18px;
  border-top: 1px solid #ffffff0c;
  color: #a291af;
  font-size: 10px;
  line-height: 1.8;
}
.scope-note > svg {
  flex-shrink: 0;
  margin-top: 2px;
}
.rank-footer {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  padding: 35px 0;
  font-size: 9px;
  color: #8c7c9b;
}
.rank-footer > span:first-child {
  letter-spacing: 0.14em;
  font-family: ui-monospace, monospace;
}
.footer-star {
  color: #bea275;
  margin-left: 12px;
}
.rank-state {
  min-height: 330px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 20px;
  color: #c7b4d9;
  font-size: 13px;
}
.gold-button {
  padding: 9px 20px;
  border: 1px solid #d7b97a55;
  background: #d7b97a10;
  color: #e3c58d;
  border-radius: 7px;
}
.loading-orbit {
  width: 36px;
  height: 36px;
  border: 1px solid #9775b538;
  border-top-color: #d4b984;
  border-radius: 50%;
  animation: spin-orbit 1s linear infinite;
}
@keyframes float-star {
  50% {
    transform: translateY(-9px);
  }
}
@keyframes spin-orbit {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 1050px) {
  .rank-main {
    padding: 0 26px;
  }
  .rank-nav {
    padding: 16px 26px;
  }
  .hero-art {
    width: 140px;
  }
  .rank-stats > div {
    padding-left: 20px;
    gap: 10px;
  }
  .profile-column {
    display: none;
  }
  .rank-table th,
  .rank-table td {
    padding-left: 16px;
    padding-right: 16px;
  }
  .podium-card {
    padding: 18px 18px 0;
  }
  .podium-track {
    margin: 0 -18px;
  }
  .provider-name {
    display: none;
  }
  .source-grid {
    gap: 28px;
  }
}
@media (max-width: 700px) {
  .rank-main {
    padding: 0 18px;
  }
  .rank-nav {
    padding: 12px 18px;
  }
  .rank-brand {
    gap: 8px;
    font-size: 14px;
  }
  .edition,
  .nav-source {
    display: none;
  }
  .nav-actions {
    gap: 10px;
  }
  .back-link {
    padding: 7px 9px;
    font-size: 10px;
  }
  .rank-hero {
    padding: 22px 0 18px;
  }
  .hero-art {
    display: none;
  }
  .hero-copy {
    width: 100%;
  }
  h1 {
    font-size: clamp(28px, 8.5vw, 40px);
    margin-bottom: 10px;
  }
  .hero-spark {
    display: none;
  }
  .hero-subtitle {
    font-size: 12px;
    max-width: 100%;
  }
  .hero-source {
    font-size: 10px;
    gap: 8px;
  }
  .rank-stats {
    padding: 12px 0;
    grid-template-columns: 1fr 1fr 1fr;
  }
  .rank-stats > div {
    flex-direction: column;
    gap: 3px;
    padding-left: 22px;
  }
  .rank-stats > div:nth-child(3) {
    border: 0;
  }
  .rank-stats > .release-stat {
    grid-column: 1 / -1;
    flex-direction: row;
    justify-content: space-between;
    align-items: center;
    margin-top: 10px;
    padding: 9px 0 0;
    border-top: 1px solid #ffffff0b;
  }
  .release-stat > span:last-child {
    display: none;
  }
  .release-stat strong {
    font-size: 12px;
  }
  .rank-stats strong {
    font-size: 24px;
  }
  .rank-stats .release-stat strong {
    font-size: 12px;
  }
  .rank-stats > div > span {
    font-size: 10px;
  }
  .ranking-section {
    margin-top: 20px;
  }
  .section-note {
    display: none;
  }
  .section-heading {
    margin-bottom: 14px;
  }
  h2 {
    font-size: 23px;
  }
  .metric-tabs button {
    padding: 10px 13px;
    font-size: 11px;
  }
  .metric-tabs {
    border-radius: 10px;
  }
  .metric-explanation {
    min-height: 56px;
    font-size: 10px;
  }
  .podium {
    gap: 8px;
    grid-template-columns: 1fr 1fr 1fr;
    margin-bottom: 23px;
  }
  .podium-card {
    border-radius: 10px;
    padding: 12px 10px 0;
  }
  .podium-place {
    font-size: 23px;
  }
  .podium-tag {
    font-size: 6px;
    letter-spacing: 0;
  }
  .podium-tag > svg {
    display: none;
  }
  .podium-model {
    margin-top: 17px;
    gap: 6px;
    flex-direction: column;
    align-items: flex-start;
    font-size: 8px;
  }
  .model-logo {
    width: 30px;
    height: 30px;
    border-radius: 8px;
  }
  .model-logo:deep(svg) {
    width: 20px;
    height: 20px;
  }
  .podium h3 {
    font-size: 11px;
    min-height: 34px;
    line-height: 1.5;
    margin-top: 8px;
  }
  .podium-bottom {
    padding: 12px 0 16px;
  }
  .podium-bottom strong {
    font-size: 26px;
  }
  .podium-bottom small {
    display: none;
  }
  .podium-caption {
    display: none;
  }
  .podium-link {
    width: 22px;
    height: 22px;
  }
  .podium-link > svg {
    width: 12px;
  }
  .podium-track {
    margin: 0 -10px;
  }
  .board-toolbar {
    padding: 15px 13px;
    flex-wrap: wrap;
    gap: 15px;
  }
  .rank-search {
    width: 100%;
  }
  .rank-search input {
    width: 100%;
    font-size: 12px;
  }
  .board-filters {
    width: 100%;
    justify-content: space-between;
    gap: 10px;
  }
  .available-filter {
    font-size: 10px;
  }
  .rank-table {
    min-width: 470px;
  }
  .rank-table th,
  .rank-table td {
    padding: 15px 11px;
  }
  .rank-table th:first-child,
  .rank-table td:first-child {
    width: 40px;
  }
  .model-name-button {
    gap: 8px;
  }
  .model-identity strong {
    font-size: 11px;
  }
  .model-identity small {
    font-size: 8px;
  }
  .table-logo {
    width: 27px;
    height: 27px;
    border-radius: 7px;
  }
  .table-logo:deep(svg) {
    width: 17px;
    height: 17px;
  }
  .score-cell strong {
    font-size: 16px;
  }
  .score-column {
    width: 73px;
  }
  .position {
    font-size: 12px;
  }
  .plaza-button,
  .not-listed {
    font-size: 10px;
  }
  .board-footer {
    flex-direction: column;
    gap: 6px;
    padding: 14px;
  }
  .detail-inner {
    padding: 20px 16px;
  }
  .detail-scores {
    grid-template-columns: repeat(2, 1fr);
  }
  .source-section {
    margin-top: 35px;
    padding: 22px 18px;
  }
  .source-heading {
    gap: 12px;
  }
  .source-heading h2 {
    font-size: 16px;
  }
  .source-symbol {
    font-size: 30px;
  }
  .source-grid {
    grid-template-columns: 1fr;
    gap: 27px;
  }
  .source-grid p {
    font-size: 11px;
  }
  .rank-footer {
    flex-direction: column;
    gap: 12px;
    padding: 26px 0;
  }
  .sync-notice {
    font-size: 11px;
    line-height: 1.6;
  }
}
@media (max-width: 400px) {
  .podium-bottom {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
    min-height: 94px;
  }
  .podium-link {
    align-self: flex-end;
  }
  .podium-bottom strong {
    font-size: 24px;
  }
}
@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation: none !important;
    transition: none !important;
    scroll-behavior: auto !important;
  }
}
</style>
