<template>
  <OpenAIHealthyTurnStateStatus :account-id="accountId" :active="active" :revision="statsRevision" />
  <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t(`${prefix}.testTitle`) }}</span>
      <button
        type="button"
        class="btn btn-secondary min-h-10"
        :aria-expanded="expanded"
        aria-controls="healthy-state-test-controls"
        @click="toggleExpanded"
      >{{ t(`${prefix}.testConfigure`) }}</button>
    </div>
    <div v-if="expanded" id="healthy-state-test-controls" class="mt-3 space-y-3">
      <p class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t(`${prefix}.testDesc`) }}</p>
      <div>
        <label for="healthy-state-test-mode" class="input-label">{{ t(`${prefix}.dynamic.mode`) }}</label>
        <select id="healthy-state-test-mode" v-model="mode" class="input" :disabled="running || saving || disabled">
          <option value="fixed">{{ t(`${prefix}.dynamic.fixed`) }}</option>
          <option value="dynamic">{{ t(`${prefix}.dynamic.dynamic`) }}</option>
        </select>
      </div>
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
      <fieldset v-if="mode === 'fixed'" :disabled="running || disabled" class="min-w-0">
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
      <fieldset v-else :disabled="running || saving || configLoading || disabled" class="min-w-0 space-y-3">
        <p class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t(`${prefix}.dynamic.description`) }}</p>
        <p v-if="configLoading" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
        <div>
          <label for="healthy-state-dynamic-url" class="input-label">{{ t(`${prefix}.dynamic.url`) }}</label>
          <input id="healthy-state-dynamic-url" v-model="apiUrl" type="password" autocomplete="new-password" class="input" :placeholder="t(`${prefix}.dynamic.urlPlaceholder`)" />
          <p v-if="dynamicConfig.configured" class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.dynamic.savedUrl`, { url: dynamicConfig.api_url_masked }) }}</p>
        </div>
        <div class="grid gap-3 sm:grid-cols-3">
          <div>
            <label for="healthy-state-dynamic-protocol" class="input-label">{{ t(`${prefix}.dynamic.protocol`) }}</label>
            <select id="healthy-state-dynamic-protocol" v-model="dynamicConfig.protocol" class="input">
              <option value="http">HTTP</option><option value="https">HTTPS</option><option value="socks5h">SOCKS5H</option>
            </select>
          </div>
          <div>
            <label for="healthy-state-dynamic-target" class="input-label">{{ t(`${prefix}.dynamic.target`) }}</label>
            <input id="healthy-state-dynamic-target" v-model.number="dynamicConfig.target_count" type="number" min="1" max="100" step="1" class="input" />
          </div>
          <div>
            <label for="healthy-state-dynamic-attempts" class="input-label">{{ t(`${prefix}.dynamic.maxAttempts`) }}</label>
            <input id="healthy-state-dynamic-attempts" v-model.number="dynamicConfig.max_attempts" type="number" :min="dynamicConfig.target_count || 1" max="1000" step="1" class="input" />
          </div>
        </div>
        <p v-if="configLoaded && !dynamicConfigValid" class="text-xs text-amber-700 dark:text-amber-400">{{ t(`${prefix}.dynamic.validation`) }}</p>
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" class="btn btn-secondary min-h-10" :disabled="!configLoaded || !dynamicConfigValid" @click="saveDynamicConfig">{{ saving ? t(`${prefix}.dynamic.saving`) : t(`${prefix}.dynamic.save`) }}</button>
          <button type="button" class="btn btn-secondary min-h-10" :disabled="!configLoaded || !dynamicConfigValid" @click="runDynamicTests">{{ t(`${prefix}.dynamic.start`) }}</button>
          <span v-if="configSaved" class="text-xs text-green-700 dark:text-green-400">{{ t(`${prefix}.dynamic.saved`) }}</span>
        </div>
      </fieldset>
      <p v-if="mode === 'dynamic' && dynamicError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ dynamicError }}<button v-if="!configLoaded" type="button" class="ml-2 underline" @click="loadDynamicConfig">{{ t(`${prefix}.dynamic.reload`) }}</button></p>
      <div v-if="mode === 'fixed'" class="flex flex-wrap items-center gap-3">
        <button type="button" class="btn btn-secondary min-h-10" :disabled="disabled || running || selected.length === 0" @click="runTests">
          <LoadingSpinner v-if="running" size="sm" class="mr-2" />
          {{ running ? t(`${prefix}.testRunning`) : t(`${prefix}.testStart`) }}
        </button>
        <button v-if="running" type="button" class="btn btn-secondary min-h-10" @click="cancelTests">{{ t('common.cancel') }}</button>
      </div>
      <div v-else-if="running || dynamicRun" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800" aria-live="polite">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <span>{{ dynamicCancelled && (!dynamicRun || dynamicRun.status === 'running') ? t(`${prefix}.dynamic.stopping`) : dynamicRun ? t(`${prefix}.dynamic.status.${dynamicRun.status}`) : t(`${prefix}.dynamic.starting`) }}</span>
          <button v-if="running" type="button" class="btn btn-secondary min-h-10" @click="cancelTests">{{ t('common.cancel') }}</button>
        </div>
        <p v-if="dynamicRun" class="mt-2 tabular-nums">{{ t(`${prefix}.dynamic.progress`, { recorded: dynamicRun.recorded, target: dynamicRun.target_count, attempts: dynamicRun.attempts, max: dynamicRun.max_attempts, batches: dynamicRun.fetched_batches }) }}</p>
        <p v-if="dynamicRun?.message" class="mt-2 break-words text-xs text-gray-500 dark:text-gray-400">{{ dynamicRun.message }}</p>
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
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { HealthyTurnStateDynamicConfig, HealthyTurnStateDynamicRun, HealthyTurnStateTestResult } from '@/api/admin/accounts'
import type { Proxy } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import OpenAIHealthyTurnStateStatus from './OpenAIHealthyTurnStateStatus.vue'

const props = defineProps<{ accountId: number; proxies: Proxy[]; active: boolean; disabled?: boolean }>()
const { t } = useI18n()
const prefix = 'admin.accounts.openai'
const expanded = ref(false)
const model = ref('gpt-6-astra')
const transport = ref<'http' | 'websocket'>('http')
const selected = ref(['account'])
const search = ref('')
const running = ref(false)
const statsRevision = ref(0)
const mode = ref<'fixed' | 'dynamic'>('fixed')
const apiUrl = ref('')
const defaultDynamicConfig = (): HealthyTurnStateDynamicConfig => ({ configured: false, api_url_masked: '', protocol: 'http', target_count: 5, max_attempts: 100 })
const dynamicConfig = ref(defaultDynamicConfig())
const configLoading = ref(false)
const configLoaded = ref(false)
const configSaved = ref(false)
const saving = ref(false)
const dynamicError = ref('')
const dynamicRun = ref<HealthyTurnStateDynamicRun | null>(null)
const dynamicCancelled = ref(false)
let configController: AbortController | null = null
let disposed = false
type DynamicOperation = { accountId: number; controller: AbortController; runId?: string; cancelled: boolean; stopSent: boolean; finished: boolean }
let dynamicOperation: DynamicOperation | null = null
const dynamicConfigValid = computed(() => {
  const { target_count: target, max_attempts: maximum } = dynamicConfig.value
  return (dynamicConfig.value.configured || apiUrl.value.trim() !== '') && Number.isInteger(target) && target >= 1 && target <= 100 && Number.isInteger(maximum) && maximum >= target && maximum <= 1000
})
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

function currentDynamicOperation(operation: DynamicOperation) {
  return !disposed && props.active && props.accountId === operation.accountId && dynamicOperation === operation
}

function dynamicApiErrorMessage(error: unknown, fallback: string) {
  // 管理接口返回经过脱敏的业务说明；非结构化网络错误可能携带提取地址，使用通用提示。
  const apiError = error as { status?: number; response?: { data?: unknown } } | null
  return apiError && (typeof apiError.status === 'number' && apiError.status > 0 || apiError.response?.data)
    ? extractApiErrorMessage(error, fallback)
    : fallback
}

async function stopDynamicOperation(operation: DynamicOperation) {
  if (!operation.runId || operation.stopSent || operation.finished) return
  operation.stopSent = true
  try {
    const result = await adminAPI.accounts.stopHealthyTurnStateDynamicRun(operation.accountId, operation.runId)
    operation.finished = result.status !== 'running'
    if (currentDynamicOperation(operation)) {
      dynamicRun.value = result
      statsRevision.value++
    }
  } catch (error) {
    if (currentDynamicOperation(operation)) dynamicError.value = `${t(`${prefix}.dynamic.stopFailed`)} ${dynamicApiErrorMessage(error, '')}`.trim()
  }
}

function cancelTests() {
  controller?.abort()
  if (dynamicOperation && !dynamicOperation.finished) {
    dynamicOperation.cancelled = true
    dynamicCancelled.value = true
    void stopDynamicOperation(dynamicOperation)
  }
}

function toggleExpanded() {
  if (expanded.value) stopForPageClose()
  expanded.value = !expanded.value
}

function stopForPageClose() {
  cancelTests()
  configController?.abort()
  apiUrl.value = ''
}

async function loadDynamicConfig() {
  if (!props.active || disposed) return
  configController?.abort()
  const current = new AbortController()
  configController = current
  const accountId = props.accountId
  configLoading.value = true
  dynamicError.value = ''
  try {
    const config = await adminAPI.accounts.getHealthyTurnStateDynamicConfig(accountId, current.signal)
    if (configController !== current || current.signal.aborted || disposed || props.accountId !== accountId) return
    dynamicConfig.value = config
    configLoaded.value = true
  } catch (error) {
    if (configController === current && !current.signal.aborted && !disposed) dynamicError.value = dynamicApiErrorMessage(error, t(`${prefix}.dynamic.loadFailed`))
  } finally {
    if (configController === current) configLoading.value = false
  }
}

function dynamicConfigInput() {
  return { api_url: apiUrl.value.trim(), protocol: dynamicConfig.value.protocol, target_count: dynamicConfig.value.target_count, max_attempts: dynamicConfig.value.max_attempts }
}

async function saveDynamicConfig() {
  if (!dynamicConfigValid.value || saving.value || running.value || !props.active || props.disabled) return
  configController?.abort()
  const current = new AbortController()
  configController = current
  const accountId = props.accountId
  saving.value = true
  dynamicError.value = ''
  try {
    const config = await adminAPI.accounts.updateHealthyTurnStateDynamicConfig(accountId, dynamicConfigInput(), current.signal)
    if (configController !== current || current.signal.aborted || disposed || props.accountId !== accountId) return
    dynamicConfig.value = config
    apiUrl.value = ''
    configSaved.value = true
  } catch (error) {
    if (configController === current && !current.signal.aborted && !disposed) dynamicError.value = dynamicApiErrorMessage(error, t(`${prefix}.dynamic.saveFailed`))
  } finally {
    if (configController === current) saving.value = false
  }
}

async function runDynamicTests() {
  if (running.value || saving.value || !configLoaded.value || !dynamicConfigValid.value || props.disabled || !props.active) return
  const current = new AbortController()
  const operation: DynamicOperation = { accountId: props.accountId, controller: current, cancelled: false, stopSent: false, finished: false }
  const request = { model: model.value.trim(), transport: transport.value }
  dynamicOperation = operation
  controller = current
  running.value = true
  dynamicRun.value = null
  dynamicCancelled.value = false
  dynamicError.value = ''
  results.value = []
  let phase: 'save' | 'start' | 'step' = 'save'
  try {
    const config = await adminAPI.accounts.updateHealthyTurnStateDynamicConfig(operation.accountId, dynamicConfigInput(), current.signal)
    if (!currentDynamicOperation(operation) || operation.cancelled) return
    dynamicConfig.value = config
    apiUrl.value = ''
    configSaved.value = true
    phase = 'start'
    // 启动请求使用有限超时且不绑定界面取消，确保迟到的 run ID 仍能用于停止；取消后绝不发送 step。
    let run = await adminAPI.accounts.startHealthyTurnStateDynamicRun(operation.accountId, request)
    operation.runId = run.id
    operation.finished = run.status !== 'running'
    if (!currentDynamicOperation(operation) || operation.cancelled) {
      await stopDynamicOperation(operation)
      return
    }
    dynamicRun.value = run
    phase = 'step'
    while (run.status === 'running' && currentDynamicOperation(operation) && !operation.cancelled) {
      const previousAttempts = run.attempts
      run = await adminAPI.accounts.stepHealthyTurnStateDynamicRun(operation.accountId, run.id, current.signal)
      if (!currentDynamicOperation(operation) || operation.cancelled) break
      dynamicRun.value = run
      operation.finished = run.status !== 'running'
      if (run.last_result && run.attempts > previousAttempts) {
        results.value = [...results.value, { ...run.last_result, key: `${run.id}-${run.attempts}`, label: t(`${prefix}.dynamic.attempt`, { count: run.attempts }) }].slice(-50)
      }
      statsRevision.value++
    }
    if (currentDynamicOperation(operation) && !operation.cancelled && run.status === 'failed') dynamicError.value = run.message || t(`${prefix}.dynamic.runFailed`)
  } catch (error) {
    if (currentDynamicOperation(operation) && !operation.cancelled) dynamicError.value = dynamicApiErrorMessage(error, t(`${prefix}.dynamic.${phase === 'save' ? 'saveFailed' : 'runFailed'}`))
    await stopDynamicOperation(operation)
  } finally {
    if (currentDynamicOperation(operation)) {
      running.value = false
      if (controller === current) controller = null
    }
  }
}

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
  configController?.abort()
  configController = null
  dynamicOperation = null
  controller = null
  running.value = false
  saving.value = false
  configLoading.value = false
  configLoaded.value = false
  configSaved.value = false
  dynamicConfig.value = defaultDynamicConfig()
  apiUrl.value = ''
  dynamicRun.value = null
  dynamicError.value = ''
  dynamicCancelled.value = false
  mode.value = 'fixed'
  results.value = []
  selected.value = ['account']
  expanded.value = false
})
watch(() => [expanded.value, mode.value, props.active] as const, () => {
  if (expanded.value && mode.value === 'dynamic' && props.active && !configLoaded.value) void loadDynamicConfig()
})
watch(() => [apiUrl.value, dynamicConfig.value.protocol, dynamicConfig.value.target_count, dynamicConfig.value.max_attempts], () => { configSaved.value = false }, { flush: 'sync' })
onMounted(() => window.addEventListener('pagehide', stopForPageClose))
onBeforeUnmount(() => {
  disposed = true
  window.removeEventListener('pagehide', stopForPageClose)
  stopForPageClose()
})
</script>
