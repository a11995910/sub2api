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
      <dl class="mt-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800" aria-live="polite" aria-atomic="true">
        <div class="flex items-center justify-between gap-3">
          <dt class="text-sm text-gray-600 dark:text-gray-400">{{ t(`${prefix}.accountCaptures`) }}</dt>
          <dd class="text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ stats.captures }}</dd>
        </div>
      </dl>
      <p v-if="!stats.models.length" class="mt-3 text-sm text-gray-600 dark:text-gray-400">{{ t(`${prefix}.empty`) }}</p>
      <table v-else class="mt-3 w-full table-fixed text-sm" :aria-label="t(`${prefix}.modelCaptures`)">
        <thead>
          <tr class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">
            <th scope="col" class="py-2 pr-3 text-left font-medium">{{ t(`${prefix}.model`) }}</th>
            <th scope="col" class="w-28 py-2 text-right font-medium">{{ t(`${prefix}.captures`) }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="row in stats.models" :key="row.model">
            <th scope="row" class="break-words py-3 pr-3 text-left font-medium text-gray-900 dark:text-gray-100">{{ row.model }}</th>
            <td class="py-3 text-right tabular-nums text-gray-900 dark:text-gray-100">{{ row.captures }}</td>
          </tr>
        </tbody>
      </table>
    </template>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { HealthyTurnStateStats } from '@/api/admin/accounts'

const props = defineProps<{ accountId: number; active: boolean; revision: number }>()
const { t } = useI18n()
const prefix = 'admin.accounts.openai.stateStats'
const stats = ref<HealthyTurnStateStats | null>(null)
const loading = ref(false)
const error = ref(false)
let controller: AbortController | null = null
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
