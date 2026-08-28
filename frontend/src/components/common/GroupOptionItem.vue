<template>
  <div class="flex min-w-0 flex-1 items-start justify-between gap-3">
    <!-- Left: name + description -->
    <div
      class="flex min-w-0 flex-1 flex-col items-start"
      :title="description || undefined"
    >
      <!-- Row 1: platform badge (name bold) -->
      <GroupBadge
        :name="name"
        :platform="platform"
        :subscription-type="subscriptionType"
        :show-rate="false"
        class="groupOptionItemBadge"
      />
      <!-- Row 2: description with top spacing -->
      <span
        v-if="description"
        class="mt-1.5 w-full whitespace-pre-line [overflow-wrap:anywhere] text-left text-xs leading-relaxed text-gray-500 dark:text-gray-400 line-clamp-3"
      >
        {{ description }}
      </span>
    </div>

    <!-- Right: rate pill + checkmark (vertically centered to first row) -->
    <div class="flex shrink-0 items-center gap-2 pt-0.5">
      <div class="flex shrink-0 flex-col items-end gap-1">
        <span
          v-if="allowImageGeneration"
          class="inline-flex items-center gap-1 whitespace-nowrap rounded-full bg-rose-50 px-2.5 py-1 text-xs font-semibold text-rose-700 ring-1 ring-rose-200 dark:bg-rose-950/30 dark:text-rose-300 dark:ring-rose-900/60"
        >
          <Icon name="sparkles" size="xs" class="h-3 w-3" />
          {{ t('availableChannels.imageEnabled') }}
        </span>
        <span
          v-if="accessCountdown"
          class="inline-flex items-center gap-1 whitespace-nowrap rounded-full bg-amber-50 px-2.5 py-1 text-xs font-semibold text-amber-700 ring-1 ring-amber-200 dark:bg-amber-950/30 dark:text-amber-300 dark:ring-amber-900/60"
        >
          <Icon name="clock" size="xs" class="h-3 w-3" />
          {{ accessCountdown }}
        </span>
        <!-- Rate pill (platform color) -->
        <span v-if="rateMultiplier !== undefined" :class="['inline-flex items-center whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold', ratePillClass]">
          <template v-if="hasPromoRate">
            <!-- 限时活动：原倍率删除线 + 折后限时倍率 -->
            <span class="mr-1 line-through opacity-50">{{ promoBaseRate }}x</span>
            <span class="font-bold">{{ promoEffectiveRate }}x</span>
          </template>
          <template v-else-if="hasCustomRate">
            <span class="mr-1 line-through opacity-50">{{ rateMultiplier }}x</span>
            <span class="font-bold">{{ userRateMultiplier }}x</span>
          </template>
          <template v-else>
            {{ rateMultiplier }}x {{ t('admin.groups.rateLabel') }}
          </template>
        </span>
        <span
          v-if="hasPromoRate"
          class="inline-flex items-center whitespace-nowrap rounded-full bg-rose-50 px-3 py-1 text-xs font-semibold text-rose-700 dark:bg-rose-900/20 dark:text-rose-300"
          :title="promoRateTitle"
        >
          {{ promoChipText }}
        </span>
        <span
          v-if="hasPeakRate"
          class="inline-flex items-center whitespace-nowrap rounded-full bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
          :title="peakRateTitle"
        >
          {{ peakRateText }}
        </span>
      </div>
      <!-- Checkmark -->
      <svg
        v-if="showCheckmark && selected"
        class="h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
        stroke-width="2"
      >
        <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
      </svg>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from './GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import type { SubscriptionType, GroupPlatform } from '@/types'
import { useAppStore } from '@/stores/app'
import {
  formatPeakRateWindow,
  formatPromoDiscountWindow,
  promoDiscountZhe,
  serverTimezoneLabel
} from '@/utils/peak-rate'

const { t } = useI18n()

interface Props {
  name: string
  platform: GroupPlatform
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  userRateMultiplier?: number | null
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  // 限时活动折扣（服务端已判定 promoActive，前端不再换算时区）
  promoDiscountEnabled?: boolean
  promoDiscountStart?: string
  promoDiscountEnd?: string
  promoDiscountRate?: number
  promoActive?: boolean
  description?: string | null
  selected?: boolean
  showCheckmark?: boolean
  allowImageGeneration?: boolean
  accessCountdown?: string | null
}

const props = withDefaults(defineProps<Props>(), {
  subscriptionType: 'standard',
  selected: false,
  showCheckmark: true,
  userRateMultiplier: null,
  allowImageGeneration: false,
  accessCountdown: null,
  peakRateEnabled: false,
  promoDiscountEnabled: false,
  promoActive: false
})

// Whether user has a custom rate different from default
const hasCustomRate = computed(() => {
  return (
    props.userRateMultiplier !== null &&
    props.userRateMultiplier !== undefined &&
    props.rateMultiplier !== undefined &&
    props.userRateMultiplier !== props.rateMultiplier
  )
})

const appStore = useAppStore()

const hasPeakRate = computed(() => {
  return Boolean(props.peakRateEnabled && props.peakStart && props.peakEnd)
})

const peakRateText = computed(() => {
  return formatPeakRateWindow(
    {
      peak_rate_enabled: props.peakRateEnabled,
      peak_start: props.peakStart,
      peak_end: props.peakEnd,
      peak_rate_multiplier: props.peakRateMultiplier
    },
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )
})

const peakRateTitle = computed(() => {
  return t('common.peakRateTooltip', { window: peakRateText.value })
})

// ===== 限时活动折扣 =====

/** 倍率格式化：最多 2 位小数并去掉尾零 */
function formatRate(value: number): string {
  return String(parseFloat(value.toFixed(2)))
}

const promoZhe = computed(() => promoDiscountZhe(props.promoDiscountRate))

const hasPromoRate = computed(() => {
  return Boolean(
    props.promoActive &&
      props.promoDiscountEnabled &&
      promoZhe.value !== null &&
      promoZhe.value < 100 &&
      props.rateMultiplier !== undefined
  )
})

/** 折前生效倍率：有用户专属倍率按专属倍率打折，否则按分组默认倍率 */
const promoBaseRate = computed(() => {
  return hasCustomRate.value && props.userRateMultiplier != null
    ? props.userRateMultiplier
    : (props.rateMultiplier ?? 0)
})

const promoEffectiveRate = computed(() => {
  return formatRate(promoBaseRate.value * (props.promoDiscountRate ?? 1))
})

const promoChipText = computed(() => {
  const zhe = promoZhe.value
  if (zhe === null) return t('common.promoRateChip')
  return t('common.promoDiscountLabel', { zhe, off: 100 - zhe })
})

const promoRateTitle = computed(() => {
  return t('common.promoRateTooltip', {
    discount: t('common.promoDiscountLabel', {
      zhe: promoZhe.value ?? 100,
      off: 100 - (promoZhe.value ?? 0)
    }),
    window: formatPromoDiscountWindow({
      promo_discount_enabled: props.promoDiscountEnabled,
      promo_discount_start: props.promoDiscountStart,
      promo_discount_end: props.promoDiscountEnd
    }),
    tz: serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  })
})

// Rate pill color matches platform badge color
const ratePillClass = computed(() => {
  switch (props.platform) {
    case 'anthropic':
      return 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'
    case 'openai':
      return 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400'
    case 'gemini':
      return 'bg-sky-50 text-sky-700 dark:bg-sky-900/20 dark:text-sky-400'
    default: // antigravity and others
      return 'bg-violet-50 text-violet-700 dark:bg-violet-900/20 dark:text-violet-400'
  }
})
</script>

<style scoped>
/* Bold the group name inside GroupBadge when used in dropdown option */
.groupOptionItemBadge :deep(span.truncate) {
  font-weight: 600;
}
</style>
