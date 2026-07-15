<template>
  <div class="overflow-hidden rounded-lg border border-gray-800 bg-gray-950 shadow-sm dark:border-dark-700 dark:shadow-none">
    <div class="flex min-w-0 items-center justify-between gap-3 border-b border-white/10 px-3 py-2">
      <p class="min-w-0 truncate text-xs font-medium text-gray-300">{{ label }}</p>
      <button
        type="button"
        class="inline-flex shrink-0 items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-gray-300 hover:bg-white/10 hover:text-white"
        :title="copied ? '已复制' : '复制代码'"
        @click="copyCode"
      >
        <Icon :name="copied ? 'check' : 'copy'" size="sm" />
        <span>{{ copied ? '已复制' : '复制' }}</span>
      </button>
    </div>
    <pre class="max-h-[32rem] overflow-auto p-4 text-sm leading-6 text-gray-100"><code>{{ code }}</code></pre>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'

const props = withDefaults(defineProps<{
  code: string
  label?: string
}>(), {
  label: '示例'
})

const { copied, copyToClipboard } = useClipboard()

function copyCode() {
  copyToClipboard(props.code, '代码已复制')
}
</script>
