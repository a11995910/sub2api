<template>
  <section class="space-y-5" data-testid="candy-account-tests">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h3 class="text-lg font-semibold">{{ t('candyTests.title') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('candyTests.hint') }}</p>
      </div>
      <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading || saving || starting || stopping" @click="refresh">
        <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />{{ t('candyTests.refresh') }}
      </button>
    </div>
    <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
    <p v-if="notice" role="status" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300">{{ notice }}</p>
    <div v-if="!draft" class="rounded-xl border border-gray-200 p-8 text-center text-sm text-gray-500 dark:border-dark-700" role="status">{{ t('candyTests.loading') }}</div>
    <form v-else class="rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800" data-testid="candy-settings" @submit.prevent="save">
      <fieldset :disabled="saving || starting || stopping" class="p-5">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <label for="candy-prompt" class="input-label mb-0">{{ t('candyTests.prompt') }}</label>
          <button type="button" class="text-xs text-primary-600 hover:underline dark:text-primary-400" data-testid="candy-reset" @click="resetQuestion">{{ t('candyTests.resetPrompt') }}</button>
        </div>
        <textarea id="candy-prompt" v-model="draft.prompt" rows="7" maxlength="32000" required class="input mt-2 w-full text-sm leading-6" data-testid="candy-prompt" />
        <div class="mt-4 grid gap-4 sm:grid-cols-3">
          <div>
            <label for="candy-answer" class="input-label">{{ t('candyTests.expectedAnswer') }}</label>
            <input id="candy-answer" v-model.trim="draft.expected_answer" maxlength="100" inputmode="numeric" pattern="[+-]?[0-9]+" required class="input w-full" data-testid="candy-answer" />
          </div>
          <div>
            <label for="candy-model" class="input-label">{{ t('candyTests.model') }}</label>
            <input id="candy-model" v-model.trim="draft.model_id" maxlength="100" :placeholder="t('candyTests.modelPlaceholder')" class="input w-full" data-testid="candy-model" />
          </div>
          <div>
            <label for="candy-effort" class="input-label">{{ t('candyTests.effort') }}</label>
            <Select id="candy-effort" v-model="draft.reasoning_effort" :options="effortOptions" :aria-label="t('candyTests.effort')" />
          </div>
        </div>
        <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('candyTests.answerHint') }}</p>

        <div class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-700">
          <div class="flex items-center justify-between gap-4">
            <div><h4 class="text-sm font-semibold">{{ t('candyTests.schedule') }}</h4><p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('candyTests.scheduleHint') }}</p></div>
            <Toggle v-model="draft.schedule_enabled" :aria-label="t('candyTests.schedule')" data-testid="candy-schedule-enabled" />
          </div>
          <div v-if="draft.schedule_enabled" class="mt-3 max-w-sm">
            <label for="candy-interval-hours" class="input-label">{{ t('candyTests.intervalHours') }}</label>
            <input id="candy-interval-hours" v-model.number="draft.interval_hours" type="number" min="1" max="168" step="1" required class="input w-full" data-testid="candy-interval-hours" />
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('candyTests.intervalHint', { hours: draft.interval_hours }) }}</p>
          </div>
          <p v-if="settings?.next_run_at" class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('candyTests.nextRun', { time: beijingTime(settings.next_run_at) }) }}</p>
        </div>

        <div class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-700">
          <div class="flex items-center justify-between gap-4">
            <div><h4 class="text-sm font-semibold">{{ t('candyTests.autoGroup') }}</h4><p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('candyTests.autoGroupHint') }}</p></div>
            <Toggle v-model="draft.auto_group_enabled" :aria-label="t('candyTests.autoGroup')" data-testid="candy-auto-group-enabled" />
          </div>
          <div v-if="draft.auto_group_enabled" class="mt-4 space-y-3">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('candyTests.groupHint') }}</p>
            <p v-if="groupsFailed" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('candyTests.groupsLoadFailed') }}</p>
            <p v-if="unavailableGroupIds.length" role="alert" class="text-sm text-amber-600 dark:text-amber-400">
              {{ t('candyTests.missingGroups', { ids: unavailableGroupIds.map(id => `#${id}`).join('、') }) }}
              <button type="button" class="ml-2 underline" @click="clearUnavailableGroups">{{ t('common.clear') }}</button>
            </p>
            <div class="grid gap-4 lg:grid-cols-2">
              <div v-for="rule in groupRules" :key="rule.status" class="rounded-lg border border-gray-200 p-4 dark:border-dark-600" :data-testid="`candy-rule-${rule.status}`">
                <h5 class="mb-3 text-sm font-semibold" :class="rule.status === 'normal' ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400'">{{ t(`candyTests.${rule.status}`) }}</h5>
                <fieldset class="mb-4"><legend class="mb-1 text-sm">{{ t('candyTests.addGroups') }}</legend><GroupSelector v-model="draft[rule.add]" :groups="groups" :data-testid="`candy-${rule.status}-add`" /></fieldset>
                <fieldset><legend class="mb-1 text-sm">{{ t('candyTests.removeGroups') }}</legend><GroupSelector v-model="draft[rule.remove]" :groups="groups" :data-testid="`candy-${rule.status}-remove`" /></fieldset>
              </div>
            </div>
            <p v-if="groupConflict" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('candyTests.conflict') }}</p>
          </div>
        </div>
      </fieldset>
      <div class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 px-5 py-4 dark:border-dark-700">
        <div class="text-xs text-gray-500 dark:text-gray-400"><p :class="{ 'text-amber-600 dark:text-amber-400': dirty }" data-testid="candy-save-state">{{ t(dirty ? 'candyTests.unsaved' : 'candyTests.savedState') }}</p><p class="mt-1">{{ t('candyTests.snapshotHint') }}</p></div>
        <button type="submit" class="btn btn-primary" :disabled="saving || starting || stopping || !dirty || groupConflict || (draft.auto_group_enabled && (groupsFailed || unavailableGroupIds.length > 0))" data-testid="candy-save">{{ t(saving ? 'candyTests.saving' : 'candyTests.save') }}</button>
      </div>
    </form>

    <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800">
      <div><p class="text-sm">{{ t('candyTests.scope') }}</p><p v-if="settings?.last_run_at" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('candyTests.lastRun', { time: beijingTime(settings.last_run_at) }) }}</p></div>
      <div class="flex flex-wrap gap-2">
        <button v-if="running && settings?.running_run_id" type="button" class="btn btn-secondary" :disabled="stopping || saving || starting" data-testid="candy-stop" @click="requestStop">{{ t('candyTests.stop') }}</button>
        <button type="button" class="btn btn-primary inline-flex items-center gap-2" :disabled="!draft || dirty || starting || saving || stopping || running" data-testid="candy-run" @click="run"><Icon name="play" size="sm" />{{ t(running || starting ? 'candyTests.running' : 'candyTests.run') }}</button>
      </div>
    </div>

    <section class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
      <header class="border-b border-gray-100 p-5 dark:border-dark-700"><h3 class="font-semibold">{{ t('candyTests.history') }}</h3><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('candyTests.historyHint') }}</p></header>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900/40 dark:text-gray-400"><tr><th class="px-5 py-3">{{ t('candyTests.time') }}</th><th class="px-4 py-3">{{ t('candyTests.account') }}</th><th class="px-4 py-3">{{ t('candyTests.result') }}</th><th class="px-4 py-3">{{ t('candyTests.answer') }}</th><th class="px-4 py-3">{{ t('candyTests.groupAction') }}</th><th class="px-4 py-3" /></tr></thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-if="resultsLoading && !results.length"><td colspan="6" class="p-6 text-center text-gray-500" role="status">{{ t('candyTests.loading') }}</td></tr>
            <tr v-for="result in results" :key="result.id" :data-testid="`candy-result-${result.id}`">
              <td class="whitespace-nowrap px-5 py-4 text-xs tabular-nums">{{ beijingTime(result.started_at) }}</td>
              <td class="px-4 py-4"><p class="font-medium">{{ result.account_name || `#${result.account_id}` }}</p><p class="mt-1 text-xs text-gray-500">#{{ result.account_id }} · {{ result.model_id || '—' }}</p></td>
              <td class="px-4 py-4"><span class="inline-flex whitespace-nowrap rounded-full px-2 py-1 text-xs" :class="statusClass(result.status)">{{ t(`candyTests.statuses.${result.status}`) }}</span><p v-if="result.error_message" class="mt-1 max-w-xs break-words text-xs text-red-600 dark:text-red-400">{{ result.error_message }}</p></td>
              <td class="px-4 py-4 font-mono">{{ result.answer || '—' }}</td>
              <td class="px-4 py-4 text-xs"><p>{{ groupActionLabel(result) }}</p><p v-if="result.group_error" class="mt-1 max-w-xs break-words text-red-600 dark:text-red-400">{{ t('candyTests.groupError') }}：{{ result.group_error }}</p></td>
              <td class="px-4 py-4 text-right"><button type="button" class="whitespace-nowrap text-xs text-primary-600 hover:underline dark:text-primary-400" :data-testid="`candy-detail-${result.id}`" @click="openDetail(result)">{{ t('candyTests.detail') }}</button></td>
            </tr>
            <tr v-if="!resultsLoading && !results.length"><td colspan="6" class="p-8 text-center text-sm text-gray-500">{{ t('candyTests.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
      <Pagination v-if="total > 0" :total="total" :page="page" :page-size="pageSize" show-jump @update:page="changePage" @update:page-size="changePageSize" />
    </section>

    <BaseDialog :show="detail !== null" :title="t('candyTests.detailTitle', { account: detail?.account_name || `#${detail?.account_id}` })" width="wide" @close="closeDetail">
      <div v-if="detail" class="space-y-4 text-sm">
        <p v-if="detailLoading" role="status">{{ t('candyTests.loading') }}</p>
        <p v-if="detailError" role="alert" class="text-red-600 dark:text-red-400">{{ detailError }}</p>
        <p>{{ t('candyTests.result') }}：<strong>{{ t(`candyTests.statuses.${detail.status}`) }}</strong> · {{ t('candyTests.answer') }}：{{ detail.answer || '—' }} · {{ t('candyTests.expectedAnswer') }}：{{ detail.settings_snapshot?.expected_answer || '—' }}</p>
        <div v-if="detail.error_message" class="rounded-lg bg-red-50 p-3 text-red-700 dark:bg-red-900/20 dark:text-red-300"><strong>{{ t('candyTests.testError') }}</strong><p class="mt-1 break-words">{{ detail.error_message }}</p></div>
        <div><h4 class="mb-1 font-medium">{{ t('candyTests.groupAction') }}</h4><p>{{ groupActionLabel(detail) }}</p><p v-if="detail.group_error" class="mt-1 text-red-600 dark:text-red-400">{{ t('candyTests.groupError') }}：{{ detail.group_error }}</p></div>
        <div><h4 class="mb-2 font-medium">{{ t('candyTests.prompt') }}</h4><pre class="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 font-sans text-sm leading-6 dark:bg-dark-900">{{ detail.settings_snapshot?.prompt || '—' }}</pre></div>
        <div><h4 class="mb-2 font-medium">{{ t('candyTests.response') }}</h4><pre class="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 font-sans text-sm leading-6 dark:bg-dark-900" data-testid="candy-response">{{ detail.response_text || t('candyTests.noResponse') }}</pre></div>
      </div>
      <template #footer><button type="button" class="btn btn-secondary" @click="closeDetail">{{ t('candyTests.close') }}</button></template>
    </BaseDialog>
    <ConfirmDialog :show="confirmStop" :title="t('candyTests.stopTitle')" :message="t('candyTests.stopConfirm')" :confirm-text="t('candyTests.stop')" danger @confirm="stop" @cancel="confirmStop = false" />
  </section>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { candyTestsAPI, DEFAULT_CANDY_PROMPT, type CandyTestResult, type CandyTestSettings, type CandyTestSettingsInput } from '@/api/admin/candyTests'
import { adminAPI } from '@/api'
import type { AdminGroup } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const settings = ref<CandyTestSettings | null>(null)
const draft = ref<CandyTestSettingsInput | null>(null)
const groups = ref<AdminGroup[]>([])
const groupsFailed = ref(false)
const loading = ref(false)
const saving = ref(false)
const starting = ref(false)
const stopping = ref(false)
const confirmStop = ref(false)
const error = ref('')
const notice = ref('')
const results = ref<CandyTestResult[]>([])
const resultsLoading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(getPersistedPageSize())
const detail = ref<CandyTestResult | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
let alive = true
let polling = false
let timer: ReturnType<typeof setInterval> | undefined
let resultsController: AbortController | undefined
let requestId = 0
let detailRequestId = 0
let settingsRequestId = 0
let stopRunId = ''

const groupRules = [
  { status: 'normal', add: 'normal_add_group_ids', remove: 'normal_remove_group_ids' },
  { status: 'degraded', add: 'degraded_add_group_ids', remove: 'degraded_remove_group_ids' },
] as const
const groupKeys = groupRules.flatMap(rule => [rule.add, rule.remove])
const effortOptions = computed(() => ['minimal', 'low', 'medium', 'high', 'xhigh'].map(value => ({ value, label: t(`pelicanShowcase.efforts.${value}`) })))
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== JSON.stringify(settings.value ? inputFrom(settings.value) : null))
const running = computed(() => !!settings.value?.running_run_id)
const groupConflict = computed(() => !!draft.value?.auto_group_enabled && groupRules.some(rule => draft.value![rule.add].some(id => draft.value![rule.remove].includes(id))))
const unavailableGroupIds = computed(() => {
  if (!draft.value || groupsFailed.value) return []
  const available = new Set(groups.value.map(group => group.id))
  return [...new Set(groupKeys.flatMap(key => draft.value![key]))].filter(id => !available.has(id))
})

function inputFrom(value: CandyTestSettingsInput): CandyTestSettingsInput {
  return {
    prompt: value.prompt, expected_answer: value.expected_answer, model_id: value.model_id,
    reasoning_effort: value.reasoning_effort, schedule_enabled: value.schedule_enabled, interval_hours: value.interval_hours,
    auto_group_enabled: value.auto_group_enabled,
    normal_add_group_ids: [...(value.normal_add_group_ids || [])], normal_remove_group_ids: [...(value.normal_remove_group_ids || [])],
    degraded_add_group_ids: [...(value.degraded_add_group_ids || [])], degraded_remove_group_ids: [...(value.degraded_remove_group_ids || [])],
  }
}

function applySettings(value: CandyTestSettings) {
  const preserveDraft = dirty.value
  settings.value = value
  if (!preserveDraft) draft.value = inputFrom(value)
}

function beijingTime(value: string) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}

function groupActionLabel(result: CandyTestResult) {
  const action = result.group_action
  if (!action || (!action.added_group_ids?.length && !action.removed_group_ids?.length)) return t('candyTests.groupUnchanged')
  const names = (ids: number[]) => ids.map(id => groups.value.find(group => group.id === id)?.name || `#${id}`).join('、') || t('candyTests.none')
  return t('candyTests.groupChanged', { added: names(action.added_group_ids || []), removed: names(action.removed_group_ids || []) })
}

function statusClass(status: CandyTestResult['status']) {
  return {
    running: 'bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300',
    normal: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
    degraded: 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300',
    error: 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300',
  }[status]
}

function resetQuestion() {
  if (!draft.value) return
  draft.value.prompt = DEFAULT_CANDY_PROMPT
  draft.value.expected_answer = '21'
}

function clearUnavailableGroups() {
  if (!draft.value) return
  const unavailable = new Set(unavailableGroupIds.value)
  for (const key of groupKeys) draft.value[key] = draft.value[key].filter(id => !unavailable.has(id))
}

function fail(err: unknown, key: string) {
  error.value = extractApiErrorMessage(err, t(key))
  notice.value = ''
}

async function refresh() {
  if (loading.value || saving.value) return
  const current = ++settingsRequestId
  loading.value = true
  error.value = ''
  const loaded = await Promise.allSettled([candyTestsAPI.getSettings(), adminAPI.groups.getAll('openai'), loadResults()])
  if (!alive) return
  if (current === settingsRequestId) {
    if (loaded[0].status === 'fulfilled') applySettings(loaded[0].value)
    else fail(loaded[0].reason, 'candyTests.loadFailed')
  }
  if (loaded[1].status === 'fulfilled') {
    groups.value = loaded[1].value.filter(group => group.platform === 'openai' && group.status === 'active')
    groupsFailed.value = false
  } else groupsFailed.value = true
  loading.value = false
}

async function loadResults(silent = false) {
  if (silent && resultsLoading.value) return
  const current = ++requestId
  resultsController?.abort()
  const controller = new AbortController()
  resultsController = controller
  resultsLoading.value = true
  try {
    const loaded = await candyTestsAPI.listResults(page.value, pageSize.value, controller.signal)
    if (!alive || current !== requestId) return
    results.value = loaded.items
    total.value = loaded.total
  } catch (err) {
    if (alive && current === requestId && !controller.signal.aborted && !silent) fail(err, 'candyTests.loadFailed')
  } finally {
    if (alive && current === requestId) resultsLoading.value = false
  }
}

async function poll() {
  if (loading.value || saving.value || starting.value || stopping.value || polling || document.hidden) return
  const request = ++settingsRequestId
  polling = true
  try {
    const [current] = await Promise.all([candyTestsAPI.getSettings(), loadResults(true)])
    if (alive && request === settingsRequestId) applySettings(current)
  } catch {
    // 后台刷新失败时保留结果与未保存配置，手动刷新显示错误。
  } finally { polling = false }
}

async function save() {
  if (!draft.value || saving.value || starting.value || stopping.value || groupConflict.value) return
  if (!draft.value.prompt.trim() || !draft.value.expected_answer.trim()) {
    fail(null, 'candyTests.required')
    return
  }
  if (!Number.isInteger(draft.value.interval_hours) || draft.value.interval_hours < 1 || draft.value.interval_hours > 168) {
    fail(null, 'candyTests.invalidInterval')
    return
  }
  if (draft.value.auto_group_enabled && (groupsFailed.value || unavailableGroupIds.value.length)) return
  saving.value = true
  settingsRequestId++
  try {
    const saved = await candyTestsAPI.updateSettings(inputFrom(draft.value))
    if (!alive) return
    settings.value = saved
    draft.value = inputFrom(saved)
    error.value = ''
    notice.value = t('candyTests.saved')
  } catch (err) { fail(err, 'candyTests.saveFailed') }
  finally { saving.value = false }
}

async function run() {
  if (!draft.value || dirty.value || starting.value || saving.value || stopping.value || running.value) return
  starting.value = true
  settingsRequestId++
  try {
    const batch = await candyTestsAPI.run({ all: true })
    if (!alive) return
    applySettings({ ...settings.value, ...batch.settings, running_run_id: batch.run_id })
    page.value = 1
    error.value = ''
    notice.value = t('candyTests.runStarted')
    await refresh()
  } catch (err) { fail(err, 'candyTests.runFailed') }
  finally { starting.value = false }
}

function requestStop() {
  stopRunId = settings.value?.running_run_id || ''
  confirmStop.value = true
}

async function stop() {
  confirmStop.value = false
  if (!stopRunId || stopping.value || saving.value || starting.value) return
  stopping.value = true
  settingsRequestId++
  try {
    await candyTestsAPI.stop(stopRunId)
    error.value = ''
    notice.value = t('candyTests.stopped')
    await refresh()
  } catch (err) { fail(err, 'candyTests.stopFailed') }
  finally { stopping.value = false }
}

function changePage(value: number) { page.value = value; void loadResults() }
function changePageSize(value: number) { pageSize.value = value; page.value = 1; void loadResults() }

async function openDetail(result: CandyTestResult) {
  const current = ++detailRequestId
  detail.value = result
  detailError.value = ''
  detailLoading.value = true
  try {
    const loaded = await candyTestsAPI.getResult(result.id)
    if (alive && current === detailRequestId) detail.value = loaded
  } catch (err) {
    if (alive && current === detailRequestId) detailError.value = extractApiErrorMessage(err, t('candyTests.loadFailed'))
  } finally {
    if (alive && current === detailRequestId) detailLoading.value = false
  }
}

function closeDetail() { detailRequestId++; detail.value = null }
function startPolling() { if (!timer) timer = setInterval(poll, 5000) }
function stopPolling() { if (timer) clearInterval(timer); timer = undefined }
onMounted(() => { void refresh(); startPolling() })
onActivated(startPolling)
onDeactivated(stopPolling)
onBeforeUnmount(() => { alive = false; resultsController?.abort(); stopPolling() })
</script>
