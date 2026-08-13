<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="card p-4 sm:p-6">
          <div class="flex flex-wrap items-end justify-between gap-4">
            <div class="flex flex-1 flex-wrap items-end gap-4">
              <div class="w-full sm:min-w-[280px] sm:flex-1">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.search') }}</label>
                <input
                  v-model.trim="filters.search"
                  data-testid="review-search"
                  class="input"
                  :placeholder="t('admin.videoTaskReviews.filters.searchPlaceholder')"
                  @keyup.enter="search"
                />
              </div>
              <div class="w-full sm:w-36">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.userId') }}</label>
                <input v-model.trim="filters.userId" data-testid="review-user-id" class="input" inputmode="numeric" @keyup.enter="search" />
              </div>
              <div class="w-full sm:w-36">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.apiKeyId') }}</label>
                <input v-model.trim="filters.apiKeyId" class="input" inputmode="numeric" @keyup.enter="search" />
              </div>
              <div class="w-full sm:w-36">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.accountId') }}</label>
                <input v-model.trim="filters.accountId" class="input" inputmode="numeric" @keyup.enter="search" />
              </div>
              <div class="w-full sm:w-36">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.platform') }}</label>
                <select v-model="filters.platform" class="input" @change="search">
                  <option value="">{{ t('common.all') }}</option>
                  <option value="openai">OpenAI</option>
                  <option value="grok">Grok</option>
                </select>
              </div>
              <div class="w-full sm:w-44">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.model') }}</label>
                <input v-model.trim="filters.model" class="input" @keyup.enter="search" />
              </div>
              <div class="w-full sm:w-40">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.taskStatus') }}</label>
                <select v-model="filters.taskStatus" class="input" @change="search">
                  <option value="">{{ t('common.all') }}</option>
                  <option value="unknown">unknown</option>
                  <option value="submission_unknown">submission_unknown</option>
                  <option value="completed">completed</option>
                </select>
              </div>
              <div class="w-full sm:w-40">
                <label class="input-label">{{ t('admin.videoTaskReviews.filters.billingStatus') }}</label>
                <select v-model="filters.billingStatus" class="input" @change="search">
                  <option value="">{{ t('common.all') }}</option>
                  <option value="reserved">reserved</option>
                  <option value="manual_review">manual_review</option>
                  <option value="settling">settling</option>
                </select>
              </div>
            </div>
            <div class="flex gap-2">
              <button data-testid="review-filter-submit" class="btn btn-primary" :disabled="loading" @click="search">
                {{ t('common.search') }}
              </button>
              <button class="btn btn-secondary" :disabled="loading" @click="resetFilters">{{ t('common.reset') }}</button>
              <button class="btn btn-secondary" :disabled="loading" :title="t('common.refresh')" @click="loadReviews">
                <Icon name="refresh" size="sm" />
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="reviews" :loading="loading" row-key="id">
          <template #cell-user="{ row }">
            <div class="max-w-[220px]">
              <div class="truncate font-medium text-gray-900 dark:text-white">{{ row.username || row.user_email }}</div>
              <div class="truncate text-xs text-gray-400">{{ row.user_email }} · ID {{ row.user_id }}</div>
            </div>
          </template>
          <template #cell-task="{ row }">
            <div class="max-w-[250px] space-y-0.5 font-mono text-xs">
              <div class="truncate text-gray-700 dark:text-gray-200" :title="row.upstream_task_id">{{ row.upstream_task_id || '—' }}</div>
              <div class="truncate text-gray-400" :title="row.request_id">{{ row.request_id }}</div>
            </div>
          </template>
          <template #cell-model="{ row }">
            <div class="max-w-[180px]">
              <div class="truncate text-gray-800 dark:text-gray-200">{{ row.model }}</div>
              <div class="text-xs text-gray-400">{{ row.platform }} · {{ row.resolution || '—' }} · {{ row.duration_seconds }}s</div>
            </div>
          </template>
          <template #cell-cost="{ row }">
            <span class="whitespace-nowrap font-medium">{{ formatCurrency(row.estimated_cost) }}</span>
          </template>
          <template #cell-status="{ row }">
            <div class="space-y-1">
              <span :class="statusClass(row.billing_status)">{{ row.billing_status }}</span>
              <div class="text-xs text-gray-400">{{ row.task_status }} · {{ row.poll_count }}</div>
            </div>
          </template>
          <template #cell-updated_at="{ value }"><span class="whitespace-nowrap text-gray-500">{{ formatTime(value) }}</span></template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap justify-end gap-2">
              <button class="btn btn-ghost btn-sm" @click="openDetail(row)">{{ t('common.view') }}</button>
              <button
                v-if="row.upstream_task_id && row.billing_status !== 'settling'"
                data-testid="review-recheck"
                class="btn btn-secondary btn-sm"
                :disabled="actionLoadingId === row.id"
                @click="recheckTask(row)"
              >{{ t('admin.videoTaskReviews.actions.recheck') }}</button>
              <button
                data-testid="review-confirm-failed"
                class="btn btn-danger btn-sm"
                :disabled="actionLoadingId === row.id || row.billing_status === 'settling'"
                @click="openConfirmation(row, 'failed')"
              >{{ t('admin.videoTaskReviews.actions.confirmFailed') }}</button>
              <button
                class="btn btn-primary btn-sm"
                :disabled="actionLoadingId === row.id"
                @click="openConfirmation(row, 'succeeded')"
              >{{ t('admin.videoTaskReviews.actions.confirmSucceeded') }}</button>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="total > 0"
          :page="page"
          :page-size="pageSize"
          :total="total"
          @update:page="changePage"
          @update:page-size="changePageSize"
        />
      </template>
    </TablePageLayout>

    <BaseDialog :show="Boolean(selectedDetail)" :title="t('admin.videoTaskReviews.detailTitle')" width="wide" @close="selectedDetail = null">
      <dl v-if="selectedDetail" class="grid grid-cols-1 gap-x-8 gap-y-5 sm:grid-cols-2">
        <div v-for="field in detailFields" :key="field.label" class="min-w-0 border-b border-gray-100 pb-3 dark:border-dark-700">
          <dt class="text-xs font-medium text-gray-400">{{ field.label }}</dt>
          <dd class="mt-1 break-all text-sm text-gray-800 dark:text-gray-200">{{ field.value || '—' }}</dd>
        </div>
      </dl>
    </BaseDialog>

    <BaseDialog :show="Boolean(confirmingTask)" :title="confirmationTitle" width="narrow" @close="closeConfirmation">
      <div class="space-y-3">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.videoTaskReviews.reasonHint') }}</p>
        <textarea
          v-model.trim="reviewReason"
          data-testid="review-reason"
          rows="4"
          class="input resize-none"
          :placeholder="t('admin.videoTaskReviews.reasonPlaceholder')"
        ></textarea>
        <p v-if="reasonError" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.videoTaskReviews.reasonRequired') }}</p>
      </div>
      <template #footer>
        <button class="btn btn-secondary" :disabled="submitting" @click="closeConfirmation">{{ t('common.cancel') }}</button>
        <button
          data-testid="review-confirm-submit"
          :class="confirmationAction === 'failed' ? 'btn btn-danger' : 'btn btn-primary'"
          :disabled="submitting"
          @click="submitConfirmation"
        >{{ submitting ? t('common.processing') : t('common.confirm') }}</button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { videoTaskReviewsAPI, type VideoTaskReview, type VideoTaskReviewQuery } from '@/api/admin/videoTaskReviews'
import { useAppStore } from '@/stores/app'
import { formatCurrency } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const reviews = ref<VideoTaskReview[]>([])
const loading = ref(false)
const actionLoadingId = ref<number | null>(null)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const selectedDetail = ref<VideoTaskReview | null>(null)
const confirmingTask = ref<VideoTaskReview | null>(null)
const confirmationAction = ref<'failed' | 'succeeded'>('failed')
const reviewReason = ref('')
const reasonError = ref(false)
const submitting = ref(false)
const filters = reactive({ search: '', userId: '', apiKeyId: '', accountId: '', platform: '', model: '', taskStatus: '', billingStatus: '' })

const columns: Column[] = [
  { key: 'user', label: t('admin.videoTaskReviews.columns.user') },
  { key: 'task', label: t('admin.videoTaskReviews.columns.task') },
  { key: 'model', label: t('admin.videoTaskReviews.columns.model') },
  { key: 'cost', label: t('admin.videoTaskReviews.columns.cost') },
  { key: 'status', label: t('admin.videoTaskReviews.columns.status') },
  { key: 'updated_at', label: t('admin.videoTaskReviews.columns.updatedAt') },
  { key: 'actions', label: t('common.actions'), class: 'text-right' },
]

const toID = (value: string) => {
  const id = Number(value)
  return Number.isInteger(id) && id > 0 ? id : undefined
}

const query = (): VideoTaskReviewQuery => ({
  page: page.value,
  page_size: pageSize.value,
  search: filters.search || undefined,
  user_id: toID(filters.userId),
  api_key_id: toID(filters.apiKeyId),
  account_id: toID(filters.accountId),
  platform: filters.platform || undefined,
  model: filters.model || undefined,
  task_status: filters.taskStatus || undefined,
  billing_status: filters.billingStatus || undefined,
})

const loadReviews = async () => {
  loading.value = true
  try {
    const result = await videoTaskReviewsAPI.list(query())
    reviews.value = result.items
    total.value = result.total
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.videoTaskReviews.loadFailed')))
  } finally {
    loading.value = false
  }
}

const search = () => { page.value = 1; void loadReviews() }
const resetFilters = () => {
  Object.assign(filters, { search: '', userId: '', apiKeyId: '', accountId: '', platform: '', model: '', taskStatus: '', billingStatus: '' })
  search()
}
const changePage = (value: number) => { page.value = value; void loadReviews() }
const changePageSize = (value: number) => { pageSize.value = value; page.value = 1; void loadReviews() }
const openDetail = (task: VideoTaskReview) => { selectedDetail.value = task }

const recheckTask = async (task: VideoTaskReview) => {
  actionLoadingId.value = task.id
  try {
    await videoTaskReviewsAPI.recheck(task.id)
    appStore.showSuccess(t('admin.videoTaskReviews.recheckSuccess'))
    await loadReviews()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.videoTaskReviews.actionFailed')))
  } finally {
    actionLoadingId.value = null
  }
}

const openConfirmation = (task: VideoTaskReview, action: 'failed' | 'succeeded') => {
  confirmingTask.value = task
  confirmationAction.value = action
  reviewReason.value = ''
  reasonError.value = false
}
const closeConfirmation = () => { if (!submitting.value) confirmingTask.value = null }
const confirmationTitle = computed(() => t(`admin.videoTaskReviews.actions.${confirmationAction.value === 'failed' ? 'confirmFailed' : 'confirmSucceeded'}`))
const submitConfirmation = async () => {
  if (!confirmingTask.value) return
  if (!reviewReason.value.trim()) { reasonError.value = true; return }
  submitting.value = true
  try {
    const action = confirmationAction.value === 'failed' ? videoTaskReviewsAPI.confirmFailed : videoTaskReviewsAPI.confirmSucceeded
    await action(confirmingTask.value.id, reviewReason.value.trim())
    appStore.showSuccess(t('admin.videoTaskReviews.actionSuccess'))
    confirmingTask.value = null
    await loadReviews()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.videoTaskReviews.actionFailed')))
  } finally {
    submitting.value = false
  }
}

const statusClass = (status: string) => [
  'inline-flex rounded-md px-2 py-1 text-xs font-medium',
  status === 'manual_review' ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300' :
    status === 'settling' ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300' :
      'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300',
]
const formatTime = (value?: string) => value ? new Date(value).toLocaleString() : '—'
const detailFields = computed(() => {
  const row = selectedDetail.value
  if (!row) return []
  return [
    ['ID', row.id], [t('admin.videoTaskReviews.columns.user'), `${row.username || '—'} / ${row.user_email} / ${row.user_id}`],
    ['API Key', `${row.api_key_name || '—'} / ${row.api_key_id}`], [t('admin.videoTaskReviews.filters.accountId'), row.account_id],
    ['Request ID', row.request_id], ['Upstream Task ID', row.upstream_task_id],
    [t('admin.videoTaskReviews.columns.model'), `${row.platform} / ${row.model} / ${row.upstream_model}`],
    [t('admin.videoTaskReviews.columns.cost'), formatCurrency(row.estimated_cost)],
    [t('admin.videoTaskReviews.columns.status'), `${row.task_status} / ${row.billing_status}`],
    [t('admin.videoTaskReviews.pollError'), row.last_poll_error], [t('admin.videoTaskReviews.columns.updatedAt'), formatTime(row.updated_at)],
  ].map(([label, value]) => ({ label: String(label), value: String(value ?? '') }))
})

onMounted(loadReviews)
</script>
