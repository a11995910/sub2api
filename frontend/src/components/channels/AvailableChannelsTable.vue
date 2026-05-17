<template>
  <div class="card overflow-hidden">
    <table class="w-full table-fixed border-collapse text-sm">
      <thead>
        <tr class="border-b border-gray-100 bg-gray-50/50 text-xs font-medium uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:bg-dark-800/50 dark:text-gray-400">
          <th class="w-[180px] px-4 py-3 text-center">{{ columns.name }}</th>
          <th class="w-[200px] px-4 py-3 text-left">{{ columns.description }}</th>
          <th class="w-[140px] px-4 py-3 text-left">{{ columns.platform }}</th>
          <th class="px-4 py-3 text-left">{{ columns.groups }}</th>
          <th class="px-4 py-3 text-left">{{ columns.supportedModels }}</th>
        </tr>
      </thead>
      <tbody v-if="loading">
        <tr>
          <td colspan="5" class="py-10 text-center">
            <Icon name="refresh" size="lg" class="inline-block animate-spin text-gray-400" />
          </td>
        </tr>
      </tbody>
      <tbody v-else-if="rows.length === 0">
        <tr>
          <td colspan="5" class="py-12 text-center">
            <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ emptyLabel }}</p>
          </td>
        </tr>
      </tbody>
      <!-- 每个渠道一个 tbody：首行 td rowspan 渠道名，后续行只渲染其余三列。
           tbody 之间强分隔线表达"渠道边界"，tbody 内部用淡分隔线区分平台。 -->
      <tbody
        v-else
        v-for="(channel, chIdx) in rows"
        :key="`${channel.name}-${chIdx}`"
        class="border-b-2 border-gray-200 last:border-b-0 dark:border-dark-600"
      >
        <tr
          v-for="(section, secIdx) in channel.platforms"
          :key="`${channel.name}-${section.platform}`"
          class="transition-colors hover:bg-gray-50/40 dark:hover:bg-dark-800/40"
          :class="{ 'border-t border-gray-100/70 dark:border-dark-700/50': secIdx > 0 }"
        >
          <!-- 渠道名：只在第一行渲染并用 rowspan 纵向合并 -->
          <td
            v-if="secIdx === 0"
            :rowspan="channel.platforms.length"
            class="px-4 py-3 text-center align-middle font-medium text-gray-900 dark:text-white"
          >
            {{ channel.name }}
          </td>

          <!-- 描述：独立一列，同样用 rowspan 纵向合并 -->
          <td
            v-if="secIdx === 0"
            :rowspan="channel.platforms.length"
            class="px-4 py-3 align-middle text-xs text-gray-500 dark:text-gray-400"
          >
            <template v-if="channel.description">{{ channel.description }}</template>
            <span v-else class="text-gray-400">-</span>
          </td>

          <!-- 平台徽章 -->
          <td class="align-top px-4 py-3">
            <span
              :class="[
                'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium uppercase',
                platformBadgeClass(section.platform),
              ]"
            >
              <PlatformIcon :platform="section.platform as GroupPlatform" size="xs" />
              {{ section.platform }}
            </span>
          </td>

          <!-- 分组：专属分组在前（紫色 shield 行），公开分组在后（灰色 globe 行）。 -->
          <td class="align-top px-4 py-3">
            <div class="flex flex-col gap-2">
              <div
                v-if="exclusiveGroups(section).length > 0"
                class="rounded-lg border border-purple-100 bg-purple-50/60 p-2 dark:border-purple-900/40 dark:bg-purple-950/20"
              >
                <div class="mb-1.5 flex items-center gap-1 text-[11px] font-semibold text-purple-700 dark:text-purple-300">
                  <Icon name="shield" size="xs" class="h-3 w-3" />
                  <span :title="t('availableChannels.exclusiveTooltip')">{{ t('availableChannels.exclusive') }}</span>
                </div>
                <div class="flex flex-wrap items-center gap-1.5">
                  <span
                    v-for="g in exclusiveGroups(section)"
                    :key="`ex-${g.id}`"
                    class="inline-flex items-center gap-1"
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
                  </span>
                </div>
              </div>
              <div
                v-if="publicGroups(section).length > 0"
                class="rounded-lg border border-gray-200 bg-gray-50/70 p-2 dark:border-dark-600 dark:bg-dark-800/50"
              >
                <div class="mb-1.5 flex items-center gap-1 text-[11px] font-semibold text-gray-600 dark:text-gray-300">
                  <Icon name="globe" size="xs" class="h-3 w-3" />
                  <span :title="t('availableChannels.publicTooltip')">{{ t('availableChannels.public') }}</span>
                </div>
                <div class="flex flex-wrap items-center gap-1.5">
                  <span
                    v-for="g in publicGroups(section)"
                    :key="`pub-${g.id}`"
                    class="inline-flex items-center gap-1"
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
                  </span>
                </div>
              </div>
              <span v-if="section.groups.length === 0" class="text-xs text-gray-400">-</span>
            </div>
          </td>

          <!-- 支持模型 -->
          <td class="align-top px-4 py-3">
            <div class="flex flex-wrap gap-1">
              <SupportedModelChip
                v-for="m in section.supported_models"
                :key="`${section.platform}-${m.name}`"
                :model="m"
                :pricing-key-prefix="pricingKeyPrefix"
                :no-pricing-label="noPricingLabel"
                :show-platform="false"
                :platform-hint="section.platform"
                :groups="groupsForModel(section, m)"
                :user-group-rates="userGroupRates"
              />
              <span v-if="section.supported_models.length === 0" class="text-xs text-gray-400">
                {{ noModelsLabel }}
              </span>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import SupportedModelChip from './SupportedModelChip.vue'
import type { UserAvailableChannel, UserAvailableGroup, UserChannelPlatformSection, UserSupportedModel } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBadgeClass } from '@/utils/platformColors'
import { filterGroupsByModelKind, resolveModelKind } from '@/utils/modelKind'

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
</script>
