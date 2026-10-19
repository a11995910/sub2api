<template>
  <section class="rounded-2xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900" data-testid="pelican-account-tests">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div><h3 class="font-semibold">{{ t('pelicanTests.accounts.title') }} · {{ accounts.length }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('pelicanTests.accounts.hint') }}</p></div>
      <div class="flex gap-2">
        <button class="btn btn-secondary" :disabled="loading || submitting" @click="load">{{ t('pelicanTests.refresh') }}</button>
        <button v-if="running" class="btn btn-secondary" :disabled="submitting" data-testid="oauth-stop" @click="stop">{{ t('pelicanTests.accounts.stop') }}</button>
        <button class="btn btn-primary" :disabled="loading || submitting || !!loadError || !accounts.length" data-testid="oauth-run-all" @click="run()">{{ t('pelicanTests.accounts.runAll') }}</button>
      </div>
    </header>
    <p v-if="loadError" class="mt-3 text-sm text-red-500" role="alert">{{ loadError }}</p>
    <div class="my-4 flex flex-wrap items-end gap-3">
      <label v-for="platform in platforms" :key="platform" class="text-xs text-gray-500">{{ platform }} {{ t('pelicanTests.accounts.model') }}
        <input v-model="models[platform]" class="input mt-1 block w-48" :disabled="submitting" :placeholder="t('pelicanTests.accounts.autoModel')" />
      </label>
      <label class="text-xs text-gray-500">{{ t('pelicanTests.accounts.effort') }}
        <select v-model="effort" class="input mt-1 block" :disabled="submitting"><option value="">{{ t('pelicanTests.accounts.autoModel') }}</option><option v-for="value in ['low', 'medium', 'high', 'xhigh']" :key="value">{{ value }}</option></select>
      </label>
      <span v-if="running" role="status" class="text-sm text-gray-500">{{ t('pelicanTests.accounts.runningCount', { count: runningCount }) }}</span>
    </div>
    <p v-if="loading" role="status">{{ t('pelicanTests.history.loading') }}</p>
    <p v-else-if="!accounts.length && !loadError" class="py-8 text-center text-gray-500">{{ t('pelicanTests.accounts.empty') }}</p>
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5" data-testid="oauth-grid">
      <article v-for="account in visibleAccounts" :key="account.id" class="min-w-0 overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700" :data-testid="`oauth-card-${account.id}`">
        <div class="aspect-[4/3] bg-gray-50 dark:bg-dark-800">
          <PelicanArtworkPreview v-if="results[account.id]?.html" :html="results[account.id].html" :title="account.name" :interactive="false" />
          <div v-else class="flex h-full items-center justify-center p-3 text-center text-sm text-gray-500" role="status">{{ t(`pelicanTests.accounts.${results[account.id]?.status || 'idle'}`) }}</div>
        </div>
        <div class="space-y-2 p-3 text-xs">
          <p class="truncate font-semibold" :title="account.name">{{ account.name }} <span class="font-normal text-gray-400">#{{ account.id }}</span></p>
          <p class="truncate text-gray-500">{{ account.platform }} · {{ results[account.id]?.model_id || models[account.platform] || t('pelicanTests.accounts.autoModel') }}</p>
          <p class="truncate text-gray-500" :title="groupNames(account)">{{ groupNames(account) }}</p>
          <p v-if="results[account.id]?.error_message" class="break-words text-red-500" role="alert">{{ results[account.id].error_message }}</p>
          <div class="flex flex-wrap gap-2">
            <button class="text-primary-600 hover:underline" :disabled="results[account.id]?.status === 'running' || loading || submitting" @click="run([account])">{{ t('pelicanTests.accounts.test') }}</button>
            <button class="text-primary-600 hover:underline" :disabled="saving || loading" :data-testid="`oauth-groups-${account.id}`" @click="editGroups(account)">{{ t('pelicanTests.accounts.groups') }}</button>
            <button v-if="results[account.id]?.html" class="text-primary-600 hover:underline" @click="preview = account">{{ t('pelicanTests.history.view') }}</button>
          </div>
        </div>
      </article>
    </div>
    <Pagination v-if="accounts.length > pageSize" :total="accounts.length" :page="page" :page-size="pageSize" @update:page="page = $event" />
    <BaseDialog :show="!!editing" :title="`${t('pelicanTests.accounts.groups')} · ${editing?.name || ''}`" @close="closeEditor">
      <template v-if="editing">
        <fieldset :disabled="saving"><GroupSelector v-model="groupIds" :groups="groups" :platform="editing.platform" :mixed-scheduling="editing.extra?.mixed_scheduling === true" /></fieldset>
        <p v-if="saveError" class="mt-3 text-sm text-red-500" role="alert">{{ saveError }}</p>
      </template>
      <template #footer><button class="btn btn-primary" :disabled="saving" data-testid="oauth-save-groups" @click="saveGroups()">{{ t('common.save') }}</button></template>
    </BaseDialog>
    <ConfirmDialog :show="!!risk" :title="t('pelicanTests.accounts.groups')" :message="risk" @confirm="saveGroups(true)" @cancel="risk = ''" />
    <BaseDialog :show="!!preview" :title="preview?.name || ''" width="extra-wide" @close="preview = null">
      <div v-if="preview" class="h-[65vh]"><PelicanArtworkPreview :html="results[preview.id]?.html || ''" :title="preview.name" /></div>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI } from '@/api/admin/accounts'
import { groupsAPI } from '@/api/admin/groups'
import { pelicanAccountsAPI, type PelicanAccountResult } from '@/api/admin/pelicanAccounts'
import type { AccountListItem, AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import Pagination from '@/components/common/Pagination.vue'
import PelicanArtworkPreview from '@/components/user/pelican/PelicanArtworkPreview.vue'
import { extractPelicanHtml } from '@/utils/pelicanHtml'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const accounts = ref<AccountListItem[]>([])
const groups = ref<AdminGroup[]>([])
const loading = ref(false)
const loadError = ref('')
const models = reactive<Record<string, string>>({})
const effort = ref('')
const submitting = ref(false)
const runningCount = computed(() => Object.values(results).filter(r => r.status === 'running').length)
const running = computed(() => runningCount.value > 0)
const page = ref(1)
const pageSize = 25
const visibleAccounts = computed(() => accounts.value.slice((page.value - 1) * pageSize, page.value * pageSize))
const platforms = computed(() => [...new Set(accounts.value.map(a => a.platform))])
const results = reactive<Record<number, PelicanAccountResult & { html: string }>>({})
const editing = ref<AccountListItem | null>(null)
const preview = ref<AccountListItem | null>(null)
const groupIds = ref<number[]>([])
const saving = ref(false)
const saveError = ref('')
const risk = ref('')
let timer: ReturnType<typeof setInterval> | undefined
let polling = false
let resultRequest = 0
const detailsLoading = new Set<string>()
let alive = true
const loadController = new AbortController()

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const next: AccountListItem[] = []
    let current = 1
    while (true) {
      const response = await accountsAPI.list(current++, 100, { type: 'oauth', lite: '1', sort_by: 'id', sort_order: 'asc' }, { signal: loadController.signal })
      next.push(...response.items)
      if (next.length >= response.total) break
      if (!response.items.length) throw new Error(t('pelicanTests.loadError'))
    }
    const nextGroups = await groupsAPI.getAllIncludingInactive()
    if (!alive) return
    accounts.value = [...new Map(next.map(a => [a.id, a])).values()]
    groups.value = nextGroups
    page.value = Math.min(page.value, Math.max(1, Math.ceil(accounts.value.length / pageSize)))
    await refreshResults()
  } catch (error) { if (alive) loadError.value = extractApiErrorMessage(error) || t('pelicanTests.loadError') }
  finally { loading.value = false }
}

async function refreshResults() {
  const request = ++resultRequest
  const summaries = await pelicanAccountsAPI.list(loadController.signal)
  if (!alive || request !== resultRequest) return
  const ids = new Set(summaries.map(r => r.account_id))
  for (const id of Object.keys(results)) if (!ids.has(Number(id))) delete results[Number(id)]
  applyResults(summaries)
  await loadVisibleArtworks()
}
function applyResults(summaries: PelicanAccountResult[]) {
  for (const summary of summaries) {
    const old = results[summary.account_id]
    results[summary.account_id] = { ...summary, html: old?.run_id === summary.run_id && summary.status === 'success' ? old.html : '' }
  }
}
async function loadVisibleArtworks() {
  await Promise.all(visibleAccounts.value.map(async account => {
    const summary = results[account.id]
    if (!summary || summary.status !== 'success' || summary.html) return
    const key = `${account.id}/${summary.run_id}`
    if (detailsLoading.has(key)) return
    detailsLoading.add(key)
    try {
      const detail = await pelicanAccountsAPI.get(account.id, loadController.signal)
      if (!alive || results[account.id]?.run_id !== detail.run_id || detail.run_id !== summary.run_id) return
      results[account.id] = { ...detail, html: detail.status === 'success' ? extractPelicanHtml(detail.response_text || '') : '' }
    } catch (error) { if (alive) loadError.value = extractApiErrorMessage(error) || t('pelicanTests.loadError') }
    finally { detailsLoading.delete(key) }
  }))
}
async function run(targets?: AccountListItem[]) {
  if (submitting.value) return
  submitting.value = true
  loadError.value = ''
  ++resultRequest
  try {
    const started = await pelicanAccountsAPI.start({
      ...(targets ? { account_ids: targets.map(a => a.id) } : { all: true }),
      models: Object.fromEntries(Object.entries(models).map(([platform, model]) => [platform, model.trim()])),
      reasoning_effort: effort.value,
    })
    if (!alive) return
    applyResults(started)
    await refreshResults()
  } catch (error) {
    if (alive) loadError.value = extractApiErrorMessage(error) || t('pelicanTests.loadError')
  } finally { submitting.value = false }
}
async function stop() {
  if (submitting.value) return
  const runs = Object.values(results).filter(r => r.status === 'running').map(r => ({ account_id: r.account_id, run_id: r.run_id }))
  submitting.value = true
  ++resultRequest
  try {
    await pelicanAccountsAPI.stop(runs)
    if (alive) await refreshResults()
  } catch (error) { if (alive) loadError.value = extractApiErrorMessage(error) || t('pelicanTests.loadError') }
  finally { submitting.value = false }
}
function groupNames(account: AccountListItem) { return (account.group_ids || []).map(id => groups.value.find(g => g.id === id)?.name || `#${id}`).join('、') || t('pelicanTests.accounts.ungrouped') }
function editGroups(account: AccountListItem) { editing.value = account; groupIds.value = [...(account.group_ids || [])]; saveError.value = ''; risk.value = '' }
function closeEditor() { if (!saving.value) { editing.value = null; risk.value = '' } }
async function saveGroups(confirmed = false) {
  if (!editing.value || saving.value) return
  const account = editing.value
  saving.value = true
  saveError.value = ''
  risk.value = ''
  try {
    if (!confirmed) {
      const check = await accountsAPI.checkMixedChannelRisk({ account_id: account.id, platform: account.platform, group_ids: groupIds.value })
      if (check.has_risk) { risk.value = check.message || t('pelicanTests.accounts.mixedRisk'); return }
    }
    const updated = await accountsAPI.update(account.id, { group_ids: [...groupIds.value], ...(confirmed ? { confirm_mixed_channel_risk: true } : {}) })
    account.group_ids = updated.group_ids
    editing.value = null
  } catch (error) { saveError.value = extractApiErrorMessage(error) || t('pelicanTests.loadError') }
  finally { saving.value = false }
}
watch(page, loadVisibleArtworks)
onMounted(() => {
  void load()
  timer = setInterval(async () => {
    if (polling || loading.value || submitting.value) return
    polling = true
    try { await refreshResults() }
    catch (error) { if (alive) loadError.value = extractApiErrorMessage(error) || t('pelicanTests.loadError') }
    finally { polling = false }
  }, 3000)
})
// 页面关闭只停止查询；后台生成仍继续，结果由服务器保存。
onBeforeUnmount(() => { alive = false; if (timer) clearInterval(timer); loadController.abort() })
</script>
