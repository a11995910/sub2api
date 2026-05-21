<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-5">
        <div class="grid gap-4 xl:grid-cols-[auto_minmax(220px,1fr)_minmax(180px,0.8fr)_minmax(220px,0.9fr)_auto] xl:items-end">
          <div>
            <label class="input-label">{{ t('modelTest.fields.type') }}</label>
            <div class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-800">
              <button
                type="button"
                class="inline-flex items-center gap-1.5 rounded-md px-3 py-2 text-sm font-medium transition-colors"
                :class="selectedKind === 'token' ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
                @click="selectedKind = 'token'"
              >
                <Icon name="chat" size="sm" />
                {{ t('modelTest.modes.text') }}
              </button>
              <button
                type="button"
                class="inline-flex items-center gap-1.5 rounded-md px-3 py-2 text-sm font-medium transition-colors"
                :class="selectedKind === 'image' ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
                @click="selectedKind = 'image'"
              >
                <Icon name="sparkles" size="sm" />
                {{ t('modelTest.modes.image') }}
              </button>
            </div>
          </div>

          <div>
            <label class="input-label">{{ t('modelTest.fields.model') }}</label>
            <select v-model="selectedModelKey" class="input" :disabled="loading || filteredModels.length === 0">
              <option value="">{{ t('modelTest.placeholders.model') }}</option>
              <option v-for="model in filteredModels" :key="model.key" :value="model.key">
                    {{ model.displayName }} · {{ platformLabel(model.platform) }}
              </option>
            </select>
          </div>

          <div>
            <label class="input-label">{{ t('modelTest.fields.group') }}</label>
            <select v-model.number="selectedGroupId" class="input" :disabled="loading || availableGroups.length === 0">
              <option :value="null">{{ t('modelTest.placeholders.group') }}</option>
              <option v-for="group in availableGroups" :key="group.id" :value="group.id">
                {{ group.name }}
              </option>
            </select>
          </div>

          <div>
            <label class="input-label">{{ t('modelTest.fields.apiKey') }}</label>
            <select v-model.number="selectedApiKeyId" class="input" :disabled="loading || keysForSelectedGroup.length === 0">
              <option :value="null">{{ t('modelTest.placeholders.apiKey') }}</option>
              <option v-for="key in keysForSelectedGroup" :key="key.id" :value="key.id">
                {{ keyLabel(key) }}
              </option>
            </select>
          </div>

          <button
            type="button"
            class="btn btn-secondary h-10"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadData"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>

        <div class="mt-4 grid gap-3 lg:grid-cols-3">
          <div class="rounded-lg border border-gray-100 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-800/60">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('modelTest.summary.groupRate') }}</p>
            <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ selectedRateLabel }}</p>
          </div>
          <div class="rounded-lg border border-gray-100 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-800/60">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('modelTest.summary.price') }}</p>
            <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ currentPricePreview }}</p>
          </div>
          <div class="rounded-lg border border-gray-100 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-800/60">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('modelTest.summary.endpoint') }}</p>
            <p class="mt-1 truncate font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ gatewayEndpoint }}</p>
          </div>
        </div>

        <div v-if="selectedGroup && keysForSelectedGroup.length === 0" class="mt-4 flex flex-col gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-200 sm:flex-row sm:items-center sm:justify-between">
          <span>{{ t('modelTest.noGroupKey', { group: selectedGroup.name }) }}</span>
          <button type="button" class="btn btn-secondary" @click="router.push('/keys')">
            <Icon name="key" size="sm" />
            {{ t('modelTest.goCreateKey') }}
          </button>
        </div>
      </section>

      <section v-if="loading" class="card py-16 text-center">
        <Icon name="refresh" size="lg" class="mx-auto animate-spin text-gray-400" />
      </section>

      <section v-else-if="filteredModels.length === 0" class="card py-16 text-center">
        <Icon name="inbox" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelTest.empty') }}</p>
      </section>

      <section v-else class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(360px,0.8fr)]">
        <form class="card p-5" @submit.prevent="runTest">
          <div class="flex flex-col gap-4">
            <div v-if="selectedModel" class="flex flex-wrap items-center gap-2">
              <span
                :class="[
                  'inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs font-medium',
                  platformBadgeClass(selectedModel.platform),
                ]"
              >
                <PlatformIcon :platform="selectedModel.platform as GroupPlatform" size="xs" />
                {{ platformLabel(selectedModel.platform) }}
              </span>
              <span class="rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                {{ selectedModel.displayName }}
              </span>
              <span v-if="selectedGroup" class="rounded-md bg-primary-50 px-2 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                {{ selectedGroup.name }}
              </span>
            </div>

            <div>
              <label class="input-label">{{ t('modelTest.fields.prompt') }}</label>
              <textarea
                v-model="prompt"
                class="input min-h-[180px] resize-y leading-6"
                :placeholder="promptPlaceholder"
              ></textarea>
            </div>

            <div class="grid gap-4 md:grid-cols-2">
              <div v-if="selectedKind === 'image'">
                <label class="input-label">{{ t('modelTest.fields.imageSize') }}</label>
                <select v-model="imageSize" class="input">
                  <option v-for="option in imageSizeOptions" :key="option.value" :value="option.value">
                    {{ option.label }}
                  </option>
                </select>
              </div>
              <div v-else>
                <label class="input-label">{{ t('modelTest.fields.maxTokens') }}</label>
                <input v-model.number="maxTokens" type="number" min="1" max="4096" class="input" />
              </div>
            </div>

            <div v-if="selectedKind === 'image'" class="rounded-lg border border-dashed border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-800/50">
              <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div>
                  <label class="input-label mb-1">{{ t('modelTest.fields.referenceImages') }}</label>
                  <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('modelTest.referenceImagesHint') }}</p>
                </div>
                <label
                  class="btn btn-secondary cursor-pointer"
                  :class="referenceImagesFull ? 'pointer-events-none opacity-60' : ''"
                >
                  <input
                    type="file"
                    accept="image/*"
                    multiple
                    class="hidden"
                    :disabled="referenceImagesFull || running"
                    @change="handleReferenceImagesSelected"
                  />
                  <Icon name="upload" size="sm" />
                  {{ t('modelTest.uploadReferenceImages') }}
                </label>
              </div>

              <p v-if="imageUploadError" class="mt-2 text-xs text-red-500">{{ imageUploadError }}</p>

              <div v-if="referenceImages.length > 0" class="mt-3 grid gap-3 sm:grid-cols-2">
                <figure
                  v-for="item in referenceImages"
                  :key="item.id"
                  class="relative flex gap-3 rounded-lg border border-gray-100 bg-white p-2 dark:border-dark-700 dark:bg-dark-900"
                >
                  <img :src="item.previewUrl" :alt="item.file.name" class="h-16 w-16 flex-shrink-0 rounded-md object-cover" />
                  <figcaption class="min-w-0 flex-1 self-center pr-8">
                    <p class="truncate text-sm font-medium text-gray-800 dark:text-gray-100">{{ item.file.name }}</p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ formatFileSize(item.file.size) }}</p>
                  </figcaption>
                  <button
                    type="button"
                    class="absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-red-500 dark:hover:bg-dark-700"
                    :title="t('modelTest.removeReferenceImage')"
                    @click="removeReferenceImage(item.id)"
                  >
                    <Icon name="x" size="sm" />
                  </button>
                </figure>
              </div>
            </div>

            <div class="flex flex-col gap-3 border-t border-gray-100 pt-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelTest.realBillingNotice') }}</p>
              <div class="flex flex-wrap gap-2">
                <button
                  v-if="running"
                  type="button"
                  class="btn btn-secondary"
                  @click="cancelRunning"
                >
                  {{ t('common.cancel') }}
                </button>
                <button
                  type="submit"
                  class="btn btn-primary"
                  :disabled="!canRun || running"
                >
                  <Icon name="play" size="sm" :class="running ? 'animate-pulse' : ''" />
                  {{ running ? t('modelTest.running') : t('modelTest.run') }}
                </button>
              </div>
            </div>
          </div>
        </form>

        <section class="card p-5">
          <div class="flex items-center justify-between gap-3 border-b border-gray-100 pb-3 dark:border-dark-700">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('modelTest.result.title') }}</h3>
            <span v-if="durationMs !== null" class="rounded-md bg-gray-100 px-2 py-1 font-mono text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
              {{ durationMs }}ms
            </span>
          </div>

          <div v-if="running" class="py-12 text-center">
            <Icon name="refresh" size="lg" class="mx-auto animate-spin text-gray-400" />
            <p class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('modelTest.result.waiting') }}</p>
          </div>

          <div v-else-if="errorMessage" class="mt-4 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-200">
            {{ errorMessage }}
          </div>

          <div v-else-if="!rawResponse" class="py-12 text-center">
            <Icon name="beaker" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelTest.result.empty') }}</p>
          </div>

          <div v-else class="mt-4 space-y-4">
            <div v-if="selectedKind === 'image' && imageOutputs.length > 0" class="grid gap-3">
              <figure
                v-for="(item, index) in imageOutputs"
                :key="index"
                class="overflow-hidden rounded-lg border border-gray-100 bg-gray-50 dark:border-dark-700 dark:bg-dark-800/60"
              >
                <img :src="item.src" :alt="item.revisedPrompt || prompt" class="w-full object-contain" />
                <figcaption v-if="item.revisedPrompt" class="border-t border-gray-100 px-3 py-2 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
                  {{ item.revisedPrompt }}
                </figcaption>
              </figure>
            </div>

            <div v-if="selectedKind === 'token' && chatOutput" class="rounded-lg border border-gray-100 bg-gray-50 p-4 text-sm leading-6 text-gray-800 dark:border-dark-700 dark:bg-dark-800/60 dark:text-gray-100">
              {{ chatOutput }}
            </div>

            <details class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800/60">
              <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-200">
                {{ t('modelTest.result.raw') }}
              </summary>
              <pre class="mt-3 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-md bg-gray-950 p-3 text-xs leading-5 text-gray-100">{{ responsePreview }}</pre>
            </details>
          </div>
        </section>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import userChannelsAPI, {
  type UserAvailableChannel,
  type UserAvailableGroup,
  type UserSupportedModelPricing,
} from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import keysAPI from '@/api/keys'
import { ModelTestError, testChatCompletion, testImageEdit, testImageGeneration } from '@/api/modelTest'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatMultiplier } from '@/utils/formatters'
import { formatScaled } from '@/utils/pricing'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import { filterGroupsByModelKind, resolveModelKind, type ModelKind } from '@/utils/modelKind'
import {
  ADAPTIVE_IMAGE_SIZE_VALUE,
  IMAGE_SIZE_PRESET_OPTIONS,
  getImageBillingTier,
} from '@/features/creativeDrawing/imageOptions'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
  type BillingMode,
} from '@/constants/channel'
import type { ApiKey, GroupPlatform } from '@/types'

interface TestModelOption {
  key: string
  name: string
  displayName: string
  platform: string
  kind: ModelKind
  pricing: UserSupportedModelPricing | null
  groups: UserAvailableGroup[]
}

interface ImageOutput {
  src: string
  revisedPrompt: string
}

interface ReferenceImage {
  id: string
  file: File
  previewUrl: string
}

const { t } = useI18n()
const appStore = useAppStore()
const route = useRoute()
const router = useRouter()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const activeKeys = ref<ApiKey[]>([])
const loading = ref(false)
const running = ref(false)
const selectedKind = ref<ModelKind>('token')
const selectedModelKey = ref('')
const selectedGroupId = ref<number | null>(null)
const selectedApiKeyId = ref<number | null>(null)
const prompt = ref('')
const adaptiveImageSizeValue = ADAPTIVE_IMAGE_SIZE_VALUE
const imageSize = ref(adaptiveImageSizeValue)
const referenceImages = ref<ReferenceImage[]>([])
const imageUploadError = ref('')
const maxTokens = ref(256)
const rawResponse = ref<unknown | null>(null)
const errorMessage = ref('')
const durationMs = ref<number | null>(null)

let runController: AbortController | null = null

const maxReferenceImages = 4
const maxReferenceImageSize = 20 * 1024 * 1024

const imageSizeOptions = computed(() =>
  IMAGE_SIZE_PRESET_OPTIONS.map((option) => ({
    ...option,
    label: option.value === adaptiveImageSizeValue
      ? t('modelTest.imageSizeOptions.adaptive')
      : option.label.replace(/×/g, 'x'),
  })),
)

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

const allModels = computed<TestModelOption[]>(() => {
  const map = new Map<string, TestModelOption>()
  for (const channel of channels.value) {
    for (const section of channel.platforms || []) {
      const platform = section.platform
      for (const model of section.supported_models || []) {
        const kind = resolveModelKind(model)
        const key = `${platform}:${model.name}:${kind}:${pricingSignature(model.pricing)}`
        let item = map.get(key)
        if (!item) {
          item = {
            key,
            name: gatewayModelName(model.name, kind),
            displayName: model.name,
            platform: model.platform || platform,
            kind,
            pricing: model.pricing,
            groups: [],
          }
          map.set(key, item)
        }

        const existing = new Set(item.groups.map((group) => group.id))
        for (const group of filterGroupsByModelKind(section.groups, kind)) {
          if (!existing.has(group.id)) {
            item.groups.push(group)
            existing.add(group.id)
          }
        }
      }
    }
  }

  return Array.from(map.values())
    .filter((item) => item.groups.length > 0)
    .sort((a, b) => a.platform.localeCompare(b.platform) || a.displayName.localeCompare(b.displayName))
})

const filteredModels = computed(() => allModels.value.filter((model) => model.kind === selectedKind.value))
const selectedModel = computed(() => allModels.value.find((model) => model.key === selectedModelKey.value) || null)
const availableGroups = computed(() => selectedModel.value?.groups || [])
const selectedGroup = computed(() => availableGroups.value.find((group) => group.id === selectedGroupId.value) || null)
const keysForSelectedGroup = computed(() => {
  if (selectedGroupId.value == null) return []
  return activeKeys.value.filter((key) => key.status === 'active' && Number(key.group_id) === selectedGroupId.value)
})
const selectedApiKey = computed(() => activeKeys.value.find((key) => key.id === selectedApiKeyId.value) || null)
const referenceImagesFull = computed(() => referenceImages.value.length >= maxReferenceImages)
const promptPlaceholder = computed(() =>
  selectedKind.value === 'image'
    ? t('modelTest.placeholders.imagePrompt')
    : t('modelTest.placeholders.textPrompt'),
)

const gatewayEndpoint = computed(() =>
  selectedKind.value === 'image'
    ? referenceImages.value.length > 0 ? '/v1/images/edits' : '/v1/images/generations'
    : '/v1/chat/completions',
)

const canRun = computed(() =>
  !!selectedModel.value &&
  !!selectedGroup.value &&
  !!selectedApiKey.value &&
  prompt.value.trim().length > 0,
)

const selectedRateLabel = computed(() => {
  if (!selectedGroup.value) return '-'
  const rate = selectedKind.value === 'image'
    ? effectiveImageRate(selectedGroup.value)
    : effectiveTextRate(selectedGroup.value)
  return `${formatMultiplier(rate)}x`
})

const currentPricePreview = computed(() => {
  const model = selectedModel.value
  const group = selectedGroup.value
  if (!model || !group) return '-'
  if (selectedKind.value === 'image') {
    const tier = imageSizeTier(imageSize.value)
    const tierLabel = imageSize.value === adaptiveImageSizeValue
      ? t('modelTest.imageSizeAdaptivePreview', { tier })
      : tier
    const price = imageTierBasePrice(group, tier)
    return price == null
      ? imageTierPrices(group)
      : `${tierLabel} ${formatScaled(price * effectiveImageRate(group), 1)} / ${t('modelTest.perImage')}`
  }
  return textPricePreview(model.pricing, group)
})

const chatOutput = computed(() => selectedKind.value === 'token' ? extractChatText(rawResponse.value) : '')
const imageOutputs = computed(() => selectedKind.value === 'image' ? extractImageOutputs(rawResponse.value) : [])
const responsePreview = computed(() => rawResponse.value == null ? '' : JSON.stringify(redactForPreview(rawResponse.value), null, 2))

watch(selectedKind, (kind) => {
  if (selectedModel.value?.kind !== kind) {
    selectedModelKey.value = filteredModels.value[0]?.key || ''
  }
})

watch(selectedModelKey, () => {
  const groups = availableGroups.value
  if (!groups.some((group) => group.id === selectedGroupId.value)) {
    selectedGroupId.value = groups[0]?.id ?? null
  }
})

watch(selectedGroupId, (groupId) => {
  selectedApiKeyId.value = null
  if (groupId != null) {
    void ensureKeysForGroup(groupId)
  } else {
    syncSelectedApiKey()
  }
})

watch(keysForSelectedGroup, syncSelectedApiKey)

watch(
  () => route.query,
  () => applyQuerySelection(),
  { deep: true },
)

function effectiveTextRate(group: UserAvailableGroup): number {
  return userGroupRates.value[group.id] ?? group.rate_multiplier ?? 1
}

function effectiveImageRate(group: UserAvailableGroup): number {
  return group.image_rate_independent ? group.image_rate_multiplier : effectiveTextRate(group)
}

function effectiveMultiplier(group: UserAvailableGroup, mode?: BillingMode): number {
  return mode === BILLING_MODE_IMAGE ? effectiveImageRate(group) : effectiveTextRate(group)
}

function textPricePreview(pricing: UserSupportedModelPricing | null, group: UserAvailableGroup): string {
  if (!pricing) return '-'
  if (pricing.billing_mode === BILLING_MODE_PER_REQUEST) {
    return formatPrice(pricing.per_request_price, 1, group, pricing.billing_mode)
  }
  if (pricing.billing_mode === BILLING_MODE_IMAGE) {
    return imageTierPrices(group)
  }
  if (pricing.billing_mode === BILLING_MODE_TOKEN) {
    const input = formatPrice(pricing.input_price, perMillionScale, group, pricing.billing_mode)
    const output = formatPrice(pricing.output_price, perMillionScale, group, pricing.billing_mode)
    return `${t('modelTest.summary.input')} ${input} / ${t('modelTest.summary.output')} ${output}`
  }
  return '-'
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

function imageTierBasePrice(group: UserAvailableGroup, tier: string): number | null {
  switch (tier) {
    case '1K':
      return group.image_price_1k
    case '4K':
      return group.image_price_4k
    default:
      return group.image_price_2k
  }
}

function imageTierPrices(group: UserAvailableGroup): string {
  return [
    ['1K', group.image_price_1k],
    ['2K', group.image_price_2k],
    ['4K', group.image_price_4k],
  ]
    .map(([tier, value]) => `${tier} ${typeof value === 'number' ? formatScaled(value * effectiveImageRate(group), 1) : '-'}`)
    .join(' / ')
}

function imageSizeTier(size: string): string {
  return getImageBillingTier(size)
}

async function loadData() {
  loading.value = true
  try {
    const [list, rates, keys] = await Promise.all([
      userChannelsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
      loadActiveKeys(),
    ])
    channels.value = list
    userGroupRates.value = rates
    activeKeys.value = keys
    applyQuerySelection()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

async function loadActiveKeys(): Promise<ApiKey[]> {
  const pageSize = 1000
  const out: ApiKey[] = []
  let page = 1
  let pages = 1
  do {
    const result = await keysAPI.list(page, pageSize, {
      status: 'active',
      sort_by: 'created_at',
      sort_order: 'desc',
    })
    out.push(...result.items)
    pages = result.pages || 1
    page += 1
  } while (page <= pages)
  return out
}

async function ensureKeysForGroup(groupId: number) {
  try {
    const result = await keysAPI.list(1, 1000, {
      status: 'active',
      group_id: groupId,
      sort_by: 'created_at',
      sort_order: 'desc',
    })
    mergeKeys(result.items)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('modelTest.loadKeysFailed')))
  } finally {
    syncSelectedApiKey()
  }
}

function mergeKeys(items: ApiKey[]) {
  const byID = new Map(activeKeys.value.map((key) => [key.id, key]))
  for (const item of items) {
    byID.set(item.id, item)
  }
  activeKeys.value = Array.from(byID.values())
}

function syncSelectedApiKey() {
  if (selectedApiKeyId.value != null && keysForSelectedGroup.value.some((key) => key.id === selectedApiKeyId.value)) {
    return
  }
  selectedApiKeyId.value = keysForSelectedGroup.value[0]?.id ?? null
}

function applyQuerySelection() {
  if (allModels.value.length === 0) return
  const queryKind = queryString(route.query.kind)
  const queryModel = queryString(route.query.model)
  const queryPlatform = queryString(route.query.platform)
  const queryGroupID = queryNumber(route.query.group_id)
  if (queryKind === 'token' || queryKind === 'image') {
    selectedKind.value = queryKind
  }

  const candidates = allModels.value.filter((model) => model.kind === selectedKind.value)
  let target = candidates.find((model) =>
    (!queryModel || model.displayName === queryModel || model.name === queryModel) &&
    (!queryPlatform || model.platform === queryPlatform) &&
    (queryGroupID == null || model.groups.some((group) => group.id === queryGroupID)),
  )
  if (!target && queryModel) {
    target = candidates.find((model) =>
      (model.displayName === queryModel || model.name === queryModel) &&
      (!queryPlatform || model.platform === queryPlatform),
    )
  }
  if (!target && queryModel) {
    target = candidates.find((model) => model.displayName === queryModel || model.name === queryModel)
  }
  if (!target) {
    target = candidates[0]
  }

  selectedModelKey.value = target?.key || ''
  const targetGroups = target?.groups || []
  selectedGroupId.value = queryGroupID != null && targetGroups.some((group) => group.id === queryGroupID)
    ? queryGroupID
    : targetGroups[0]?.id ?? null
}

function queryString(value: unknown): string {
  if (Array.isArray(value)) return String(value[0] || '')
  return String(value || '')
}

function queryNumber(value: unknown): number | null {
  const parsed = Number(queryString(value))
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null
}

function keyLabel(key: ApiKey): string {
  return `${key.name} · ${maskKey(key.key)}`
}

function gatewayModelName(name: string, kind: ModelKind): string {
  if (kind === 'image' && name === 'image-2') {
    return 'gpt-image-2'
  }
  return name
}

function maskKey(value: string): string {
  if (value.length <= 14) return value
  return `${value.slice(0, 7)}...${value.slice(-4)}`
}

async function runTest() {
  const model = selectedModel.value
  const apiKey = selectedApiKey.value
  if (!model || !selectedGroup.value || !apiKey) {
    appStore.showWarning(t('modelTest.validation.missingSelection'))
    return
  }
  const cleanPrompt = prompt.value.trim()
  if (!cleanPrompt) {
    appStore.showWarning(t('modelTest.validation.promptRequired'))
    return
  }

  runController = new AbortController()
  running.value = true
  rawResponse.value = null
  errorMessage.value = ''
  durationMs.value = null
  const startedAt = performance.now()
  try {
    if (selectedKind.value === 'image') {
      rawResponse.value = referenceImages.value.length > 0
        ? await testImageEdit({
          apiKey: apiKey.key,
          model: model.name,
          prompt: cleanPrompt,
          size: imageSize.value,
          images: referenceImages.value.map((item) => item.file),
          signal: runController.signal,
        })
        : await testImageGeneration({
          apiKey: apiKey.key,
          model: model.name,
          prompt: cleanPrompt,
          size: imageSize.value,
          signal: runController.signal,
        })
    } else {
      rawResponse.value = await testChatCompletion({
        apiKey: apiKey.key,
        model: model.name,
        prompt: cleanPrompt,
        maxTokens: normalizedMaxTokens(),
        signal: runController.signal,
      })
    }
    durationMs.value = Math.round(performance.now() - startedAt)
    appStore.showSuccess(t('modelTest.runSuccess'))
  } catch (err: unknown) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      return
    }
    errorMessage.value = err instanceof ModelTestError
      ? err.message
      : extractApiErrorMessage(err, t('modelTest.runFailed'))
    appStore.showError(errorMessage.value)
  } finally {
    running.value = false
    runController = null
  }
}

function normalizedMaxTokens(): number {
  const parsed = Number(maxTokens.value)
  if (!Number.isFinite(parsed)) return 256
  return Math.max(1, Math.min(4096, Math.floor(parsed)))
}

function cancelRunning() {
  runController?.abort()
  running.value = false
}

function handleReferenceImagesSelected(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  imageUploadError.value = ''
  if (files.length === 0) return

  const remainingSlots = maxReferenceImages - referenceImages.value.length
  if (remainingSlots <= 0) {
    imageUploadError.value = t('modelTest.referenceImageLimit', { count: maxReferenceImages })
    input.value = ''
    return
  }

  const accepted: ReferenceImage[] = []
  for (const file of files.slice(0, remainingSlots)) {
    if (!file.type.startsWith('image/')) {
      imageUploadError.value = t('modelTest.referenceImageTypeError')
      continue
    }
    if (file.size > maxReferenceImageSize) {
      imageUploadError.value = t('modelTest.referenceImageSizeError', {
        size: formatFileSize(maxReferenceImageSize),
      })
      continue
    }
    accepted.push({
      id: `${Date.now()}-${Math.random().toString(36).slice(2)}`,
      file,
      previewUrl: URL.createObjectURL(file),
    })
  }

  if (files.length > remainingSlots) {
    imageUploadError.value = t('modelTest.referenceImageLimit', { count: maxReferenceImages })
  }
  referenceImages.value = [...referenceImages.value, ...accepted]
  input.value = ''
}

function removeReferenceImage(id: string) {
  const target = referenceImages.value.find((item) => item.id === id)
  if (target) {
    URL.revokeObjectURL(target.previewUrl)
  }
  referenceImages.value = referenceImages.value.filter((item) => item.id !== id)
}

function clearReferenceImages() {
  for (const item of referenceImages.value) {
    URL.revokeObjectURL(item.previewUrl)
  }
  referenceImages.value = []
}

function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }
  return `${size >= 10 || unitIndex === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[unitIndex]}`
}

function extractChatText(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const obj = payload as Record<string, any>
  const choice = Array.isArray(obj.choices) ? obj.choices[0] : null
  const content = choice?.message?.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((item) => typeof item === 'string' ? item : item?.text || item?.content || '')
      .filter(Boolean)
      .join('\n')
  }
  if (typeof choice?.text === 'string') return choice.text
  if (typeof obj.output_text === 'string') return obj.output_text
  return ''
}

function extractImageOutputs(payload: unknown): ImageOutput[] {
  if (!payload || typeof payload !== 'object') return []
  const obj = payload as Record<string, any>
  const items = Array.isArray(obj.data) ? obj.data : []
  return items
    .map((item): ImageOutput | null => {
      if (typeof item?.b64_json === 'string' && item.b64_json) {
        return {
          src: `data:image/png;base64,${item.b64_json}`,
          revisedPrompt: String(item.revised_prompt || ''),
        }
      }
      if (typeof item?.url === 'string' && item.url) {
        return {
          src: item.url,
          revisedPrompt: String(item.revised_prompt || ''),
        }
      }
      return null
    })
    .filter((item): item is ImageOutput => item !== null)
}

function redactForPreview(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(redactForPreview)
  }
  if (value && typeof value === 'object') {
    const out: Record<string, unknown> = {}
    for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
      const lowerKey = key.toLowerCase()
      if (typeof item === 'string' && (lowerKey.includes('b64') || lowerKey.includes('base64') || lowerKey === 'result')) {
        out[key] = item.length > 80 ? `[base64 ${item.length} chars]` : item
      } else if (typeof item === 'string' && item.length > 2000) {
        out[key] = `${item.slice(0, 2000)}...`
      } else {
        out[key] = redactForPreview(item)
      }
    }
    return out
  }
  return value
}

onMounted(() => {
  void loadData()
})

onBeforeUnmount(() => {
  runController?.abort()
  clearReferenceImages()
})
</script>
