<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-4">
        <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-center">
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-80">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('modelMarket.searchPlaceholder')"
                class="input pl-10"
              />
            </div>
            <select v-model="platformFilter" class="input w-full sm:w-48">
              <option value="">{{ t('modelMarket.allPlatforms') }}</option>
              <option v-for="platform in platforms" :key="platform" :value="platform">
                {{ platformLabel(platform) }}
              </option>
            </select>
          </div>

          <button
            @click="loadModels"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </section>

      <section class="grid gap-4 md:grid-cols-3">
        <div class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('modelMarket.stats.models') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ marketModels.length }}</p>
        </div>
        <div class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('modelMarket.stats.platforms') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ platforms.length }}</p>
        </div>
        <div class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('modelMarket.stats.groups') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ groupCount }}</p>
        </div>
      </section>

      <section v-if="loading" class="card py-16 text-center">
        <Icon name="refresh" size="lg" class="mx-auto animate-spin text-gray-400" />
      </section>

      <section v-else-if="filteredModels.length === 0" class="card py-16 text-center">
        <Icon name="inbox" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelMarket.empty') }}</p>
      </section>

      <section v-else class="space-y-4">
        <article
          v-for="model in filteredModels"
          :key="model.key"
          class="card overflow-hidden"
        >
          <div class="flex flex-col gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700 md:flex-row md:items-center md:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h3 class="break-all text-base font-semibold text-gray-900 dark:text-white">{{ model.name }}</h3>
                <span
                  :class="[
                    'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium uppercase',
                    platformBadgeClass(model.platform),
                  ]"
                >
                  <PlatformIcon :platform="model.platform as GroupPlatform" size="xs" />
                  {{ platformLabel(model.platform) }}
                </span>
                <span class="rounded-md bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                  {{ billingModeLabel(model.pricing?.billing_mode) }}
                </span>
              </div>
              <div class="mt-2 flex flex-wrap gap-1.5">
                <GroupBadge
                  v-for="entry in model.entries"
                  :key="entry.key"
                  :name="entry.group.name"
                  :platform="entry.group.platform as GroupPlatform"
                  :subscription-type="(entry.group.subscription_type || 'standard') as SubscriptionType"
                  :rate-multiplier="entry.group.rate_multiplier"
                  :user-rate-multiplier="userGroupRates[entry.group.id] ?? null"
                  always-show-rate
                />
              </div>
            </div>
            <span
              v-if="model.pricing?.intervals?.length"
              class="inline-flex w-fit items-center gap-1 rounded-full bg-amber-100 px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
            >
              <Icon name="filter" size="xs" />
              {{ t('modelMarket.intervalCount', { count: model.pricing.intervals.length }) }}
            </span>
          </div>

          <div class="overflow-x-auto">
            <table class="w-full min-w-[1040px] text-sm">
              <thead>
                <tr class="border-b border-gray-100 bg-gray-50/70 text-xs font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/60 dark:text-gray-400">
                  <th class="px-4 py-3 text-left">{{ t('modelMarket.columns.group') }}</th>
                  <th class="px-4 py-3 text-left">{{ t('modelMarket.columns.multiplier') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('modelMarket.columns.input') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('modelMarket.columns.output') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('modelMarket.columns.cacheWrite') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('modelMarket.columns.cacheRead') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('modelMarket.columns.imageOutput') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('modelMarket.columns.perRequest') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('modelMarket.columns.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="entry in model.entries"
                  :key="entry.key"
                  class="border-b border-gray-100 last:border-b-0 dark:border-dark-700"
                >
                  <td class="px-4 py-3">
                    <div class="flex items-center gap-2">
                      <span class="font-medium text-gray-900 dark:text-white">{{ entry.group.name }}</span>
                      <span v-if="entry.group.is_exclusive" class="rounded bg-purple-100 px-1.5 py-0.5 text-[10px] font-semibold text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
                        {{ t('availableChannels.exclusive') }}
                      </span>
                    </div>
                  </td>
                  <td class="px-4 py-3 font-mono text-xs text-gray-600 dark:text-gray-300">
                    {{ formatRate(effectiveMultiplier(entry.group, model.pricing?.billing_mode)) }}x
                  </td>
                  <td class="px-4 py-3 text-right font-mono text-gray-900 dark:text-white">{{ formatPrice(model.pricing?.input_price, perMillionScale, entry.group, model.pricing?.billing_mode) }}</td>
                  <td class="px-4 py-3 text-right font-mono text-gray-900 dark:text-white">{{ formatPrice(model.pricing?.output_price, perMillionScale, entry.group, model.pricing?.billing_mode) }}</td>
                  <td class="px-4 py-3 text-right font-mono text-gray-900 dark:text-white">{{ formatPrice(model.pricing?.cache_write_price, perMillionScale, entry.group, model.pricing?.billing_mode) }}</td>
                  <td class="px-4 py-3 text-right font-mono text-gray-900 dark:text-white">{{ formatPrice(model.pricing?.cache_read_price, perMillionScale, entry.group, model.pricing?.billing_mode) }}</td>
                  <td class="px-4 py-3 text-right font-mono text-gray-900 dark:text-white">{{ formatImagePrice(model, entry.group) }}</td>
                  <td class="px-4 py-3 text-right font-mono text-gray-900 dark:text-white">{{ formatPrice(model.pricing?.per_request_price, 1, entry.group, model.pricing?.billing_mode) }}</td>
                  <td class="px-4 py-3 text-right">
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm whitespace-nowrap"
                      @click="goTest(model, entry.group)"
                    >
                      <Icon name="beaker" size="sm" />
                      {{ t('modelMarket.test') }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import userChannelsAPI, {
  type UserAvailableChannel,
  type UserAvailableGroup,
  type UserSupportedModelPricing,
} from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import { formatScaled } from '@/utils/pricing'
import { formatMultiplier } from '@/utils/formatters'
import { filterGroupsByModelKind, resolveModelKind, type ModelKind } from '@/utils/modelKind'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
  type BillingMode,
} from '@/constants/channel'
import type { GroupPlatform, SubscriptionType } from '@/types'

interface MarketEntry {
  key: string
  group: UserAvailableGroup
}

interface MarketModel {
  key: string
  name: string
  platform: string
  kind: ModelKind
  pricing: UserSupportedModelPricing | null
  entries: MarketEntry[]
}

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const platformFilter = ref('')
const perMillionScale = 1_000_000

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

const marketModels = computed<MarketModel[]>(() => {
  const map = new Map<string, MarketModel>()
  for (const channel of channels.value) {
    for (const section of channel.platforms || []) {
      const platform = section.platform
      for (const model of section.supported_models || []) {
        const key = `${platform}:${model.name}:${pricingSignature(model.pricing)}`
        const kind = resolveModelKind(model)
        let item = map.get(key)
        if (!item) {
          item = {
            key,
            name: model.name,
            platform: model.platform || platform,
            kind,
            pricing: model.pricing,
            entries: [],
          }
          map.set(key, item)
        }
        const existing = new Set(item.entries.map((entry) => entry.group.id))
        for (const group of filterGroupsByModelKind(section.groups, item.kind)) {
          if (!existing.has(group.id)) {
            item.entries.push({ key: `${key}:${group.id}`, group })
            existing.add(group.id)
          }
        }
      }
    }
  }
  return Array.from(map.values())
    .filter((item) => item.entries.length > 0)
    .sort((a, b) => a.platform.localeCompare(b.platform) || a.name.localeCompare(b.name))
})

const platforms = computed(() =>
  Array.from(new Set(marketModels.value.map((item) => item.platform))).sort()
)

const groupCount = computed(() => {
  const ids = new Set<number>()
  for (const model of marketModels.value) {
    for (const entry of model.entries) {
      ids.add(entry.group.id)
    }
  }
  return ids.size
})

const filteredModels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return marketModels.value.filter((model) => {
    if (platformFilter.value && model.platform !== platformFilter.value) return false
    if (!q) return true
    return (
      model.name.toLowerCase().includes(q) ||
      model.platform.toLowerCase().includes(q) ||
      model.entries.some((entry) => entry.group.name.toLowerCase().includes(q))
    )
  })
})

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

function formatImagePrice(model: MarketModel, group: UserAvailableGroup): string {
  if (model.kind === 'image') {
    return [
      ['1K', group.image_price_1k],
      ['2K', group.image_price_2k],
      ['4K', group.image_price_4k],
    ]
      .map(([tier, value]) => `${tier} ${typeof value === 'number' ? formatScaled(value * effectiveImageRate(group), 1) : '-'}`)
      .join(' / ')
  }
  const scale = model.pricing?.billing_mode === BILLING_MODE_TOKEN ? perMillionScale : 1
  return formatPrice(model.pricing?.image_output_price, scale, group, model.pricing?.billing_mode)
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

function goTest(model: MarketModel, group: UserAvailableGroup) {
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
    const [list, rates] = await Promise.all([
      userChannelsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
    ])
    channels.value = list
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(loadModels)
</script>
