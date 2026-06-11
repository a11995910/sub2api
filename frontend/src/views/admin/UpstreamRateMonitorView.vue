<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="relative w-full sm:w-72">
            <Icon
              name="search"
              size="md"
              class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
            />
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.upstreamRateMonitor.searchPlaceholder')"
              class="input pl-10"
              @input="handleSearch"
            />
          </div>

          <div class="w-full sm:w-44">
            <Select
              v-model="enabledFilter"
              :options="enabledOptions"
              @change="handleFilterChange"
            />
          </div>

          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="reload"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="batchRefreshing || loading || enabledRows.length === 0"
              @click="refreshVisibleEnabled"
            >
              <Icon name="sync" size="md" class="mr-2" :class="batchRefreshing ? 'animate-spin' : ''" />
              {{ t('admin.upstreamRateMonitor.refreshVisible') }}
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.upstreamRateMonitor.createButton') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="monitors" :loading="loading">
          <template #cell-name="{ row }">
            <div class="min-w-[240px] space-y-1">
              <div class="flex items-center gap-1.5">
                <span class="font-medium text-gray-900 dark:text-white">{{ row.name }}</span>
                <HelpTooltip
                  v-if="row.password_decrypt_failed"
                  :content="t('admin.upstreamRateMonitor.passwordDecryptFailed')"
                >
                  <Icon name="exclamationTriangle" size="sm" class="text-red-500" />
                </HelpTooltip>
              </div>
              <div class="flex flex-wrap items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                <code class="code max-w-[320px] truncate">{{ row.base_url }}</code>
                <span>{{ row.username }}</span>
              </div>
            </div>
          </template>

          <template #cell-last_status="{ row }">
            <span class="badge" :class="statusBadgeClass(row.last_status)">
              {{ statusLabel(row.last_status) }}
            </span>
          </template>

          <template #cell-last_group_count="{ row }">
            <button
              type="button"
              class="inline-flex items-center rounded bg-gray-100 px-2 py-0.5 text-xs font-medium text-primary-700 hover:bg-gray-200 dark:bg-dark-700 dark:text-primary-300 dark:hover:bg-dark-600"
              @click="openSnapshotDialog(row)"
            >
              {{ t('admin.upstreamRateMonitor.groupCount', { count: row.last_group_count || 0 }) }}
            </button>
          </template>

          <template #cell-last_checked_at="{ row }">
            <div class="flex min-w-[130px] flex-col text-xs">
              <span class="text-gray-700 dark:text-gray-200">{{ formatDateLabel(row.last_checked_at) }}</span>
              <span v-if="row.last_checked_at" class="text-gray-400">{{ formatRelativeTime(row.last_checked_at) }}</span>
            </div>
          </template>

          <template #cell-enabled="{ row }">
            <Toggle :modelValue="row.enabled" @update:modelValue="toggleEnabled(row)" />
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-1.5">
              <button
                type="button"
                class="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :disabled="refreshingId !== null || row.password_decrypt_failed"
                :title="t('admin.upstreamRateMonitor.refreshNow')"
                @click="refreshMonitor(row)"
              >
                <Icon name="refresh" size="sm" :class="refreshingId === row.id ? 'animate-spin' : ''" />
              </button>
              <button
                type="button"
                class="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('admin.upstreamRateMonitor.viewSnapshot')"
                @click="openSnapshotDialog(row)"
              >
                <Icon name="chart" size="sm" />
              </button>
              <button
                type="button"
                class="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('common.edit')"
                @click="openEditDialog(row)"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button
                type="button"
                class="rounded-md p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :title="t('common.delete')"
                @click="handleDelete(row)"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.upstreamRateMonitor.emptyTitle')"
              :description="t('admin.upstreamRateMonitor.emptyDescription')"
              :action-text="t('admin.upstreamRateMonitor.createButton')"
              @action="openCreateDialog"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="onPageChange"
          @update:pageSize="onPageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showFormDialog"
      :title="editing ? t('admin.upstreamRateMonitor.editTitle') : t('admin.upstreamRateMonitor.createTitle')"
      width="wide"
      @close="closeFormDialog"
    >
      <form class="space-y-5" @submit.prevent="submitForm">
        <div class="grid gap-4 md:grid-cols-2">
          <Input
            v-model="form.name"
            :label="t('admin.upstreamRateMonitor.form.name')"
            :placeholder="t('admin.upstreamRateMonitor.form.namePlaceholder')"
            required
            :error="formErrors.name"
          />
          <Input
            v-model="form.base_url"
            :label="t('admin.upstreamRateMonitor.form.baseUrl')"
            :placeholder="t('admin.upstreamRateMonitor.form.baseUrlPlaceholder')"
            required
            :error="formErrors.base_url"
          />
          <Input
            v-model="form.username"
            :label="t('admin.upstreamRateMonitor.form.username')"
            :placeholder="t('admin.upstreamRateMonitor.form.usernamePlaceholder')"
            autocomplete="username"
            required
            :error="formErrors.username"
          />
          <Input
            v-model="form.password"
            type="password"
            :label="t('admin.upstreamRateMonitor.form.password')"
            :placeholder="editing ? t('admin.upstreamRateMonitor.form.passwordEditPlaceholder') : t('admin.upstreamRateMonitor.form.passwordPlaceholder')"
            autocomplete="new-password"
            :required="!editing"
            :error="formErrors.password"
          />
        </div>

        <label class="flex items-center justify-between rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-700">
          <div>
            <div class="text-sm font-medium text-gray-900 dark:text-gray-100">
              {{ t('admin.upstreamRateMonitor.form.enabled') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.upstreamRateMonitor.form.enabledHint') }}
            </div>
          </div>
          <Toggle v-model="form.enabled" />
        </label>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeFormDialog">
            {{ t('common.cancel') }}
          </button>
          <button type="button" class="btn btn-primary" :disabled="saving" @click="submitForm">
            <Icon v-if="saving" name="sync" size="md" class="mr-2 animate-spin" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showSnapshotDialog"
      :title="snapshotTitle"
      width="extra-wide"
      @close="showSnapshotDialog = false"
    >
      <div v-if="snapshotMonitor" class="space-y-4">
        <div class="grid gap-3 md:grid-cols-4">
          <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-700">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstreamRateMonitor.snapshot.site') }}</div>
            <code class="mt-1 block truncate text-xs text-gray-800 dark:text-gray-200">{{ snapshotMonitor.base_url }}</code>
          </div>
          <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-700">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstreamRateMonitor.snapshot.status') }}</div>
            <span class="badge mt-1" :class="statusBadgeClass(snapshotMonitor.last_status)">
              {{ statusLabel(snapshotMonitor.last_status) }}
            </span>
          </div>
          <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-700">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstreamRateMonitor.snapshot.groups') }}</div>
            <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ snapshotMonitor.last_group_count || 0 }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-700">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstreamRateMonitor.snapshot.checkedAt') }}</div>
            <div class="mt-1 text-sm text-gray-800 dark:text-gray-200">
              {{ formatDateLabel(snapshotMonitor.last_checked_at) }}
            </div>
          </div>
        </div>

        <div
          v-if="snapshotMonitor.last_error"
          class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300"
        >
          {{ snapshotMonitor.last_error }}
        </div>

        <div v-if="snapshotMonitor.last_snapshot.length > 0" class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
          <table class="w-full min-w-[860px] divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamRateMonitor.snapshot.columns.group') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamRateMonitor.snapshot.columns.platform') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamRateMonitor.snapshot.columns.rate') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamRateMonitor.snapshot.columns.imageRate') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamRateMonitor.snapshot.columns.subscriptionType') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamRateMonitor.snapshot.columns.rpmLimit') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamRateMonitor.snapshot.columns.flags') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900">
              <tr v-for="group in sortedSnapshot" :key="`${group.id}-${group.name}`">
                <td class="px-4 py-3">
                  <div class="max-w-[260px]">
                    <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ group.name }}</div>
                    <div v-if="group.description" class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400">
                      {{ group.description }}
                    </div>
                  </div>
                </td>
                <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">
                  {{ group.platform || '-' }}
                </td>
                <td class="px-4 py-3">
                  <span class="badge badge-primary">{{ formatRate(group.rate_multiplier) }}</span>
                </td>
                <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">
                  <div class="flex flex-col gap-1">
                    <span>{{ formatOptionalRate(group.image_rate_multiplier) }}</span>
                    <span v-if="group.image_rate_independent" class="text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.upstreamRateMonitor.snapshot.imageIndependent') }}
                    </span>
                  </div>
                </td>
                <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">
                  {{ group.subscription_type || '-' }}
                </td>
                <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">
                  {{ group.rpm_limit && group.rpm_limit > 0 ? group.rpm_limit : '-' }}
                </td>
                <td class="px-4 py-3">
                  <div class="flex flex-wrap gap-1">
                    <span v-if="group.status" class="badge badge-gray">{{ group.status }}</span>
                    <span v-if="group.is_exclusive" class="badge badge-warning">
                      {{ t('admin.upstreamRateMonitor.snapshot.exclusive') }}
                    </span>
                    <span v-if="group.allow_image_generation" class="badge badge-success">
                      {{ t('admin.upstreamRateMonitor.snapshot.image') }}
                    </span>
                    <span v-if="group.image_super_resolution_enabled" class="badge badge-primary">
                      {{ t('admin.upstreamRateMonitor.snapshot.superResolution') }}
                    </span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <EmptyState
          v-else
          :title="t('admin.upstreamRateMonitor.snapshot.emptyTitle')"
          :description="t('admin.upstreamRateMonitor.snapshot.emptyDescription')"
        />
      </div>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showSnapshotDialog = false">
            {{ t('common.close') }}
          </button>
          <button
            v-if="snapshotMonitor"
            type="button"
            class="btn btn-primary"
            :disabled="refreshingId !== null || snapshotMonitor.password_decrypt_failed"
            @click="refreshMonitor(snapshotMonitor)"
          >
            <Icon name="refresh" size="md" class="mr-2" :class="refreshingId === snapshotMonitor.id ? 'animate-spin' : ''" />
            {{ t('admin.upstreamRateMonitor.refreshNow') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('common.delete')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  ListParams,
  UpstreamRateGroupSnapshot,
  UpstreamRateMonitor,
  UpstreamRateMonitorStatus,
} from '@/api/admin/upstreamRateMonitor'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import Input from '@/components/common/Input.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime, formatRelativeTime } from '@/utils/format'
import { formatMultiplier } from '@/utils/formatters'

const { t } = useI18n()
const appStore = useAppStore()

const monitors = ref<UpstreamRateMonitor[]>([])
const loading = ref(false)
const saving = ref(false)
const refreshingId = ref<number | null>(null)
const batchRefreshing = ref(false)
const searchQuery = ref('')
const enabledFilter = ref<'' | 'true' | 'false'>('')
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })

const showFormDialog = ref(false)
const editing = ref<UpstreamRateMonitor | null>(null)
const showDeleteDialog = ref(false)
const deleting = ref<UpstreamRateMonitor | null>(null)
const showSnapshotDialog = ref(false)
const snapshotMonitor = ref<UpstreamRateMonitor | null>(null)

const form = reactive({
  name: '',
  base_url: '',
  username: '',
  password: '',
  enabled: true,
})
const formErrors = reactive({
  name: '',
  base_url: '',
  username: '',
  password: '',
})

let abortController: AbortController | null = null
let searchTimeout: ReturnType<typeof setTimeout> | null = null

const enabledOptions = computed(() => [
  { value: '', label: t('admin.upstreamRateMonitor.allEnabled') },
  { value: 'true', label: t('admin.upstreamRateMonitor.onlyEnabled') },
  { value: 'false', label: t('admin.upstreamRateMonitor.onlyDisabled') },
])

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.upstreamRateMonitor.columns.name'), sortable: false },
  { key: 'last_status', label: t('admin.upstreamRateMonitor.columns.status'), sortable: false },
  { key: 'last_group_count', label: t('admin.upstreamRateMonitor.columns.groupCount'), sortable: false },
  { key: 'last_checked_at', label: t('admin.upstreamRateMonitor.columns.lastCheckedAt'), sortable: false },
  { key: 'enabled', label: t('admin.upstreamRateMonitor.columns.enabled'), sortable: false },
  { key: 'actions', label: t('admin.upstreamRateMonitor.columns.actions'), sortable: false },
])

const enabledRows = computed(() => monitors.value.filter((item) => item.enabled && !item.password_decrypt_failed))

const deleteConfirmMessage = computed(() => {
  return t('admin.upstreamRateMonitor.deleteConfirm', { name: deleting.value?.name || '' })
})

const snapshotTitle = computed(() => {
  return t('admin.upstreamRateMonitor.snapshot.title', { name: snapshotMonitor.value?.name || '' })
})

const sortedSnapshot = computed<UpstreamRateGroupSnapshot[]>(() => {
  const rows = snapshotMonitor.value?.last_snapshot || []
  return [...rows].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0) || a.id - b.id)
})

function statusLabel(status: UpstreamRateMonitorStatus): string {
  return t(`admin.upstreamRateMonitor.status.${status}`)
}

function statusBadgeClass(status: UpstreamRateMonitorStatus): string {
  if (status === 'success') return 'badge-success'
  if (status === 'failed') return 'badge-danger'
  return 'badge-gray'
}

function formatDateLabel(value: string | null): string {
  return value ? formatDateTime(value) : t('common.time.never')
}

function formatRate(value: number): string {
  return `${formatMultiplier(Number(value || 0))}x`
}

function formatOptionalRate(value: number | null | undefined): string {
  if (value === null || value === undefined) return '-'
  return formatRate(value)
}

function upsertMonitor(row: UpstreamRateMonitor) {
  const index = monitors.value.findIndex((item) => item.id === row.id)
  if (index >= 0) {
    monitors.value[index] = row
  } else {
    monitors.value.unshift(row)
  }
  if (snapshotMonitor.value?.id === row.id) {
    snapshotMonitor.value = row
  }
}

async function reload() {
  abortController?.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  loading.value = true
  try {
    const params: ListParams = {
      page: pagination.page,
      page_size: pagination.page_size,
    }
    if (enabledFilter.value === 'true') params.enabled = true
    if (enabledFilter.value === 'false') params.enabled = false
    if (searchQuery.value.trim()) params.search = searchQuery.value.trim()

    const res = await adminAPI.upstreamRateMonitor.list(params, { signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    monitors.value = res.items || []
    pagination.total = res.total
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('admin.upstreamRateMonitor.loadFailed')))
  } finally {
    if (abortController === ctrl) {
      loading.value = false
      abortController = null
    }
  }
}

function handleSearch() {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    void reload()
  }, 300)
}

function handleFilterChange() {
  pagination.page = 1
  void reload()
}

function onPageChange(page: number) {
  pagination.page = page
  void reload()
}

function onPageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void reload()
}

function resetForm() {
  form.name = ''
  form.base_url = ''
  form.username = ''
  form.password = ''
  form.enabled = true
  clearFormErrors()
}

function clearFormErrors() {
  formErrors.name = ''
  formErrors.base_url = ''
  formErrors.username = ''
  formErrors.password = ''
}

function openCreateDialog() {
  editing.value = null
  resetForm()
  showFormDialog.value = true
}

function openEditDialog(row: UpstreamRateMonitor) {
  editing.value = row
  clearFormErrors()
  form.name = row.name
  form.base_url = row.base_url
  form.username = row.username
  form.password = ''
  form.enabled = row.enabled
  showFormDialog.value = true
}

function closeFormDialog() {
  showFormDialog.value = false
  editing.value = null
  resetForm()
}

function validateForm(): boolean {
  clearFormErrors()
  let ok = true
  if (!form.name.trim()) {
    formErrors.name = t('admin.upstreamRateMonitor.validation.nameRequired')
    ok = false
  }
  if (!form.base_url.trim()) {
    formErrors.base_url = t('admin.upstreamRateMonitor.validation.baseUrlRequired')
    ok = false
  }
  if (!form.username.trim()) {
    formErrors.username = t('admin.upstreamRateMonitor.validation.usernameRequired')
    ok = false
  }
  if (!editing.value && !form.password.trim()) {
    formErrors.password = t('admin.upstreamRateMonitor.validation.passwordRequired')
    ok = false
  }
  return ok
}

async function submitForm() {
  if (saving.value || !validateForm()) return
  saving.value = true
  try {
    if (editing.value) {
      const payload: {
        name: string
        base_url: string
        username: string
        enabled: boolean
        password?: string
      } = {
        name: form.name.trim(),
        base_url: form.base_url.trim(),
        username: form.username.trim(),
        enabled: form.enabled,
      }
      if (form.password.trim()) payload.password = form.password.trim()
      const updated = await adminAPI.upstreamRateMonitor.update(editing.value.id, payload)
      upsertMonitor(updated)
      appStore.showSuccess(t('admin.upstreamRateMonitor.updateSuccess'))
    } else {
      const created = await adminAPI.upstreamRateMonitor.create({
        name: form.name.trim(),
        base_url: form.base_url.trim(),
        username: form.username.trim(),
        password: form.password.trim(),
        enabled: form.enabled,
      })
      upsertMonitor(created)
      pagination.total += 1
      appStore.showSuccess(t('admin.upstreamRateMonitor.createSuccess'))
    }
    closeFormDialog()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.upstreamRateMonitor.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(row: UpstreamRateMonitor) {
  const next = !row.enabled
  try {
    const updated = await adminAPI.upstreamRateMonitor.update(row.id, { enabled: next })
    upsertMonitor(updated)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.upstreamRateMonitor.saveFailed')))
  }
}

async function syncMonitorById(id: number) {
  const row = await adminAPI.upstreamRateMonitor.get(id)
  upsertMonitor(row)
}

async function refreshMonitor(row: UpstreamRateMonitor, options: { silent?: boolean } = {}) {
  if (refreshingId.value !== null && refreshingId.value !== row.id) return false
  refreshingId.value = row.id
  try {
    const updated = await adminAPI.upstreamRateMonitor.refresh(row.id)
    upsertMonitor(updated)
    if (!options.silent) appStore.showSuccess(t('admin.upstreamRateMonitor.refreshSuccess'))
    return true
  } catch (err: unknown) {
    try {
      await syncMonitorById(row.id)
    } catch {
      // 刷新失败后再取最新失败状态是锦上添花，取不到时保留当前行。
    }
    if (!options.silent) {
      appStore.showError(extractApiErrorMessage(err, t('admin.upstreamRateMonitor.refreshFailed')))
    }
    return false
  } finally {
    refreshingId.value = null
  }
}

async function refreshVisibleEnabled() {
  if (batchRefreshing.value) return
  const rows = enabledRows.value
  if (rows.length === 0) {
    appStore.showInfo(t('admin.upstreamRateMonitor.noEnabledRows'))
    return
  }
  batchRefreshing.value = true
  let success = 0
  let failed = 0
  try {
    for (const row of rows) {
      const ok = await refreshMonitor(row, { silent: true })
      if (ok) success += 1
      else failed += 1
    }
    if (failed > 0) {
      appStore.showError(t('admin.upstreamRateMonitor.batchRefreshPartial', { success, failed }))
    } else {
      appStore.showSuccess(t('admin.upstreamRateMonitor.batchRefreshSuccess', { count: success }))
    }
  } finally {
    batchRefreshing.value = false
  }
}

function openSnapshotDialog(row: UpstreamRateMonitor) {
  snapshotMonitor.value = row
  showSnapshotDialog.value = true
}

function handleDelete(row: UpstreamRateMonitor) {
  deleting.value = row
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deleting.value) return
  try {
    await adminAPI.upstreamRateMonitor.del(deleting.value.id)
    const id = deleting.value.id
    monitors.value = monitors.value.filter((item) => item.id !== id)
    if (snapshotMonitor.value?.id === id) showSnapshotDialog.value = false
    pagination.total = Math.max(0, pagination.total - 1)
    appStore.showSuccess(t('admin.upstreamRateMonitor.deleteSuccess'))
    showDeleteDialog.value = false
    deleting.value = null
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.upstreamRateMonitor.deleteFailed')))
  }
}

onMounted(reload)

onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
  abortController?.abort()
})
</script>
