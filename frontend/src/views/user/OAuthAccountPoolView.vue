<template>
  <AppLayout>
    <div class="space-y-8">
      <div v-if="loading" class="space-y-8" aria-busy="true">
        <div
          class="grid divide-x divide-gray-200 border-y border-gray-200 py-4 dark:divide-dark-700 dark:border-dark-700"
          :class="authStore.isAdmin ? 'grid-cols-3' : 'grid-cols-1'"
        >
          <div v-for="item in authStore.isAdmin ? 3 : 1" :key="item" class="space-y-2 px-4 first:pl-0">
            <div class="h-3 w-16 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-6 w-24 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
          </div>
        </div>
        <section v-for="groupIndex in 2" :key="groupIndex" class="space-y-3">
          <div class="h-6 w-40 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div
              v-for="cardIndex in 2"
              :key="cardIndex"
              class="animate-pulse rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"
              :class="authStore.isAdmin ? 'h-72' : 'h-48'"
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

      <div v-else class="space-y-8">
        <dl
          data-testid="pool-summary"
          class="grid divide-x divide-gray-200 border-y border-gray-200 py-4 dark:divide-dark-700 dark:border-dark-700"
          :class="authStore.isAdmin ? 'grid-cols-3' : 'grid-cols-1'"
        >
          <div class="min-w-0 px-3 first:pl-0 sm:px-5">
            <dt class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('oauthAccountPool.visibleAccounts') }}
            </dt>
            <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">
              {{ formatRequests(summary.account_count) }}
            </dd>
          </div>
          <div v-if="authStore.isAdmin" class="min-w-0 px-3 sm:px-5">
            <dt class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('oauthAccountPool.totalRequests') }}
            </dt>
            <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">
              {{ formatRequests(summary.requests ?? 0) }}
            </dd>
          </div>
          <div v-if="authStore.isAdmin" class="min-w-0 px-3 sm:px-5">
            <dt class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('oauthAccountPool.totalTokens') }}
            </dt>
            <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">
              {{ formatTokens(summary.tokens ?? 0) }}
            </dd>
          </div>
        </dl>

        <section
          v-for="(group, groupIndex) in groups"
          :key="`${group.name}-${groupIndex}`"
          class="space-y-3"
        >
          <div class="flex flex-wrap items-end justify-between gap-2 border-b border-gray-200 pb-3 dark:border-dark-700">
            <div class="flex min-w-0 items-center gap-3">
              <h2 class="min-w-0 break-words text-base font-semibold text-gray-900 dark:text-white">
                {{ group.name }}
              </h2>
              <span class="shrink-0 text-xs text-gray-400 dark:text-gray-500">
                {{ t('oauthAccountPool.accountCount', { count: group.summary.account_count }) }}
              </span>
            </div>
            <div
              v-if="authStore.isAdmin"
              class="flex flex-wrap gap-x-4 gap-y-1 text-xs tabular-nums text-gray-500 dark:text-gray-400"
            >
              <span>{{ t('oauthAccountPool.totalRequests') }} {{ formatRequests(group.summary.requests ?? 0) }}</span>
              <span>{{ t('oauthAccountPool.totalTokens') }} {{ formatTokens(group.summary.tokens ?? 0) }}</span>
            </div>
          </div>

          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <article
              v-for="(account, accountIndex) in group.accounts"
              :key="`${account.identifier}-${accountIndex}`"
              class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800"
              :class="{ 'min-h-72': authStore.isAdmin }"
            >
              <div class="flex min-w-0 items-start justify-between gap-3">
                <div class="flex min-w-0 items-start gap-2">
                  <span class="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-emerald-500" aria-hidden="true"></span>
                  <h3
                    class="min-w-0 break-all text-sm font-semibold leading-5 text-gray-900 dark:text-white"
                    :title="account.identifier || t('oauthAccountPool.unknownIdentifier')"
                  >
                    {{ account.identifier || t('oauthAccountPool.unknownIdentifier') }}
                  </h3>
                </div>
                <div class="flex shrink-0 flex-col items-end gap-1.5">
                  <span
                    class="rounded px-2 py-0.5 text-xs font-medium"
                    :class="planBadgeClass(account.plan_type)"
                  >
                    {{ account.plan_type || t('oauthAccountPool.unknownPlan') }}
                  </span>
                  <AccountConcurrencyBadge
                    data-testid="account-concurrency"
                    :current="account.current_concurrency"
                    :max="account.concurrency"
                    :tooltip="t('oauthAccountPool.connections')"
                  />
                </div>
              </div>

              <div
                v-if="authStore.isAdmin && account.stats"
                class="mt-5 border-y border-gray-100 py-2 dark:border-dark-700/80"
              >
                <div class="grid grid-cols-[minmax(3.75rem,0.8fr)_minmax(0,1fr)_minmax(0,1fr)] gap-x-3 px-1 text-[11px] text-gray-400 dark:text-gray-500">
                  <span></span>
                  <span class="text-right">{{ t('oauthAccountPool.requests') }}</span>
                  <span class="text-right">{{ t('oauthAccountPool.tokens') }}</span>
                </div>
                <div
                  v-for="row in accountStatRows(account)"
                  :key="row.label"
                  class="grid grid-cols-[minmax(3.75rem,0.8fr)_minmax(0,1fr)_minmax(0,1fr)] gap-x-3 px-1 py-1.5 text-xs"
                >
                  <span class="font-medium text-gray-600 dark:text-gray-300">{{ row.label }}</span>
                  <span class="text-right tabular-nums text-gray-900 dark:text-white">
                    {{ formatRequests(row.stats.requests) }}
                  </span>
                  <span class="text-right tabular-nums text-gray-900 dark:text-white">
                    {{ formatTokens(row.stats.tokens) }}
                  </span>
                </div>
              </div>

              <div class="mt-4">
                <p class="mb-2 text-[11px] font-medium text-gray-500 dark:text-gray-400">
                  {{ t('oauthAccountPool.quotaStatus') }}
                </p>
                <OAuthUsageWindows
                  :usage="account.usage"
                  :empty-text="t('oauthAccountPool.noUsageSnapshot')"
                  :show-now-when-idle="true"
                />
              </div>
            </article>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { oauthAccountPoolAPI } from '@/api'
import type {
  OAuthAccountPoolAccount,
  OAuthAccountPoolGroup,
  OAuthAccountPoolSummary,
} from '@/api/oauthAccountPool'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import OAuthUsageWindows from '@/components/account/OAuthUsageWindows.vue'
import AccountConcurrencyBadge from '@/components/account/AccountConcurrencyBadge.vue'
import { useAuthStore } from '@/stores/auth'
import { formatCompactNumber } from '@/utils/format'

const { t } = useI18n()
const authStore = useAuthStore()
const groups = ref<OAuthAccountPoolGroup[]>([])
const summary = ref<OAuthAccountPoolSummary>({ account_count: 0, requests: 0, tokens: 0 })
const loading = ref(true)
const error = ref(false)

const formatRequests = (value: number) => formatCompactNumber(value, { allowBillions: false })
const formatTokens = (value: number) => formatCompactNumber(value)

const accountStatRows = (account: OAuthAccountPoolAccount) => {
  if (!account.stats) return []
  return [
    { label: t('oauthAccountPool.period5h'), stats: account.stats.five_hour },
    { label: t('oauthAccountPool.period7d'), stats: account.stats.seven_day },
    { label: t('oauthAccountPool.cumulative'), stats: account.stats.total },
  ]
}

const planBadgeClass = (planType: string) => {
  switch (planType) {
    case 'Pro 20x':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'Team':
      return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300'
    case 'Plus':
      return 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300'
    case 'K12':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'
  }
}

const loadPool = async () => {
  loading.value = true
  error.value = false
  try {
    const result = await oauthAccountPoolAPI.get()
    groups.value = result.groups || []
    summary.value = result.summary || { account_count: 0, requests: 0, tokens: 0 }
  } catch {
    groups.value = []
    summary.value = { account_count: 0, requests: 0, tokens: 0 }
    error.value = true
  } finally {
    loading.value = false
  }
}

onMounted(loadPool)
</script>
