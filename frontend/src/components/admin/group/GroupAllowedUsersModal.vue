<template>
  <BaseDialog :show="show" title="专属分组用户" width="wide" @close="emit('close')">
    <div v-if="group" class="space-y-4">
      <div class="rounded-lg border border-purple-100 bg-purple-50 px-4 py-3 dark:border-purple-900/40 dark:bg-purple-900/20">
        <div class="flex flex-wrap items-center gap-2">
          <span class="font-semibold text-gray-900 dark:text-white">{{ group.name }}</span>
          <span class="rounded-full bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
            {{ group.platform }}
          </span>
        </div>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
          当前仅展示仍有效的专属授权用户；已过期授权不会出现在列表中。
        </p>
      </div>

      <div v-if="loading" class="flex justify-center py-10">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <div v-else-if="items.length === 0" class="rounded-lg border border-dashed border-gray-200 py-10 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
        暂无授权用户
      </div>

      <div v-else class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-600">
          <thead class="bg-gray-50 dark:bg-dark-700">
            <tr>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">用户</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">状态</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">授权来源</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">剩余时间</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">到期时间</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">更新时间</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-800">
            <tr v-for="item in items" :key="`${item.user_id}-${item.group_id}`">
              <td class="px-4 py-3">
                <div class="font-medium text-gray-900 dark:text-white">{{ item.user_email || `#${item.user_id}` }}</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">{{ item.username || '-' }}</div>
              </td>
              <td class="px-4 py-3">
                <span
                  :class="[
                    'rounded-full px-2 py-0.5 text-xs font-medium',
                    item.user_status === 'active'
                      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : 'bg-gray-100 text-gray-600 dark:bg-dark-600 dark:text-gray-300'
                  ]"
                >
                  {{ item.user_status || '-' }}
                </span>
              </td>
              <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">{{ sourceLabel(item.source) }}</td>
              <td class="px-4 py-3">
                <span
                  :class="[
                    'rounded-full px-2 py-0.5 text-xs font-medium',
                    item.expires_at
                      ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
                      : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                  ]"
                >
                  {{ remainingText(item.expires_at) }}
                </span>
              </td>
              <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">{{ item.expires_at ? formatDateTime(item.expires_at) : '永久' }}</td>
              <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ item.updated_at ? formatDateTime(item.updated_at) : '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { adminAPI } from '@/api/admin'
import type { AdminGroup, UserAllowedGroupAccess } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { formatDateTime } from '@/utils/format'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ show: boolean; group: AdminGroup | null }>()
const emit = defineEmits(['close'])
const appStore = useAppStore()

const loading = ref(false)
const items = ref<UserAllowedGroupAccess[]>([])
const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0,
  pages: 0
})

const sourceLabel = (source?: string | null): string => {
  if (source === 'affiliate_payment_reward') return '邀请奖励'
  if (source === 'manual' || !source) return '手动授权'
  return source
}

const remainingText = (value?: string | null): string => {
  if (!value) return '永久有效'
  const expiresAt = new Date(value)
  const diffMs = expiresAt.getTime() - Date.now()
  if (!Number.isFinite(diffMs) || diffMs <= 0) return '已过期'
  const totalMinutes = Math.ceil(diffMs / 60000)
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  if (days > 0) return `剩余 ${days} 天 ${hours} 小时`
  if (hours > 0) return `剩余 ${hours} 小时 ${minutes} 分钟`
  return `剩余 ${minutes} 分钟`
}

const load = async () => {
  if (!props.group) return
  loading.value = true
  try {
    const res = await adminAPI.groups.getGroupAllowedUsers(
      props.group.id,
      pagination.page,
      pagination.page_size
    )
    items.value = res.items
    pagination.total = res.total
    pagination.pages = res.pages
  } catch (error) {
    console.error('Failed to load group allowed users:', error)
    appStore.showError('加载专属分组用户失败')
  } finally {
    loading.value = false
  }
}

const handlePageChange = (page: number) => {
  pagination.page = Math.max(1, Math.min(page, pagination.pages || 1))
  load()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  load()
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      pagination.page = 1
      load()
    }
  }
)
</script>
