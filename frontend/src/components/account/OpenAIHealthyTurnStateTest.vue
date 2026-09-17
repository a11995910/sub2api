<template>
  <OpenAIHealthyTurnStateStatus :account-id="accountId" :active="active" :proxies="proxies" :revision="statsRevision" />
  <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t(`${prefix}.testTitle`) }}</span>
      <button
        type="button"
        class="btn btn-secondary min-h-10"
        :aria-expanded="expanded"
        aria-controls="healthy-state-test-controls"
        @click="expanded = !expanded"
      >{{ t(`${prefix}.testConfigure`) }}</button>
    </div>
    <div v-if="expanded" id="healthy-state-test-controls" class="mt-3 space-y-3">
      <p class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t(`${prefix}.testDesc`) }}</p>
      <div class="grid gap-3 sm:grid-cols-2">
        <div>
          <label for="healthy-state-test-model" class="input-label">{{ t(`${prefix}.testModel`) }}</label>
          <input id="healthy-state-test-model" v-model="model" class="input" :disabled="running || disabled" :placeholder="t(`${prefix}.testModelPlaceholder`)" maxlength="200" />
        </div>
        <div>
          <label for="healthy-state-test-transport" class="input-label">{{ t(`${prefix}.testTransport`) }}</label>
          <select id="healthy-state-test-transport" v-model="transport" class="input" :disabled="running || disabled">
            <option value="http">HTTP Responses</option>
            <option value="websocket">WebSocket Responses</option>
          </select>
        </div>
      </div>
      <fieldset :disabled="running || disabled" class="min-w-0">
        <legend class="input-label">{{ t(`${prefix}.testProxies`) }}</legend>
        <input v-if="proxyOptions.length > 6" v-model="search" class="input mb-2" :aria-label="t(`${prefix}.testProxySearch`)" :placeholder="t(`${prefix}.testProxySearch`)" />
        <div class="max-h-44 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600">
          <label v-for="option in filteredOptions" :key="option.key" class="flex min-h-10 cursor-pointer items-center gap-3 rounded px-2 text-sm hover:bg-gray-50 dark:hover:bg-dark-700">
            <input v-model="selected" type="checkbox" :value="option.key" :disabled="selected.length >= 5 && !selected.includes(option.key)" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span class="min-w-0 break-words">{{ option.label }}</span>
          </label>
          <p v-if="filteredOptions.length === 0" class="p-2 text-sm text-gray-500">{{ t(`${prefix}.testNoProxies`) }}</p>
        </div>
      </fieldset>
      <div class="flex flex-wrap items-center gap-3">
        <button type="button" class="btn btn-secondary min-h-10" :disabled="disabled || running || selected.length === 0" @click="runTests">
          <LoadingSpinner v-if="running" size="sm" class="mr-2" />
          {{ running ? t(`${prefix}.testRunning`) : t(`${prefix}.testStart`) }}
        </button>
        <button v-if="running" type="button" class="btn btn-secondary min-h-10" @click="cancelTests">{{ t('common.cancel') }}</button>
      </div>
    </div>
    <ul v-if="results.length" class="mt-3 space-y-2" aria-live="polite" :aria-label="t(`${prefix}.testResults`)">
      <li v-for="row in results" :key="row.key" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800">
        <div class="flex flex-wrap justify-between gap-2">
          <span class="min-w-0 break-words font-medium">{{ row.label }}</span>
          <span :class="row.status === 'recorded' || row.status === 'already_recorded' ? 'text-green-700 dark:text-green-400' : 'text-gray-600 dark:text-gray-300'">
            {{ t(`${prefix}.testStatus.${row.status}`) }}<span v-if="row.http_status"> · HTTP {{ row.http_status }}</span>
          </span>
        </div>
        <p v-if="row.model" class="mt-1 break-words text-xs text-gray-500 dark:text-gray-400">{{ row.model }} · {{ row.transport === 'websocket' ? 'WebSocket' : 'HTTP' }}</p>
        <p v-if="row.message" class="mt-1 break-words text-xs text-gray-500 dark:text-gray-400">{{ row.message }}</p>
        <p v-if="row.expires_at" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.testExpires`, { time: new Date(row.expires_at).toLocaleTimeString() }) }}</p>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { HealthyTurnStateTestResult } from '@/api/admin/accounts'
import type { Proxy } from '@/types'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import OpenAIHealthyTurnStateStatus from './OpenAIHealthyTurnStateStatus.vue'

const props = defineProps<{ accountId: number; proxies: Proxy[]; active: boolean; disabled?: boolean }>()
const { t } = useI18n()
const prefix = 'admin.accounts.openai'
const expanded = ref(false)
const model = ref('gpt-5.6-sol')
const transport = ref<'http' | 'websocket'>('http')
const selected = ref(['account'])
const search = ref('')
const running = ref(false)
const statsRevision = ref(0)
type TestRow = Omit<Partial<HealthyTurnStateTestResult>, 'status'> & {
  key: string; label: string
  status: HealthyTurnStateTestResult['status'] | 'pending' | 'running' | 'cancelled'
}
const results = ref<TestRow[]>([])
let controller: AbortController | null = null
const proxyOptions = computed(() => [
  { key: 'account', label: t(`${prefix}.testCurrentProxy`), id: null },
  { key: 'direct', label: t(`${prefix}.testDirect`), id: 0 },
  ...props.proxies.filter(proxy => proxy.status === 'active' && (!proxy.expires_at || new Date(proxy.expires_at).getTime() > Date.now()))
    .map(proxy => ({ key: String(proxy.id), label: `${proxy.name} (${proxy.host}:${proxy.port})`, id: proxy.id }))
])
const filteredOptions = computed(() => proxyOptions.value.filter(option => option.label.toLowerCase().includes(search.value.toLowerCase())))

function cancelTests() { controller?.abort() }

async function runTests() {
  if (running.value || props.disabled || !props.active) return
  const choices = proxyOptions.value.filter(option => selected.value.includes(option.key)).slice(0, 5)
  if (!choices.length) return
  const accountId = props.accountId
  const testModel = model.value.trim()
  const testTransport = transport.value
  const current = new AbortController()
  controller = current
  running.value = true
  results.value = choices.map(choice => ({ key: choice.key, label: choice.label, status: 'pending' }))
  try {
    for (const choice of choices) {
      if (current.signal.aborted) break
      const row = results.value.find(item => item.key === choice.key)!
      row.status = 'running'
      try {
        const result = await adminAPI.accounts.testHealthyTurnState(accountId, {
          model: testModel, transport: testTransport, proxy_id: choice.id
        }, current.signal)
        if (current.signal.aborted) break
        Object.assign(row, result)
        statsRevision.value++
      } catch (error: unknown) {
        if (current.signal.aborted) break
        row.status = 'failed'
        row.message = error instanceof Error ? error.message : t(`${prefix}.testRequestFailed`)
      }
    }
  } finally {
    if (controller === current) {
      for (const row of results.value) {
        if (row.status === 'pending' || row.status === 'running') row.status = 'cancelled'
      }
      running.value = false
      controller = null
    }
  }
}

watch(() => [props.accountId, props.active] as const, () => {
  cancelTests()
  controller = null
  running.value = false
  results.value = []
  selected.value = ['account']
  expanded.value = false
})
onBeforeUnmount(cancelTests)
</script>
