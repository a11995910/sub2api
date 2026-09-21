<template>
  <section :aria-label="t(`${prefix}.codexTurnTicket`)" :class="compact ? 'mt-2 space-y-1' : 'rounded-lg border border-gray-200 p-3 dark:border-dark-600'" data-testid="codex-ticket-status">
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-xs font-medium text-gray-700 dark:text-gray-300">{{ t(`${prefix}.codexTurnTicket`) }}</h3>
      <button v-if="accountId && !compact" type="button" class="btn btn-secondary min-h-11 px-3 text-xs" :disabled="loading || !active" @click="refresh">{{ t(`${prefix}.codexTicketRefresh`) }}</button>
    </div>
    <p v-if="!compact" class="mt-1 text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`${prefix}.codexTurnTicketDesc`) }}</p>
    <p v-if="error" role="alert" class="mt-2 text-xs text-amber-700 dark:text-amber-400">{{ t(`${prefix}.codexTicketRefreshError`) }}</p>
    <p v-if="!displayTickets.length" class="mt-2 text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.codexTurnTicketEmpty`) }}</p>
    <div v-for="ticket in displayTickets" :key="ticket.model" class="space-y-1 text-xs" :class="compact ? 'border-t border-gray-100 pt-2 dark:border-dark-700' : 'mt-3 rounded-md bg-gray-50 p-2 dark:bg-dark-800'">
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <span class="min-w-0 break-all font-medium text-gray-700 dark:text-gray-300">{{ ticket.model }}</span>
        <span v-if="ticket.ready" class="text-emerald-700 dark:text-emerald-400">{{ t(`${prefix}.codexTurnTicketReady`, { time: remaining(ticket.remaining_seconds) }) }}</span>
        <span v-else-if="ticket.blocked" class="text-amber-700 dark:text-amber-400">{{ t(`${prefix}.codexTurnTicketPaused`) }}</span>
        <span v-else class="text-gray-600 dark:text-gray-400">{{ t(`${prefix}.codexTurnTicketMissing`) }}</span>
      </div>
      <p v-if="!ticket.ready && ticket.invalid_reason" class="text-amber-700 dark:text-amber-400" data-testid="codex-ticket-invalid-reason">{{ t(`${prefix}.codexTicketInvalidReasons.${ticket.invalid_reason}`) }}</p>
      <p v-if="ticket.harvest_status === 'collecting'" class="text-primary-700 dark:text-primary-300" data-testid="codex-ticket-collecting">
        {{ t(`${prefix}.codexTicketCollecting`) }}<span v-if="(ticket.attempt_total ?? 0) > 1 && (ticket.attempt_index ?? 0) > 0"> · {{ t(`${prefix}.codexTicketProgress`, { index: ticket.attempt_index, total: ticket.attempt_total }) }}</span>
      </p>
      <p v-else-if="ticket.harvest_status === 'paused'" class="text-amber-700 dark:text-amber-400">{{ t(`${prefix}.codexTicketPauseReasons.${ticket.pause_reason || 'collector_unavailable'}`) }}</p>
      <p v-if="ticket.last_result" class="break-words text-gray-600 dark:text-gray-400" data-testid="codex-ticket-result">
        {{ t(`${prefix}.codexTicketLastResult`, { result: t(`${prefix}.codexTicketResults.${ticket.last_result}`) }) }}<span v-if="ticket.last_http_status"> · HTTP {{ ticket.last_http_status }}</span><span v-if="ticket.last_attempt_at"> · {{ formatDateTime(ticket.last_attempt_at) }}</span>
      </p>
      <p v-if="ticket.harvest_status !== 'collecting' && ticket.harvest_status !== 'paused' && ticket.next_attempt_at" class="text-gray-600 dark:text-gray-400" data-testid="codex-ticket-next-attempt">{{ t(`${prefix}.codexTicketNextAttempt`, { time: formatDateTime(ticket.next_attempt_at) }) }}</p>
      <p v-else-if="ticket.harvest_status === 'waiting'" class="text-gray-600 dark:text-gray-400" data-testid="codex-ticket-waiting">{{ t(`${prefix}.codexTicketWaiting`, { seconds: ticket.retry_interval_seconds ?? 6 }) }}</p>
      <p v-else-if="ticket.harvest_status === 'ready' && ticket.refresh_due_at && !compact" class="text-gray-600 dark:text-gray-400">{{ t(`${prefix}.codexTicketRefreshDue`, { time: formatDateTime(ticket.refresh_due_at) }) }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { OpenAICodexTicketStatus } from '@/types'
import { formatDateTime } from '@/utils/format'
const props = withDefaults(defineProps<{ tickets: OpenAICodexTicketStatus[]; compact?: boolean; accountId?: number; active?: boolean }>(), { compact: false, active: true })
const { t } = useI18n()
const prefix = 'admin.accounts.openai'
const refreshedTickets = ref<OpenAICodexTicketStatus[] | null>(null)
const displayTickets = computed(() => refreshedTickets.value ?? props.tickets)
const loading = ref(false)
const error = ref(false)
let controller: AbortController | null = null
let pollTimer: ReturnType<typeof setTimeout> | null = null
let disposed = false
function remaining(seconds: number) {
  const value = Math.max(0, Math.floor(seconds))
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`
}
function clearPollTimer() {
  if (pollTimer !== null) clearTimeout(pollTimer)
  pollTimer = null
}
function schedulePoll() {
  clearPollTimer()
  if (props.accountId && props.active && !props.compact && !disposed) pollTimer = setTimeout(() => { void refresh() }, 5000)
}
async function refresh() {
  if (!props.accountId || !props.active || props.compact || disposed || loading.value) return
  clearPollTimer()
  if (document.hidden) {
    schedulePoll()
    return
  }
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = false
  try {
    const account = await adminAPI.accounts.getById(props.accountId, current.signal)
    if (controller === current && !current.signal.aborted) refreshedTickets.value = account.codex_turn_tickets ?? []
  } catch {
    if (controller === current && !current.signal.aborted) error.value = true
  } finally {
    if (controller === current) {
      loading.value = false
      schedulePoll()
    }
  }
}
watch(() => [props.accountId, props.active, props.compact] as const, () => {
  clearPollTimer()
  controller?.abort()
  controller = null
  loading.value = false
  error.value = false
  refreshedTickets.value = null
  if (props.active && !props.compact && props.accountId) void refresh()
}, { immediate: true })
onBeforeUnmount(() => {
  disposed = true
  clearPollTimer()
  controller?.abort()
})
</script>
