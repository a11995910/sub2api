<template>
  <AppLayout>
    <div class="space-y-8">
      <div v-if="loading" class="space-y-8" aria-busy="true">
        <section v-for="groupIndex in 2" :key="groupIndex" class="space-y-3">
          <div class="h-6 w-40 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
          <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            <div
              v-for="cardIndex in 3"
              :key="cardIndex"
              class="h-32 animate-pulse rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"
            ></div>
          </div>
        </section>
      </div>

      <div v-else-if="error" class="card p-10 text-center">
        <Icon name="exclamationTriangle" size="xl" class="mx-auto text-red-500" />
        <h2 class="mt-4 text-base font-semibold text-gray-900 dark:text-white">
          {{ t('oauthAccountPool.errorTitle') }}
        </h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t('oauthAccountPool.errorDescription') }}
        </p>
        <button type="button" class="btn btn-secondary mt-5" @click="loadPool">
          <Icon name="refresh" size="sm" class="mr-2" />
          {{ t('common.retry') }}
        </button>
      </div>

      <EmptyState
        v-else-if="groups.length === 0"
        :title="t('oauthAccountPool.emptyTitle')"
        :description="t('oauthAccountPool.emptyDescription')"
      />

      <section
        v-for="(group, groupIndex) in groups"
        v-else
        :key="`${group.name}-${groupIndex}`"
        class="space-y-3"
      >
        <div class="flex items-center gap-3 border-b border-gray-200 pb-3 dark:border-dark-700">
          <h2 class="min-w-0 break-words text-base font-semibold text-gray-900 dark:text-white">
            {{ group.name }}
          </h2>
          <span class="shrink-0 text-xs text-gray-400 dark:text-gray-500">
            {{ t('oauthAccountPool.accountCount', { count: group.accounts.length }) }}
          </span>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          <article
            v-for="(account, accountIndex) in group.accounts"
            :key="`${account.name}-${accountIndex}`"
            class="min-h-32 rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="mb-4 flex min-w-0 items-center gap-2">
              <span class="h-2 w-2 shrink-0 rounded-full bg-emerald-500" aria-hidden="true"></span>
              <h3 class="min-w-0 break-words text-sm font-semibold text-gray-900 dark:text-white">
                {{ account.name }}
              </h3>
            </div>
            <OAuthUsageWindows
              :usage="account.usage"
              :empty-text="t('oauthAccountPool.noUsageSnapshot')"
              :show-now-when-idle="true"
            />
          </article>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { oauthAccountPoolAPI } from '@/api'
import type { OAuthAccountPoolGroup } from '@/api/oauthAccountPool'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import OAuthUsageWindows from '@/components/account/OAuthUsageWindows.vue'

const { t } = useI18n()
const groups = ref<OAuthAccountPoolGroup[]>([])
const loading = ref(true)
const error = ref(false)

const loadPool = async () => {
  loading.value = true
  error.value = false
  try {
    const result = await oauthAccountPoolAPI.get()
    groups.value = result.groups || []
  } catch {
    groups.value = []
    error.value = true
  } finally {
    loading.value = false
  }
}

onMounted(loadPool)
</script>
