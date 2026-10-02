<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="card flex flex-wrap items-center justify-between gap-4 p-5">
        <div>
          <div class="flex flex-wrap items-center gap-3">
            <label class="flex cursor-pointer items-center gap-2 font-semibold text-gray-900 dark:text-white">
              <input v-model="config.enabled" type="checkbox" :disabled="!ready || saving" class="h-4 w-4" data-testid="rotation-enabled" />
              {{ t('timeRotation.enabled') }}
            </label>
            <span class="rounded-full bg-primary-50 px-3 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              {{ t('timeRotation.timezone') }}
            </span>
          </div>
          <p class="mt-2 text-sm text-gray-500">{{ t('timeRotation.description') }}</p>
        </div>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="load">
            {{ t('common.refresh') }}
          </button>
          <button type="button" class="btn btn-primary" :disabled="!ready || saving" data-testid="rotation-save" @click="save">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </div>
      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        {{ error }}
      </p>
      <p v-if="loading" class="py-8 text-center text-gray-500">{{ t('common.loading') }}</p>
      <template v-else-if="ready">
        <div class="card p-5">
          <div class="flex flex-wrap items-center gap-3">
            <span class="font-semibold text-gray-900 dark:text-white">{{ t('timeRotation.mode') }}</span>
            <label
              class="flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm"
              :class="config.mode !== 'smart' ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-200 dark:border-dark-600'"
            >
              <input v-model="config.mode" type="radio" value="manual" :disabled="saving" data-testid="rotation-mode-manual" />
              {{ t('timeRotation.manualMode') }}
            </label>
            <label
              class="flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm"
              :class="config.mode === 'smart' ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-200 dark:border-dark-600'"
            >
              <input v-model="config.mode" type="radio" value="smart" :disabled="saving" data-testid="rotation-mode-smart" />
              {{ t('timeRotation.smartMode') }}
            </label>
          </div>
          <p class="mt-3 text-sm text-gray-500">
            {{ config.mode === 'smart' ? t('timeRotation.smartDescription') : t('timeRotation.manualDescription') }}
          </p>
        </div>

        <template v-if="config.mode === 'smart'">
          <section class="card p-5">
            <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
              <div>
                <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('timeRotation.smartAccounts') }}</h2>
                <p class="mt-1 text-xs text-gray-500">{{ t('timeRotation.smartAccountsHint') }}</p>
              </div>
              <span class="text-sm text-primary-600 dark:text-primary-400">
                {{ t('timeRotation.selected', { n: smart.account_ids.length }) }}
              </span>
            </div>
            <div v-if="smart.account_ids.length" class="mb-3 flex max-h-32 flex-wrap gap-2 overflow-y-auto">
              <button
                v-for="id in smart.account_ids"
                :key="id"
                type="button"
                class="rounded-lg bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
                :disabled="saving"
                :data-smart-remove-id="id"
                :aria-label="t('timeRotation.removeAccount', { name: accountName(id) })"
                @click="removeSmartAccount(id)"
              >
                {{ accountName(id) }} <span aria-hidden="true">×</span>
              </button>
            </div>
            <input v-model="smartQuery" class="input mb-3 max-w-md" :placeholder="t('timeRotation.search')" type="search" data-testid="smart-account-search" />
            <div class="grid max-h-72 gap-1 overflow-y-auto sm:grid-cols-2 lg:grid-cols-3">
              <label v-for="account in smartCandidates.slice(0, 200)" :key="account.id" class="flex items-start gap-3 rounded-lg p-2 hover:bg-gray-50 dark:hover:bg-dark-700">
                <input v-model="smart.account_ids" type="checkbox" :value="account.id" class="mt-1 h-4 w-4" :disabled="saving" :data-smart-account-id="account.id" />
                <span class="min-w-0 text-sm">
                  <span class="block break-all text-gray-900 dark:text-gray-100">{{ account.name }}</span>
                  <span class="text-xs text-gray-500">#{{ account.id }}</span>
                </span>
              </label>
              <p v-if="!smartCandidates.length" class="py-6 text-center text-sm text-gray-500 sm:col-span-2 lg:col-span-3">{{ t('timeRotation.empty') }}</p>
            </div>
            <p v-if="smartCandidates.length > 200" class="mt-3 text-xs text-gray-500">{{ t('timeRotation.smartSearchMore') }}</p>
          </section>

          <section class="card p-5">
            <h2 class="mb-4 font-semibold text-gray-900 dark:text-white">{{ t('timeRotation.healthPolicy') }}</h2>
            <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <label v-for="field in healthFields" :key="field.key" class="input-label">
                {{ t(`timeRotation.${field.key}`) }}
                <input v-model.number="smart[field.key]" type="number" :min="field.min" :max="field.max" step="1" class="input mt-1" :disabled="saving" :data-testid="field.key" />
              </label>
            </div>
            <p class="mt-3 text-xs text-gray-500">{{ t('timeRotation.smartSafetyHint') }}</p>
          </section>

          <section class="card p-5" data-testid="rotation-status">
            <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
              <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('timeRotation.planPreview') }}</h2>
              <button type="button" class="btn btn-secondary text-sm" :disabled="statusLoading || saving" data-testid="refresh-status" @click="loadStatus">
                {{ t('timeRotation.refreshPreview') }}
              </button>
            </div>
            <p class="mb-3 text-xs text-gray-500">{{ t('timeRotation.savedStatusHint') }}</p>
            <p v-if="statusError" class="text-sm text-amber-700 dark:text-amber-300">{{ t('timeRotation.statusFailed') }}</p>
            <template v-else-if="status">
              <p class="mb-2 text-sm" :class="status.ready ? 'text-gray-500' : 'text-amber-700 dark:text-amber-300'" data-testid="status-state">
                {{ statusMessage }}
              </p>
              <p class="mb-3 text-xs text-gray-500">
                {{ t('timeRotation.savedRevision', { n: status.revision }) }}
                <span v-if="status.updated_at"> · {{ t('timeRotation.updatedAt') }} {{ formatBeijingTime(status.updated_at) }}</span>
              </p>
              <template v-if="status.enabled && status.mode === 'smart' && status.ready">
                <div class="overflow-x-auto">
                  <table class="w-full text-left text-sm">
                    <thead>
                      <tr class="border-b border-gray-100 text-xs text-gray-500 dark:border-dark-600">
                        <th class="p-2">{{ t('timeRotation.accountName') }}</th>
                        <th class="p-2">{{ t('timeRotation.accountRole') }}</th>
                        <th class="p-2">{{ t('timeRotation.effectivePriority') }}</th>
                        <th class="p-2">{{ t('timeRotation.latestLatency') }}</th>
                        <th class="p-2">{{ t('timeRotation.streaks') }}</th>
                        <th class="p-2">{{ t('timeRotation.recoveryAt') }}</th>
                        <th class="p-2">{{ t('timeRotation.accountReason') }}</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="account in status.accounts" :key="account.account_id" class="border-b border-gray-50 dark:border-dark-700">
                        <td class="p-2">{{ account.name || accountName(account.account_id) }} <span class="text-xs text-gray-500">#{{ account.account_id }}</span></td>
                        <td class="p-2" :class="{ 'text-green-600 dark:text-green-400': account.role === 'normal', 'text-amber-600 dark:text-amber-400': account.role === 'cooling', 'text-blue-600 dark:text-blue-400': account.role === 'recovering' }">{{ t(`timeRotation.role.${account.role}`) }}</td>
                        <td class="p-2">{{ account.effective_priority }} <span class="text-xs text-gray-500">({{ t('timeRotation.originalPriority') }} {{ account.original_priority }})</span></td>
                        <td class="p-2">
                          {{ formatLatency(account.last_ttft_ms) }} / {{ formatLatency(account.last_duration_ms) }}
                          <span v-if="account.last_sample_at" class="block text-xs text-gray-500">{{ formatBeijingTime(account.last_sample_at) }}</span>
                        </td>
                        <td class="p-2">{{ account.slow_streak ?? 0 }} / {{ account.healthy_streak ?? 0 }}</td>
                        <td class="p-2">{{ account.cooldown_until ? formatBeijingTime(account.cooldown_until) : account.next_probe_at ? formatBeijingTime(account.next_probe_at) : '—' }}</td>
                        <td class="p-2 text-gray-500">{{ account.reason }}</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </template>
            </template>
            <p v-else class="text-sm text-gray-500">{{ t('timeRotation.statusUnavailable') }}</p>
            <p class="mt-3 text-xs text-gray-500">{{ t('timeRotation.statusHint') }}</p>
          </section>
        </template>

        <div v-else class="grid gap-5 xl:grid-cols-3">
          <section v-for="(slot, index) in config.slots" :key="index" class="card overflow-hidden" :data-testid="`rotation-slot-${index}`">
            <div class="border-b border-gray-100 bg-gray-50/60 p-5 dark:border-dark-700 dark:bg-dark-800">
              <div class="mb-4 flex items-center justify-between">
                <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('timeRotation.slot', { n: index + 1 }) }}</h2>
                <span class="text-sm text-primary-600 dark:text-primary-400">{{ t('timeRotation.selected', { n: slot.account_ids.length }) }}</span>
              </div>
              <div class="grid grid-cols-2 gap-3">
                <label class="input-label">
                  {{ t('timeRotation.start') }}
                  <input v-model="slot.start" type="time" class="input mt-1" :disabled="saving" />
                </label>
                <label class="input-label">
                  {{ t('timeRotation.end') }}
                  <input v-model="slot.end" type="text" inputmode="numeric" placeholder="24:00" pattern="([01][0-9]|2[0-3]):[0-5][0-9]|24:00" class="input mt-1" :disabled="saving" />
                </label>
                <label class="input-label">
                  {{ t('timeRotation.activePriority') }}
                  <input v-model.number="slot.active_priority" type="number" min="1" max="2147483647" step="1" class="input mt-1" :disabled="saving" />
                </label>
                <label class="input-label">
                  {{ t('timeRotation.inactivePriority') }}
                  <input v-model.number="slot.inactive_priority" type="number" min="1" max="2147483647" step="1" class="input mt-1" :disabled="saving" />
                </label>
              </div>
              <p class="mt-2 text-xs text-gray-500">{{ t('timeRotation.timeHint') }}</p>
            </div>
            <div class="space-y-3 p-5">
              <label class="input-label" :for="`rotation-search-${index}`">{{ t('timeRotation.accounts') }}</label>
              <div v-if="slot.account_ids.length" class="flex max-h-32 flex-wrap gap-2 overflow-y-auto">
                <button
                  v-for="id in slot.account_ids"
                  :key="id"
                  type="button"
                  class="rounded-lg bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
                  :disabled="saving"
                  :aria-label="t('timeRotation.removeAccount', { name: accountName(id) })"
                  @click="toggleAccount(index, id)"
                >
                  {{ accountName(id) }} <span aria-hidden="true">×</span>
                </button>
              </div>
              <input :id="`rotation-search-${index}`" v-model="queries[index]" class="input" :placeholder="t('timeRotation.search')" type="search" />
              <div class="max-h-80 min-h-40 space-y-1 overflow-y-auto">
                <label
                  v-for="account in candidates(index).slice(0, 100)"
                  :key="account.id"
                  class="flex items-start gap-3 rounded-lg p-2 hover:bg-gray-50 dark:hover:bg-dark-700"
                  :class="{ 'opacity-40': assignedElsewhere(index, account.id) }"
                >
                  <input type="checkbox" class="mt-1 h-4 w-4" :checked="slot.account_ids.includes(account.id)" :disabled="saving || assignedElsewhere(index, account.id)" :data-account-id="account.id" @change="toggleAccount(index, account.id)" />
                  <span class="min-w-0 text-sm">
                    <span class="block break-all text-gray-900 dark:text-gray-100">{{ account.name }}</span>
                    <span class="text-xs text-gray-500">
                      #{{ account.id }} · {{ t('timeRotation.currentPriority', { n: account.priority }) }}
                      <template v-if="assignedElsewhere(index, account.id)"> · {{ t('timeRotation.assigned') }}</template>
                    </span>
                  </span>
                </label>
                <p v-if="!candidates(index).length" class="py-6 text-center text-sm text-gray-500">{{ t('timeRotation.empty') }}</p>
              </div>
              <p v-if="candidates(index).length > 100" class="text-xs text-gray-500">{{ t('timeRotation.searchMore') }}</p>
            </div>
          </section>
        </div>
        <div class="rounded-xl border border-blue-100 bg-blue-50/60 p-4 text-sm leading-6 text-blue-800 dark:border-blue-900 dark:bg-blue-900/10 dark:text-blue-200">
          <p>{{ config.mode === 'smart' ? t('timeRotation.smartPolicyHint') : t('timeRotation.priorityHint') }}</p>
          <p>{{ config.mode === 'smart' ? t('timeRotation.smartRestoreHint') : t('timeRotation.restoreHint') }}</p>
          <p>{{ t('timeRotation.schedulingHint') }}</p>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { list } from '@/api/admin/accounts'
import {
  timeRotationAPI,
  type AccountTimeRotationConfig,
  type AccountRotationSlot,
  type SmartTimeRotationConfig,
  type TimeRotationStatus
} from '@/api/admin/timeRotation'
import type { AccountListItem } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const healthFields = [
  { key: 'ttft_threshold_seconds', min: 1, max: 300 },
  { key: 'slow_request_count', min: 2, max: 20 },
  { key: 'healthy_request_count', min: 2, max: 20 },
  { key: 'sample_window_minutes', min: 1, max: 120 },
  { key: 'cooldown_minutes', min: 1, max: 1440 },
  { key: 'waiting_priority', min: 1, max: 2147483647 },
  { key: 'probe_interval_seconds', min: 1, max: 600 }
] as const
const defaultSmart = (): SmartTimeRotationConfig => ({
  account_ids: [], ttft_threshold_seconds: 20, slow_request_count: 3,
  healthy_request_count: 3, sample_window_minutes: 10, cooldown_minutes: 30,
  waiting_priority: 50, probe_interval_seconds: 60
})

const config = ref<AccountTimeRotationConfig>({ enabled: false, revision: 0, slots: [], mode: 'manual' })
const accounts = ref<AccountListItem[]>([])
const queries = ref(['', '', ''])
const smartQuery = ref('')
const loading = ref(false)
const ready = ref(false)
const saving = ref(false)
const error = ref('')
const statusError = ref(false)
const statusLoading = ref(false)
const status = ref<TimeRotationStatus | null>(null)
const controller = new AbortController()
let statusRequest = 0
let statusTimer: ReturnType<typeof setInterval> | undefined

onBeforeUnmount(() => {
  if (statusTimer) clearInterval(statusTimer)
  controller.abort()
  statusRequest++
})

const smart = computed(() => config.value.smart || defaultSmart())
watch(() => config.value.mode, mode => {
  if (mode === 'smart' && !config.value.smart) config.value.smart = defaultSmart()
}, { immediate: true })

const smartCandidates = computed(() => filterAccounts(smartQuery.value))
const statusMessage = computed(() => {
  if (!status.value) return ''
  if (!status.value.enabled) return t('timeRotation.statusDisabled')
  if (status.value.mode !== 'smart') return t('timeRotation.statusManual')
  return t(status.value.ready ? 'timeRotation.statusReady' : 'timeRotation.statusStale')
})

function filterAccounts(query: string) {
  const normalized = query.trim().toLowerCase()
  return accounts.value.filter(account => !normalized || account.name.toLowerCase().includes(normalized) || String(account.id).includes(normalized))
}

function accountName(id: number) {
  return accounts.value.find(account => account.id === id)?.name || t('timeRotation.missingAccount', { id })
}

function assignedElsewhere(index: number, id: number) {
  return config.value.slots.some((slot, i) => i !== index && slot.account_ids.includes(id))
}

function candidates(index: number) {
  return filterAccounts(queries.value[index] || '')
}

function toggleAccount(index: number, id: number) {
  const slot = config.value.slots[index]
  if (slot.account_ids.includes(id)) slot.account_ids = slot.account_ids.filter(value => value !== id)
  else if (!assignedElsewhere(index, id)) slot.account_ids.push(id)
}

function removeSmartAccount(id: number) {
  smart.value.account_ids = smart.value.account_ids.filter(value => value !== id)
}

function parseTime(value: string, end = false) {
  if (end && value === '24:00') return 1440
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  if (!match || Number(match[1]) > 23 || Number(match[2]) > 59) return -1
  return Number(match[1]) * 60 + Number(match[2])
}

function validManualSlot(slot: AccountRotationSlot) {
  return parseTime(slot.start) >= 0 && parseTime(slot.end, true) >= 0 && slot.start !== slot.end &&
    [slot.active_priority, slot.inactive_priority].every(n => Number.isInteger(n) && n >= 1 && n <= 2147483647)
}

function formatBeijingTime(value: string) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('timeRotation.unknown')
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23'
  }).format(date)
}

function formatLatency(value: number | null | undefined) {
  return value == null || !Number.isFinite(value) ? t('timeRotation.unknown') : `${(value / 1000).toFixed(2)}s`
}

function validSmart() {
  const value = smart.value
  if (config.value.enabled && value.account_ids.length === 0) return false
  if (!value.account_ids.every(id => Number.isSafeInteger(id) && id > 0) || new Set(value.account_ids).size !== value.account_ids.length) return false
  if (!healthFields.every(field => {
    const n = value[field.key]
    return n != null && Number.isInteger(n) && n >= field.min && n <= field.max
  })) return false
  return (Math.max(value.healthy_request_count ?? 0, value.slow_request_count ?? 0) - 1) * (value.probe_interval_seconds ?? 0) < (value.sample_window_minutes ?? 0) * 60
}

async function loadStatus() {
  const request = ++statusRequest
  statusLoading.value = true
  statusError.value = false
  try {
    const result = await timeRotationAPI.status()
    if (request === statusRequest) status.value = result
  } catch {
    if (request === statusRequest) {
      status.value = null
      statusError.value = true
    }
  } finally {
    if (request === statusRequest) statusLoading.value = false
  }
}

function applySavedConfig(saved: AccountTimeRotationConfig) {
  config.value = { ...saved, mode: saved.mode || 'manual' }
  config.value.smart = { ...defaultSmart(), ...saved.smart }
}

async function load() {
  loading.value = true
  ready.value = false
  error.value = ''
  statusRequest++
  status.value = null
  try {
    const saved = await timeRotationAPI.get()
    const all: AccountListItem[] = []
    for (let page = 1; ; page++) {
      const result = await list(page, 100, {
        platform: 'openai', type: 'oauth', lite: 'true', include_scheduler_score: 'false', sort_by: 'id', sort_order: 'asc'
      }, { signal: controller.signal })
      all.push(...result.items.filter(account => !account.parent_account_id))
      if (page >= result.pages || result.items.length === 0) break
    }
    if (controller.signal.aborted) return
    applySavedConfig(saved)
    accounts.value = all
    ready.value = true
    void loadStatus()
  } catch (err) {
    if (!controller.signal.aborted) error.value = extractApiErrorMessage(err, t('timeRotation.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  error.value = ''
  const isSmart = config.value.mode === 'smart'
  if (isSmart ? !validSmart() : config.value.slots.some(slot => !validManualSlot(slot))) {
    error.value = t(isSmart ? 'timeRotation.invalidSmart' : 'timeRotation.invalid')
    return
  }
  saving.value = true
  // 保存开始后，旧预览即使较晚返回，也不能覆盖新版本的运行状态。
  statusRequest++
  statusLoading.value = false
  try {
    const saved = await timeRotationAPI.save(config.value)
    applySavedConfig(saved)
    appStore.showSuccess(t('timeRotation.saved'))
    void loadStatus()
  } catch (err) {
    error.value = extractApiErrorMessage(err, t('timeRotation.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await load()
  if (controller.signal.aborted) return
  statusTimer = setInterval(() => {
    if (ready.value && !loading.value && !saving.value && !statusLoading.value && config.value.mode === 'smart' && !document.hidden) void loadStatus()
  }, 15000)
})
</script>
