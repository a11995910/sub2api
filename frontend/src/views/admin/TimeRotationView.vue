<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="card flex flex-wrap items-center justify-between gap-4 p-5">
        <div>
          <div class="flex items-center gap-3">
            <label class="flex cursor-pointer items-center gap-2 font-semibold text-gray-900 dark:text-white">
              <input v-model="config.enabled" type="checkbox" :disabled="!ready || saving" class="h-4 w-4" data-testid="rotation-enabled" />
              {{ t('timeRotation.enabled') }}
            </label>
            <span class="rounded-full bg-primary-50 px-3 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{ t('timeRotation.timezone') }}</span>
          </div>
          <p class="mt-2 text-sm text-gray-500">{{ t('timeRotation.description') }}</p>
        </div>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="load">{{ t('common.refresh') }}</button>
          <button type="button" class="btn btn-primary" :disabled="!ready || saving" data-testid="rotation-save" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button>
        </div>
      </div>
      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <p v-if="loading" class="py-8 text-center text-gray-500">{{ t('common.loading') }}</p>
      <template v-else-if="ready">
        <div class="grid gap-5 xl:grid-cols-3">
          <section v-for="(slot, index) in config.slots" :key="index" class="card overflow-hidden" :data-testid="`rotation-slot-${index}`">
            <div class="border-b border-gray-100 bg-gray-50/60 p-5 dark:border-dark-700 dark:bg-dark-800">
              <div class="mb-4 flex items-center justify-between">
                <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('timeRotation.slot', { n: index + 1 }) }}</h2>
                <span class="text-sm text-primary-600 dark:text-primary-400">{{ t('timeRotation.selected', { n: slot.account_ids.length }) }}</span>
              </div>
              <div class="grid grid-cols-2 gap-3">
                <label class="input-label">{{ t('timeRotation.start') }}
                  <input v-model="slot.start" type="time" class="input mt-1" :disabled="saving" />
                </label>
                <label class="input-label">{{ t('timeRotation.end') }}
                  <input v-model="slot.end" type="text" inputmode="numeric" placeholder="24:00" pattern="([01][0-9]|2[0-3]):[0-5][0-9]|24:00" class="input mt-1" :disabled="saving" />
                </label>
                <label class="input-label">{{ t('timeRotation.activePriority') }}
                  <input v-model.number="slot.active_priority" type="number" min="1" max="2147483647" step="1" class="input mt-1" :disabled="saving" />
                </label>
                <label class="input-label">{{ t('timeRotation.inactivePriority') }}
                  <input v-model.number="slot.inactive_priority" type="number" min="1" max="2147483647" step="1" class="input mt-1" :disabled="saving" />
                </label>
              </div>
              <p class="mt-2 text-xs text-gray-500">{{ t('timeRotation.timeHint') }}</p>
            </div>
            <div class="space-y-3 p-5">
              <label class="input-label" :for="`rotation-search-${index}`">{{ t('timeRotation.accounts') }}</label>
              <div v-if="slot.account_ids.length" class="flex max-h-32 flex-wrap gap-2 overflow-y-auto">
                <button v-for="id in slot.account_ids" :key="id" type="button" class="rounded-lg bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300" :disabled="saving" :aria-label="t('timeRotation.removeAccount', { name: accountName(id) })" @click="toggleAccount(index, id)">
                  {{ accountName(id) }} <span aria-hidden="true">×</span>
                </button>
              </div>
              <input :id="`rotation-search-${index}`" v-model="queries[index]" class="input" :placeholder="t('timeRotation.search')" type="search" />
              <div class="max-h-80 min-h-40 space-y-1 overflow-y-auto">
                <label v-for="account in candidates(index).slice(0, 100)" :key="account.id" class="flex items-start gap-3 rounded-lg p-2 hover:bg-gray-50 dark:hover:bg-dark-700" :class="{ 'opacity-40': assignedElsewhere(index, account.id) }">
                  <input type="checkbox" class="mt-1 h-4 w-4" :checked="slot.account_ids.includes(account.id)" :disabled="saving || assignedElsewhere(index, account.id)" :data-account-id="account.id" @change="toggleAccount(index, account.id)" />
                  <span class="min-w-0 text-sm">
                    <span class="block break-all text-gray-900 dark:text-gray-100">{{ account.name }}</span>
                    <span class="text-xs text-gray-500">#{{ account.id }} · {{ t('timeRotation.currentPriority', { n: account.priority }) }}<template v-if="assignedElsewhere(index, account.id)"> · {{ t('timeRotation.assigned') }}</template></span>
                  </span>
                </label>
                <p v-if="!candidates(index).length" class="py-6 text-center text-sm text-gray-500">{{ t('timeRotation.empty') }}</p>
              </div>
              <p v-if="candidates(index).length > 100" class="text-xs text-gray-500">{{ t('timeRotation.searchMore') }}</p>
            </div>
          </section>
        </div>
        <div class="rounded-xl border border-blue-100 bg-blue-50/60 p-4 text-sm leading-6 text-blue-800 dark:border-blue-900 dark:bg-blue-900/10 dark:text-blue-200">
          <p>{{ t('timeRotation.priorityHint') }}</p>
          <p>{{ t('timeRotation.restoreHint') }}</p>
          <p>{{ t('timeRotation.schedulingHint') }}</p>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { list } from '@/api/admin/accounts'
import { timeRotationAPI, type AccountTimeRotationConfig } from '@/api/admin/timeRotation'
import type { AccountListItem } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const config = ref<AccountTimeRotationConfig>({ enabled: false, revision: 0, slots: [] })
const accounts = ref<AccountListItem[]>([])
const queries = ref(['', '', ''])
const loading = ref(false)
const ready = ref(false)
const saving = ref(false)
const error = ref('')
const controller = new AbortController()
onBeforeUnmount(() => controller.abort())

function accountName(id: number) {
  return accounts.value.find(a => a.id === id)?.name || t('timeRotation.missingAccount', { id })
}
function assignedElsewhere(index: number, id: number) {
  return config.value.slots.some((slot, i) => i !== index && slot.account_ids.includes(id))
}
function candidates(index: number) {
  const query = queries.value[index].trim().toLowerCase()
  return accounts.value.filter(a => !query || a.name.toLowerCase().includes(query) || String(a.id).includes(query))
}
function toggleAccount(index: number, id: number) {
  const slot = config.value.slots[index]
  if (slot.account_ids.includes(id)) slot.account_ids = slot.account_ids.filter(value => value !== id)
  else if (!assignedElsewhere(index, id)) slot.account_ids.push(id)
}
async function load() {
  loading.value = true
  ready.value = false
  error.value = ''
  try {
    const saved = await timeRotationAPI.get()
    const all: AccountListItem[] = []
    for (let page = 1; ; page++) {
      const result = await list(page, 100, { platform: 'openai', type: 'oauth', lite: 'true', include_scheduler_score: 'false', sort_by: 'id', sort_order: 'asc' }, { signal: controller.signal })
      all.push(...result.items.filter(a => !a.parent_account_id))
      if (page >= result.pages || result.items.length === 0) break
    }
    config.value = saved
    accounts.value = all
    ready.value = true
  } catch (err) {
    error.value = extractApiErrorMessage(err, t('timeRotation.loadFailed'))
  } finally { loading.value = false }
}
async function save() {
  error.value = ''
  const timeValid = (value: string, end: boolean) => /^([01]\d|2[0-3]):[0-5]\d$/.test(value) || (end && value === '24:00')
  if (config.value.slots.some(slot => !timeValid(slot.start, false) || !timeValid(slot.end, true) || slot.start === slot.end || [slot.active_priority, slot.inactive_priority].some(n => !Number.isInteger(n) || n < 1 || n > 2147483647))) {
    error.value = t('timeRotation.invalid')
    return
  }
  saving.value = true
  try {
    config.value = await timeRotationAPI.save(config.value)
    appStore.showSuccess(t('timeRotation.saved'))
  } catch (err) { error.value = extractApiErrorMessage(err, t('timeRotation.saveFailed')) }
  finally { saving.value = false }
}
onMounted(load)
</script>
