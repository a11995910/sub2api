<template>
  <section class="rounded-lg border border-gray-200 p-3 dark:border-dark-600" :aria-label="t(`${prefix}.title`)">
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t(`${prefix}.title`) }}</h3>
      <button type="button" class="btn btn-secondary min-h-11" :disabled="loading || !active" @click="refresh">{{ t(`${prefix}.refresh`) }}</button>
    </div>
    <p class="mt-1 text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`${prefix}.hint`) }}</p>
    <p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ t(`${prefix}.error`) }}</p>
    <p v-else-if="loading && !stats" role="status" class="mt-3 text-sm text-gray-600 dark:text-gray-400">{{ t('common.loading') }}</p>
    <template v-else-if="stats">
      <div v-if="stats.maintenance" class="mt-3 rounded-lg border p-3 text-sm" :class="maintenanceClass" role="status" aria-live="polite" aria-atomic="true">
        <p class="font-medium">{{ t(`${prefix}.maintenanceTitle`) }} · {{ t(`${prefix}.maintenanceStatus.${stats.maintenance.status}`) }}</p>
        <p v-if="stats.maintenance.message" class="mt-1 break-words text-xs leading-relaxed">{{ stats.maintenance.message }}</p>
        <p v-if="stats.maintenance.next_retry_at" class="mt-1 text-xs tabular-nums">{{ t(`${prefix}.nextRetry`, { time: formatDateTime(stats.maintenance.next_retry_at) }) }}</p>
      </div>
      <dl class="mt-3 space-y-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800" aria-live="polite" aria-atomic="true">
        <div class="flex items-center justify-between gap-3">
          <dt class="text-sm text-gray-600 dark:text-gray-400">{{ t(`${prefix}.accountAvailable`) }}</dt>
          <dd class="text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ stats.available + stats.in_use }}</dd>
        </div>
        <div class="flex items-center justify-between gap-3">
          <dt class="text-sm text-gray-600 dark:text-gray-400">{{ t(`${prefix}.accountCaptures`) }}</dt>
          <dd class="text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ stats.captures }}</dd>
        </div>
      </dl>
      <p v-if="stats.in_use" class="mt-1 text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.inUse`, { count: stats.in_use }) }}</p>
      <p v-if="!stats.models.length" class="mt-3 text-sm text-gray-600 dark:text-gray-400">{{ t(`${prefix}.${stats.maintenance && !['disabled', 'unconfigured'].includes(stats.maintenance.status) ? 'emptyConfigured' : 'empty'}`) }}</p>
      <div v-else class="mt-3 overflow-x-auto">
        <table class="w-full min-w-[520px] table-fixed text-sm" :aria-label="t(`${prefix}.modelCaptures`)">
          <thead>
            <tr class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">
              <th scope="col" class="py-2 pr-3 text-left font-medium">{{ t(`${prefix}.model`) }}</th>
              <th scope="col" class="w-20 py-2 text-right font-medium">{{ t(`${prefix}.available`) }}</th>
              <th scope="col" class="w-16 py-2 text-right font-medium">{{ t(`${prefix}.captures`) }}</th>
              <th scope="col" class="w-16 py-2 text-right font-medium">{{ t(`${prefix}.attempts`) }}</th>
              <th scope="col" class="w-16 py-2 text-right font-medium">{{ t(`${prefix}.successes`) }}</th>
              <th scope="col" class="w-16 py-2 text-right font-medium">{{ t(`${prefix}.failures`) }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="row in stats.models" :key="row.model">
              <th scope="row" class="break-words py-3 pr-3 text-left font-medium text-gray-900 dark:text-gray-100">{{ row.model }}</th>
              <td class="py-3 text-right tabular-nums text-gray-900 dark:text-gray-100">{{ row.available + row.in_use }}</td>
              <td class="py-3 text-right tabular-nums text-gray-900 dark:text-gray-100">{{ row.captures }}</td>
              <td class="py-3 text-right tabular-nums text-gray-900 dark:text-gray-100">{{ row.attempts }}</td>
              <td class="py-3 text-right tabular-nums text-gray-900 dark:text-gray-100">{{ row.successes }}</td>
              <td class="py-3 text-right tabular-nums text-gray-900 dark:text-gray-100">{{ row.failures }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="mt-2 text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`${prefix}.historyHint`) }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { HealthyTurnStateStats } from '@/api/admin/accounts'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ accountId: number; active: boolean; revision: number }>()
const { t } = useI18n()
const prefix = 'admin.accounts.openai.stateStats'
const stats = ref<HealthyTurnStateStats | null>(null)
const loading = ref(false)
const error = ref(false)
const maintenanceClass = computed(() => {
  switch (stats.value?.maintenance?.status) {
    case 'error':
      return 'border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300'
    case 'backoff':
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300'
    case 'queued':
      return 'border-primary-200 bg-primary-50 text-primary-800 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-300'
    default:
      return 'border-gray-200 text-gray-700 dark:border-dark-600 dark:text-gray-300'
  }
})
let controller: AbortController | null = null
let pollTimer: ReturnType<typeof setTimeout> | null = null
let disposed = false
function clearPollTimer() {
  if (pollTimer !== null) clearTimeout(pollTimer)
  pollTimer = null
}
function schedulePoll() {
  clearPollTimer()
  if (props.active && !disposed) pollTimer = setTimeout(() => { void refresh() }, 15000)
}
async function refresh() {
  if (!props.active || disposed || loading.value) return
  clearPollTimer()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = false
  try {
    const value = await adminAPI.accounts.getHealthyTurnStateStats(props.accountId, current.signal)
    if (controller === current && !current.signal.aborted) stats.value = value
  } catch {
    if (controller === current && !current.signal.aborted) error.value = true
  } finally {
    if (controller === current) {
      loading.value = false
      schedulePoll()
    }
  }
}
watch(() => [props.accountId, props.active, props.revision] as const, (value, previous) => {
  clearPollTimer()
  controller?.abort()
  controller = null
  loading.value = false
  if (!previous || value[0] !== previous[0] || !value[1]) stats.value = null
  if (props.active) void refresh()
}, { immediate: true })
onBeforeUnmount(() => {
  disposed = true
  clearPollTimer()
  controller?.abort()
})
</script>
