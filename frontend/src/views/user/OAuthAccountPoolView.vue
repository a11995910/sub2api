<template>
  <AppLayout>
    <div class="space-y-5 pb-8">
      <div v-if="loading" class="space-y-5" aria-busy="true">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700">
          <div class="space-y-2">
            <div class="h-4 w-28 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-48 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div class="h-7 w-36 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        </div>
        <section v-for="groupIndex in 2" :key="groupIndex" class="space-y-3">
          <div class="flex items-center justify-between border-b border-gray-200 pb-2.5 dark:border-dark-700">
            <div class="h-5 w-40 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-4 w-16 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div
            class="grid grid-cols-1 gap-3 sm:grid-cols-2"
            :class="authStore.isAdmin ? 'lg:grid-cols-2' : 'lg:grid-cols-3 xl:grid-cols-4'"
          >
            <div
              v-for="cardIndex in 4"
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

      <div v-else class="space-y-5">
        <div
          data-testid="pool-summary"
          class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700"
        >
          <div class="min-w-0">
            <p class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('oauthAccountPool.overviewLabel') }}
            </p>
            <p class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">
              {{ t('oauthAccountPool.overviewDescription') }}
            </p>
          </div>
          <div class="flex shrink-0 flex-wrap items-center gap-2 text-sm tabular-nums">
            <span class="rounded-md bg-gray-100 px-2.5 py-1 text-gray-600 dark:bg-dark-800 dark:text-gray-300">
              {{ t('oauthAccountPool.groupCount', { count: groups.length }) }}
            </span>
            <span class="rounded-md bg-gray-100 px-2.5 py-1 text-gray-600 dark:bg-dark-800 dark:text-gray-300">
              {{ t('oauthAccountPool.visibleAccounts') }} {{ formatRequests(summary.account_count) }}
            </span>
            <span
              v-if="authStore.isAdmin"
              class="rounded-md bg-gray-100 px-2.5 py-1 text-gray-600 dark:bg-dark-800 dark:text-gray-300"
            >
              {{ t('oauthAccountPool.totalRequests') }} {{ formatRequests(summary.requests ?? 0) }}
            </span>
            <span
              v-if="authStore.isAdmin"
              class="rounded-md bg-gray-100 px-2.5 py-1 text-gray-600 dark:bg-dark-800 dark:text-gray-300"
            >
              {{ t('oauthAccountPool.totalTokens') }} {{ formatTokens(summary.tokens ?? 0) }}
            </span>
          </div>
        </div>

        <section
          v-for="(group, groupIndex) in groups"
          :key="`${group.name}-${groupIndex}`"
          class="space-y-3"
        >
          <div class="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-gray-200 pb-2.5 dark:border-dark-700">
            <div class="flex min-w-0 items-baseline gap-2.5">
              <h2 class="min-w-0 break-words text-base font-semibold text-gray-900 dark:text-white">
                {{ group.name }}
              </h2>
              <span class="shrink-0 text-sm text-gray-500 dark:text-gray-400 sm:text-xs">
                {{ t('oauthAccountPool.accountCount', { count: group.summary.account_count }) }}
              </span>
            </div>
            <span
              :class="[
                'inline-flex shrink-0 items-center gap-1.5 rounded-md px-2 py-1 text-sm font-medium sm:text-xs',
                statusBadgeClass(groupStatus(group)),
              ]"
            >
              <span
                :class="['size-1.5 rounded-full', statusDotClass(groupStatus(group))]"
                aria-hidden="true"
              ></span>
              {{ statusLabel(groupStatus(group)) }}
            </span>
          </div>

          <div
            class="grid grid-cols-1 gap-3 sm:grid-cols-2"
            :class="authStore.isAdmin ? 'lg:grid-cols-2' : 'lg:grid-cols-3 xl:grid-cols-4'"
          >
            <article
              v-for="(account, accountIndex) in group.accounts"
              :key="`${account.identifier}-${accountIndex}`"
              class="min-w-0 rounded-lg border border-gray-200/80 bg-white p-3.5 dark:border-dark-700 dark:bg-dark-800"
            >
              <div class="flex min-w-0 items-start justify-between gap-3">
                <div class="flex min-w-0 items-start gap-2.5">
                  <span
                    :class="['mt-1.5 size-2 shrink-0 rounded-full', statusDotClass(accountStatus(account))]"
                    aria-hidden="true"
                  ></span>
                  <h3
                    class="min-w-0 break-all text-sm font-semibold leading-5 text-gray-900 dark:text-white"
                    :title="account.identifier || t('oauthAccountPool.unknownIdentifier')"
                  >
                    {{ account.identifier || t('oauthAccountPool.unknownIdentifier') }}
                  </h3>
                </div>
                <span
                  class="shrink-0 rounded px-2 py-0.5 text-xs font-medium"
                  :class="planBadgeClass(account.plan_type)"
                >
                  {{ account.plan_type || t('oauthAccountPool.unknownPlan') }}
                </span>
              </div>

              <div class="mt-3 flex items-end justify-between gap-3 border-t border-gray-100 pt-2.5 dark:border-dark-700/80">
                <div class="min-w-0">
                  <p class="text-sm text-gray-500 dark:text-gray-400 sm:text-xs">
                    {{ t('oauthAccountPool.accountStatus') }}
                  </p>
                  <p class="mt-0.5 text-base font-medium text-gray-900 dark:text-white sm:text-sm">
                    {{ statusLabel(accountStatus(account)) }}
                  </p>
                </div>
                <div class="shrink-0 text-right">
                  <p class="text-sm text-gray-500 dark:text-gray-400 sm:text-xs">
                    {{ t('oauthAccountPool.connectionsShort') }}
                  </p>
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
                class="mt-3 border-y border-gray-100 py-2 dark:border-dark-700/80"
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

              <div v-if="authStore.isAdmin && account.usage" class="mt-3">
                <p class="mb-2 text-sm font-medium text-gray-500 dark:text-gray-400 sm:text-xs">
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

type AccountPoolStatus = 'available' | 'active' | 'busy'

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

const accountStatus = (account: OAuthAccountPoolAccount): AccountPoolStatus => {
  if (account.concurrency > 0 && account.current_concurrency >= account.concurrency) {
    return 'busy'
  }
  if (account.current_concurrency > 0) return 'active'
  return 'available'
}

const groupStatus = (group: OAuthAccountPoolGroup): AccountPoolStatus => {
  const statuses = group.accounts.map(accountStatus)
  if (statuses.includes('available')) return 'available'
  if (statuses.includes('active')) return 'active'
  return 'busy'
}

const statusLabel = (status: AccountPoolStatus) => {
  const labels: Record<AccountPoolStatus, string> = {
    available: t('oauthAccountPool.status.available'),
    active: t('oauthAccountPool.status.active'),
    busy: t('oauthAccountPool.status.busy'),
  }
  return labels[status]
}

const statusDotClass = (status: AccountPoolStatus) => {
  switch (status) {
    case 'busy':
      return 'bg-amber-500'
    case 'active':
      return 'bg-sky-500'
    default:
      return 'bg-emerald-500'
  }
}

const statusBadgeClass = (status: AccountPoolStatus) => {
  switch (status) {
    case 'busy':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'active':
      return 'bg-sky-100 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300'
    default:
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
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
