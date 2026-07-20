<template>
  <div class="table-wrapper h-full overflow-y-auto p-4">
    <div v-if="loading" class="flex min-h-[280px] items-center justify-center">
      <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
    </div>

    <div v-else-if="rows.length === 0" class="flex min-h-[280px] flex-col items-center justify-center text-center">
      <Icon name="inbox" size="xl" class="mb-3 h-12 w-12 text-gray-400" />
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ emptyLabel }}</p>
    </div>

    <div v-else class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
      <article
        v-for="card in cards"
        :key="`${card.channel.name}-${card.channelIndex}-${card.section.platform}`"
        class="flex min-h-[280px] flex-col overflow-hidden rounded-lg border bg-white shadow-sm transition-colors hover:border-gray-300 dark:bg-dark-800 dark:hover:border-dark-500"
        :class="[platformBorderClass(card.section.platform)]"
      >
        <div class="flex items-start justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                {{ card.channel.name }}
              </h3>
              <span class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700 dark:text-gray-400">
                {{ card.channel.platforms.length }}
              </span>
            </div>
            <p class="mt-1 line-clamp-2 min-h-[2rem] text-xs leading-4 text-gray-500 dark:text-gray-400">
              <template v-if="card.channel.description">{{ card.channel.description }}</template>
              <span v-else class="text-gray-400">-</span>
            </p>
          </div>

          <span
            :class="[
              'inline-flex flex-shrink-0 items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium uppercase',
              platformBadgeClass(card.section.platform),
            ]"
          >
            <PlatformIcon :platform="card.section.platform as GroupPlatform" size="xs" />
            {{ card.section.platform }}
          </span>
        </div>

        <div class="flex flex-1 flex-col gap-4 p-4">
          <section class="space-y-2">
            <div class="flex items-center justify-between gap-2">
              <h4 class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ columns.supportedModels }}
              </h4>
              <span class="text-[11px] text-gray-400 dark:text-gray-500">
                {{ card.section.supported_models.length }}
              </span>
            </div>
            <div class="flex max-h-32 flex-wrap content-start gap-1.5 overflow-y-auto pr-1">
              <SupportedModelChip
                v-for="m in card.section.supported_models"
                :key="`${card.section.platform}-${m.name}`"
                :model="m"
                :pricing-key-prefix="pricingKeyPrefix"
                :no-pricing-label="noPricingLabel"
                :show-platform="false"
                :platform-hint="card.section.platform"
                :groups="groupsForModel(card.section, m)"
                :user-group-rates="userGroupRates"
              />
              <span v-if="card.section.supported_models.length === 0" class="text-xs text-gray-400">
                {{ noModelsLabel }}
              </span>
            </div>
          </section>

          <section class="mt-auto space-y-2 border-t border-gray-100 pt-3 dark:border-dark-700">
            <h4 class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ columns.groups }}
            </h4>
            <div class="flex flex-col gap-2">
              <div
                v-if="exclusiveGroups(card.section).length > 0"
                class="flex flex-wrap items-center gap-1.5"
              >
                <span
                  class="inline-flex items-center gap-0.5 text-[10px] font-medium uppercase text-purple-600 dark:text-purple-400"
                  :title="t('availableChannels.exclusiveTooltip')"
                >
                  <Icon name="shield" size="xs" class="h-3 w-3" />
                  {{ t('availableChannels.exclusive') }}
                </span>
                <span
                  v-for="g in exclusiveGroups(card.section)"
                  :key="`ex-${card.channel.name}-${card.channelIndex}-${card.section.platform}-${g.id}`"
                  class="inline-flex flex-wrap items-center gap-1"
                >
                  <GroupBadge
                    :name="g.name"
                    :platform="g.platform as GroupPlatform"
                    :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="g.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[g.id] ?? null"
                    always-show-rate
                  />
                  <span
                    v-if="g.allow_image_generation"
                    class="inline-flex items-center gap-1 rounded-full bg-rose-100 px-2 py-0.5 text-[10px] font-semibold text-rose-700 ring-1 ring-rose-200 dark:bg-rose-950/40 dark:text-rose-300 dark:ring-rose-900/60"
                    :title="imageRateTitle(g)"
                  >
                    <Icon name="sparkles" size="xs" class="h-3 w-3" />
                    {{ t('availableChannels.imageEnabled') }}
                  </span>
                  <span
                    v-if="hasPeakRate(g)"
                    class="inline-flex items-center gap-1 rounded-md bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                    :title="peakRateTitle(g)"
                  >
                    <Icon name="clock" size="xs" class="h-3 w-3" />
                    {{ peakRateLabel(g) }}
                  </span>
                </span>
              </div>

              <div
                v-if="publicGroups(card.section).length > 0"
                class="flex flex-wrap items-center gap-1.5"
              >
                <span
                  class="inline-flex items-center gap-0.5 text-[10px] font-medium uppercase text-gray-500 dark:text-gray-400"
                  :title="t('availableChannels.publicTooltip')"
                >
                  <Icon name="globe" size="xs" class="h-3 w-3" />
                  {{ t('availableChannels.public') }}
                </span>
                <span
                  v-for="g in publicGroups(card.section)"
                  :key="`pub-${card.channel.name}-${card.channelIndex}-${card.section.platform}-${g.id}`"
                  class="inline-flex flex-wrap items-center gap-1"
                >
                  <GroupBadge
                    :name="g.name"
                    :platform="g.platform as GroupPlatform"
                    :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="g.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[g.id] ?? null"
                    always-show-rate
                  />
                  <span
                    v-if="g.allow_image_generation"
                    class="inline-flex items-center gap-1 rounded-full bg-rose-100 px-2 py-0.5 text-[10px] font-semibold text-rose-700 ring-1 ring-rose-200 dark:bg-rose-950/40 dark:text-rose-300 dark:ring-rose-900/60"
                    :title="imageRateTitle(g)"
                  >
                    <Icon name="sparkles" size="xs" class="h-3 w-3" />
                    {{ t('availableChannels.imageEnabled') }}
                  </span>
                  <span
                    v-if="hasPeakRate(g)"
                    class="inline-flex items-center gap-1 rounded-md bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                    :title="peakRateTitle(g)"
                  >
                    <Icon name="clock" size="xs" class="h-3 w-3" />
                    {{ peakRateLabel(g) }}
                  </span>
                </span>
              </div>

              <span v-if="card.section.groups.length === 0" class="text-xs text-gray-400">-</span>
            </div>
          </section>
        </div>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import SupportedModelChip from './SupportedModelChip.vue'
import type { UserAvailableChannel, UserAvailableGroup, UserChannelPlatformSection, UserSupportedModel } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBadgeClass, platformBorderClass } from '@/utils/platformColors'
import { filterGroupsByModelKind, resolveModelKind } from '@/utils/modelKind'
import { useAppStore } from '@/stores/app'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'

const props = defineProps<{
  columns: {
    name: string
    description: string
    platform: string
    groups: string
    supportedModels: string
  }
  rows: UserAvailableChannel[]
  loading: boolean
  pricingKeyPrefix: string
  noPricingLabel: string
  noModelsLabel: string
  emptyLabel: string
  /** 用户专属倍率（group_id → multiplier）；无专属时由 GroupBadge 仅显示默认倍率。 */
  userGroupRates: Record<number, number>
}>()

// Suppress unused warning — props is accessed via template automatically but
// the explicit reference here keeps the linter from flagging userGroupRates.
void props.userGroupRates

const { t } = useI18n()

const cards = computed(() =>
  props.rows.flatMap((channel, channelIndex) =>
    channel.platforms.map((section) => ({
      channel,
      channelIndex,
      section
    }))
  )
)

function exclusiveGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => g.is_exclusive)
}

function publicGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => !g.is_exclusive)
}

function groupsForModel(section: UserChannelPlatformSection, model: UserSupportedModel): UserAvailableGroup[] {
  return filterGroupsByModelKind(section.groups, resolveModelKind(model))
}

function effectiveTextRate(group: UserAvailableGroup): number {
  return props.userGroupRates[group.id] ?? group.rate_multiplier ?? 1
}

function effectiveImageRate(group: UserAvailableGroup): number {
  return group.image_rate_independent ? group.image_rate_multiplier : effectiveTextRate(group)
}

function imageRateTitle(group: UserAvailableGroup): string {
  return t('availableChannels.imageRateTooltip', { rate: effectiveImageRate(group) })
}

const appStore = useAppStore()

function hasPeakRate(group: UserAvailableGroup): boolean {
  return groupHasPeakRate(group)
}

function peakRateLabel(group: UserAvailableGroup): string {
  return formatPeakRateWindow(group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
}

function peakRateTitle(group: UserAvailableGroup): string {
  return t('common.peakRateTooltip', { window: peakRateLabel(group) }) + t('common.peakRateImageNote')
}
</script>
