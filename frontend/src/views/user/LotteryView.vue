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
          class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200"
          role="status"
        >
          {{ t('lottery.disabled') }}
        </div>

        <section class="grid gap-6 xl:grid-cols-[minmax(0,3fr)_minmax(20rem,2fr)]">
          <!-- 转盘主体卡片 -->
          <div
            class="relative min-w-0 overflow-hidden rounded-2xl border border-primary-100 bg-gradient-to-b from-primary-50/90 via-white to-white p-6 dark:border-primary-900/40 dark:from-primary-950/40 dark:via-dark-900 dark:to-dark-900 sm:p-8"
          >
            <div
              class="pointer-events-none absolute -top-24 left-1/2 h-56 w-[30rem] -translate-x-1/2 rounded-full bg-primary-400/20 blur-3xl dark:bg-primary-500/15"
              aria-hidden="true"
            ></div>

            <div class="relative mx-auto flex max-w-lg flex-col items-center">
              <div class="relative aspect-square w-full max-w-[24rem]">
                <div class="lottery-pointer" aria-hidden="true"></div>
                <div class="lottery-wheel-shell absolute inset-3 sm:inset-4">
                  <div
                    ref="wheelRef"
                    class="lottery-wheel"
                    :class="{ 'lottery-wheel-spinning': spinning }"
                    :style="wheelStyle"
                    aria-hidden="true"
                  ></div>
                  <div class="lottery-wheel-center">
                    <span class="text-3xl font-bold tabular-nums text-gray-900 dark:text-white">
                      {{ overview.state.available_chances }}
                    </span>
                    <span class="mt-1 text-xs font-medium tracking-wide text-gray-500 dark:text-gray-400">
                      {{ t('lottery.availableChances') }}
                    </span>
                  </div>
                </div>
              </div>

              <button
                type="button"
                class="btn btn-primary mt-6 min-w-40 shadow-lg shadow-primary-600/25 transition-transform hover:-translate-y-0.5"
                :disabled="!canDraw"
                @click="handleDraw"
              >
                <Icon name="gift" size="sm" />
                {{ drawButtonText }}
              </button>

              <div
                v-if="lastResult"
                class="mt-6 w-full rounded-xl border border-gray-200/80 bg-white/80 px-5 py-4 text-center backdrop-blur-sm dark:border-dark-700 dark:bg-dark-800/70"
                aria-live="polite"
              >
                <p class="text-xs font-medium uppercase tracking-wider text-gray-400 dark:text-gray-500">
                  {{ t('lottery.resultTitle') }}
                </p>
                <p
                  class="mt-1.5 text-lg font-semibold"
                  :class="lastResult.reward_amount > 0
                    ? 'text-primary-600 dark:text-primary-400'
                    : 'text-gray-700 dark:text-gray-300'"
                >
                  {{ resultText }}
                </p>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                  {{ t('lottery.balanceAfter') }}: {{ formatSpiritStones(lastResult.balance_after) }}
                </p>
              </div>
            </div>
          </div>

          <!-- 右侧信息栏 -->
          <div class="min-w-0 space-y-6">
            <!-- 统计卡片 -->
            <dl class="grid grid-cols-2 gap-3">
              <div class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/60">
                <dt class="truncate text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('lottery.totalEarned') }}</dt>
                <dd class="mt-1.5 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ overview.state.total_earned }}
                </dd>
              </div>
              <div class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/60">
                <dt class="truncate text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('lottery.totalDrawn') }}</dt>
                <dd class="mt-1.5 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ overview.state.total_drawn }}
                </dd>
              </div>
              <div class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/60">
                <dt class="truncate text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('lottery.todayAwarded') }}</dt>
                <dd class="mt-1.5 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ overview.state.today_awarded_chances }}
                </dd>
              </div>
              <div class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/60">
                <dt class="truncate text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('lottery.todayUsage') }}</dt>
                <dd class="mt-1.5 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ formatMillions(overview.today_usage_m) }}<span class="ml-0.5 text-sm font-semibold text-gray-400">M</span>
                </dd>
              </div>
            </dl>

            <!-- 今日进度 -->
            <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800/60">
              <div class="flex items-center justify-between gap-4 text-sm">
                <span class="font-semibold text-gray-800 dark:text-gray-200">{{ awardModeLabel }}</span>
                <span class="shrink-0 rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-semibold tabular-nums text-primary-700 dark:bg-primary-950/60 dark:text-primary-300">
                  {{ formatPercent(overview.today_progress_percent) }}%
                </span>
              </div>
              <div class="mt-3 h-2.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                <div
                  class="h-full rounded-full bg-gradient-to-r from-primary-500 to-primary-400 transition-[width] duration-500"
                  :style="{ width: `${Math.min(100, overview.today_progress_percent)}%` }"
                ></div>
              </div>
              <p class="mt-2.5 text-sm text-gray-500 dark:text-gray-400">
                {{ progressDescription }}
              </p>
            </div>

            <!-- 奖项与概率 -->
            <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800/60">
              <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('lottery.prizeList') }}
              </h2>
              <ul class="mt-3 space-y-1" role="list">
                <li
                  v-for="(prize, index) in visiblePrizes"
                  :key="prize.id"
                  class="flex min-w-0 items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-gray-50 dark:hover:bg-dark-700/50"
                >
                  <span
                    class="size-3 shrink-0 rounded-full ring-2 ring-white dark:ring-dark-800"
                    :style="{ backgroundColor: segmentColor(index, prize.is_thanks) }"
                    aria-hidden="true"
                  ></span>
                  <span class="min-w-0 flex-1 truncate text-sm text-gray-700 dark:text-gray-300">
                    {{ prize.name }}
                  </span>
                  <span class="shrink-0 text-sm font-medium tabular-nums text-gray-500 dark:text-gray-400">
                    {{ t('lottery.probability', { value: formatPercent(prize.probability_percent) }) }}
                  </span>
                </li>
              </ul>
            </div>
          </div>
        </section>

        <!-- 最近抽奖记录 -->
        <section class="rounded-2xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800/60 sm:p-6">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('lottery.recentDraws') }}
          </h2>
          <div v-if="overview.recent_draws.length" class="-mx-5 -my-2 mt-3 overflow-x-auto whitespace-nowrap sm:-mx-6">
            <div class="inline-block min-w-full px-5 py-2 align-middle sm:px-6">
              <table class="w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead>
                  <tr>
                    <th class="whitespace-nowrap py-3 pr-4 text-left text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{{ t('lottery.drawTime') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{{ t('lottery.prize') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{{ t('lottery.reward') }}</th>
                    <th class="whitespace-nowrap px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{{ t('lottery.chanceAfter') }}</th>
                    <th class="whitespace-nowrap py-3 pl-4 text-right text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{{ t('lottery.balanceAfter') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-950/5 dark:divide-white/10">
                  <tr v-for="draw in overview.recent_draws" :key="draw.id" class="transition-colors hover:bg-gray-50/70 dark:hover:bg-dark-700/40">
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
          <p v-else class="mt-3 border-t border-gray-100 py-10 text-center text-sm text-gray-400 dark:border-dark-700/70 dark:text-gray-500">
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
const wheelRef = ref<HTMLElement | null>(null)
let resultTimer: number | undefined

// 奖项配色：高饱和糖果色系，转盘上区分度更高
const prizeColors = ['#6366f1', '#f59e0b', '#10b981', '#ec4899', '#8b5cf6']
const thanksColor = '#cbd5e1'

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
  background: `repeating-conic-gradient(rgb(255 255 255 / 22%) 0deg 1deg, transparent 1deg 30deg), ${wheelBackground.value}`,
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
    const nextRotation = targetRotation(result.draw.prize_id)
    if (reduceMotion) {
      wheelRotation.value = nextRotation
      applyDrawResult(result)
      return
    }
    // 先保持旧角度并清除动画类，再在下一帧写入目标角度，确保 transition 能捕获起点和终点
    spinning.value = false
    void wheelRef.value?.offsetWidth
    requestAnimationFrame(() => {
      spinning.value = true
      wheelRotation.value = nextRotation
    })
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
/* 转盘外环：双层柔和投影，营造浮起质感 */
.lottery-wheel-shell {
  border-radius: 50%;
  background: linear-gradient(180deg, #ffffff, #eef2ff);
  box-shadow:
    0 20px 45px rgb(79 70 229 / 14%),
    0 2px 8px rgb(17 24 39 / 8%);
  padding: 0.625rem;
}

/* 转盘盘面：扇区 conic-gradient 由 :style 的 background 注入，此处叠加网格纹理 */
.lottery-wheel {
  position: absolute;
  inset: 0.625rem;
  border-radius: 50%;
  background-image: repeating-conic-gradient(rgb(255 255 255 / 22%) 0deg 1deg, transparent 1deg 30deg);
  box-shadow:
    inset 0 0 0 6px rgb(255 255 255 / 90%),
    inset 0 2px 12px rgb(17 24 39 / 18%);
}

.lottery-wheel-spinning {
  transition: transform 3.2s cubic-bezier(0.12, 0.72, 0.14, 1);
}

.lottery-wheel-center {
  position: absolute;
  inset: 50% auto auto 50%;
  display: flex;
  width: 7.25rem;
  height: 7.25rem;
  transform: translate(-50%, -50%);
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: #ffffff;
  box-shadow:
    0 0 0 6px rgb(255 255 255 / 55%),
    0 8px 24px rgb(17 24 39 / 18%);
}

/* 顶部指针：主色渐变三角 */
.lottery-pointer {
  position: absolute;
  top: -0.25rem;
  left: 50%;
  z-index: 2;
  width: 0;
  height: 0;
  transform: translateX(-50%);
  border-right: 0.8rem solid transparent;
  border-left: 0.8rem solid transparent;
  border-top: 1.9rem solid rgb(79 70 229);
  filter: drop-shadow(0 3px 3px rgb(79 70 229 / 35%));
}

:global(.dark) .lottery-wheel-shell {
  background: linear-gradient(180deg, rgb(31 41 55), rgb(17 24 39));
  box-shadow:
    0 20px 45px rgb(0 0 0 / 40%),
    0 0 0 1px rgb(255 255 255 / 8%);
}

:global(.dark) .lottery-wheel {
  background-image: repeating-conic-gradient(rgb(255 255 255 / 10%) 0deg 1deg, transparent 1deg 30deg);
  box-shadow:
    inset 0 0 0 6px rgb(31 41 55 / 95%),
    inset 0 2px 12px rgb(0 0 0 / 45%);
}

:global(.dark) .lottery-wheel-center {
  background: rgb(17 24 39);
  box-shadow:
    0 0 0 6px rgb(255 255 255 / 8%),
    0 8px 24px rgb(0 0 0 / 45%);
}

:global(.dark) .lottery-pointer {
  border-top-color: rgb(129 140 248);
  filter: drop-shadow(0 3px 3px rgb(0 0 0 / 45%));
}

@media (prefers-reduced-motion: reduce) {
  .lottery-wheel-spinning {
    transition: none;
  }
}
</style>
