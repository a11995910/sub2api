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
          <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="load()">
            {{ t('common.refresh') }}
          </button>
          <span v-if="dirty" class="self-center text-xs text-amber-700 dark:text-amber-300">{{ t('timeRotation.unsaved') }}</span>
          <button type="button" class="btn btn-primary" :disabled="!ready || saving" data-testid="rotation-save" @click="save">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </div>
      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        {{ error }}
        <button v-if="saveConflict" type="button" class="ml-2 font-medium underline" @click="reloadAfterConflict">
          {{ t('timeRotation.reloadAfterConflict') }}
        </button>
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
                  <span class="block text-xs text-gray-500">#{{ account.id }} · {{ smartAccountStatus(account) }}</span>
                  <span v-if="smartAccountReason(account)" class="block text-xs text-amber-700 dark:text-amber-300">{{ smartAccountReason(account) }}</span>
                </span>
              </label>
              <p v-if="!smartCandidates.length" class="py-6 text-center text-sm text-gray-500 sm:col-span-2 lg:col-span-3">{{ t('timeRotation.empty') }}</p>
            </div>
            <p v-if="smartCandidates.length > 200" class="mt-3 text-xs text-gray-500">{{ t('timeRotation.smartSearchMore') }}</p>
          </section>

          <section class="card p-5">
            <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
              <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('timeRotation.smartPeriods') }}</h2>
              <span class="text-xs text-gray-500">{{ t('timeRotation.smartImmediate') }}</span>
            </div>
            <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              <div v-for="(period, index) in smart.periods" :key="index" class="rounded-xl border border-gray-100 p-4 dark:border-dark-600" :data-testid="`smart-period-${index}`">
                <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
                  <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('timeRotation.slot', { n: index + 1 }) }}</span>
                  <div class="flex gap-3 text-xs">
                    <button type="button" class="text-primary-600 disabled:opacity-40" :disabled="saving || !canSplitPeriod(index)" @click="splitPeriod(index)">
                      {{ t('timeRotation.splitPeriod') }}
                    </button>
                    <button type="button" class="text-red-600 disabled:opacity-40" :disabled="saving || smart.periods.length <= 1" @click="removePeriod(index)">
                      {{ t('timeRotation.removePeriod') }}
                    </button>
                  </div>
                </div>
                <div class="grid grid-cols-2 gap-3">
                  <label class="input-label">
                    {{ t('timeRotation.start') }}
                    <input v-model="period.start" type="time" class="input mt-1" :disabled="saving" />
                  </label>
                  <label class="input-label">
                    {{ t('timeRotation.end') }}
                    <input v-model="period.end" type="text" inputmode="numeric" placeholder="24:00" class="input mt-1" :disabled="saving" />
                  </label>
                  <label class="input-label col-span-2">
                    {{ t('timeRotation.primaryCount') }}
                    <input v-model.number="period.primary_count" type="number" min="1" max="10000" step="1" class="input mt-1" :disabled="saving" />
                  </label>
                </div>
              </div>
            </div>
            <p class="mt-3 text-xs text-gray-500">{{ t('timeRotation.smartPeriodHint') }}</p>
          </section>

          <section class="card p-5">
            <div class="grid gap-4 md:grid-cols-2">
              <label class="input-label">
                {{ t('timeRotation.rotationMinutes') }}
                <input v-model.number="smart.rotation_minutes" type="number" min="15" max="240" step="1" class="input mt-1" :disabled="saving" data-testid="rotation-minutes" />
              </label>
              <label class="input-label">
                {{ t('timeRotation.quotaReserve') }}
                <input v-model.number="smart.quota_reserve_percent" type="number" min="1" max="50" step="1" class="input mt-1" data-testid="quota-reserve" :disabled="saving" />
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
                <p class="mb-3 text-sm text-gray-500">
                  {{ status.period ? `${status.period.start}–${status.period.end} · ${t('timeRotation.primaryCount')}: ${periodPrimaryCount(status.period)}` : t('timeRotation.noCurrentPeriod') }}
                  <span v-if="status.next_rotation_at"> · {{ t('timeRotation.nextRotation') }} {{ formatBeijingTime(status.next_rotation_at) }}</span>
                </p>
                <div class="overflow-x-auto">
                  <table class="w-full text-left text-sm">
                    <thead>
                      <tr class="border-b border-gray-100 text-xs text-gray-500 dark:border-dark-600">
                        <th class="p-2">{{ t('timeRotation.accountName') }}</th>
                        <th class="p-2">{{ t('timeRotation.accountRole') }}</th>
                        <th class="p-2">{{ t('timeRotation.quota7d') }}</th>
                        <th class="p-2">{{ t('timeRotation.quota5h') }}</th>
                        <th class="p-2">{{ t('timeRotation.accountReason') }}</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="account in status.accounts" :key="account.account_id" class="border-b border-gray-50 dark:border-dark-700">
                        <td class="p-2">{{ account.name || accountName(account.account_id) }} <span class="text-xs text-gray-500">#{{ account.account_id }}</span></td>
                        <td class="p-2">{{ t(`timeRotation.role.${account.role}`) }}</td>
                        <td class="p-2">{{ formatQuota(account.quota_7d_remaining) }}</td>
                        <td class="p-2">{{ formatQuota(account.quota_5h_remaining) }}</td>
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
const defaultSmart = (): SmartTimeRotationConfig => ({
  account_ids: [],
  periods: [
    { start: '00:00', end: '08:00', primary_count: 1 },
    { start: '08:00', end: '10:00', primary_count: 2 },
    { start: '10:00', end: '14:00', primary_count: 4 },
    { start: '14:00', end: '18:00', primary_count: 4 },
    { start: '18:00', end: '22:00', primary_count: 3 },
    { start: '22:00', end: '24:00', primary_count: 2 }
  ],
  rotation_minutes: 60,
  quota_reserve_percent: 10
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
let statusRefreshTimer: ReturnType<typeof setInterval> | undefined
const loadedConfigSnapshot = ref('')
const saveConflict = ref(false)
let mounted = true

onBeforeUnmount(() => {
  mounted = false
  controller.abort()
  statusRequest++
  if (statusRefreshTimer) clearInterval(statusRefreshTimer)
})

const smart = computed(() => config.value.smart || defaultSmart())
watch(() => config.value.mode, mode => {
  if (mode === 'smart' && !config.value.smart) config.value.smart = defaultSmart()
}, { immediate: true })

const smartCandidates = computed(() => filterAccounts(smartQuery.value))
const dirty = computed(() => ready.value && loadedConfigSnapshot.value !== JSON.stringify(config.value))
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

function isFuture(value: string | null | undefined) {
  return Boolean(value && Number.isFinite(new Date(value).getTime()) && new Date(value).getTime() > Date.now())
}

function smartAccountStatus(account: AccountListItem) {
  if (account.status !== 'active') return account.status === 'error' ? t('timeRotation.accountError') : t('timeRotation.accountInactive')
  if (!account.schedulable) return t('timeRotation.accountUnschedulable')
  if (isFuture(account.temp_unschedulable_until)) return t('timeRotation.accountTemporarilyUnavailable')
  if (isFuture(account.overload_until)) return t('timeRotation.accountOverloaded')
  if (isFuture(account.rate_limit_reset_at)) return t('timeRotation.accountRateLimited')
  if (account.auto_pause_on_expired && account.expires_at && account.expires_at * 1000 <= Date.now()) return t('timeRotation.accountExpired')
  return t('timeRotation.accountSchedulable')
}

function smartAccountReason(account: AccountListItem) {
  if (account.temp_unschedulable_until && isFuture(account.temp_unschedulable_until)) return t('timeRotation.accountUntil', { time: formatBeijingTime(account.temp_unschedulable_until) })
  if (account.overload_until && isFuture(account.overload_until)) return t('timeRotation.accountUntil', { time: formatBeijingTime(account.overload_until) })
  if (account.rate_limit_reset_at && isFuture(account.rate_limit_reset_at)) return t('timeRotation.accountUntil', { time: formatBeijingTime(account.rate_limit_reset_at) })
  if (account.temp_unschedulable_reason) return account.temp_unschedulable_reason
  if (account.auto_pause_on_expired && account.expires_at && account.expires_at * 1000 <= Date.now()) return t('timeRotation.accountExpired')
  return ''
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

function canSplitPeriod(index: number) {
  const period = smart.value.periods[index]
  return smart.value.periods.length < 24 && parseTime(period.start) >= 0 && parseTime(period.end, true) - parseTime(period.start) > 1
}

function splitPeriod(index: number) {
  if (!canSplitPeriod(index)) return
  const period = smart.value.periods[index]
  const minute = Math.floor((parseTime(period.start) + parseTime(period.end, true)) / 2)
  const boundary = `${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`
  smart.value.periods.splice(index, 1, { ...period, end: boundary }, { ...period, start: boundary })
}

function removePeriod(index: number) {
  const periods = smart.value.periods
  if (periods.length <= 1) return
  if (index > 0) periods[index - 1].end = periods[index].end
  else periods[1].start = periods[0].start
  periods.splice(index, 1)
}

function validManualSlot(slot: AccountRotationSlot) {
  return parseTime(slot.start) >= 0 && parseTime(slot.end, true) >= 0 && slot.start !== slot.end &&
    [slot.active_priority, slot.inactive_priority].every(n => Number.isInteger(n) && n >= 1 && n <= 2147483647)
}

function periodPrimaryCount(period: TimeRotationStatus['period']) {
  return period && 'primary_count' in period ? period.primary_count : null
}

function formatBeijingTime(value: string) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('timeRotation.unknown')
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23'
  }).format(date)
}

function formatQuota(value: number | null | undefined) {
  return value == null || !Number.isFinite(value) ? t('timeRotation.unknown') : `${Math.round(value * 10) / 10}%`
}

function validSmart() {
  const value = smart.value
  if (config.value.enabled && value.account_ids.length === 0) return false
  if (!value.account_ids.every(id => Number.isSafeInteger(id) && id > 0) || new Set(value.account_ids).size !== value.account_ids.length) return false
  if (!Number.isInteger(value.rotation_minutes) || value.rotation_minutes < 15 || value.rotation_minutes > 240 || 1440 % value.rotation_minutes !== 0) return false
  if (!Number.isInteger(value.quota_reserve_percent) || value.quota_reserve_percent < 1 || value.quota_reserve_percent > 50) return false
  if (value.periods.length < 1 || value.periods.length > 24) return false
  let cursor = 0
  return value.periods.every(period => {
    const start = parseTime(period.start)
    const end = parseTime(period.end, true)
    const valid = start === cursor && end > start && Number.isInteger(period.primary_count) && period.primary_count >= 1 && period.primary_count <= 10000
    cursor = end
    return valid
  }) && cursor === 1440
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
  if (config.value.mode === 'smart' && !config.value.smart) config.value.smart = defaultSmart()
}

async function load(force = false) {
  if (!force && dirty.value) {
    error.value = t('timeRotation.refreshUnsaved')
    return
  }
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
    loadedConfigSnapshot.value = JSON.stringify(config.value)
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
  saveConflict.value = false
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
    loadedConfigSnapshot.value = JSON.stringify(config.value)
    appStore.showSuccess(t('timeRotation.saved'))
    void loadStatus()
  } catch (err) {
    const statusCode = (err as { response?: { status?: number } })?.response?.status
    if (statusCode === 409) {
      saveConflict.value = true
      error.value = extractApiErrorMessage(err, t('timeRotation.saveConflict'))
    } else {
      error.value = extractApiErrorMessage(err, t('timeRotation.saveFailed'))
    }
  } finally {
    saving.value = false
  }
}

async function reloadAfterConflict() {
  saveConflict.value = false
  await load(true)
}

function refreshSmartStatus() {
  if (ready.value && config.value.mode === 'smart' && !statusLoading.value) void loadStatus()
}

onMounted(async () => {
  await load()
  if (mounted) statusRefreshTimer = setInterval(refreshSmartStatus, 20_000)
})
</script>
