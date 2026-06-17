<template>
  <AppLayout>
    <div class="space-y-5">
      <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div class="min-w-0">
            <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('modelMarket.title') }}</h1>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('modelMarket.description') }}</p>
          </div>
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
            <div class="relative w-full sm:w-80">
              <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500" />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('modelMarket.searchPlaceholder')"
                class="input pl-10"
              />
            </div>
            <button
              @click="loadModels"
              :disabled="loading"
              class="btn btn-secondary justify-center"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>

        <div class="mt-5 flex gap-2 overflow-x-auto pb-1">
          <button
            v-for="group in visibleGroups"
            :key="group.group.id"
            type="button"
            class="inline-flex shrink-0 items-center gap-2 rounded-lg border px-3 py-2 text-sm font-medium transition"
            :class="selectedGroupId === group.group.id
              ? 'border-primary-600 bg-primary-600 text-white shadow-sm'
              : 'border-gray-200 bg-white text-gray-700 shadow-sm hover:border-primary-200 hover:bg-primary-50 hover:text-primary-700 dark:border-dark-700 dark:bg-dark-900/50 dark:text-gray-200 dark:hover:border-primary-800 dark:hover:bg-primary-900/20 dark:hover:text-primary-300'"
            @click="selectedGroupId = group.group.id"
          >
            <PlatformIcon :platform="group.group.platform as GroupPlatform" size="xs" />
            <span class="max-w-48 truncate">{{ group.group.name }}</span>
            <span
              class="rounded-full px-1.5 py-0.5 text-[11px]"
              :class="selectedGroupId === group.group.id ? 'bg-white/20 text-white' : 'bg-white text-gray-500 dark:bg-dark-800 dark:text-gray-400'"
            >
              x{{ formatRate(effectiveTextRate(group.group)) }}
            </span>
          </button>
        </div>
      </section>

      <section v-if="loading" class="card py-16 text-center">
        <Icon name="refresh" size="lg" class="mx-auto animate-spin text-gray-400" />
      </section>

      <section v-else-if="visibleGroups.length === 0" class="card py-16 text-center">
        <Icon name="inbox" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelMarket.empty') }}</p>
      </section>

      <template v-else>
        <section v-if="selectedGroup" class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <PlatformIcon :platform="selectedGroup.group.platform as GroupPlatform" size="sm" />
                <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ selectedGroup.group.name }}</h2>
                <span v-if="selectedGroup.group.is_exclusive" class="rounded bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
                  {{ t('availableChannels.exclusive') }}
                </span>
                <span v-if="selectedGroup.group.subscription_type === 'subscription'" class="rounded bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                  {{ t('modelMarket.subscriptionGroup') }}
                </span>
              </div>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('modelMarket.groupSummary', { count: selectedModels.length, rate: formatRate(effectiveTextRate(selectedGroup.group)) }) }}
              </p>
            </div>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="!selectedModels[0]"
              @click="selectedModels[0] && goTestSelected(selectedModels[0])"
            >
              <Icon name="beaker" size="sm" />
              {{ t('modelMarket.test') }}
            </button>
          </div>
        </section>

        <section v-if="selectedModels.length === 0" class="card py-16 text-center">
          <Icon name="inbox" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelMarket.noModelsInGroup') }}</p>
        </section>

        <section v-else class="grid gap-4 xl:grid-cols-2 2xl:grid-cols-3">
          <article
            v-for="model in selectedModels"
            :key="model.key"
            class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm transition hover:border-primary-200 hover:shadow-md dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-900"
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
                      'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium',
                      platformBadgeClass(model.platform),
                    ]"
                  >
                    {{ platformLabel(model.platform) }}
                  </span>
                  <span class="rounded-md bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                    {{ billingModeLabel(model.pricing?.billing_mode) }}
                  </span>
                  <span v-if="model.pricing?.intervals?.length" class="rounded-md bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
                    {{ t('modelMarket.intervalCount', { count: model.pricing.intervals.length }) }}
                  </span>
                </div>
              </div>
              <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="goTestSelected(model)">
                <Icon name="beaker" size="sm" />
                {{ t('modelMarket.test') }}
              </button>
            </div>

            <div class="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-3">
              <template v-if="model.kind === 'image'">
                <PriceTile
                  label="1K"
                  :value="formatSelectedImageTier('1k')"
                  :official-value="formatSelectedOfficialImageTier('1k')"
                  :discount-value="formatSelectedImageTierDiscount('1k')"
                />
                <PriceTile
                  label="2K"
                  :value="formatSelectedImageTier('2k')"
                  :official-value="formatSelectedOfficialImageTier('2k')"
                  :discount-value="formatSelectedImageTierDiscount('2k')"
                />
                <PriceTile
                  label="4K"
                  :value="formatSelectedImageTier('4k')"
                  :official-value="formatSelectedOfficialImageTier('4k')"
                  :discount-value="formatSelectedImageTierDiscount('4k')"
                />
              </template>
              <template v-else-if="model.pricing?.billing_mode === BILLING_MODE_PER_REQUEST">
                <PriceTile
                  :label="t('modelMarket.columns.perRequest')"
                  :value="formatSelectedPrice(model.pricing?.per_request_price, 1, model.pricing?.billing_mode)"
                  :official-value="formatOfficialPrice(model.pricing?.per_request_price, 1)"
                  :discount-value="formatSelectedDiscount(model.pricing?.per_request_price, 1, model.pricing?.billing_mode)"
                />
                <PriceTile :label="t('modelMarket.columns.multiplier')" :value="selectedTextRateLabel" compact />
                <PriceTile :label="t('modelMarket.columns.cacheRead')" value="-" />
              </template>
              <template v-else>
                <PriceTile
                  :label="t('modelMarket.columns.input')"
                  :value="formatSelectedPrice(model.pricing?.input_price, perMillionScale, model.pricing?.billing_mode)"
                  :official-value="formatOfficialPrice(model.pricing?.input_price, perMillionScale)"
                  :discount-value="formatSelectedDiscount(model.pricing?.input_price, perMillionScale, model.pricing?.billing_mode)"
                />
                <PriceTile
                  :label="t('modelMarket.columns.cacheRead')"
                  :value="formatSelectedPrice(model.pricing?.cache_read_price, perMillionScale, model.pricing?.billing_mode)"
                  :official-value="formatOfficialPrice(model.pricing?.cache_read_price, perMillionScale)"
                  :discount-value="formatSelectedDiscount(model.pricing?.cache_read_price, perMillionScale, model.pricing?.billing_mode)"
                />
                <PriceTile
                  :label="t('modelMarket.columns.output')"
                  :value="formatSelectedPrice(model.pricing?.output_price, perMillionScale, model.pricing?.billing_mode)"
                  :official-value="formatOfficialPrice(model.pricing?.output_price, perMillionScale)"
                  :discount-value="formatSelectedDiscount(model.pricing?.output_price, perMillionScale, model.pricing?.billing_mode)"
                />
              </template>
            </div>

            <div class="mt-4 flex items-center justify-between border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <span>{{ t('modelMarket.effectiveRate') }}</span>
              <span>{{ selectedMultiplierLabel(model.pricing?.billing_mode) }}</span>
            </div>
          </article>
        </section>
      </template>
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
import userChannelsAPI, {
  type UserAvailableChannel,
  type UserAvailableGroup,
  type UserSupportedModelPricing,
} from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import { formatScaled, formatUSDScaled } from '@/utils/pricing'
import { formatMultiplier } from '@/utils/formatters'
import { filterGroupsByModelKind, resolveModelKind, type ModelKind } from '@/utils/modelKind'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
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
                h('span', { class: 'min-w-0 truncate text-right font-mono text-sm font-medium text-gray-600 dark:text-gray-300', title: props.officialValue }, props.officialValue),
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

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const availableGroups = ref<UserAvailableGroup[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const selectedGroupId = ref<number | null>(null)
const perMillionScale = 1_000_000
const usdToSpiritStoneRate = 7.1

const toAvailableGroup = (group: Group): UserAvailableGroup => ({
  id: group.id,
  name: group.name,
  platform: group.platform,
  subscription_type: group.subscription_type,
  rate_multiplier: group.rate_multiplier,
  is_exclusive: group.is_exclusive,
  allow_image_generation: group.allow_image_generation,
  image_super_resolution_enabled: group.image_super_resolution_enabled,
  image_rate_independent: group.image_rate_independent,
  cache_hit_quarter_to_input_enabled: group.cache_hit_quarter_to_input_enabled ?? false,
  image_rate_multiplier: group.image_rate_multiplier,
  image_price_1k: group.image_price_1k,
  image_price_2k: group.image_price_2k,
  image_price_4k: group.image_price_4k,
})

const pricingSignature = (pricing: UserSupportedModelPricing | null): string => {
  if (!pricing) return 'no-pricing'
  return JSON.stringify({
    billing_mode: pricing.billing_mode,
    input_price: pricing.input_price,
    output_price: pricing.output_price,
    cache_write_price: pricing.cache_write_price,
    cache_read_price: pricing.cache_read_price,
    image_output_price: pricing.image_output_price,
    per_request_price: pricing.per_request_price,
    intervals: pricing.intervals,
  })
}

function compareMarketModels(a: GroupMarketModel, b: GroupMarketModel): number {
  const kindOrder = Number(a.kind === 'image') - Number(b.kind === 'image')
  if (kindOrder !== 0) return kindOrder
  return a.platform.localeCompare(b.platform) || a.name.localeCompare(b.name)
}

function compareMarketGroups(a: MarketGroup, b: MarketGroup): number {
  const kindOrder = Number(a.group.allow_image_generation) - Number(b.group.allow_image_generation)
  if (kindOrder !== 0) return kindOrder
  return a.group.platform.localeCompare(b.group.platform) || a.group.name.localeCompare(b.group.name)
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
        for (const group of filterGroupsByModelKind(section.groups, kind)) {
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

const visibleGroups = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return marketGroups.value
  return marketGroups.value.filter((item) =>
    item.group.name.toLowerCase().includes(q) ||
    item.group.platform.toLowerCase().includes(q) ||
    item.models.some((model) => model.name.toLowerCase().includes(q) || model.platform.toLowerCase().includes(q)),
  )
})

const selectedGroup = computed(() => {
  if (!selectedGroupId.value) return visibleGroups.value[0] ?? null
  return visibleGroups.value.find((item) => item.group.id === selectedGroupId.value) ?? visibleGroups.value[0] ?? null
})

const selectedModels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  const models = selectedGroup.value?.models ?? []
  if (!q) return models
  return models.filter((model) =>
    model.name.toLowerCase().includes(q) ||
    model.platform.toLowerCase().includes(q) ||
    selectedGroup.value?.group.name.toLowerCase().includes(q),
  )
})

const selectedTextRateLabel = computed(() => {
  const group = selectedGroup.value?.group
  return group ? `x${formatRate(effectiveTextRate(group))}` : '-'
})

watch(visibleGroups, (groups) => {
  if (groups.length === 0) {
    selectedGroupId.value = null
    return
  }
  if (!selectedGroupId.value || !groups.some((item) => item.group.id === selectedGroupId.value)) {
    selectedGroupId.value = groups[0].group.id
  }
}, { immediate: true })

function effectiveTextRate(group: UserAvailableGroup): number {
  return userGroupRates.value[group.id] ?? group.rate_multiplier ?? 1
}

function effectiveImageRate(group: UserAvailableGroup): number {
  return group.image_rate_independent ? group.image_rate_multiplier : effectiveTextRate(group)
}

function effectiveMultiplier(group: UserAvailableGroup, mode?: BillingMode): number {
  return mode === BILLING_MODE_IMAGE ? effectiveImageRate(group) : effectiveTextRate(group)
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

function formatOfficialPrice(value: number | null | undefined, scale: number): string {
  return formatUSDScaled(value ?? null, scale)
}

function formatDiscountPercent(value: number | null | undefined, scale: number, group: UserAvailableGroup, mode?: BillingMode): string {
  if (value == null) return ''
  const official = value * scale
  if (!Number.isFinite(official) || official <= 0) return ''
  const current = value * effectiveMultiplier(group, mode) * scale
  const officialConverted = official * usdToSpiritStoneRate
  if (!Number.isFinite(current) || officialConverted <= 0 || current >= officialConverted) return ''
  const discount = (current / officialConverted - 1) * 100
  return `${discount.toFixed(1)}%`
}

function formatImageTier(group: UserAvailableGroup, tier: '1k' | '2k' | '4k'): string {
  const value = tier === '1k' ? group.image_price_1k : tier === '2k' ? group.image_price_2k : group.image_price_4k
  if (typeof value !== 'number') return '-'
  return formatScaled(value * effectiveImageRate(group), 1)
}

function formatOfficialImageTier(group: UserAvailableGroup, tier: '1k' | '2k' | '4k'): string {
  const value = tier === '1k' ? group.image_price_1k : tier === '2k' ? group.image_price_2k : group.image_price_4k
  return formatUSDScaled(typeof value === 'number' ? value : null, 1)
}

function formatImageTierDiscount(group: UserAvailableGroup, tier: '1k' | '2k' | '4k'): string {
  const value = tier === '1k' ? group.image_price_1k : tier === '2k' ? group.image_price_2k : group.image_price_4k
  return formatDiscountPercent(typeof value === 'number' ? value : null, 1, group, BILLING_MODE_IMAGE)
}

function formatSelectedPrice(value: number | null | undefined, scale: number, mode?: BillingMode): string {
  const group = selectedGroup.value?.group
  if (!group) return '-'
  return formatPrice(value, scale, group, mode)
}

function formatSelectedDiscount(value: number | null | undefined, scale: number, mode?: BillingMode): string {
  const group = selectedGroup.value?.group
  if (!group) return ''
  return formatDiscountPercent(value, scale, group, mode)
}

function formatSelectedImageTier(tier: '1k' | '2k' | '4k'): string {
  const group = selectedGroup.value?.group
  if (!group) return '-'
  return formatImageTier(group, tier)
}

function formatSelectedOfficialImageTier(tier: '1k' | '2k' | '4k'): string {
  const group = selectedGroup.value?.group
  if (!group) return '-'
  return formatOfficialImageTier(group, tier)
}

function formatSelectedImageTierDiscount(tier: '1k' | '2k' | '4k'): string {
  const group = selectedGroup.value?.group
  if (!group) return ''
  return formatImageTierDiscount(group, tier)
}

function selectedMultiplierLabel(mode?: BillingMode): string {
  const group = selectedGroup.value?.group
  if (!group) return '-'
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

function goTestSelected(model: GroupMarketModel) {
  const group = selectedGroup.value?.group
  if (!group) return
  goTest(model, group)
}

async function loadModels() {
  loading.value = true
  try {
    const [list, groups, rates] = await Promise.all([
      userChannelsAPI.getAvailable(),
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

onMounted(loadModels)
</script>
