<template>
  <div class="group/token-price relative">
    <button
      type="button"
      class="w-full rounded-lg bg-gray-50 p-4 text-left ring-1 ring-gray-950/5 hover:bg-gray-100/70 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500 dark:bg-dark-900/60 dark:ring-white/10 dark:hover:bg-dark-900"
      :aria-describedby="tooltipId"
      data-testid="token-input-price"
    >
      <div class="flex items-start justify-between gap-4">
        <div class="flex min-w-0 flex-col gap-1">
          <p class="text-sm font-medium text-gray-600 dark:text-gray-300">
            {{ t('modelMarket.inputPrice') }}
          </p>
          <p class="truncate font-mono text-xl font-semibold tabular-nums text-gray-950 dark:text-white" :title="inputValue">
            {{ inputValue }}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-1.5 text-gray-500 dark:text-gray-400">
          <Icon name="infoCircle" size="xs" class="shrink-0" />
          <p class="text-sm" data-testid="token-price-unit">
            {{ t('modelMarket.perMillionTokens') }}
          </p>
        </div>
      </div>
    </button>

    <div
      :id="tooltipId"
      role="tooltip"
      class="invisible absolute left-0 top-full z-30 mt-2 w-full rounded-lg bg-gray-950 p-4 text-white opacity-0 shadow-lg ring-1 ring-black/10 group-hover/token-price:visible group-hover/token-price:opacity-100 group-focus-within/token-price:visible group-focus-within/token-price:opacity-100 dark:bg-dark-700 dark:shadow-none dark:ring-white/10"
    >
      <div class="flex flex-col gap-1">
        <p class="text-sm font-semibold">{{ t('modelMarket.priceDetails') }}</p>
        <p class="text-sm text-gray-300">
          {{ t('modelMarket.columns.input') }}：{{ t('modelMarket.inputMeaning') }}
        </p>
      </div>

      <dl class="mt-3 divide-y divide-white/10">
        <div class="flex items-start justify-between gap-4 pb-3">
          <dt class="min-w-0">
            <p class="text-sm font-medium text-white">{{ t('modelMarket.columns.output') }}</p>
            <p class="text-sm text-gray-300">{{ t('modelMarket.outputMeaning') }}</p>
          </dt>
          <dd class="shrink-0 font-mono text-sm font-medium tabular-nums text-white">{{ outputValue }}</dd>
        </div>
        <div class="flex items-start justify-between gap-4 py-3">
          <dt class="min-w-0">
            <p class="text-sm font-medium text-white">{{ t('modelMarket.columns.cacheRead') }}</p>
            <p class="text-sm text-gray-300">{{ t('modelMarket.cacheReadMeaning') }}</p>
          </dt>
          <dd class="shrink-0 font-mono text-sm font-medium tabular-nums text-white">{{ cacheReadValue }}</dd>
        </div>
        <div v-if="hasCacheWrite" class="flex items-start justify-between gap-4 py-3">
          <dt class="min-w-0">
            <p class="text-sm font-medium text-white">{{ t('modelMarket.columns.cacheWrite') }}</p>
            <p class="text-sm text-gray-300">{{ t('modelMarket.cacheWriteMeaning') }}</p>
          </dt>
          <dd class="shrink-0 font-mono text-sm font-medium tabular-nums text-white">{{ cacheWriteValue }}</dd>
        </div>
      </dl>

      <div v-if="showReference" class="flex flex-wrap items-center justify-between gap-2 border-t border-white/10 pt-3">
        <p v-if="showOfficialPrice" class="text-sm text-gray-300">
          {{ t('modelMarket.officialReference') }}
          <span class="font-mono tabular-nums text-white">{{ officialInputValue }}</span>
        </p>
        <p v-if="normalizedDiscount" class="text-sm font-medium text-emerald-300">
          {{ t('modelMarket.discountCompared', { value: normalizedDiscount }) }}
        </p>
      </div>

      <div class="absolute -top-1 left-6 size-2 rotate-45 bg-gray-950 dark:bg-dark-700" aria-hidden="true"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  inputValue: string
  outputValue: string
  cacheReadValue: string
  cacheWriteValue?: string
  officialInputValue?: string
  discountValue?: string
}>(), {
  cacheWriteValue: '',
  officialInputValue: '',
  discountValue: '',
})

const { t } = useI18n()
const instance = getCurrentInstance()
const tooltipId = `token-price-details-${instance?.uid ?? 'fallback'}`

const hasCacheWrite = computed(() => Boolean(props.cacheWriteValue && props.cacheWriteValue !== '-'))
const showOfficialPrice = computed(() => Boolean(props.officialInputValue && props.officialInputValue !== '-'))
const normalizedDiscount = computed(() => props.discountValue.replace(/^-/, ''))
const showReference = computed(() => showOfficialPrice.value || Boolean(normalizedDiscount.value))
</script>
