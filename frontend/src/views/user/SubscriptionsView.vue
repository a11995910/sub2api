<template>
  <AppLayout>
    <div class="space-y-6">
      <section ref="plansSectionRef" class="space-y-4">
        <div class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('userSubscriptions.plansTitle') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{ t('userSubscriptions.plansDescription') }}
            </p>
          </div>
          <div class="inline-flex items-center gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm font-medium text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-300">
            <Icon name="gem" size="sm" />
            {{ t('userSubscriptions.currentBalance', { balance: formatSpiritStones(authStore.user?.balance || 0) }) }}
          </div>
        </div>

        <div v-if="plansLoading" class="flex justify-center rounded-xl border border-dashed border-gray-200 py-10 dark:border-dark-700">
          <div class="h-7 w-7 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
        </div>

        <div v-else-if="plans.length === 0" class="rounded-xl border border-dashed border-gray-200 bg-white p-8 text-center dark:border-dark-700 dark:bg-dark-800">
          <Icon name="gift" size="xl" class="mx-auto mb-3 text-gray-400" />
          <p class="font-medium text-gray-900 dark:text-white">{{ t('userSubscriptions.noPlans') }}</p>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('userSubscriptions.noPlansDesc') }}</p>
        </div>

        <div v-else class="grid gap-4 xl:grid-cols-3">
          <article
            v-for="plan in plans"
            :key="plan.id"
            class="flex min-h-[22rem] flex-col overflow-hidden rounded-xl border bg-white dark:bg-dark-800"
            :class="[
              platformBorderClass(plan.group_platform || ''),
              highlightedGroupId === plan.group_id ? 'ring-2 ring-emerald-400 ring-offset-2 ring-offset-white dark:ring-offset-dark-950' : ''
            ]"
          >
            <div class="border-b border-gray-100 p-5 dark:border-dark-700">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">
                    {{ plan.group_name || `Group #${plan.group_id}` }}
                  </p>
                  <h3 class="mt-1 break-words text-base font-semibold text-gray-900 dark:text-white">
                    {{ plan.name }}
                  </h3>
                </div>
                <span :class="['shrink-0 rounded-md border px-2 py-0.5 text-[11px] font-medium', platformBadgeClass(plan.group_platform || '')]">
                  {{ platformLabel(plan.group_platform || '') }}
                </span>
              </div>
              <p v-if="plan.description" class="mt-3 text-sm leading-6 text-gray-500 dark:text-dark-400">
                {{ plan.description }}
              </p>
            </div>

            <div class="flex flex-1 flex-col gap-4 p-5">
              <div class="flex items-end gap-2">
                <span class="text-2xl font-semibold text-emerald-600 dark:text-emerald-300">
                  {{ formatSpiritStones(plan.price) }}
                </span>
                <span v-if="plan.original_price && plan.original_price > plan.price" class="pb-1 text-sm text-gray-400 line-through">
                  {{ formatSpiritStones(plan.original_price) }}
                </span>
              </div>

              <div class="grid grid-cols-2 gap-2 text-xs text-gray-600 dark:text-dark-300">
                <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700/60">
                  <p class="text-gray-400">{{ t('userSubscriptions.validity') }}</p>
                  <p class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ formatValidity(plan) }}</p>
                </div>
                <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700/60">
                  <p class="text-gray-400">{{ t('userSubscriptions.rateMultiplier') }}</p>
                  <p class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ formatMultiplier(plan.rate_multiplier) }}</p>
                </div>
              </div>

              <ul v-if="plan.features?.length" class="space-y-2 text-sm text-gray-600 dark:text-dark-300">
                <li v-for="feature in plan.features.slice(0, 4)" :key="feature" class="flex gap-2">
                  <Icon name="checkCircle" size="sm" class="mt-0.5 shrink-0 text-emerald-500" />
                  <span class="break-words">{{ feature }}</span>
                </li>
              </ul>

              <div class="mt-auto space-y-3">
                <button
                  class="btn w-full justify-center"
                  :class="canAfford(plan) ? 'btn-primary' : 'btn-secondary'"
                  :disabled="!canAfford(plan) || purchasingPlanId === plan.id"
                  @click="openPurchaseConfirm(plan)"
                >
                  <Icon v-if="purchasingPlanId === plan.id" name="refresh" size="sm" class="mr-2 animate-spin" />
                  <Icon v-else name="gem" size="sm" class="mr-2" />
                  {{ purchaseButtonText(plan) }}
                </button>
                <router-link v-if="!canAfford(plan) && paymentEnabled" to="/payment" class="btn btn-secondary w-full justify-center">
                  {{ t('userSubscriptions.rechargeSpiritStones') }}
                </router-link>
              </div>
            </div>
          </article>
        </div>
      </section>

      <!-- Loading State -->
      <div v-if="loading" class="flex justify-center py-12">
        <div
          class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
        ></div>
      </div>

      <!-- Empty State -->
      <div v-else-if="subscriptions.length === 0" class="card p-12 text-center">
        <div
          class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
        >
          <Icon name="creditCard" size="xl" class="text-gray-400" />
        </div>
        <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('userSubscriptions.noActiveSubscriptions') }}
        </h3>
        <p class="text-gray-500 dark:text-dark-400">
          {{ t('userSubscriptions.noActiveSubscriptionsDesc') }}
        </p>
      </div>

      <!-- Subscriptions Grid -->
      <div v-else class="grid gap-6 lg:grid-cols-2">
        <div
          v-for="subscription in subscriptions"
          :key="subscription.id"
          class="overflow-hidden rounded-2xl border bg-white dark:bg-dark-800"
          :class="platformBorderClass(subscription.group?.platform || '')"
        >
          <!-- Header -->
          <div
            class="flex items-center justify-between border-b border-gray-100 p-4 dark:border-dark-700"
          >
            <div class="flex items-center gap-3">
              <div :class="['h-1.5 w-1.5 shrink-0 rounded-full', platformAccentDotClass(subscription.group?.platform || '')]" />
              <div>
                <div class="flex items-center gap-2">
                  <h3 class="font-semibold text-gray-900 dark:text-white">
                    {{ subscription.group?.name || `Group #${subscription.group_id}` }}
                  </h3>
                  <span :class="['rounded-md border px-2 py-0.5 text-[11px] font-medium', platformBadgeClass(subscription.group?.platform || '')]">
                    {{ platformLabel(subscription.group?.platform || '') }}
                  </span>
                </div>
                <p v-if="subscription.group?.description" class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                  {{ subscription.group.description }}
                </p>
                <div class="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-gray-400 dark:text-gray-500">
                  <span>{{ t('payment.planCard.rate') }}: ×{{ subscription.group?.rate_multiplier ?? 1 }}</span>
                  <span v-if="subscriptionHasPeakRate(subscription)" class="text-amber-700 dark:text-amber-300">
                    {{ t('payment.planCard.peakRate') }}: {{ subscriptionPeakRateLabel(subscription) }}
                  </span>
                </div>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <span
                :class="[
                  'rounded-full px-2 py-0.5 text-xs font-medium',
                  subscription.status === 'active'
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                    : subscription.status === 'expired'
                      ? 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400'
                      : 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
                ]"
              >
                {{ t(`userSubscriptions.status.${subscription.status}`) }}
              </span>
              <button
                v-if="subscription.status === 'active'"
                :class="['rounded-lg px-3 py-1.5 text-xs font-semibold text-white transition-colors', platformButtonClass(subscription.group?.platform || '')]"
                @click="scrollToPlans(subscription.group_id)"
              >
                {{ t('payment.renewNow') }}
              </button>
            </div>
          </div>

          <!-- Usage Progress -->
          <div class="space-y-4 p-4">
            <!-- Expiration Info -->
            <div v-if="subscription.expires_at" class="flex items-center justify-between text-sm">
              <span class="text-gray-500 dark:text-dark-400">{{
                t('userSubscriptions.expires')
              }}</span>
              <span :class="getExpirationClass(subscription.expires_at)">
                {{ formatExpirationDate(subscription.expires_at) }}
              </span>
            </div>
            <div v-else class="flex items-center justify-between text-sm">
              <span class="text-gray-500 dark:text-dark-400">{{
                t('userSubscriptions.expires')
              }}</span>
              <span class="text-gray-700 dark:text-gray-300">{{
                t('userSubscriptions.noExpiration')
              }}</span>
            </div>

            <!-- Daily Usage -->
            <div v-if="subscription.group?.daily_limit_usd" class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('userSubscriptions.daily') }}
                </span>
                <span class="text-sm text-gray-500 dark:text-dark-400">
                  {{ formatQuota(subscription.daily_usage_usd || 0) }} / {{
                    formatQuota(subscription.group.daily_limit_usd)
                  }}
                </span>
              </div>
              <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                <div
                  class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                  :class="
                    getProgressBarClass(
                      subscription.daily_usage_usd,
                      subscription.group.daily_limit_usd
                    )
                  "
                  :style="{
                    width: getProgressWidth(
                      subscription.daily_usage_usd,
                      subscription.group.daily_limit_usd
                    )
                  }"
                ></div>
              </div>
              <p
                v-if="subscription.daily_window_start"
                class="text-xs text-gray-500 dark:text-dark-400"
              >
                {{ formatDailyUsageWindow(subscription) }}
              </p>
            </div>

            <!-- Weekly Usage -->
            <div v-if="subscription.group?.weekly_limit_usd" class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('userSubscriptions.weekly') }}
                </span>
                <span class="text-sm text-gray-500 dark:text-dark-400">
                  {{ formatQuota(subscription.weekly_usage_usd || 0) }} / {{
                    formatQuota(subscription.group.weekly_limit_usd)
                  }}
                </span>
              </div>
              <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                <div
                  class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                  :class="
                    getProgressBarClass(
                      subscription.weekly_usage_usd,
                      subscription.group.weekly_limit_usd
                    )
                  "
                  :style="{
                    width: getProgressWidth(
                      subscription.weekly_usage_usd,
                      subscription.group.weekly_limit_usd
                    )
                  }"
                ></div>
              </div>
              <p
                v-if="subscription.weekly_window_start"
                class="text-xs text-gray-500 dark:text-dark-400"
              >
                {{
                  t('userSubscriptions.resetIn', {
                    time: formatResetTime(subscription.weekly_window_start, 168)
                  })
                }}
              </p>
            </div>

            <!-- Monthly Usage -->
            <div v-if="subscription.group?.monthly_limit_usd" class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('userSubscriptions.monthly') }}
                </span>
                <span class="text-sm text-gray-500 dark:text-dark-400">
                  {{ formatQuota(subscription.monthly_usage_usd || 0) }} / {{
                    formatQuota(subscription.group.monthly_limit_usd)
                  }}
                </span>
              </div>
              <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                <div
                  class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                  :class="
                    getProgressBarClass(
                      subscription.monthly_usage_usd,
                      subscription.group.monthly_limit_usd
                    )
                  "
                  :style="{
                    width: getProgressWidth(
                      subscription.monthly_usage_usd,
                      subscription.group.monthly_limit_usd
                    )
                  }"
                ></div>
              </div>
              <p
                v-if="subscription.monthly_window_start"
                class="text-xs text-gray-500 dark:text-dark-400"
              >
                {{
                  t('userSubscriptions.resetIn', {
                    time: formatResetTime(subscription.monthly_window_start, 720)
                  })
                }}
              </p>
            </div>

            <!-- No limits configured - Unlimited badge -->
            <div
              v-if="
                !subscription.group?.daily_limit_usd &&
                !subscription.group?.weekly_limit_usd &&
                !subscription.group?.monthly_limit_usd
              "
              class="flex items-center justify-center rounded-xl bg-gradient-to-r from-emerald-50 to-teal-50 py-6 dark:from-emerald-900/20 dark:to-teal-900/20"
            >
              <div class="flex items-center gap-3">
                <span class="text-4xl text-emerald-600 dark:text-emerald-400">∞</span>
                <div>
                  <p class="text-sm font-medium text-emerald-700 dark:text-emerald-300">
                    {{ t('userSubscriptions.unlimited') }}
                  </p>
                  <p class="text-xs text-emerald-600/70 dark:text-emerald-400/70">
                    {{ t('userSubscriptions.unlimitedDesc') }}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <ConfirmDialog
        :show="showPurchaseConfirm"
        :title="t('userSubscriptions.confirmPurchaseTitle')"
        :message="purchaseConfirmMessage"
        :confirm-text="t('userSubscriptions.confirmPurchaseButton')"
        @confirm="confirmPurchasePlan"
        @cancel="closePurchaseConfirm"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'
import { paymentAPI } from '@/api/payment'
import type { SubscriptionPlan } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTimeToMinute, formatSpiritStones } from '@/utils/format'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { platformBorderClass, platformBadgeClass, platformButtonClass, platformLabel } from '@/utils/platformColors'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { getRemainingDurationParts, isOneTimeDailyQuota, type RemainingDurationParts } from '@/utils/subscriptionQuota'

function platformAccentDotClass(p: string): string {
  switch (p) {
    case 'anthropic': return 'bg-orange-500'
    case 'openai': return 'bg-emerald-500'
    case 'antigravity': return 'bg-purple-500'
    case 'gemini': return 'bg-blue-500'
    default: return 'bg-gray-400'
  }
}

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const subscriptions = ref<UserSubscription[]>([])
const plans = ref<SubscriptionPlan[]>([])
const loading = ref(true)
const plansLoading = ref(true)
const purchasingPlanId = ref<number | null>(null)
const highlightedGroupId = ref<number | null>(null)
const plansSectionRef = ref<HTMLElement | null>(null)
const showPurchaseConfirm = ref(false)
const pendingPurchasePlan = ref<SubscriptionPlan | null>(null)
const currentBalance = computed(() => authStore.user?.balance || 0)
const paymentEnabled = computed(() => appStore.cachedPublicSettings?.payment_enabled !== false)
const formatQuota = (value: number) => formatSpiritStones(value)
const purchaseConfirmMessage = computed(() => {
  const plan = pendingPurchasePlan.value
  if (!plan) return ''
  return t('userSubscriptions.confirmPurchaseMessage', {
    plan: plan.name,
    price: formatSpiritStones(plan.price),
    balance: formatSpiritStones(currentBalance.value)
  })
})

function subscriptionHasPeakRate(subscription: UserSubscription): boolean {
  return hasPeakRate(subscription.group)
}

function subscriptionPeakRateLabel(subscription: UserSubscription): string {
  return formatPeakRateWindow(subscription.group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
}

async function loadSubscriptions() {
  try {
    loading.value = true
    subscriptions.value = await subscriptionsAPI.getMySubscriptions()
  } catch (error) {
    console.error('Failed to load subscriptions:', error)
    appStore.showError(t('userSubscriptions.failedToLoad'))
  } finally {
    loading.value = false
  }
}

async function loadPlans() {
  try {
    plansLoading.value = true
    const response = await paymentAPI.getPlans()
    plans.value = (response.data || []).map(normalizePlan)
  } catch (error) {
    console.error('Failed to load subscription plans:', error)
    appStore.showError(t('userSubscriptions.failedToLoadPlans'))
  } finally {
    plansLoading.value = false
  }
}

function normalizePlan(plan: SubscriptionPlan & { features?: string[] | string }): SubscriptionPlan {
  return {
    ...plan,
    features: Array.isArray(plan.features)
      ? plan.features
      : String(plan.features || '').split('\n').map((item) => item.trim()).filter(Boolean)
  }
}

function canAfford(plan: SubscriptionPlan): boolean {
  return currentBalance.value >= Number(plan.price || 0)
}

function purchaseButtonText(plan: SubscriptionPlan): string {
  if (purchasingPlanId.value === plan.id) return t('userSubscriptions.purchasing')
  if (!canAfford(plan)) return t('userSubscriptions.insufficientBalance')
  return t('userSubscriptions.purchaseWithBalance')
}

function openPurchaseConfirm(plan: SubscriptionPlan) {
  if (!canAfford(plan) || purchasingPlanId.value) return
  pendingPurchasePlan.value = plan
  showPurchaseConfirm.value = true
}

function closePurchaseConfirm() {
  showPurchaseConfirm.value = false
  pendingPurchasePlan.value = null
}

async function confirmPurchasePlan() {
  const plan = pendingPurchasePlan.value
  closePurchaseConfirm()
  if (!plan) return
  await purchasePlan(plan)
}

async function purchasePlan(plan: SubscriptionPlan) {
  if (!canAfford(plan) || purchasingPlanId.value) return
  try {
    purchasingPlanId.value = plan.id
    const result = await subscriptionsAPI.purchaseWithBalance(plan.id)
    appStore.showSuccess(t('userSubscriptions.purchaseSuccess', {
      plan: plan.name,
      code: result.redeem_code?.code || ''
    }))
    await Promise.all([
      loadSubscriptions(),
      loadPlans(),
      authStore.refreshUser().catch(() => null)
    ])
  } catch (error) {
    console.error('Failed to purchase subscription with balance:', error)
    appStore.showError(extractI18nErrorMessage(error, t, 'payment.errors', t('userSubscriptions.purchaseFailed')))
  } finally {
    purchasingPlanId.value = null
  }
}

function scrollToPlans(groupId?: number) {
  highlightedGroupId.value = groupId || null
  plansSectionRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  if (groupId) {
    window.setTimeout(() => {
      if (highlightedGroupId.value === groupId) {
        highlightedGroupId.value = null
      }
    }, 2200)
  }
}

function formatValidity(plan: SubscriptionPlan): string {
  const days = Number(plan.validity_days || 0)
  const unit = plan.validity_unit || 'day'
  if (unit === 'month') return t('userSubscriptions.validityMonths', { count: days })
  if (unit === 'week') return t('userSubscriptions.validityWeeks', { count: days })
  return t('userSubscriptions.validityDays', { count: days })
}

function formatMultiplier(value?: number): string {
  if (!value || value <= 0) return t('common.none')
  return `${value}x`
}

function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return '0%'
  const percentage = Math.min(((used || 0) / limit) * 100, 100)
  return `${percentage}%`
}

function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return 'bg-gray-400'
  const percentage = ((used || 0) / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

function formatExpirationDate(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))

  if (days < 0) {
    return t('userSubscriptions.status.expired')
  }

  const dateStr = formatDateTimeToMinute(expires)

  if (days === 0) {
    return `${dateStr} (${t('common.today')})`
  }
  if (days === 1) {
    return `${dateStr} (${t('common.tomorrow')})`
  }

  return t('userSubscriptions.daysRemaining', { days }) + ` (${dateStr})`
}

function getExpirationClass(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))

  if (days <= 0) return 'text-red-600 dark:text-red-400 font-medium'
  if (days <= 3) return 'text-red-600 dark:text-red-400'
  if (days <= 7) return 'text-orange-600 dark:text-orange-400'
  return 'text-gray-700 dark:text-gray-300'
}

function formatDurationParts(parts: RemainingDurationParts): string {
  if (parts.days > 0) {
    return `${parts.days}d ${parts.hours}h`
  }

  if (parts.hours > 0) {
    return `${parts.hours}h ${parts.minutes}m`
  }

  return `${parts.minutes}m`
}

function formatDailyUsageWindow(subscription: UserSubscription): string {
  if (isOneTimeDailyQuota(subscription) && subscription.expires_at) {
    const parts = getRemainingDurationParts(subscription.expires_at)
    if (!parts) return t('userSubscriptions.windowNotActive')
    return t('userSubscriptions.quotaEndsIn', { time: formatDurationParts(parts) })
  }

  return t('userSubscriptions.resetIn', {
    time: formatResetTime(subscription.daily_window_start, 24)
  })
}

function formatResetTime(windowStart: string | null, windowHours: number): string {
  if (!windowStart) return t('userSubscriptions.windowNotActive')

  const start = new Date(windowStart)
  const end = new Date(start.getTime() + windowHours * 60 * 60 * 1000)
  const parts = getRemainingDurationParts(end)

  return parts ? formatDurationParts(parts) : t('userSubscriptions.windowNotActive')
}

onMounted(() => {
  loadSubscriptions()
  loadPlans()
  authStore.refreshUser().catch(() => null)
})
</script>
