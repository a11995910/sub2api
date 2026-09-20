<template>
  <section :aria-label="t(`${prefix}.codexTurnTicket`)" :class="compact ? 'mt-2 space-y-1' : 'rounded-lg border border-gray-200 p-3 dark:border-dark-600'" data-testid="codex-ticket-status">
    <h3 class="text-xs font-medium text-gray-700 dark:text-gray-300">{{ t(`${prefix}.codexTurnTicket`) }}</h3>
    <p v-if="!compact" class="mt-1 text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`${prefix}.codexTurnTicketDesc`) }}</p>
    <p v-if="!tickets.length" class="mt-2 text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.codexTurnTicketEmpty`) }}</p>
    <div v-for="ticket in tickets" :key="ticket.model" class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs" :class="{ 'mt-2': !compact }">
      <span class="min-w-0 break-all font-medium text-gray-700 dark:text-gray-300">{{ ticket.model }}</span>
      <span v-if="ticket.ready" class="text-emerald-700 dark:text-emerald-400">{{ t(`${prefix}.codexTurnTicketReady`, { time: remaining(ticket.remaining_seconds) }) }}</span>
      <span v-else-if="ticket.blocked" class="text-amber-700 dark:text-amber-400">{{ t(`${prefix}.codexTurnTicketPaused`) }}</span>
      <span v-else class="text-gray-600 dark:text-gray-400">{{ t(`${prefix}.codexTurnTicketMissing`) }}</span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { OpenAICodexTicketStatus } from '@/types'
withDefaults(defineProps<{ tickets: OpenAICodexTicketStatus[]; compact?: boolean }>(), { compact: false })
const { t } = useI18n()
const prefix = 'admin.accounts.openai'
function remaining(seconds: number) {
  const value = Math.max(0, Math.floor(seconds))
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`
}
</script>
