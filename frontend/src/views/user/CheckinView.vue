<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else>
        <section class="overflow-hidden rounded-lg border border-primary-100 bg-white dark:border-primary-900/40 dark:bg-dark-800">
          <div class="grid gap-0 lg:grid-cols-[minmax(0,1fr)_340px]">
            <div class="p-6 sm:p-8">
              <div class="mb-5 flex items-start justify-between gap-4">
                <div>
                  <p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('checkin.monthProgress', { count: summary?.month_count || 0 }) }}</p>
                  <h1 class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ settings?.content || t('checkin.title') }}</h1>
                </div>
                <span
                  class="rounded-full px-3 py-1 text-xs font-medium"
                  :class="summary?.today_checked ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'"
                >
                  {{ summary?.today_checked ? t('checkin.checkedToday') : t('checkin.notCheckedToday') }}
                </span>
              </div>

              <button
                class="btn btn-primary h-14 w-full justify-center text-base shadow-glow sm:w-auto sm:min-w-60"
                :disabled="submitting || !canCheckin"
                @click="handleCheckin"
              >
                <Icon v-if="submitting" name="refresh" size="md" class="mr-2 animate-spin" />
                <Icon v-else name="checkCircle" size="md" class="mr-2" />
                {{ primaryActionText }}
              </button>

              <div class="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.dailyReward') }}</p>
                  <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ formatSpiritStones(settings?.daily_reward || 0) }}</p>
                </div>
                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.day4Bonus') }}</p>
                  <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ formatSpiritStones(settings?.extra_reward_4 || 0) }}</p>
                </div>
                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.day16Bonus') }}</p>
                  <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ formatSpiritStones(settings?.extra_reward_16 || 0) }}</p>
                </div>
              </div>
            </div>

            <div class="border-t border-gray-200 bg-gray-50 p-6 dark:border-dark-700 dark:bg-dark-900/40 lg:border-l lg:border-t-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('checkin.nextBonus') }}</p>
              <p class="mt-2 text-3xl font-semibold text-gray-900 dark:text-white">{{ nextBonusText }}</p>
              <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('checkin.currentBalance', { balance: formatSpiritStones(authStore.user?.balance || 0) }) }}</p>
            </div>
          </div>
        </section>

        <section class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
          <div class="card p-4">
            <div class="mb-3 flex items-center justify-between gap-3">
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('checkin.monthCalendar', { year: summary?.year, month: summary?.month }) }}</h2>
              <button class="btn btn-secondary btn-sm" :disabled="loading" @click="loadOverview">
                <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
              </button>
            </div>
            <div class="grid grid-cols-7 gap-1.5">
              <div v-for="day in weekdayLabels" :key="day" class="h-7 text-center text-xs font-medium leading-7 text-gray-500 dark:text-gray-400">
                {{ day }}
              </div>
              <div v-for="blank in leadingBlanks" :key="`blank-${blank}`" class="h-9 rounded-md border border-transparent sm:h-10" />
              <div
                v-for="day in calendarDays"
                :key="day.date"
                class="flex h-9 flex-col items-center justify-center rounded-md border text-sm sm:h-10"
                :class="dayClass(day)"
              >
                <span class="font-medium">{{ day.day }}</span>
                <span v-if="day.record" class="mt-0.5 text-[10px] leading-none">{{ formatDayReward(day.record) }}</span>
              </div>
            </div>
          </div>

          <div class="card p-5">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('checkin.rulesTitle') }}</h2>
            <div class="mt-4 space-y-3 text-sm text-gray-600 dark:text-gray-300">
              <p>{{ t('checkin.ruleDaily', { reward: formatSpiritStones(settings?.daily_reward || 0) }) }}</p>
              <p>{{ t('checkin.ruleBonus4', { reward: formatSpiritStones(settings?.extra_reward_4 || 0) }) }}</p>
              <p>{{ t('checkin.ruleBonus16', { reward: formatSpiritStones(settings?.extra_reward_16 || 0) }) }}</p>
              <p>{{ t('checkin.ruleMonthReset') }}</p>
            </div>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { checkinAPI, type CheckinMonthSummary, type CheckinRecord, type CheckinSettings } from '@/api/checkin'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatSpiritStones } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const loading = ref(false)
const submitting = ref(false)
const settings = ref<CheckinSettings | null>(null)
const summary = ref<CheckinMonthSummary | null>(null)
const checkinRewardCycleDays = 16

const canCheckin = computed(() => Boolean(settings.value?.enabled && !summary.value?.today_checked && (settings.value.daily_reward || 0) > 0))
const primaryActionText = computed(() => {
  if (!settings.value?.enabled) return t('checkin.disabled')
  if (summary.value?.today_checked) return t('checkin.alreadyChecked')
  return t('checkin.checkNow')
})
const nextBonusText = computed(() => {
  const next = summary.value?.next_extra_milestone
  if (!next) return t('checkin.noMoreBonus')
  const consecutiveCount = summary.value?.consecutive_count || 0
  const current = summary.value?.consecutive_cycle_day ?? (consecutiveCount > 0
    ? (consecutiveCount - 1) % checkinRewardCycleDays + 1
    : 0)
  const days = next >= current ? next - current : checkinRewardCycleDays - current + next
  return t('checkin.daysToBonus', { days: Math.max(0, days), milestone: next })
})
const weekdayLabels = computed(() => [
  t('checkin.weekdays.sun'),
  t('checkin.weekdays.mon'),
  t('checkin.weekdays.tue'),
  t('checkin.weekdays.wed'),
  t('checkin.weekdays.thu'),
  t('checkin.weekdays.fri'),
  t('checkin.weekdays.sat'),
])
const recordByDate = computed(() => {
  const map = new Map<string, CheckinRecord>()
  for (const record of summary.value?.records || []) {
    map.set(dateKey(record.checkin_date), record)
  }
  return map
})
const leadingBlanks = computed(() => {
  if (!summary.value) return 0
  return new Date(summary.value.year, summary.value.month - 1, 1).getDay()
})
const calendarDays = computed(() => {
  if (!summary.value) return []
  return Array.from({ length: summary.value.days_in_month }, (_, index) => {
    const day = index + 1
    const key = `${summary.value!.year}-${String(summary.value!.month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    return {
      day,
      date: key,
      isToday: key === summary.value!.today,
      record: recordByDate.value.get(key),
    }
  })
})

async function loadOverview() {
  loading.value = true
  try {
    const data = await checkinAPI.getOverview()
    settings.value = data.settings
    summary.value = data.summary
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('checkin.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function handleCheckin() {
  if (!canCheckin.value || submitting.value) return
  submitting.value = true
  try {
    const result = await checkinAPI.checkin()
    summary.value = result.summary
    await authStore.refreshUser()
    appStore.showSuccess(t('checkin.success', { reward: formatSpiritStones(result.reward) }))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('checkin.failed')))
  } finally {
    submitting.value = false
  }
}

function dayClass(day: { isToday: boolean; record?: CheckinRecord }) {
  if (day.record?.extra_reward && day.record.extra_reward > 0) {
    return 'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-700 dark:bg-amber-900/25 dark:text-amber-200'
  }
  if (day.record) {
    return 'border-green-200 bg-green-50 text-green-700 dark:border-green-800 dark:bg-green-900/25 dark:text-green-300'
  }
  if (day.isToday) {
    return 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-700 dark:bg-primary-900/25 dark:text-primary-300'
  }
  return 'border-gray-200 bg-white text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400'
}

function formatDayReward(record: CheckinRecord) {
  const reward = record.daily_reward + record.extra_reward
  return `+${reward.toFixed(reward > 0 && reward < 1 ? 2 : 0)}`
}

function dateKey(value: string) {
  return String(value || '').slice(0, 10)
}

onMounted(() => {
  loadOverview()
})
</script>
