<template>
  <CapacityBadge
    :color-class="colorClass"
    :current="current"
    :max="max"
    :tooltip="tooltip"
  >
    <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
    </svg>
  </CapacityBadge>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import CapacityBadge from '@/components/account/CapacityBadge.vue'

const props = withDefaults(defineProps<{
  current: number
  max: number
  tooltip?: string
}>(), {
  tooltip: '',
})

const colorClass = computed(() => {
  if (props.current >= props.max) {
    return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  }
  if (props.current > 0) {
    return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  }
  return 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
})
</script>
