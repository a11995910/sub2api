<template>
  <AppLayout>
    <div class="space-y-7">
      <header class="flex flex-col gap-4 border-b border-gray-200 pb-5 dark:border-dark-700 sm:flex-row sm:items-end sm:justify-between">
        <div class="min-w-0">
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">
            {{ t('admin.lottery.title') }}
          </h1>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.lottery.description') }}
          </p>
        </div>
        <button
          type="submit"
          form="lottery-config-form"
          class="btn btn-primary shrink-0"
          :disabled="loading || saving || Boolean(validationError)"
        >
          <Icon name="check" size="sm" />
          {{ saving ? t('admin.lottery.saving') : t('admin.lottery.save') }}
        </button>
      </header>

      <div v-if="loading" class="flex min-h-80 items-center justify-center">
        <LoadingSpinner size="lg" />
      </div>

      <template v-else>
        <form id="lottery-config-form" class="space-y-8" @submit.prevent="saveConfig">
          <section class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(22rem,1fr)]">
            <div class="space-y-6">
              <div class="max-w-xs">
                <label for="lottery-threshold" class="input-label">
                  {{ t('admin.lottery.threshold') }}
                </label>
                <div class="mt-2 flex items-center gap-3">
                  <input
                    id="lottery-threshold"
                    v-model.number="form.usage_threshold_m"
                    name="usage_threshold_m"
                    type="number"
                    min="0.000001"
                    step="0.001"
                    class="input min-w-0 flex-1 max-sm:text-base"
                  />
                  <span class="shrink-0 text-sm text-gray-500 dark:text-gray-400">
                    {{ t('admin.lottery.thresholdUnit') }}
                  </span>
                </div>
              </div>
            </div>

            <fieldset>
              <legend class="text-sm font-medium text-gray-800 dark:text-gray-200">
                {{ t('admin.lottery.awardMode') }}
              </legend>
              <div class="mt-2 grid grid-cols-2 rounded-md bg-gray-100 p-1 dark:bg-dark-700" role="radiogroup">
                <button
                  v-for="option in awardModeOptions"
                  :key="option.value"
                  type="button"
                  role="radio"
                  :aria-checked="form.award_mode === option.value"
                  class="rounded px-3 py-2 text-sm font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500"
                  :class="form.award_mode === option.value
                    ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white dark:shadow-none'
                    : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
                  @click="form.award_mode = option.value"
                >
                  {{ option.label }}
                </button>
              </div>
              <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
                {{ activeAwardModeDescription }}
              </p>
            </fieldset>
          </section>

          <section class="border-t border-gray-200 pt-6 dark:border-dark-700">
            <div class="flex items-center justify-between gap-4">
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                {{ t('admin.lottery.prizes') }}
              </h2>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="form.prizes.length >= 5"
                @click="addPrize"
              >
                <Icon name="plus" size="sm" />
                {{ t('admin.lottery.addPrize') }}
              </button>
            </div>

            <div class="mt-4 hidden grid-cols-[minmax(12rem,1fr)_10rem_9rem_2.5rem] gap-3 px-3 text-sm font-medium text-gray-500 sm:grid">
              <span>{{ t('admin.lottery.prizeName') }}</span>
              <span>{{ t('admin.lottery.rewardAmount') }}</span>
              <span>{{ t('admin.lottery.probability') }}</span>
              <span class="sr-only">{{ t('admin.lottery.actions') }}</span>
            </div>

            <div class="mt-2 divide-y divide-gray-950/5 border-y border-gray-200 dark:divide-white/10 dark:border-dark-700">
              <div
                v-for="(prize, index) in form.prizes"
                :key="prize.id || index"
                class="grid gap-3 py-4 sm:grid-cols-[minmax(12rem,1fr)_10rem_9rem_2.5rem] sm:items-center sm:px-3"
              >
                <div>
                  <label :for="`lottery-prize-name-${index}`" class="input-label sm:sr-only">
                    {{ t('admin.lottery.prizeName') }}
                  </label>
                  <input
                    :id="`lottery-prize-name-${index}`"
                    v-model="prize.name"
                    :name="`prizes[${index}].name`"
                    type="text"
                    maxlength="40"
                    class="input mt-1 w-full max-sm:text-base sm:mt-0"
                  />
                </div>
                <div>
                  <label :for="`lottery-prize-reward-${index}`" class="input-label sm:sr-only">
                    {{ t('admin.lottery.rewardAmount') }}
                  </label>
                  <input
                    :id="`lottery-prize-reward-${index}`"
                    v-model.number="prize.reward_amount"
                    :name="`prizes[${index}].reward_amount`"
                    type="number"
                    min="0.00000001"
                    step="0.01"
                    class="input mt-1 w-full tabular-nums max-sm:text-base sm:mt-0"
                  />
                </div>
                <div>
                  <label :for="`lottery-prize-probability-${index}`" class="input-label sm:sr-only">
                    {{ t('admin.lottery.probability') }}
                  </label>
                  <div class="relative mt-1 sm:mt-0">
                    <input
                      :id="`lottery-prize-probability-${index}`"
                      v-model.number="prize.probability_percent"
                      :name="`prizes[${index}].probability_percent`"
                      type="number"
                      min="0.01"
                      max="100"
                      step="0.01"
                      class="input w-full pr-8 tabular-nums max-sm:text-base"
                    />
                    <span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-gray-400">%</span>
                  </div>
                </div>
                <button
                  type="button"
                  class="btn btn-ghost btn-sm justify-self-end text-red-600 dark:text-red-400"
                  :title="t('admin.lottery.removePrize')"
                  @click="removePrize(index)"
                >
                  <Icon name="trash" size="sm" />
                  <span class="sr-only">{{ t('admin.lottery.removePrize') }}</span>
                </button>
              </div>

              <div class="grid gap-3 bg-gray-50 py-4 dark:bg-dark-800/60 sm:grid-cols-[minmax(12rem,1fr)_10rem_9rem_2.5rem] sm:items-center sm:px-3">
                <div class="min-w-0">
                  <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.lottery.thanks') }}</p>
                  <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.lottery.thanksAuto') }}</p>
                </div>
                <span class="hidden sm:block"></span>
                <span class="text-sm font-medium tabular-nums text-gray-800 dark:text-gray-200">
                  {{ formatPercent(thanksProbability) }}%
                </span>
                <span></span>
              </div>
            </div>

            <div class="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <p class="text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.lottery.probabilitySummary', {
                  configured: formatPercent(configuredProbability),
                  thanks: formatPercent(thanksProbability)
                }) }}
              </p>
              <p v-if="validationError" class="text-sm text-red-600 dark:text-red-400" role="alert">
                {{ validationError }}
              </p>
            </div>
          </section>
        </form>

        <section class="border-t border-gray-200 pt-6 dark:border-dark-700">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('admin.lottery.drawRecords') }}
          </h2>

          <div v-if="draws.length" class="-mx-4 -my-2 mt-3 overflow-x-auto whitespace-nowrap sm:-mx-6 lg:-mx-8">
            <div class="inline-block min-w-full px-4 py-2 align-middle sm:px-6 lg:px-8">
              <table class="w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead>
                  <tr>
                    <th class="whitespace-nowrap py-3 pr-4 text-left text-sm font-medium text-gray-500">{{ t('admin.lottery.user') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-left text-sm font-medium text-gray-500">{{ t('admin.lottery.result') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium text-gray-500">{{ t('admin.lottery.reward') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium text-gray-500">{{ t('admin.lottery.probabilityAtDraw') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium text-gray-500">{{ t('admin.lottery.chanceChange') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium text-gray-500">{{ t('admin.lottery.balanceAfter') }}</th>
                    <th class="whitespace-nowrap py-3 pl-4 text-right text-sm font-medium text-gray-500">{{ t('admin.lottery.time') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-950/5 dark:divide-white/10">
                  <tr v-for="draw in draws" :key="draw.id">
                    <td class="py-3 pr-4 text-sm text-gray-700 dark:text-gray-300">{{ draw.user_email || `#${draw.user_id}` }}</td>
                    <td class="px-4 py-3 text-sm font-medium text-gray-900 dark:text-white">{{ draw.prize_name }}</td>
                    <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ formatSpiritStones(draw.reward_amount) }}</td>
                    <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ formatPercent(draw.probability_percent) }}%</td>
                    <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ draw.chance_before }} → {{ draw.chance_after }}</td>
                    <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ formatSpiritStones(draw.balance_after) }}</td>
                    <td class="py-3 pl-4 text-right text-sm text-gray-500 dark:text-gray-400">{{ formatDateTime(draw.created_at) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <p v-else class="mt-3 border-t border-gray-200 py-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
            {{ t('admin.lottery.noRecords') }}
          </p>
          <Pagination
            v-if="drawTotal > 0"
            class="mt-3"
            :page="drawPage"
            :page-size="drawPageSize"
            :total="drawTotal"
            @update:page="changeDrawPage"
            @update:page-size="changeDrawPageSize"
          />
        </section>
      </template>

      <TotpStepUpDialog :controller="lotteryStepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { lotteryAdminAPI, type LotteryConfigInput, type LotteryPrizeInput } from '@/api/admin/lottery'
import type { LotteryAwardMode, LotteryConfig, LotteryDraw } from '@/api/lottery'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp
} from '@/composables/useStepUp'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime, formatSpiritStones } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()
const lotteryStepUp = useStepUp()

const loading = ref(true)
const saving = ref(false)
const draws = ref<LotteryDraw[]>([])
const drawPage = ref(1)
const drawPageSize = ref(20)
const drawTotal = ref(0)

const form = reactive<LotteryConfigInput>({
  usage_threshold_m: 1,
  award_mode: 'daily_once',
  prizes: []
})

const awardModeOptions = computed<Array<{ value: LotteryAwardMode; label: string }>>(() => [
  { value: 'daily_once', label: t('admin.lottery.dailyOnce') },
  { value: 'per_threshold', label: t('admin.lottery.perThreshold') }
])
const activeAwardModeDescription = computed(() => form.award_mode === 'per_threshold'
  ? t('admin.lottery.perThresholdDescription')
  : t('admin.lottery.dailyOnceDescription'))
const configuredProbability = computed(() => form.prizes.reduce((sum, prize) => sum + (Number(prize.probability_percent) || 0), 0))
const thanksProbability = computed(() => Math.max(0, 100 - configuredProbability.value))
const validationError = computed(() => {
  if (!Number.isFinite(form.usage_threshold_m) || form.usage_threshold_m <= 0) {
    return t('admin.lottery.invalidThreshold')
  }
  if (form.prizes.length > 5) return t('admin.lottery.maxPrizes')
  if (form.prizes.some((prize) => !prize.name.trim()
    || !Number.isFinite(prize.reward_amount) || prize.reward_amount <= 0
    || !Number.isFinite(prize.probability_percent) || prize.probability_percent <= 0)) {
    return t('admin.lottery.invalidPrize')
  }
  if (configuredProbability.value > 100.000001) return t('admin.lottery.invalidProbability')
  return ''
})

function applyConfig(value: LotteryConfig) {
  form.usage_threshold_m = value.usage_threshold_m
  form.award_mode = value.award_mode || 'daily_once'
  form.prizes = value.prizes
    .filter((prize) => !prize.is_thanks)
    .map((prize) => ({
      id: prize.id,
      name: prize.name,
      reward_amount: prize.reward_amount,
      probability_percent: prize.probability_percent
    }))
}

function addPrize() {
  if (form.prizes.length >= 5) return
  form.prizes.push({ name: '', reward_amount: 1, probability_percent: 1 })
}

function removePrize(index: number) {
  form.prizes.splice(index, 1)
}

function formatPercent(value: number) {
  return String(Number(value.toFixed(2)))
}

async function loadDraws() {
  const page = await lotteryAdminAPI.listDraws(drawPage.value, drawPageSize.value)
  draws.value = page.items
  drawTotal.value = page.total
  drawPage.value = page.page
  drawPageSize.value = page.page_size
}

async function loadPage() {
  loading.value = true
  try {
    const [loadedConfig] = await Promise.all([
      lotteryAdminAPI.getConfig(),
      loadDraws()
    ])
    applyConfig(loadedConfig)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.lottery.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  if (validationError.value || saving.value) return
  saving.value = true
  try {
    const payload: LotteryConfigInput = {
      usage_threshold_m: form.usage_threshold_m,
      award_mode: form.award_mode,
      prizes: form.prizes.map<LotteryPrizeInput>((prize) => ({
        id: prize.id,
        name: prize.name.trim(),
        reward_amount: prize.reward_amount,
        probability_percent: prize.probability_percent
      }))
    }
    applyConfig(await lotteryStepUp.run(() => lotteryAdminAPI.updateConfig(payload)))
    appStore.showSuccess(t('admin.lottery.saved'))
  } catch (error: unknown) {
    if (isStepUpCancelled(error)) return
    if (isStepUpBlocked(error)) {
      appStore.showError(
        stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
          ? t('stepUp.adminApiKeyForbidden')
          : t('stepUp.notEnabled')
      )
      return
    }
    appStore.showError(extractApiErrorMessage(error, t('admin.lottery.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function changeDrawPage(page: number) {
  drawPage.value = page
  try {
    await loadDraws()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.lottery.loadFailed')))
  }
}

async function changeDrawPageSize(pageSize: number) {
  drawPageSize.value = pageSize
  drawPage.value = 1
  await changeDrawPage(1)
}

onMounted(loadPage)
</script>
