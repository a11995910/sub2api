<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="card flex flex-wrap items-center justify-between gap-4 p-5">
        <div>
          <h1 class="text-xl font-semibold">{{ t('timeRotation.title') }}</h1>
          <p class="mt-2 text-sm text-gray-500">{{ t('timeRotation.paced.description') }}</p>
        </div>
        <div class="flex items-center gap-3">
          <span v-if="dirty" class="text-sm text-amber-700">{{ t('timeRotation.unsaved') }}</span>
          <button class="btn btn-secondary" :disabled="loading || saving" data-testid="rotation-reload" @click="load()">{{ t('common.refresh') }}</button>
          <button class="btn btn-primary" :disabled="!ready || saving || (replacingLegacy && !replaceConfirmed)" data-testid="rotation-save" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button>
        </div>
      </div>

      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20">
        {{ error }}
        <button v-if="saveConflict" class="ml-2 underline" data-testid="reload-conflict" @click="load(true)">{{ t('timeRotation.reloadAfterConflict') }}</button>
      </p>
      <p v-if="loading" class="p-8 text-center text-gray-500">{{ t('common.loading') }}</p>
      <template v-else-if="ready">
        <div v-if="replacingLegacy" class="rounded-xl border border-amber-200 bg-amber-50 p-5 text-sm text-amber-900 dark:bg-amber-950/30" data-testid="legacy-notice">
          <p>{{ t('timeRotation.paced.legacyNotice') }}</p>
          <label class="mt-3 flex items-start gap-2">
            <input v-model="replaceConfirmed" type="checkbox" :disabled="saving" data-testid="replace-legacy" />
            <span>{{ t('timeRotation.paced.replaceLegacy') }}</span>
          </label>
        </div>

        <div class="grid gap-6 lg:grid-cols-2">
          <section class="card space-y-5 p-5">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <h2 class="font-semibold">{{ t('timeRotation.paced.schedule') }}</h2>
              <label class="flex items-center gap-2 text-sm"><input v-model="enabled" type="checkbox" :disabled="saving" data-testid="rotation-enabled" />{{ t('timeRotation.enabled') }}</label>
            </div>
            <label class="block text-sm">
              {{ t('timeRotation.paced.account') }}
              <input v-model="query" type="search" class="input mt-2" :placeholder="t('timeRotation.search')" :disabled="saving" data-testid="account-search" />
              <select v-model.number="policy.account_id" class="input mt-2" :disabled="saving" data-testid="paced-account">
                <option :value="0">{{ t('timeRotation.paced.chooseAccount') }}</option>
                <option v-for="account in candidates" :key="account.id" :value="account.id">{{ account.name }} · #{{ account.id }}</option>
                <option v-if="policy.account_id && !accounts.some(a => a.id === policy.account_id)" :value="policy.account_id">{{ t('timeRotation.missingAccount', { id: policy.account_id }) }}</option>
              </select>
            </label>
            <p v-if="selectedAccount && (selectedAccount.status !== 'active' || !selectedAccount.schedulable)" class="text-sm text-amber-700">{{ t('timeRotation.paced.accountUnavailable') }}</p>
            <div class="grid grid-cols-2 gap-4">
              <label class="text-sm">{{ t('timeRotation.start') }}<input v-model="policy.start" class="input mt-2" type="time" :disabled="saving" data-testid="paced-start" /></label>
              <label class="text-sm">{{ t('timeRotation.end') }}<input v-model="policy.end" class="input mt-2" type="text" placeholder="18:30" :disabled="saving" data-testid="paced-end" /></label>
            </div>
            <p class="text-xs text-gray-500">{{ t('timeRotation.timezone') }} · {{ t('timeRotation.timeHint') }}</p>
            <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
              <div class="relative h-3 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600" aria-hidden="true">
                <span v-for="(part, index) in scheduleParts" :key="index" class="absolute inset-y-0 bg-primary-500" :style="{ left: `${part.start / 14.4}%`, width: `${part.length / 14.4}%` }" />
              </div>
              <div class="mt-2 flex justify-between text-xs text-gray-500"><span>00:00</span><span>12:00</span><span>24:00</span></div>
              <p class="mt-3 text-sm" data-testid="schedule-summary">{{ t('timeRotation.paced.scheduleSummary', { work: (workMinutes / 60).toFixed(1), rest: ((1440 - workMinutes) / 60).toFixed(1) }) }}</p>
            </div>
            <p class="text-sm leading-6 text-gray-500">{{ t('timeRotation.paced.scheduleHint') }}</p>
          </section>

          <section class="card space-y-5 p-5">
            <h2 class="font-semibold">{{ t('timeRotation.paced.capacity') }}</h2>
            <div class="grid gap-4 sm:grid-cols-2">
              <label v-for="field in fields" :key="field.key" class="text-sm">
                {{ t(`timeRotation.paced.fields.${field.key}`) }}
                <input v-model.number="policy[field.key]" class="input mt-2" type="number" :min="field.min" :max="field.max" step="1" :disabled="saving" :data-testid="field.key" />
              </label>
            </div>
            <p class="rounded-xl bg-primary-50 p-4 text-sm leading-6 text-primary-800 dark:bg-primary-950/20" data-testid="capacity-summary">{{ t('timeRotation.paced.capacitySummary', { rpm: policy.sustained_rpm, burst: policy.burst_capacity, ceiling: policy.rpm, interval: (60 / policy.sustained_rpm).toFixed(1) }) }}</p>
            <p class="text-sm leading-6 text-gray-500">{{ t('timeRotation.paced.capacityHint') }}</p>
            <p class="text-sm leading-6 text-gray-500">{{ t('timeRotation.paced.waitHint') }}</p>
          </section>
        </div>

        <section class="card space-y-4 p-5" data-testid="paced-status">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div><h2 class="font-semibold">{{ t('timeRotation.planPreview') }}</h2><p class="mt-1 text-xs text-gray-500">{{ t('timeRotation.savedStatusHint') }}</p></div>
            <button class="btn btn-secondary" :disabled="statusLoading || saving" data-testid="refresh-status" @click="loadStatus">{{ t('common.refresh') }}</button>
          </div>
          <p v-if="statusError || !status" class="text-sm text-amber-700">{{ t('timeRotation.statusUnavailable') }}</p>
          <template v-else>
            <p class="text-sm">{{ t('timeRotation.savedRevision', { n: status.revision }) }} · {{ statusMessage }}</p>
            <template v-if="live && live.paced && status.ready && status.mode === 'paced'">
              <p class="text-sm font-medium">{{ accountName(live.account_id) }} · {{ liveState }}</p>
              <div class="grid grid-cols-2 gap-3 lg:grid-cols-5">
                <div v-for="metric in liveMetrics" :key="metric.label" class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ metric.label }}</p><p class="mt-2 text-xl font-semibold tabular-nums">{{ metric.value }}</p></div>
              </div>
              <p class="text-sm text-gray-500">{{ t(live.paced.schedule_open ? 'timeRotation.paced.closesAt' : 'timeRotation.paced.opensAt', { time: formatTime(live.paced.next_change_at) }) }}</p>
              <p v-if="live.paced.retry_after > 0 && live.state === 'account_rate_limited'" class="text-sm text-amber-700">{{ t('timeRotation.paced.blocked', { reason: t(`timeRotation.paced.reasons.${live.paced.blocked_by}`), seconds: live.paced.retry_after }) }}</p>
            </template>
          </template>
          <p class="text-xs leading-5 text-gray-500">{{ t('timeRotation.paced.statusHint') }}</p>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { list } from '@/api/admin/accounts'
import { timeRotationAPI, type AccountPacedConfig, type AccountTimeRotationConfig, type TimeRotationStatus } from '@/api/admin/timeRotation'
import type { AccountListItem } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const defaults = (): AccountPacedConfig => ({ account_id: 0, start: '08:30', end: '18:30', rpm: 40, sustained_rpm: 8, burst_capacity: 40, max_concurrency: 12, max_wait_seconds: 15 })
const fields = [
  { key: 'sustained_rpm', min: 1, max: 10000 }, { key: 'burst_capacity', min: 1, max: 10000 },
  { key: 'rpm', min: 1, max: 10000 }, { key: 'max_concurrency', min: 1, max: 1000 },
  { key: 'max_wait_seconds', min: 0, max: 65 }
] as const
const policy = ref(defaults())
const enabled = ref(false)
const saved = ref<AccountTimeRotationConfig | null>(null)
const accounts = ref<AccountListItem[]>([])
const query = ref('')
const loading = ref(false)
const ready = ref(false)
const saving = ref(false)
const error = ref('')
const saveConflict = ref(false)
const replaceConfirmed = ref(false)
const status = ref<TimeRotationStatus | null>(null)
const statusError = ref(false)
const statusLoading = ref(false)
const snapshot = ref('')
const controller = new AbortController()
let statusRequest = 0
let timer: ReturnType<typeof setInterval> | undefined
let mounted = true
const serialize = () => JSON.stringify({ enabled: enabled.value, paced: policy.value })
const dirty = computed(() => ready.value && snapshot.value !== serialize())
const replacingLegacy = computed(() => Boolean(saved.value?.enabled && saved.value.mode !== 'paced'))
const selectedAccount = computed(() => accounts.value.find(a => a.id === policy.value.account_id))
const candidates = computed(() => accounts.value.filter(a => a.id === policy.value.account_id || !query.value.trim() || `${a.name} ${a.id}`.toLowerCase().includes(query.value.trim().toLowerCase())))
const live = computed(() => status.value?.protection?.find(item => item.account_id === saved.value?.paced?.account_id))
const statusMessage = computed(() => {
  if (!status.value?.ready) return t('timeRotation.paced.statusStale')
  if (!status.value.enabled) return t('timeRotation.statusDisabled')
  if (status.value.mode !== 'paced') return t('timeRotation.paced.legacyRunning')
  return t('timeRotation.paced.statusReady')
})
const liveState = computed(() => t(`timeRotation.paced.states.${live.value?.state || 'account_protection_unavailable'}`))
const liveMetrics = computed(() => {
  const item = live.value
  const p = item?.paced
  const known = item && item.used != null && item.state !== 'account_protection_unavailable'
  return [
    { label: t('timeRotation.paced.fields.rpm'), value: known ? `${item.used} / ${item.rpm}` : '—' },
    { label: t('timeRotation.paced.availableTokens'), value: known && p ? `${p.available_tokens.toFixed(1)} / ${p.burst_capacity}` : '—' },
    { label: t('timeRotation.paced.fields.sustained_rpm'), value: known && p ? `${p.sustained_rpm} RPM` : '—' },
    { label: t('timeRotation.paced.fields.max_concurrency'), value: known && p ? `${item.in_flight} / ${p.max_concurrency}` : '—' },
    { label: t('timeRotation.paced.waiting'), value: known && p ? String(p.waiting) : '—' }
  ]
})
function minute(value: string, end = false) {
  if (end && value === '24:00') return 1440
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(value)) return -1
  const [h, m] = value.split(':').map(Number)
  return h * 60 + m
}
const scheduleParts = computed(() => {
  const start = minute(policy.value.start), end = minute(policy.value.end, true)
  if (start < 0 || end < 0 || start === end) return []
  return end > start ? [{ start, length: end - start }] : [{ start, length: 1440 - start }, { start: 0, length: end }]
})
const workMinutes = computed(() => scheduleParts.value.reduce((sum, part) => sum + part.length, 0))
function valid() {
  const p = policy.value
  return Number.isSafeInteger(p.account_id) && p.account_id >= 0 && (!enabled.value || (p.account_id > 0 && !!selectedAccount.value)) &&
    workMinutes.value > 0 && workMinutes.value < 1440 && fields.every(f => Number.isInteger(p[f.key]) && p[f.key] >= f.min && p[f.key] <= f.max) &&
    p.sustained_rpm <= p.rpm && p.burst_capacity <= p.rpm
}
function accountName(id: number) { return accounts.value.find(a => a.id === id)?.name || t('timeRotation.missingAccount', { id }) }
function formatTime(value: string) {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(date) : '—'
}
function apply(value: AccountTimeRotationConfig) {
  saved.value = value
  policy.value = value.mode === 'paced' && value.paced ? { ...value.paced } : defaults()
  enabled.value = value.mode === 'paced' && value.enabled
  replaceConfirmed.value = false
  snapshot.value = serialize()
}
async function loadStatus() {
  const request = ++statusRequest
  statusLoading.value = true
  statusError.value = false
  try {
    const result = await timeRotationAPI.status()
    if (mounted && request === statusRequest) status.value = result
  } catch {
    if (mounted && request === statusRequest) { status.value = null; statusError.value = true }
  } finally {
    if (mounted && request === statusRequest) statusLoading.value = false
  }
}
async function load(force = false) {
  if (!force && dirty.value) { error.value = t('timeRotation.refreshUnsaved'); return }
  loading.value = true
  ready.value = false
  error.value = ''
  saveConflict.value = false
  statusRequest++
  status.value = null
  try {
    const value = await timeRotationAPI.get()
    const all: AccountListItem[] = []
    for (let page = 1; ; page++) {
      const result = await list(page, 100, { platform: 'openai', type: 'oauth', lite: 'true', include_scheduler_score: 'false', sort_by: 'id', sort_order: 'asc' }, { signal: controller.signal })
      all.push(...result.items.filter(a => !a.parent_account_id))
      if (page >= result.pages || !result.items.length) break
    }
    if (!mounted) return
    accounts.value = all
    apply(value)
    ready.value = true
    void loadStatus()
  } catch (err) {
    if (mounted) error.value = extractApiErrorMessage(err, t('timeRotation.loadFailed'))
  } finally { if (mounted) loading.value = false }
}
async function save() {
  if (!ready.value || saving.value || (replacingLegacy.value && !replaceConfirmed.value)) return
  error.value = ''
  saveConflict.value = false
  if (!valid()) { error.value = t('timeRotation.paced.invalid'); return }
  saving.value = true
  statusRequest++
  status.value = null
  statusLoading.value = false
  try {
    const result = await timeRotationAPI.save({ enabled: enabled.value, revision: saved.value?.revision ?? 0, mode: 'paced', paced: { ...policy.value }, slots: [], strict_rest: false, request_limits: [] })
    if (!mounted) return
    apply(result)
    appStore.showSuccess(t('timeRotation.saved'))
    void loadStatus()
  } catch (err) {
    if (!mounted) return
    saveConflict.value = (err as { response?: { status?: number } }).response?.status === 409
    error.value = extractApiErrorMessage(err, t(saveConflict.value ? 'timeRotation.saveConflict' : 'timeRotation.saveFailed'))
  } finally { if (mounted) saving.value = false }
}
onMounted(async () => {
  await load()
  if (mounted) timer = setInterval(() => { if (ready.value && !saving.value && !statusLoading.value) void loadStatus() }, 5000)
})
onBeforeUnmount(() => {
  mounted = false
  controller.abort()
  statusRequest++
  if (timer) clearInterval(timer)
})
</script>
