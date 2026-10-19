<template>
  <section class="rounded-2xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900" data-testid="pelican-account-tests">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div><h3 class="font-semibold">{{ t('pelicanTests.accounts.title') }} · {{ accounts.length }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('pelicanTests.accounts.hint') }}</p></div>
      <div class="flex gap-2">
        <button class="btn btn-secondary" :disabled="loading || running" @click="load">{{ t('pelicanTests.refresh') }}</button>
        <button v-if="running" class="btn btn-secondary" data-testid="oauth-stop" @click="stop">{{ t('pelicanTests.accounts.stop') }}</button>
        <button v-else class="btn btn-primary" :disabled="loading || !!loadError || !accounts.length" data-testid="oauth-run-all" @click="run(accounts)">{{ t('pelicanTests.accounts.runAll') }}</button>
      </div>
    </header>
    <p v-if="loadError" class="mt-3 text-sm text-red-500" role="alert">{{ loadError }}</p>
    <div class="my-4 flex flex-wrap items-end gap-3">
      <label v-for="platform in platforms" :key="platform" class="text-xs text-gray-500">{{ platform }} {{ t('pelicanTests.accounts.model') }}
        <input v-model="models[platform]" class="input mt-1 block w-48" :disabled="running" :placeholder="t('pelicanTests.accounts.autoModel')" />
      </label>
      <label class="text-xs text-gray-500">{{ t('pelicanTests.accounts.effort') }}
        <select v-model="effort" class="input mt-1 block" :disabled="running"><option value="">{{ t('pelicanTests.accounts.autoModel') }}</option><option v-for="value in ['low', 'medium', 'high', 'xhigh']" :key="value">{{ value }}</option></select>
      </label>
      <span v-if="running" role="status" class="text-sm text-gray-500">{{ finished }} / {{ batchTotal }}</span>
    </div>
    <p v-if="loading" role="status">{{ t('pelicanTests.history.loading') }}</p>
    <p v-else-if="!accounts.length && !loadError" class="py-8 text-center text-gray-500">{{ t('pelicanTests.accounts.empty') }}</p>
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5" data-testid="oauth-grid">
      <article v-for="account in visibleAccounts" :key="account.id" class="min-w-0 overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700" :data-testid="`oauth-card-${account.id}`">
        <div class="aspect-[4/3] bg-gray-50 dark:bg-dark-800">
          <PelicanArtworkPreview v-if="results[account.id]?.html" :html="results[account.id].html" :title="account.name" :interactive="false" />
          <div v-else class="flex h-full items-center justify-center p-3 text-center text-sm text-gray-500" role="status">{{ t(`pelicanTests.accounts.${results[account.id]?.state || 'idle'}`) }}</div>
        </div>
        <div class="space-y-2 p-3 text-xs">
          <p class="truncate font-semibold" :title="account.name">{{ account.name }} <span class="font-normal text-gray-400">#{{ account.id }}</span></p>
          <p class="truncate text-gray-500">{{ account.platform }} · {{ results[account.id]?.model || models[account.platform] || t('pelicanTests.accounts.autoModel') }}</p>
          <p class="truncate text-gray-500" :title="groupNames(account)">{{ groupNames(account) }}</p>
          <p v-if="results[account.id]?.error" class="break-words text-red-500" role="alert">{{ results[account.id].error }}</p>
          <div class="flex flex-wrap gap-2">
            <button class="text-primary-600 hover:underline" :disabled="running || loading" @click="run([account])">{{ t('pelicanTests.accounts.test') }}</button>
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
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI } from '@/api/admin/accounts'
import { groupsAPI } from '@/api/admin/groups'
import { testPelicanAccount } from '@/api/admin/pelicanAccounts'
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
const running = ref(false)
const finished = ref(0)
const batchTotal = ref(0)
const page = ref(1)
const pageSize = 25
const visibleAccounts = computed(() => accounts.value.slice((page.value - 1) * pageSize, page.value * pageSize))
const platforms = computed(() => [...new Set(accounts.value.map(a => a.platform))])
const results = reactive<Record<number, { state: string; html: string; model: string; error: string }>>({})
const editing = ref<AccountListItem | null>(null)
const preview = ref<AccountListItem | null>(null)
const groupIds = ref<number[]>([])
const saving = ref(false)
const saveError = ref('')
const risk = ref('')
const controllers = new Set<AbortController>()
let stopped = false
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
    page.value = 1
  } catch (error) { if (alive) loadError.value = extractApiErrorMessage(error) || t('pelicanTests.loadError') }
  finally { loading.value = false }
}

async function run(targets: AccountListItem[]) {
  if (running.value) return
  stopped = false
  running.value = true
  finished.value = 0
  batchTotal.value = targets.length
  const queue = [...targets]
  for (const account of queue) results[account.id] = { state: 'queued', html: '', model: '', error: '' }
  async function worker() {
    while (!stopped && alive) {
      const account = queue.shift()
      if (!account) return
      const result = results[account.id]
      result.state = 'running'
      const controller = new AbortController()
      controllers.add(controller)
      const timeout = setTimeout(() => controller.abort(), 10 * 60 * 1000)
      try {
        const answer = await testPelicanAccount(account.id, models[account.platform]?.trim() || '', effort.value, controller.signal)
        if (stopped || !alive) { result.state = 'stopped'; continue }
        result.model = answer.model
        const html = extractPelicanHtml(answer.text)
        if (!html) throw new Error(t('pelicanTests.accounts.invalid'))
        result.html = html
        result.state = 'success'
      } catch (error) {
        result.state = stopped ? 'stopped' : 'failed'
        result.error = stopped ? '' : controller.signal.aborted ? t('pelicanTests.accounts.timeout') : extractApiErrorMessage(error) || t('pelicanTests.accounts.failed')
      } finally { clearTimeout(timeout); controllers.delete(controller); finished.value++ }
    }
  }
  await Promise.all(Array.from({ length: Math.min(3, queue.length) }, worker))
  for (const account of queue) results[account.id].state = 'stopped'
  running.value = false
}
function stop() { stopped = true; for (const controller of controllers) controller.abort() }
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
onMounted(load)
onBeforeUnmount(() => { alive = false; stop(); loadController.abort() })
</script>
