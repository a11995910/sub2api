<template>
  <div v-if="hasUsage" class="space-y-1.5">
    <UsageProgressBar
      v-if="usage.five_hour"
      label="5h"
      :utilization="usage.five_hour.utilization"
      :resets-at="usage.five_hour.resets_at"
      :window-stats="showWindowStats ? usage.five_hour.window_stats : null"
      :show-now-when-idle="showNowWhenIdle"
      color="indigo"
    />
    <UsageProgressBar
      v-if="usage.seven_day"
      label="7d"
      :utilization="usage.seven_day.utilization"
      :resets-at="usage.seven_day.resets_at"
      :window-stats="showWindowStats ? usage.seven_day.window_stats : null"
      :show-now-when-idle="showNowWhenIdle"
      color="emerald"
    />
    <UsageProgressBar
      v-if="showExtendedWindows && usage.seven_day_sonnet"
      label="7d S"
      :utilization="usage.seven_day_sonnet.utilization"
      :resets-at="usage.seven_day_sonnet.resets_at"
      color="purple"
    />
    <UsageProgressBar
      v-if="showExtendedWindows && usage.seven_day_fable"
      label="7d F"
      :utilization="usage.seven_day_fable.utilization"
      :resets-at="usage.seven_day_fable.resets_at"
      color="amber"
    />
  </div>
  <span v-else class="text-xs text-gray-400 dark:text-gray-500">{{ emptyText }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { UsageProgress } from '@/types'
import UsageProgressBar from './UsageProgressBar.vue'

type OAuthUsageWindow = Pick<UsageProgress, 'utilization' | 'resets_at'> &
  Partial<Pick<UsageProgress, 'window_stats'>>

export interface OAuthUsageWindowsValue {
  five_hour?: OAuthUsageWindow | null
  seven_day?: OAuthUsageWindow | null
  seven_day_sonnet?: OAuthUsageWindow | null
  seven_day_fable?: OAuthUsageWindow | null
}

const props = withDefaults(defineProps<{
  usage: OAuthUsageWindowsValue
  showWindowStats?: boolean
  showExtendedWindows?: boolean
  showNowWhenIdle?: boolean
  emptyText?: string
}>(), {
  showWindowStats: false,
  showExtendedWindows: false,
  showNowWhenIdle: false,
  emptyText: '-'
})

const hasUsage = computed(() => Boolean(
  props.usage.five_hour ||
  props.usage.seven_day ||
  (props.showExtendedWindows && (props.usage.seven_day_sonnet || props.usage.seven_day_fable))
))
</script>
