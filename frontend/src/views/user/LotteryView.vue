<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="border-b border-gray-200 pb-5 dark:border-dark-700">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">
          {{ t('lottery.title') }}
        </h1>
        <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
          {{ t('lottery.description') }}
        </p>
      </header>

      <div v-if="loading" class="flex min-h-80 items-center justify-center">
        <LoadingSpinner size="lg" />
      </div>

      <template v-else-if="overview">
        <div
          v-if="!overview.config.enabled"
          class="border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200"
          role="status"
        >
          {{ t('lottery.disabled') }}
        </div>

        <section class="grid gap-8 xl:grid-cols-[minmax(0,3fr)_minmax(20rem,2fr)]">
          <div class="card min-w-0 border border-gray-200 p-5 dark:border-dark-700 sm:p-7">
            <div class="mx-auto flex max-w-lg flex-col items-center">
              <div class="relative aspect-square w-full max-w-[25rem]">
                <div class="lottery-pointer" aria-hidden="true"></div>
                <div class="lottery-wheel-shell absolute inset-4 sm:inset-5">
                  <div
                    class="lottery-wheel"
                    :class="{ 'lottery-wheel-spinning': spinning }"
                    :style="wheelStyle"
                    aria-hidden="true"
                  ></div>
                  <div class="lottery-wheel-center">
                    <span class="text-3xl font-semibold tabular-nums text-gray-900 dark:text-white">
                      {{ overview.state.available_chances }}
                    </span>
                    <span class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                      {{ t('lottery.availableChances') }}
                    </span>
                  </div>
                </div>
              </div>

              <button
                type="button"
                class="btn btn-primary mt-5 min-w-36"
                :disabled="!canDraw"
                @click="handleDraw"
              >
                <Icon name="gift" size="sm" />
                {{ drawButtonText }}
              </button>

              <div
                v-if="lastResult"
                class="mt-5 w-full border-t border-gray-200 pt-5 text-center dark:border-dark-700"
                aria-live="polite"
              >
                <p class="text-sm text-gray-500 dark:text-gray-400">
                  {{ t('lottery.resultTitle') }}
                </p>
                <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
                  {{ resultText }}
                </p>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                  {{ t('lottery.balanceAfter') }}: {{ formatSpiritStones(lastResult.balance_after) }}
                </p>
              </div>
            </div>
          </div>

          <div class="min-w-0 space-y-7">
            <dl class="grid grid-cols-2 border-y border-gray-200 dark:border-dark-700">
              <div class="border-b border-r border-gray-200 py-4 pr-4 dark:border-dark-700">
                <dt class="truncate text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.totalEarned') }}</dt>
                <dd class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">
                  {{ overview.state.total_earned }}
                </dd>
              </div>
              <div class="border-b border-gray-200 py-4 pl-4 dark:border-dark-700">
                <dt class="truncate text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.totalDrawn') }}</dt>
                <dd class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">
                  {{ overview.state.total_drawn }}
                </dd>
              </div>
              <div class="border-r border-gray-200 py-4 pr-4 dark:border-dark-700">
                <dt class="truncate text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.todayAwarded') }}</dt>
                <dd class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">
                  {{ overview.state.today_awarded_chances }}
                </dd>
              </div>
              <div class="py-4 pl-4">
                <dt class="truncate text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.todayUsage') }}</dt>
                <dd class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">
                  {{ formatMillions(overview.today_usage_m) }}M
                </dd>
              </div>
            </dl>

            <div>
              <div class="flex items-center justify-between gap-4 text-sm">
                <span class="font-medium text-gray-800 dark:text-gray-200">{{ awardModeLabel }}</span>
                <span class="shrink-0 tabular-nums text-gray-500 dark:text-gray-400">
                  {{ formatPercent(overview.today_progress_percent) }}%
                </span>
              </div>
              <div class="mt-2 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                <div
                  class="h-full rounded-full bg-primary-600"
                  :style="{ width: `${Math.min(100, overview.today_progress_percent)}%` }"
                ></div>
              </div>
              <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
                {{ progressDescription }}
              </p>
            </div>

            <div>
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                {{ t('lottery.prizeList') }}
              </h2>
              <ul class="mt-3 divide-y divide-gray-950/5 dark:divide-white/10" role="list">
                <li
                  v-for="(prize, index) in visiblePrizes"
                  :key="prize.id"
                  class="flex min-w-0 items-center gap-3 py-3 first:pt-0 last:pb-0"
                >
                  <span
                    class="size-3 shrink-0 rounded-sm"
                    :style="{ backgroundColor: segmentColor(index, prize.is_thanks) }"
                    aria-hidden="true"
                  ></span>
                  <span class="min-w-0 flex-1 truncate text-sm text-gray-700 dark:text-gray-300">
                    {{ prize.name }}
                  </span>
                  <span class="shrink-0 text-sm tabular-nums text-gray-500 dark:text-gray-400">
                    {{ t('lottery.probability', { value: formatPercent(prize.probability_percent) }) }}
                  </span>
                </li>
              </ul>
            </div>
          </div>
        </section>

        <section>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('lottery.recentDraws') }}
          </h2>
          <div v-if="overview.recent_draws.length" class="-mx-4 -my-2 mt-3 overflow-x-auto whitespace-nowrap sm:-mx-6 lg:-mx-8">
            <div class="inline-block min-w-full px-4 py-2 align-middle sm:px-6 lg:px-8">
              <table class="w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead>
                  <tr>
                    <th class="whitespace-nowrap py-3 pr-4 text-left text-sm font-medium text-gray-500">{{ t('lottery.drawTime') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-left text-sm font-medium text-gray-500">{{ t('lottery.prize') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium text-gray-500">{{ t('lottery.reward') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium text-gray-500">{{ t('lottery.chanceAfter') }}</th>
                    <th class="whitespace-nowrap py-3 pl-4 text-right text-sm font-medium text-gray-500">{{ t('lottery.balanceAfter') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-950/5 dark:divide-white/10">
                  <tr v-for="draw in overview.recent_draws" :key="draw.id">
                    <td class="py-3 pr-4 text-sm text-gray-500 dark:text-gray-400">{{ formatDateTime(draw.created_at) }}</td>
                    <td class="px-4 py-3 text-sm font-medium text-gray-900 dark:text-white">{{ draw.prize_name }}</td>
                    <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ formatSpiritStones(draw.reward_amount) }}</td>
                    <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ draw.chance_after }}</td>
                    <td class="py-3 pl-4 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ formatSpiritStones(draw.balance_after) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <p v-else class="mt-3 border-t border-gray-200 py-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
            {{ t('lottery.noDraws') }}
          </p>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { lotteryAPI, type LotteryDraw, type LotteryDrawResult, type LotteryOverview } from '@/api/lottery'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime, formatSpiritStones } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const loading = ref(true)
const drawing = ref(false)
const spinning = ref(false)
const overview = ref<LotteryOverview | null>(null)
const lastResult = ref<LotteryDraw | null>(null)
const wheelRotation = ref(0)
let resultTimer: number | undefined

const prizeColors = ['#dc2626', '#d97706', '#059669', '#2563eb', '#7c3aed']
const thanksColor = '#71717a'

const visiblePrizes = computed(() => overview.value?.config.prizes.filter((prize) => prize.probability_percent > 0) ?? [])
const wheelSegments = computed(() => {
  let cursor = 0
  return visiblePrizes.value.map((prize, index) => {
    const start = cursor
    const end = cursor + prize.probability_percent
    cursor = end
    return { prize, start, end, color: segmentColor(index, prize.is_thanks) }
  })
})
const wheelBackground = computed(() => {
  if (!wheelSegments.value.length) return thanksColor
  const stops = wheelSegments.value.map((segment) => `${segment.color} ${segment.start}% ${segment.end}%`)
  return `conic-gradient(${stops.join(', ')})`
})
const wheelStyle = computed(() => ({
  background: wheelBackground.value,
  transform: `rotate(${wheelRotation.value}deg)`
}))
const canDraw = computed(() => Boolean(
  overview.value?.config.enabled &&
  overview.value.state.available_chances > 0 &&
  !drawing.value
))
const drawButtonText = computed(() => {
  if (drawing.value) return t('lottery.drawing')
  if (!overview.value?.state.available_chances) return t('lottery.noChance')
  return t('lottery.drawNow')
})
const awardModeLabel = computed(() => overview.value?.state.today_award_mode === 'per_threshold'
  ? t('lottery.perThresholdMode')
  : t('lottery.dailyOnceMode'))
const progressDescription = computed(() => {
  const key = overview.value?.state.today_award_mode === 'per_threshold'
    ? 'lottery.perThresholdProgress'
    : 'lottery.dailyOnceProgress'
  return t(key, { target: formatMillions(overview.value?.today_next_target_m ?? 0) })
})
const resultText = computed(() => {
  if (!lastResult.value || lastResult.value.reward_amount <= 0) return t('lottery.resultThanks')
  return t('lottery.resultWin', { reward: formatSpiritStones(lastResult.value.reward_amount) })
})

function segmentColor(index: number, isThanks: boolean) {
  return isThanks ? thanksColor : prizeColors[index % prizeColors.length]
}

function formatPercent(value: number) {
  return String(Number(value.toFixed(2)))
}

function formatMillions(value: number) {
  return String(Number(value.toFixed(3)))
}

function targetRotation(prizeID: string) {
  const segment = wheelSegments.value.find((item) => item.prize.id === prizeID)
  if (!segment) return wheelRotation.value + 5 * 360
  const midpointDegrees = ((segment.start + segment.end) / 2) * 3.6
  const targetModulo = (360 - midpointDegrees) % 360
  const currentModulo = ((wheelRotation.value % 360) + 360) % 360
  const delta = (targetModulo - currentModulo + 360) % 360
  return wheelRotation.value + 5 * 360 + delta
}

async function loadOverview() {
  loading.value = true
  try {
    overview.value = await lotteryAPI.getOverview()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('lottery.loadFailed')))
  } finally {
    loading.value = false
  }
}

function applyDrawResult(result: LotteryDrawResult) {
  if (!overview.value) return
  overview.value.state.available_chances = result.available_chances
  overview.value.state.total_drawn += 1
  overview.value.recent_draws = [result.draw, ...overview.value.recent_draws].slice(0, 10)
  lastResult.value = result.draw
  drawing.value = false
  spinning.value = false
  void authStore.refreshUser()
}

async function handleDraw() {
  if (!canDraw.value) return
  drawing.value = true
  lastResult.value = null
  try {
    const result = await lotteryAPI.draw()
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    spinning.value = !reduceMotion
    wheelRotation.value = targetRotation(result.draw.prize_id)
    if (reduceMotion) {
      applyDrawResult(result)
      return
    }
    resultTimer = window.setTimeout(() => applyDrawResult(result), 3300)
  } catch (error: unknown) {
    drawing.value = false
    spinning.value = false
    appStore.showError(extractApiErrorMessage(error, t('lottery.drawFailed')))
  }
}

onMounted(loadOverview)
onBeforeUnmount(() => {
  if (resultTimer !== undefined) window.clearTimeout(resultTimer)
})
</script>

<style scoped>
.lottery-wheel-shell {
  border: 1px solid rgb(17 24 39 / 12%);
  border-radius: 50%;
  box-shadow: 0 16px 40px rgb(17 24 39 / 12%);
}

.lottery-wheel {
  position: absolute;
  inset: 0;
  border: 10px solid white;
  border-radius: 50%;
  box-shadow: inset 0 0 0 1px rgb(17 24 39 / 10%);
}

.lottery-wheel-spinning {
  transition: transform 3.2s cubic-bezier(0.12, 0.72, 0.14, 1);
}

.lottery-wheel-center {
  position: absolute;
  inset: 50% auto auto 50%;
  display: flex;
  width: 7.5rem;
  height: 7.5rem;
  transform: translate(-50%, -50%);
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(17 24 39 / 10%);
  border-radius: 50%;
  background: white;
  box-shadow: 0 8px 24px rgb(17 24 39 / 16%);
}

.lottery-pointer {
  position: absolute;
  top: 0;
  left: 50%;
  z-index: 2;
  width: 0;
  height: 0;
  transform: translateX(-50%);
  border-right: 0.875rem solid transparent;
  border-left: 0.875rem solid transparent;
  border-top: 2rem solid rgb(17 24 39);
  filter: drop-shadow(0 2px 2px rgb(17 24 39 / 20%));
}

:global(.dark) .lottery-wheel-shell {
  border-color: rgb(255 255 255 / 10%);
  box-shadow: none;
}

:global(.dark) .lottery-wheel {
  border-color: rgb(31 41 55);
  box-shadow: inset 0 0 0 1px rgb(255 255 255 / 10%);
}

:global(.dark) .lottery-wheel-center {
  border-color: rgb(255 255 255 / 10%);
  background: rgb(31 41 55);
  box-shadow: none;
}

:global(.dark) .lottery-pointer {
  border-top-color: rgb(229 231 235);
  filter: none;
}

@media (prefers-reduced-motion: reduce) {
  .lottery-wheel-spinning {
    transition: none;
  }
}
</style>
