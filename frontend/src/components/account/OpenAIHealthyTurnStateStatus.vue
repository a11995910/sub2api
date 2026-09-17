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
      <dl class="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-4" aria-live="polite" aria-atomic="true">
        <div v-for="item in summary" :key="item.key" class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
          <dt class="text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.${item.key}`) }}</dt>
          <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ item.value }}</dd>
        </div>
      </dl>
      <p class="mt-2 text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.attempts`, { count: stats.attempts, busy: stats.in_use }) }}</p>
      <p v-if="!stats.records.length" class="mt-3 text-sm text-gray-600 dark:text-gray-400">{{ t(`${prefix}.empty`) }}</p>
      <details v-else class="mt-3">
        <summary class="min-h-11 cursor-pointer py-3 text-sm font-medium">{{ t(`${prefix}.records`) }}</summary>
        <ul class="max-h-80 space-y-3 overflow-y-auto">
          <li v-for="(row, index) in stats.records" :key="index" class="rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
            <div class="flex flex-wrap justify-between gap-2 text-sm">
              <span class="break-words font-medium">{{ row.model }} · {{ row.transport === 'websocket' ? 'WebSocket' : 'HTTP' }}</span>
              <span :class="row.status === 'available' ? 'text-green-700 dark:text-green-400' : 'text-gray-600 dark:text-gray-400'">{{ t(`${prefix}.state.${row.status}`) }}</span>
            </div>
            <p class="mt-2 break-words">{{ routeName(row.proxy_id) }}</p>
            <p class="mt-1">{{ t(`${prefix}.results`, { success: row.successes, failed: row.failures }) }}</p>
            <p class="mt-1">{{ t(`${prefix}.capturedAt`, { time: formatTime(row.last_captured_at) }) }}</p>
            <p class="mt-1">{{ t(`${prefix}.expiresAt`, { time: formatTime(row.expires_at) }) }}</p>
            <p class="mt-1">{{ t(`${prefix}.usedAt`, { time: formatTime(row.last_used_at) }) }}</p>
            <p v-if="row.last_outcome" class="mt-1">{{ t(`${prefix}.outcome.${row.last_outcome}`) }}<span v-if="row.last_http_status"> · HTTP {{ row.last_http_status }}</span></p>
          </li>
        </ul>
      </details>
      <details v-if="stats.probes.length" class="mt-2">
        <summary class="min-h-11 cursor-pointer py-3 text-sm font-medium">{{ t(`${prefix}.probes`) }}</summary>
        <ul class="max-h-64 space-y-2 overflow-y-auto">
          <li v-for="(probe, index) in stats.probes" :key="index" class="rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
            <p class="break-words">{{ probe.model }} · {{ probe.transport === 'websocket' ? 'WebSocket' : 'HTTP' }} · {{ routeName(probe.proxy_id) }}</p>
            <p class="mt-1">{{ t(`admin.accounts.openai.testStatus.${probe.status}`) }}<span v-if="probe.http_status"> · HTTP {{ probe.http_status }}</span></p>
            <p class="mt-1 text-gray-600 dark:text-gray-400">{{ formatTime(probe.created_at) }}</p>
          </li>
        </ul>
      </details>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { HealthyTurnStateStats } from '@/api/admin/accounts'
import type { Proxy } from '@/types'

const props = defineProps<{ accountId: number; active: boolean; proxies: Proxy[]; revision: number }>()
const { t } = useI18n()
const prefix = 'admin.accounts.openai.stateStats'
const stats = ref<HealthyTurnStateStats | null>(null)
const loading = ref(false)
const error = ref(false)
let controller: AbortController | null = null
const summary = computed(() => {
  const value = stats.value
  if (!value) return []
  const complete = value.successes + value.failures
  return [
    { key: 'available', value: value.available },
    { key: 'captures', value: value.captures },
    { key: 'successFailure', value: `${value.successes} / ${value.failures}` },
    { key: 'successRate', value: complete ? `${(value.successes * 100 / complete).toFixed(1)}%` : '—' }
  ]
})
function formatTime(value: string | null) { return value ? new Date(value).toLocaleString() : '—' }
function routeName(id: number) {
  return id === 0 ? t('admin.accounts.openai.testDirect') : props.proxies.find(proxy => proxy.id === id)?.name ?? t(`${prefix}.proxy`, { id })
}
async function refresh() {
  controller?.abort()
  if (!props.active) return
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
    if (controller === current) loading.value = false
  }
}
watch(() => [props.accountId, props.active, props.revision] as const, (value, previous) => {
  controller?.abort()
  if (!previous || value[0] !== previous[0] || !value[1]) stats.value = null
  if (props.active) void refresh()
}, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
