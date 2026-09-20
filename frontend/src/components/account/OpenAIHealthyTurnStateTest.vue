<template>
  <section class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-600" :aria-label="t(`${prefix}.testTitle`)">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t(`${prefix}.testTitle`) }}</h3>
      <span class="rounded-md bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ t(`${prefix}.dynamic.dynamic`) }}</span>
    </div>
    <p class="text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`${prefix}.dynamic.description`) }}</p>
    <p class="rounded-lg bg-gray-50 p-3 text-xs leading-relaxed text-gray-600 dark:bg-dark-800 dark:text-gray-400">{{ t(`${prefix}.dynamic.saveWithAccount`) }}</p>
    <p v-if="loading" role="status" class="text-sm text-gray-600 dark:text-gray-400">{{ t('common.loading') }}</p>
    <div v-if="loadError" role="alert" class="text-sm text-red-600 dark:text-red-400">
      {{ loadError }}
      <button type="button" class="ml-2 min-h-11 underline" :disabled="loading || disabled" @click="loadSettings">{{ t(`${prefix}.dynamic.reload`) }}</button>
    </div>
    <fieldset :disabled="loading || saving || disabled || !loaded" class="min-w-0 space-y-4" @input="dirty = true" @change="dirty = true">
      <div>
        <label for="healthy-state-dynamic-url" class="input-label">{{ t(`${prefix}.dynamic.url`) }}</label>
        <input id="healthy-state-dynamic-url" v-model="apiUrl" type="password" autocomplete="new-password" class="input" :placeholder="t(`${prefix}.dynamic.urlPlaceholder`)" aria-describedby="healthy-state-dynamic-url-hint" />
        <p id="healthy-state-dynamic-url-hint" class="mt-1 break-words text-xs text-gray-600 dark:text-gray-400">
          <span v-if="dynamicConfig.shared_proxy_conflict">{{ t(`${prefix}.dynamic.sharedProxyConflict`) }}</span>
          <span v-else>{{ dynamicConfig.configured ? t(`${prefix}.dynamic.savedUrl`, { url: dynamicConfig.api_url_masked }) : t(`${prefix}.dynamic.urlRequired`) }}</span>
        </p>
      </div>
      <fieldset class="min-w-0" aria-describedby="healthy-state-model-hint">
        <legend class="input-label">{{ t(`${prefix}.testModel`) }}</legend>
        <p id="healthy-state-model-hint" class="mb-2 text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`${prefix}.dynamic.modelsHint`) }}</p>
        <p v-if="dynamicConfig.models_inheritance_message" role="status" class="mb-2 text-xs leading-relaxed text-amber-700 dark:text-amber-400">{{ dynamicConfig.models_inheritance_message }}</p>
        <p v-else-if="dynamicConfig.models_inherited && dynamicConfig.models.length" role="status" class="mb-2 text-xs leading-relaxed text-primary-700 dark:text-primary-300">{{ t(`${prefix}.dynamic.modelsInherited`) }}</p>
        <div class="grid max-h-64 gap-1 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600 sm:grid-cols-2">
          <label v-for="model in supportedModels" :key="model.id" class="flex min-h-11 cursor-pointer items-center gap-3 rounded px-2 text-sm hover:bg-gray-50 dark:hover:bg-dark-700">
            <input v-model="dynamicConfig.models" type="checkbox" :value="model.id" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500" @change="clearInheritanceNotice" />
            <span class="min-w-0 break-words">{{ model.display_name || model.id }}<span v-if="model.display_name && model.display_name !== model.id" class="block text-xs text-gray-500 dark:text-gray-400">{{ model.id }}</span></span>
          </label>
          <label v-for="model in unsupportedSelections" :key="model" class="flex min-h-11 cursor-pointer items-center gap-3 rounded px-2 text-sm text-amber-700 dark:text-amber-400">
            <input v-model="dynamicConfig.models" type="checkbox" :value="model" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500" @change="clearInheritanceNotice" />
            <span class="min-w-0 break-words">{{ t(`${prefix}.dynamic.unsupportedModel`, { model }) }}</span>
          </label>
          <p v-if="loaded && supportedModels.length === 0" class="p-2 text-sm text-gray-600 dark:text-gray-400 sm:col-span-2">{{ t(`${prefix}.dynamic.noModels`) }}</p>
        </div>
        <p v-if="loaded && (!dynamicConfig.models.length || unsupportedSelections.length)" class="mt-2 text-xs text-amber-700 dark:text-amber-400">{{ t(`${prefix}.dynamic.modelsRequired`) }}</p>
      </fieldset>
      <div class="grid gap-3 sm:grid-cols-2">
        <div>
          <label for="healthy-state-dynamic-target" class="input-label">{{ t(`${prefix}.dynamic.target`) }}</label>
          <div class="flex flex-wrap items-center gap-2">
            <input id="healthy-state-dynamic-target" v-model.number="dynamicConfig.target_count" type="number" min="1" max="100" step="1" class="input min-w-24 flex-1" aria-describedby="healthy-state-dynamic-target-hint healthy-state-dynamic-target-summary" />
            <button type="button" class="btn btn-secondary min-h-11 text-xs" :disabled="dynamicConfig.target_count === defaultTargetCount" @click="applyDefaultTarget">{{ t(`${prefix}.dynamic.targetPreset`, { count: defaultTargetCount }) }}</button>
          </div>
          <p id="healthy-state-dynamic-target-hint" class="mt-1 text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`${prefix}.dynamic.targetHint`) }}</p>
          <p v-if="totalTarget !== null" id="healthy-state-dynamic-target-summary" class="mt-1 text-xs font-medium text-primary-700 dark:text-primary-300">{{ t(`${prefix}.dynamic.targetSummary`, { models: dynamicConfig.models.length, total: totalTarget }) }}</p>
        </div>
        <div>
          <label for="healthy-state-dynamic-attempts" class="input-label">{{ t(`${prefix}.dynamic.maxAttempts`) }}</label>
          <input id="healthy-state-dynamic-attempts" v-model.number="dynamicConfig.max_attempts" type="number" :min="dynamicConfig.target_count || 1" max="1000" step="1" class="input" />
        </div>
        <div>
          <label for="healthy-state-dynamic-protocol" class="input-label">{{ t(`${prefix}.dynamic.protocol`) }}</label>
          <select id="healthy-state-dynamic-protocol" v-model="dynamicConfig.protocol" class="input">
            <option value="http">HTTP</option><option value="https">HTTPS</option><option value="socks5h">SOCKS5H</option>
          </select>
        </div>
        <div>
          <label for="healthy-state-test-transport" class="input-label">{{ t(`${prefix}.testTransport`) }}</label>
          <select id="healthy-state-test-transport" v-model="dynamicConfig.transport" class="input">
            <option value="http">HTTP Responses</option><option value="websocket">WebSocket Responses</option>
          </select>
        </div>
      </div>
    </fieldset>
    <p v-if="saveError" ref="errorElement" role="alert" tabindex="-1" class="text-sm text-red-600 dark:text-red-400">{{ saveError }}</p>
    <p v-if="saving" role="status" class="text-sm text-gray-600 dark:text-gray-400">{{ t(`${prefix}.dynamic.saving`) }}</p>
  </section>
  <OpenAIHealthyTurnStateStatus :account-id="accountId" :active="active" :revision="statsRevision" />
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { HealthyTurnStateDynamicConfig, HealthyTurnStateSupportedModel } from '@/api/admin/accounts'
import { extractApiErrorMessage } from '@/utils/apiError'
import OpenAIHealthyTurnStateStatus from './OpenAIHealthyTurnStateStatus.vue'

const props = defineProps<{ accountId: number; active: boolean; disabled?: boolean }>()
const { t } = useI18n()
const prefix = 'admin.accounts.openai'
const defaultTargetCount = 4
const defaultConfig = (): HealthyTurnStateDynamicConfig => ({ configured: false, api_url_masked: '', protocol: 'http', target_count: defaultTargetCount, max_attempts: 100, models: [], transport: 'http' })
const dynamicConfig = ref(defaultConfig())
const loadedProtocol = ref<HealthyTurnStateDynamicConfig['protocol']>('http')
const loadedModels = ref<string[]>([])
const supportedModels = ref<HealthyTurnStateSupportedModel[]>([])
const apiUrl = ref('')
// 仅界面输入和快捷调整标记变更，读取旧配置和刷新上游模型不会阻断账号其他字段的保存。
const dirty = ref(false)
const loaded = ref(false)
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
const statsRevision = ref(0)
const errorElement = ref<HTMLElement | null>(null)
let controller: AbortController | null = null
let disposed = false
const unsupportedSelections = computed(() => dynamicConfig.value.models.filter(model => !supportedModels.value.some(supported => supported.id === model)))
const modelsChanged = computed(() => JSON.stringify([...dynamicConfig.value.models].sort()) !== JSON.stringify([...loadedModels.value].sort()))
const totalTarget = computed(() => {
  const target = dynamicConfig.value.target_count
  return Number.isInteger(target) && target >= 1 && target <= 100 ? dynamicConfig.value.models.length * target : null
})
const validConfig = computed(() => {
  const { target_count: target, max_attempts: attempts, models } = dynamicConfig.value
  return (dynamicConfig.value.configured || apiUrl.value.trim() !== '') && models.length > 0 && unsupportedSelections.value.length === 0 && Number.isInteger(target) && target >= 1 && target <= 100 && Number.isInteger(attempts) && attempts >= target && attempts <= 1000
})

function apiErrorMessage(error: unknown, fallback: string) {
  // 管理接口返回经过脱敏的业务说明；非结构化网络错误可能携带提取地址，使用通用提示。
  const apiError = error as { status?: number; response?: { data?: unknown } } | null
  return apiError && (typeof apiError.status === 'number' && apiError.status > 0 || apiError.response?.data)
    ? extractApiErrorMessage(error, fallback)
    : fallback
}

function clearInheritanceNotice() {
  dynamicConfig.value.models_inherited = false
  dynamicConfig.value.models_inheritance_message = ''
}

function applyDefaultTarget() {
  dynamicConfig.value.target_count = defaultTargetCount
  if (dynamicConfig.value.max_attempts < defaultTargetCount) dynamicConfig.value.max_attempts = defaultTargetCount
  dirty.value = true
}

async function loadSettings() {
  if (!props.active || disposed) return
  controller?.abort()
  const current = new AbortController()
  controller = current
  const accountId = props.accountId
  loading.value = true
  loadError.value = ''
  saveError.value = ''
  loaded.value = false
  const [configResult, modelsResult] = await Promise.allSettled([
    adminAPI.accounts.getHealthyTurnStateDynamicConfig(accountId, current.signal),
    adminAPI.accounts.getHealthyTurnStateModels(accountId, current.signal)
  ])
  if (controller !== current || current.signal.aborted || disposed || props.accountId !== accountId) return
  loading.value = false
  if (configResult.status === 'fulfilled') {
    dynamicConfig.value = { ...defaultConfig(), ...configResult.value, models: [...(configResult.value.models || [])] }
    loadedProtocol.value = dynamicConfig.value.protocol
    loadedModels.value = [...dynamicConfig.value.models]
  }
  if (modelsResult.status === 'fulfilled') supportedModels.value = modelsResult.value
  if (configResult.status === 'rejected') loadError.value = apiErrorMessage(configResult.reason, t(`${prefix}.dynamic.loadFailed`))
  else if (modelsResult.status === 'rejected') loadError.value = apiErrorMessage(modelsResult.reason, t(`${prefix}.dynamic.modelsLoadFailed`))
  else loaded.value = true
}

async function showSaveError(message: string) {
  saveError.value = message
  await nextTick()
  errorElement.value?.focus()
  return false
}

async function saveConfig(allowUnchanged = false): Promise<boolean> {
  if (saving.value || disposed || !props.active) return false
  if (allowUnchanged && !dirty.value) return true
  if (!loaded.value) return showSaveError(loadError.value || t(`${prefix}.dynamic.loadBeforeSave`))
  if (!validConfig.value) return showSaveError(t(`${prefix}.dynamic.validation`))
  controller?.abort()
  const current = new AbortController()
  controller = current
  const accountId = props.accountId
  saving.value = true
  saveError.value = ''
  try {
    const config = await adminAPI.accounts.updateHealthyTurnStateDynamicConfig(accountId, {
      api_url: apiUrl.value.trim(), protocol: dynamicConfig.value.protocol,
      // 仅显式修改全局配置时更新，避免旧弹窗保存账号设置时覆盖已更新的协议。
      update_shared_proxy: apiUrl.value.trim() !== '' || dynamicConfig.value.protocol !== loadedProtocol.value,
      // 沿用支持交集时不缩减全局选择，只有用户实际修改模型才更新下次默认。
      update_default_models: modelsChanged.value,
      target_count: dynamicConfig.value.target_count, max_attempts: dynamicConfig.value.max_attempts,
      models: [...dynamicConfig.value.models], transport: dynamicConfig.value.transport
    }, current.signal)
    if (controller !== current || current.signal.aborted || disposed || props.accountId !== accountId) return false
    dynamicConfig.value = { ...config, models: [...config.models] }
    loadedProtocol.value = config.protocol
    loadedModels.value = [...config.models]
    apiUrl.value = ''
    dirty.value = false
    statsRevision.value++
    return true
  } catch (error) {
    if (controller === current && !current.signal.aborted && !disposed) await showSaveError(apiErrorMessage(error, t(`${prefix}.dynamic.saveFailed`)))
    return false
  } finally {
    if (controller === current) saving.value = false
  }
}

watch(() => [props.accountId, props.active] as const, () => {
  controller?.abort()
  controller = null
  dynamicConfig.value = defaultConfig()
  loadedProtocol.value = 'http'
  loadedModels.value = []
  supportedModels.value = []
  apiUrl.value = ''
  dirty.value = false
  loaded.value = false
  loading.value = false
  saving.value = false
  loadError.value = ''
  saveError.value = ''
  if (props.active) void loadSettings()
}, { immediate: true })
watch(() => [apiUrl.value, dynamicConfig.value.protocol, dynamicConfig.value.target_count, dynamicConfig.value.max_attempts, dynamicConfig.value.transport, dynamicConfig.value.models.join(',')], () => { saveError.value = '' })
onBeforeUnmount(() => { disposed = true; controller?.abort(); apiUrl.value = '' })
defineExpose({ saveConfig, dirty })
</script>
