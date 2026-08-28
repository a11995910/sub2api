<template>
  <AppLayout>
    <div class="space-y-5">
      <section class="rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-950/5 dark:bg-dark-800 dark:shadow-none dark:ring-white/10">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div class="min-w-0">
            <h1 class="text-balance text-xl font-semibold text-gray-900 dark:text-white">{{ t('modelMarket.title') }}</h1>
            <p class="mt-1 text-pretty text-base text-gray-500 sm:text-sm dark:text-gray-400">{{ t('modelMarket.description') }}</p>
          </div>
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
            <div class="inline-grid grid-cols-[1fr_2rem]">
              <label for="model-market-sort" class="sr-only">{{ t('modelMarket.sort.label') }}</label>
              <select
                id="model-market-sort"
                v-model="modelSort"
                name="model-sort"
                class="input col-span-full row-start-1 min-w-52 appearance-none pr-8 max-sm:text-base"
              >
                <option value="recommended">{{ t('modelMarket.sort.recommended') }}</option>
                <option value="name-asc">{{ t('modelMarket.sort.nameAsc') }}</option>
                <option value="name-desc">{{ t('modelMarket.sort.nameDesc') }}</option>
              </select>
              <Icon name="chevronDown" size="xs" class="pointer-events-none col-start-2 row-start-1 shrink-0 place-self-center text-gray-400" />
            </div>
            <button
              type="button"
              :disabled="loading"
              class="btn btn-secondary relative justify-center"
              :title="t('common.refresh')"
              :aria-label="t('common.refresh')"
              @click="loadModels"
            >
              <span class="absolute left-1/2 top-1/2 size-[max(100%,3rem)] -translate-x-1/2 -translate-y-1/2 pointer-fine:hidden" aria-hidden="true"></span>
              <Icon name="refresh" size="xs" class="shrink-0" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>

        <div v-if="marketGroups.length" class="mt-5 border-t border-gray-950/10 pt-4 dark:border-white/10">
          <PlazaFilterBar
            :platforms="platforms"
            :groups="groupOptions"
            :rates="rates"
            :platform="selectedPlatform"
            :group-id="selectedGroupId"
            :rate="selectedRate"
            :search="searchQuery"
            @update:platform="selectedPlatform = $event"
            @update:group-id="selectedGroupId = $event"
            @update:rate="selectedRate = $event"
            @update:search="searchQuery = $event"
          />
        </div>
      </section>

      <section v-if="loading" class="card py-16 text-center">
        <Icon name="refresh" size="lg" class="mx-auto animate-spin text-gray-400" />
      </section>

      <section v-else-if="filteredGroups.length === 0" class="card py-16 text-center">
        <Icon name="inbox" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelMarket.empty') }}</p>
      </section>

      <section
        v-for="marketGroup in filteredGroups"
        v-else
        :key="marketGroup.group.id"
        class="space-y-4"
        data-testid="market-group-section"
      >
        <div class="rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-950/5 dark:bg-dark-800 dark:shadow-none dark:ring-white/10">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <PlatformIcon :platform="marketGroup.group.platform as GroupPlatform" size="sm" />
                <h2 class="min-w-0 text-balance text-lg font-semibold text-gray-900 dark:text-white" data-testid="market-group-title">
                  {{ marketGroup.group.name }}
                </h2>
                <span
                  class="inline-flex shrink-0 items-center rounded-full bg-emerald-50 px-3 py-1 text-sm font-semibold text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300"
                  data-testid="market-group-rate"
                >
                  {{ t('modelMarket.rateBadge', { rate: formatRate(effectiveTextRate(marketGroup.group)) }) }}
                </span>
                <span v-if="marketGroup.group.is_exclusive" class="rounded bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
                  {{ t('availableChannels.exclusive') }}
                </span>
                <span v-if="marketGroup.group.subscription_type === 'subscription'" class="rounded bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                  {{ t('modelMarket.subscriptionGroup') }}
                </span>
              </div>
              <p v-if="marketGroup.group.description?.trim()" class="mt-2 whitespace-pre-line text-pretty text-base text-gray-600 sm:text-sm dark:text-gray-300">
                {{ marketGroup.group.description }}
              </p>
            </div>
            <button
              type="button"
              class="btn btn-secondary shrink-0"
              :disabled="!marketGroup.models[0]"
              @click="marketGroup.models[0] && goTest(marketGroup.models[0], marketGroup.group)"
            >
              <Icon name="beaker" size="xs" class="shrink-0" />
              {{ t('modelMarket.test') }}
            </button>
          </div>
        </div>

        <div v-if="marketGroup.models.length === 0" class="card py-16 text-center">
          <Icon name="inbox" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelMarket.noModelsInGroup') }}</p>
        </div>

        <div v-else class="grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
          <article
            v-for="model in marketGroup.models"
            :key="model.key"
            class="rounded-lg bg-white p-4 shadow-sm ring-1 ring-gray-950/5 dark:bg-dark-800 dark:shadow-none dark:ring-white/10"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <PlatformIcon :platform="model.platform as GroupPlatform" size="sm" />
                  <h3 class="truncate text-base font-semibold text-gray-900 dark:text-white">{{ model.name }}</h3>
                </div>
                <div class="mt-2 flex flex-wrap items-center gap-1.5">
                  <span
                    :class="[
                      'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium',
                      platformBadgeClass(model.platform),
                    ]"
                  >
                    {{ platformLabel(model.platform) }}
                  </span>
                  <span class="rounded-md bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                    {{ billingModeLabel(model.pricing?.billing_mode) }}
                  </span>
                  <span v-if="model.pricing?.intervals?.length" class="rounded-md bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
                    {{ t('modelMarket.intervalCount', { count: model.pricing.intervals.length }) }}
                  </span>
                </div>
              </div>
              <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="goTest(model, marketGroup.group)">
                <Icon name="beaker" size="xs" class="shrink-0" />
                {{ t('modelMarket.test') }}
              </button>
            </div>

            <div
              class="mt-4"
              :class="model.kind === 'token' && model.pricing?.billing_mode !== BILLING_MODE_PER_REQUEST ? '' : 'grid grid-cols-1 gap-2 sm:grid-cols-3'"
            >
              <template v-if="model.kind === 'image'">
                <PriceTile
                  label="1K"
                  :value="formatImageTier(marketGroup.group, '1k')"
                  :official-value="formatOfficialImageTier(marketGroup.group, '1k', model.pricing?.price_currency)"
                  :discount-value="formatImageTierDiscount(marketGroup.group, '1k', model.pricing?.price_currency)"
                />
                <PriceTile
                  label="2K"
                  :value="formatImageTier(marketGroup.group, '2k')"
                  :official-value="formatOfficialImageTier(marketGroup.group, '2k', model.pricing?.price_currency)"
                  :discount-value="formatImageTierDiscount(marketGroup.group, '2k', model.pricing?.price_currency)"
                />
                <PriceTile
                  label="4K"
                  :value="formatImageTier(marketGroup.group, '4k')"
                  :official-value="formatOfficialImageTier(marketGroup.group, '4k', model.pricing?.price_currency)"
                  :discount-value="formatImageTierDiscount(marketGroup.group, '4k', model.pricing?.price_currency)"
                />
              </template>
              <template v-else-if="model.kind === 'video'">
                <PriceTile
                  v-for="resolution in videoResolutionsForModel(model.name)"
                  :key="resolution"
                  :label="formatVideoTierLabel(model, resolution, marketGroup.group)"
                  :value="formatVideoTier(model, resolution, marketGroup.group)"
                />
              </template>
              <template v-else-if="model.pricing?.billing_mode === BILLING_MODE_PER_REQUEST">
                <PriceTile
                  :label="t('modelMarket.columns.perRequest')"
                  :value="formatPrice(model.pricing?.per_request_price, 1, marketGroup.group, model.pricing?.billing_mode)"
                  :official-value="formatOfficialPrice(model.pricing?.per_request_price, 1, model.pricing?.price_currency)"
                  :discount-value="formatDiscountPercent(model.pricing?.per_request_price, 1, marketGroup.group, model.pricing?.billing_mode, model.pricing?.price_currency)"
                />
                <PriceTile :label="t('modelMarket.columns.multiplier')" :value="`x${formatRate(effectiveTextRate(marketGroup.group))}`" compact />
                <PriceTile :label="t('modelMarket.columns.cacheRead')" value="-" />
              </template>
              <template v-else>
                <TokenPriceSummary
                  :input-value="formatPrice(model.pricing?.input_price, perMillionScale, marketGroup.group, model.pricing?.billing_mode)"
                  :output-value="formatPrice(model.pricing?.output_price, perMillionScale, marketGroup.group, model.pricing?.billing_mode)"
                  :cache-read-value="formatPrice(model.pricing?.cache_read_price, perMillionScale, marketGroup.group, model.pricing?.billing_mode)"
                  :cache-write-value="formatPrice(model.pricing?.cache_write_price, perMillionScale, marketGroup.group, model.pricing?.billing_mode)"
                  :official-input-value="formatOfficialPrice(model.pricing?.input_price, perMillionScale, model.pricing?.price_currency)"
                  :discount-value="formatDiscountPercent(model.pricing?.input_price, perMillionScale, marketGroup.group, model.pricing?.billing_mode, model.pricing?.price_currency)"
                />
              </template>
            </div>

            <div class="mt-4 flex items-center justify-between border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <span>{{ t('modelMarket.effectiveRate') }}</span>
              <span>{{ multiplierLabel(marketGroup.group, multiplierModeForModel(model)) }}</span>
            </div>
          </article>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlazaFilterBar from '@/components/modelPlaza/PlazaFilterBar.vue'
import TokenPriceSummary from '@/components/user/model-market/TokenPriceSummary.vue'
import userChannelsAPI, {
  type UserAvailableChannel,
  type UserAvailableGroup,
  type UserSupportedModelPricing,
} from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import { formatOriginalCurrencyScaled, formatScaled } from '@/utils/pricing'
import { formatMultiplier } from '@/utils/formatters'
import { filterGroupsByModelAvailability, resolveModelKind, type ModelKind } from '@/utils/modelKind'
import {
  resolveVideoPriceQuote,
  videoResolutionsForModel,
  type VideoResolution,
} from '@/utils/videoPricing'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
  BILLING_MODE_VIDEO,
  PRICE_CURRENCY_CNY,
  type PriceCurrency,
  type BillingMode,
} from '@/constants/channel'
import type { Group, GroupPlatform } from '@/types'

const PriceTile = defineComponent({
  name: 'PriceTile',
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true },
    officialValue: { type: String, default: '' },
    discountValue: { type: String, default: '' },
    compact: { type: Boolean, default: false },
  },
  setup(props) {
    const { t } = useI18n()
    return () =>
      h('div', { class: 'min-h-[138px] rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900/50' }, [
        h('p', { class: 'text-xs text-gray-500 dark:text-gray-400' }, props.label),
        h('div', { class: props.compact ? 'mt-3' : 'mt-3 space-y-2' }, [
          h('div', { class: 'flex items-baseline justify-between gap-3' }, [
            h('span', { class: 'shrink-0 text-xs text-gray-500 dark:text-gray-400' }, t('modelMarket.currentPrice')),
            h('span', { class: 'min-w-0 truncate text-right font-mono text-base font-bold text-gray-900 dark:text-white', title: props.value }, props.value),
          ]),
          !props.compact && props.officialValue && props.officialValue !== '-'
            ? h('div', { class: 'flex items-baseline justify-between gap-3' }, [
                h('span', { class: 'shrink-0 text-xs text-gray-500 dark:text-gray-400' }, t('modelMarket.officialPrice')),
                h('span', { class: 'min-w-0 whitespace-nowrap text-right font-mono text-sm font-medium text-gray-600 dark:text-gray-300', title: props.officialValue }, props.officialValue),
              ])
            : null,
          !props.compact && props.discountValue
            ? h('div', { class: 'flex overflow-hidden rounded-md border border-red-200 bg-red-50 shadow-sm dark:border-red-900/70 dark:bg-red-950/30' }, [
                h('span', { class: 'shrink-0 bg-gradient-to-r from-orange-500 to-rose-500 px-2.5 py-1 text-xs font-bold text-white' }, t('modelMarket.discount')),
                h('span', { class: 'min-w-0 flex-1 truncate px-2 py-1 text-right font-mono text-sm font-bold text-red-700 dark:text-red-200', title: props.discountValue }, props.discountValue),
              ])
            : null,
        ]),
      ])
  },
})

interface GroupMarketModel {
  key: string
  name: string
  platform: string
  kind: ModelKind
  pricing: UserSupportedModelPricing | null
}

interface MarketGroup {
  group: UserAvailableGroup
  models: GroupMarketModel[]
}

type ModelSort = 'recommended' | 'name-asc' | 'name-desc'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const availableGroups = ref<UserAvailableGroup[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const selectedPlatform = ref('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const modelSort = ref<ModelSort>('recommended')
const perMillionScale = 1_000_000
const modelMarketUSDToCNYRate = computed<number | null>(() => {
  const value = Number(appStore.cachedPublicSettings?.model_market_usd_to_cny_rate)
  return Number.isFinite(value) && value >= 0.01 && value <= 100 ? value : null
})
const toAvailableGroup = (group: Group): UserAvailableGroup => ({
  id: group.id,
  name: group.name,
  description: group.description,
  platform: group.platform,
  subscription_type: group.subscription_type,
  rate_multiplier: group.rate_multiplier,
  promo_discount_enabled: group.promo_discount_enabled,
  promo_discount_start: group.promo_discount_start,
  promo_discount_end: group.promo_discount_end,
  promo_discount_rate: group.promo_discount_rate,
  promo_active: group.promo_active,
  is_exclusive: group.is_exclusive,
  allow_image_generation: group.allow_image_generation,
  image_super_resolution_enabled: group.image_super_resolution_enabled,
  image_rate_independent: group.image_rate_independent,
  cache_hit_quarter_to_input_enabled: group.cache_hit_quarter_to_input_enabled ?? false,
  cache_hit_target_percent: group.cache_hit_target_percent ?? 90,
  cache_hit_target_tolerance_percent: group.cache_hit_target_tolerance_percent ?? 0.5,
  cache_hit_half_life_days: group.cache_hit_half_life_days ?? 1,
  image_rate_multiplier: group.image_rate_multiplier,
  image_price_1k: group.image_price_1k,
  image_price_2k: group.image_price_2k,
  image_price_4k: group.image_price_4k,
  video_rate_independent: group.video_rate_independent,
  video_rate_multiplier: group.video_rate_multiplier,
  video_price_480p: group.video_price_480p,
  video_price_720p: group.video_price_720p,
  video_price_1080p: group.video_price_1080p,
  peak_rate_enabled: group.peak_rate_enabled ?? false,
  peak_start: group.peak_start ?? '',
  peak_end: group.peak_end ?? '',
  peak_rate_multiplier: group.peak_rate_multiplier ?? 1,
})

const pricingSignature = (pricing: UserSupportedModelPricing | null): string => {
  if (!pricing) return 'no-pricing'
  return JSON.stringify({
    billing_mode: pricing.billing_mode,
    price_currency: pricing.price_currency,
    input_price: pricing.billing_mode === BILLING_MODE_VIDEO ? null : pricing.input_price,
    output_price: pricing.output_price,
    cache_write_price: pricing.cache_write_price,
    cache_read_price: pricing.cache_read_price,
    image_output_price: pricing.image_output_price,
    per_request_price: pricing.per_request_price,
    intervals: pricing.intervals,
  })
}

function isGptModel(model: GroupMarketModel): boolean {
  const name = model.name.trim().toLowerCase()
  return model.kind === 'token' && (name === 'gpt' || name.startsWith('gpt-'))
}

function compareModelNames(a: GroupMarketModel, b: GroupMarketModel): number {
  return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
}

function compareMarketModels(a: GroupMarketModel, b: GroupMarketModel): number {
  const gptOrder = Number(isGptModel(b)) - Number(isGptModel(a))
  if (gptOrder !== 0) return gptOrder

  const order: Record<ModelKind, number> = { token: 0, image: 1, video: 2 }
  const kindOrder = order[a.kind] - order[b.kind]
  if (kindOrder !== 0) return kindOrder

  if (isGptModel(a) && isGptModel(b)) return compareModelNames(b, a)
  return a.platform.localeCompare(b.platform) || compareModelNames(a, b)
}

function compareMarketGroups(a: MarketGroup, b: MarketGroup): number {
  const rateOrder = effectiveTextRate(a.group) - effectiveTextRate(b.group)
  if (rateOrder !== 0) return rateOrder
  return a.group.name.localeCompare(b.group.name, undefined, { numeric: true, sensitivity: 'base' })
}

const marketGroups = computed<MarketGroup[]>(() => {
  const groups = new Map<number, { group: UserAvailableGroup; models: Map<string, GroupMarketModel> }>()
  const ensureGroup = (group: UserAvailableGroup) => {
    let bucket = groups.get(group.id)
    if (!bucket) {
      bucket = { group, models: new Map<string, GroupMarketModel>() }
      groups.set(group.id, bucket)
    }
    return bucket
  }

  for (const group of availableGroups.value) {
    ensureGroup(group)
  }

  for (const channel of channels.value) {
    for (const section of channel.platforms || []) {
      const platform = section.platform
      for (const group of section.groups || []) {
        ensureGroup(group)
      }
      for (const model of section.supported_models || []) {
        const kind = resolveModelKind(model)
        for (const group of filterGroupsByModelAvailability(section.groups, model)) {
          const bucket = ensureGroup(group)
          const modelPlatform = model.platform || platform
          const key = `${group.id}:${modelPlatform}:${model.name}:${pricingSignature(model.pricing)}`
          if (!bucket.models.has(key)) {
            bucket.models.set(key, {
              key,
              name: model.name,
              platform: modelPlatform,
              kind,
              pricing: model.pricing,
            })
          }
        }
      }
    }
  }
  return Array.from(groups.values())
    .map((item) => ({
      group: item.group,
      models: Array.from(item.models.values()).sort(compareMarketModels),
    }))
    .sort(compareMarketGroups)
})

const platforms = computed(() =>
  [...new Set(marketGroups.value.map((item) => item.group.platform).filter(Boolean))].sort(),
)

const groupOptions = computed(() => marketGroups.value.map((item) => ({
  id: item.group.id,
  name: item.group.name,
  platform: item.group.platform,
  rate: effectiveTextRate(item.group),
})))

const rates = computed(() =>
  [...new Set(marketGroups.value.map((item) => effectiveTextRate(item.group)))].sort((a, b) => a - b),
)

function sortModels(models: GroupMarketModel[]): GroupMarketModel[] {
  if (modelSort.value === 'name-asc') return [...models].sort(compareModelNames)
  if (modelSort.value === 'name-desc') return [...models].sort((a, b) => compareModelNames(b, a))
  return [...models].sort(compareMarketModels)
}

const filteredGroups = computed<MarketGroup[]>(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return marketGroups.value
    .filter((item) => selectedPlatform.value === 'all' || item.group.platform === selectedPlatform.value)
    .filter((item) => selectedGroupId.value === 'all' || item.group.id === selectedGroupId.value)
    .filter((item) => selectedRate.value === 'all' || effectiveTextRate(item.group) === selectedRate.value)
    .map((item) => ({
      ...item,
      models: sortModels(q
        ? item.models.filter((model) => model.name.toLowerCase().includes(q))
        : item.models),
    }))
    .filter((item) => !q || item.models.length > 0)
})

watch(platforms, (items) => {
  if (selectedPlatform.value !== 'all' && !items.includes(selectedPlatform.value)) {
    selectedPlatform.value = 'all'
  }
})

watch(groupOptions, (items) => {
  if (selectedGroupId.value !== 'all' && !items.some((item) => item.id === selectedGroupId.value)) {
    selectedGroupId.value = 'all'
  }
})

watch(rates, (items) => {
  if (selectedRate.value !== 'all' && !items.includes(selectedRate.value)) {
    selectedRate.value = 'all'
  }
})

function effectiveTextRate(group: UserAvailableGroup): number {
  return userGroupRates.value[group.id] ?? group.rate_multiplier ?? 1
}

function effectiveImageRate(group: UserAvailableGroup): number {
  return group.image_rate_independent ? group.image_rate_multiplier : effectiveTextRate(group)
}

function effectiveVideoRate(group: UserAvailableGroup): number {
  return group.video_rate_independent ? (group.video_rate_multiplier ?? 1) : effectiveTextRate(group)
}

function effectiveMultiplier(group: UserAvailableGroup, mode?: BillingMode): number {
  if (mode === BILLING_MODE_IMAGE) return effectiveImageRate(group)
  if (mode === BILLING_MODE_VIDEO) return effectiveVideoRate(group)
  return effectiveTextRate(group)
}

function formatRate(value: number): string {
  return formatMultiplier(value)
}

function formatPrice(
  value: number | null | undefined,
  scale: number,
  group: UserAvailableGroup,
  mode?: BillingMode,
): string {
  if (value == null) return '-'
  return formatScaled(value * effectiveMultiplier(group, mode), scale)
}

function formatOfficialPrice(
  value: number | null | undefined,
  scale: number,
  currency?: PriceCurrency,
): string {
  return formatOriginalCurrencyScaled(value ?? null, scale, currency)
}

function basePriceInCNY(
  value: number | null | undefined,
  scale: number,
  currency?: PriceCurrency,
): number | null {
  if (value == null) return null
  const base = value * scale
  if (!Number.isFinite(base) || base <= 0) return null
  if (currency === PRICE_CURRENCY_CNY) return base
  const rate = modelMarketUSDToCNYRate.value
  return rate == null ? null : base * rate
}

function formatDiscountPercent(
  value: number | null | undefined,
  scale: number,
  group: UserAvailableGroup,
  mode?: BillingMode,
  currency?: PriceCurrency,
): string {
  if (value == null) return ''
  const baseCNY = basePriceInCNY(value, scale, currency)
  if (baseCNY == null) return ''
  const current = value * effectiveMultiplier(group, mode) * scale
  if (!Number.isFinite(current) || current >= baseCNY) return ''
  const discount = (1 - current / baseCNY) * 100
  return `${discount.toFixed(1)}%`
}

function formatImageTier(group: UserAvailableGroup, tier: '1k' | '2k' | '4k'): string {
  const value = tier === '1k' ? group.image_price_1k : tier === '2k' ? group.image_price_2k : group.image_price_4k
  if (typeof value !== 'number') return '-'
  return formatScaled(value * effectiveImageRate(group), 1)
}

function formatOfficialImageTier(
  group: UserAvailableGroup,
  tier: '1k' | '2k' | '4k',
  currency?: PriceCurrency,
): string {
  const value = tier === '1k' ? group.image_price_1k : tier === '2k' ? group.image_price_2k : group.image_price_4k
  return formatOriginalCurrencyScaled(typeof value === 'number' ? value : null, 1, currency)
}

function formatImageTierDiscount(
  group: UserAvailableGroup,
  tier: '1k' | '2k' | '4k',
  currency?: PriceCurrency,
): string {
  const value = tier === '1k' ? group.image_price_1k : tier === '2k' ? group.image_price_2k : group.image_price_4k
  return formatDiscountPercent(typeof value === 'number' ? value : null, 1, group, BILLING_MODE_IMAGE, currency)
}

function videoQuote(model: GroupMarketModel, resolution: VideoResolution, group: UserAvailableGroup) {
  return resolveVideoPriceQuote({
    group,
    pricing: model.pricing,
    modelName: model.name,
    resolution,
    userGroupRate: userGroupRates.value[group.id],
  })
}

function formatVideoTier(
  model: GroupMarketModel,
  resolution: VideoResolution,
  group: UserAvailableGroup,
): string {
  const resolved = videoQuote(model, resolution, group)
  return resolved ? formatScaled(resolved.effectivePrice, 1) : '-'
}

function formatVideoTierLabel(
  model: GroupMarketModel,
  resolution: VideoResolution,
  group: UserAvailableGroup,
): string {
  const resolved = videoQuote(model, resolution, group)
  const usesRequestUnit = resolved?.billingUnit === 'request' || (
    !resolved && (
      model.pricing?.billing_mode === BILLING_MODE_IMAGE ||
      model.pricing?.billing_mode === BILLING_MODE_PER_REQUEST
    )
  )
  const unit = usesRequestUnit
    ? t('modelMarket.columns.perRequest')
    : t('modelTest.perSecond')
  return `${resolution} / ${unit}`
}

function multiplierModeForModel(model: GroupMarketModel): BillingMode | undefined {
  if (model.kind === 'video' && model.pricing?.billing_mode !== BILLING_MODE_TOKEN) {
    return BILLING_MODE_VIDEO
  }
  return model.pricing?.billing_mode
}

function multiplierLabel(group: UserAvailableGroup, mode?: BillingMode): string {
  return `x${formatRate(effectiveMultiplier(group, mode))}`
}

function billingModeLabel(mode?: BillingMode): string {
  switch (mode) {
    case BILLING_MODE_TOKEN:
      return t('availableChannels.pricing.billingModeToken')
    case BILLING_MODE_PER_REQUEST:
      return t('availableChannels.pricing.billingModePerRequest')
    case BILLING_MODE_IMAGE:
      return t('availableChannels.pricing.billingModeImage')
    case BILLING_MODE_VIDEO:
      return t('availableChannels.pricing.billingModeVideo')
    default:
      return t('modelMarket.noPricing')
  }
}

function goTest(model: GroupMarketModel, group: UserAvailableGroup) {
  router.push({
    path: '/model-test',
    query: {
      model: model.name,
      group_id: String(group.id),
      kind: model.kind,
      platform: model.platform,
    },
  })
}

async function loadModels() {
  loading.value = true
  try {
    const [list, groups, rates] = await Promise.all([
      userChannelsAPI.getCatalog(),
      userGroupsAPI.getAvailable().then((items) => items.map(toAvailableGroup)),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
    ])
    channels.value = list
    availableGroups.value = groups
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (!appStore.cachedPublicSettings) {
    void appStore.fetchPublicSettings()
  }
  void loadModels()
})
</script>
