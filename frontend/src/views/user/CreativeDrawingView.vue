<template>
  <AppLayout>
    <div class="creative-shell">
      <aside class="creative-history">
        <div class="flex items-center gap-2">
          <button class="creative-new-button" @click="startNewConversation">
            <Icon name="sparkles" size="sm" />
            新建创作
          </button>
          <button class="creative-icon-button" title="清空历史" @click="clearConversations">
            <Icon name="trash" size="sm" />
          </button>
        </div>

        <div class="creative-history-list">
          <button
            v-for="conversation in conversations"
            :key="conversation.id"
            class="creative-history-item"
            :class="{ 'creative-history-item-active': conversation.id === activeConversationId }"
            @click="selectConversation(conversation.id)"
          >
            <span class="truncate text-sm font-semibold text-slate-700 dark:text-slate-100">{{ conversation.title }}</span>
            <span class="mt-1 text-xs text-slate-400">{{ conversation.turns.length }} 次 · {{ formatConversationTime(conversation.updatedAt) }}</span>
          </button>

          <div v-if="conversations.length === 0" class="rounded-2xl border border-dashed border-slate-200 p-4 text-sm leading-6 text-slate-500 dark:border-dark-700 dark:text-dark-400">
            暂无创作历史。发送第一张图后会自动保存到本地浏览器。
          </div>
        </div>
      </aside>

      <section class="creative-main">
        <div v-if="activeConversation?.turns.length" class="creative-turns">
          <article v-for="turn in activeConversation.turns" :key="turn.id" class="creative-turn">
            <div class="creative-turn-meta">
              <span class="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-600 dark:bg-dark-800 dark:text-dark-200">
                {{ turn.mode === 'edit' ? '参考图作画' : '文生图' }}
              </span>
              <span>{{ turn.model }}</span>
              <span>{{ formatImageSizeDisplay(turn.size) }}</span>
              <span>{{ turn.count }} 张</span>
              <span>{{ formatConversationTime(turn.createdAt) }}</span>
            </div>
            <p class="mt-3 whitespace-pre-wrap text-sm leading-7 text-slate-700 dark:text-dark-200">{{ turn.prompt }}</p>

            <div v-if="turn.references.length" class="mt-3 flex flex-wrap gap-2">
              <img
                v-for="reference in turn.references"
                :key="reference.id"
                :src="reference.dataUrl"
                :alt="reference.name"
                class="h-16 w-16 rounded-2xl object-cover ring-1 ring-slate-200 dark:ring-dark-700"
                referrerpolicy="no-referrer"
              >
            </div>

            <div v-if="turn.status === 'generating'" class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <div v-for="index in Math.max(turn.count, 1)" :key="index" class="creative-image-skeleton">
                <Icon name="sparkles" size="lg" class="animate-pulse text-blue-600" />
                <span>生成中</span>
              </div>
            </div>

            <div v-else-if="turn.status === 'error'" class="mt-4 rounded-2xl border border-red-100 bg-red-50 p-4 text-sm leading-6 text-red-700 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-200">
              {{ turn.error || '生成失败' }}
            </div>

            <div v-else class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <figure v-for="(image, index) in turn.images" :key="image.id" class="creative-result">
                <button class="block w-full overflow-hidden rounded-2xl bg-slate-100 dark:bg-dark-800" @click="previewImage = buildStoredImageUrl(image)">
                  <img :src="buildStoredImageUrl(image)" :alt="turn.prompt" class="creative-result-image" referrerpolicy="no-referrer">
                </button>
                <figcaption class="mt-2 flex items-center justify-between gap-2 text-xs text-slate-500 dark:text-dark-400">
                  <span class="truncate">{{ image.revised_prompt || image.size || `结果 ${index + 1}` }}</span>
                  <div class="flex shrink-0 items-center gap-1">
                    <button class="creative-mini-button" title="作为参考图继续创作" @click="useResultAsReference(image, index)">
                      <Icon name="image" size="xs" />
                    </button>
                    <a class="creative-mini-button" title="打开图片" :href="buildStoredImageUrl(image)" target="_blank" rel="noreferrer">
                      <Icon name="externalLink" size="xs" />
                    </a>
                  </div>
                </figcaption>
              </figure>
            </div>
          </article>
        </div>

        <div v-else class="creative-empty">
          <div class="inline-flex items-center gap-1.5 rounded-full bg-slate-100 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-dark-800 dark:text-dark-200">
            <Icon name="sparkles" size="xs" class="text-blue-600" />
            生图预设
          </div>
          <h1 class="mt-4 text-4xl font-bold tracking-normal text-slate-950 dark:text-white md:text-5xl">
            Turn ideas into images
          </h1>
          <p class="mx-auto mt-4 max-w-xl text-center text-base leading-7 text-slate-600 dark:text-dark-300">
            选择一组真实案例预设快速开始，也可以直接在下方输入自己的画面描述。
          </p>

          <div class="mt-8 grid w-full max-w-6xl gap-4 md:grid-cols-2 xl:grid-cols-4">
            <article v-for="preset in IMAGE_PROMPT_PRESETS" :key="preset.id" class="creative-preset">
              <div class="relative aspect-[16/10] overflow-hidden bg-slate-100">
                <img :src="preset.imageSrc" :alt="preset.title" class="h-full w-full object-cover">
                <span class="absolute bottom-2 left-2 rounded-full bg-white/90 px-2 py-1 text-xs font-bold text-slate-900">{{ preset.size }}</span>
                <span class="absolute bottom-2 right-2 rounded-full bg-black/45 px-2 py-1 text-xs font-bold text-white">{{ preset.count }} 张</span>
              </div>
              <div class="flex flex-1 flex-col p-4 text-center">
                <h3 class="font-semibold text-slate-950 dark:text-white">{{ preset.title }}</h3>
                <p class="mt-2 min-h-[48px] text-sm leading-6 text-slate-600 dark:text-dark-300">{{ preset.hint }}</p>
                <button class="mt-auto border-t border-slate-100 pt-3 text-sm font-semibold text-blue-600 hover:text-blue-700 dark:border-dark-800" @click="applyPreset(preset)">
                  套用这个预设
                </button>
              </div>
            </article>
          </div>
        </div>

      </section>
    </div>

    <PromptMarketDialog v-model:open="marketOpen" @apply="applyMarketPrompt" />

    <teleport to="body">
      <div class="creative-composer-wrap" :class="{ 'creative-composer-wrap-collapsed': appStore.sidebarCollapsed }">
        <div v-if="referenceImages.length" class="creative-reference-strip">
          <div v-for="reference in referenceImages" :key="reference.id" class="relative h-16 w-16">
            <img :src="reference.dataUrl" :alt="reference.name" class="h-full w-full rounded-2xl object-cover shadow-sm ring-1 ring-slate-200 dark:ring-dark-700" referrerpolicy="no-referrer">
            <div v-if="reference.loading" class="creative-reference-loading">
              <Icon name="refresh" size="xs" class="animate-spin" />
            </div>
            <div v-else-if="reference.loadError" class="creative-reference-error" title="参考图加载失败">
              <Icon name="x" size="xs" />
            </div>
            <button class="absolute -right-1 -top-1 rounded-full bg-white p-1 text-slate-500 shadow hover:text-red-600 dark:bg-dark-800" title="移除参考图" @click="removeReference(reference.id)">
              <Icon name="x" size="xs" />
            </button>
          </div>
        </div>

        <div class="creative-composer">
          <textarea
            v-model="prompt"
            class="creative-textarea"
            :placeholder="referenceImages.length ? '描述如何参考这些图片生成新图...' : '输入画面描述，使用 gpt-image-2 直接生成图片...'"
            @keydown.meta.enter.prevent="submit"
            @keydown.ctrl.enter.prevent="submit"
          />

          <div class="creative-toolbar">
            <div class="creative-toolbar-main flex flex-wrap items-center gap-2">
              <span class="creative-pill creative-pill-active">
                <Icon name="image" size="xs" />
                作画
              </span>
              <select v-model="quickSizeValue" class="creative-select creative-size-select" title="尺寸">
                <option v-for="item in creativeSizeOptions" :key="item.value || 'auto'" :value="item.value">
                  尺寸 {{ item.label }}
                </option>
                <option v-if="showCurrentSizeOption" :value="currentImageSize">
                  尺寸 {{ formatImageSizeDisplay(currentImageSize) }} · {{ currentImageBillingTier }}
                </option>
              </select>
              <button class="creative-pill" @click="marketOpen = true">
                <Icon name="globe" size="xs" />
                热门模板
              </button>
              <button class="creative-pill" @click="paramsOpen = !paramsOpen">
                <Icon name="filter" size="xs" />
                参数
              </button>
            </div>

            <div class="creative-toolbar-actions flex items-center gap-2">
              <input ref="fileInputRef" type="file" accept="image/*" multiple class="hidden" @change="handleFileChange">
              <button class="creative-icon-button" title="上传参考图" @click="fileInputRef?.click()">
                <Icon name="upload" size="sm" />
              </button>
              <button class="creative-send" :disabled="isSubmitting || hasLoadingReferences" title="发送" @click="submit">
                <Icon v-if="isSubmitting || hasLoadingReferences" name="refresh" size="sm" class="animate-spin" />
                <Icon v-else name="arrowUp" size="sm" />
              </button>
            </div>
          </div>

          <div v-if="paramsOpen" class="creative-params">
            <label class="creative-field">
              <span>API 密钥</span>
              <select v-model.number="selectedApiKeyId">
                <option :value="0">选择 OpenAI 分组密钥</option>
                <option v-for="key in drawableKeys" :key="key.id" :value="key.id">
                  {{ key.name }} · {{ maskKey(key.key) }}
                </option>
              </select>
            </label>
            <label class="creative-field">
              <span>数量</span>
              <input v-model.number="imageCount" min="1" max="4" type="number">
            </label>
            <label class="creative-field">
              <span>尺寸模式</span>
              <select v-model="sizeSelection.mode">
                <option v-for="item in IMAGE_SIZE_MODE_OPTIONS" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
            </label>
            <label v-if="sizeSelection.mode === 'ratio'" class="creative-field">
              <span>比例</span>
              <select v-model="sizeSelection.aspectRatio">
                <option v-for="item in IMAGE_ASPECT_RATIO_OPTIONS" :key="item.value || 'auto'" :value="item.value">{{ item.label }}</option>
              </select>
            </label>
            <label v-if="sizeSelection.mode === 'ratio'" class="creative-field">
              <span>分辨率</span>
              <select v-model="sizeSelection.resolution">
                <option v-for="item in IMAGE_RESOLUTION_OPTIONS" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
            </label>
            <label v-if="sizeSelection.mode === 'ratio' && sizeSelection.aspectRatio === CUSTOM_IMAGE_ASPECT_RATIO" class="creative-field">
              <span>自定义比例</span>
              <input v-model="sizeSelection.customRatio" placeholder="16:9">
            </label>
            <label v-if="sizeSelection.mode === 'custom'" class="creative-field">
              <span>宽度</span>
              <input v-model="sizeSelection.customWidth" inputmode="numeric">
            </label>
            <label v-if="sizeSelection.mode === 'custom'" class="creative-field">
              <span>高度</span>
              <input v-model="sizeSelection.customHeight" inputmode="numeric">
            </label>
            <label class="creative-field">
              <span>格式</span>
              <select v-model="outputFormat">
                <option v-for="item in CREATIVE_OUTPUT_FORMAT_OPTIONS" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
            </label>
          </div>

          <div class="mt-2 flex items-center justify-end px-2 text-xs text-slate-400">
            <span>{{ estimatedConsumptionLabel }}</span>
          </div>
        </div>
      </div>
    </teleport>

    <teleport to="body">
      <transition name="fade">
        <div v-if="previewImage" class="fixed inset-0 z-[1100] flex items-center justify-center bg-black/80 p-4" @click.self="previewImage = ''">
          <button class="absolute right-5 top-5 rounded-full bg-white/10 p-2 text-white backdrop-blur hover:bg-white/20" title="关闭" @click="previewImage = ''">
            <Icon name="x" size="lg" />
          </button>
          <img :src="previewImage" alt="图片预览" class="max-h-full max-w-full rounded-2xl object-contain shadow-2xl">
        </div>
      </transition>
    </teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores'
import keysAPI from '@/api/keys'
import type { ApiKey } from '@/types'
import {
  CREATIVE_OUTPUT_FORMAT_OPTIONS,
  createCreativeImageEdit,
  createCreativeImageGeneration,
  type CreativeImageModel,
  type CreativeOutputFormat,
  type CreativeImageRequest,
  type CreativeImageResult
} from '@/api/creativeDrawing'
import {
  CUSTOM_IMAGE_ASPECT_RATIO,
  DEFAULT_IMAGE_CUSTOM_HEIGHT,
  DEFAULT_IMAGE_CUSTOM_RATIO,
  DEFAULT_IMAGE_CUSTOM_WIDTH,
  IMAGE_ASPECT_RATIO_OPTIONS,
  IMAGE_RESOLUTION_OPTIONS,
  IMAGE_SIZE_MODE_OPTIONS,
  IMAGE_SIZE_PRESET_OPTIONS,
  buildImageSize,
  formatImageSizeDisplay,
  getImageBillingTier,
  getImageSizeSelectionFromSize,
  type ImageSizeSelection
} from '@/features/creativeDrawing/imageOptions'
import { IMAGE_PROMPT_PRESETS, type ImagePromptPreset } from '@/features/creativeDrawing/imagePresets'
import PromptMarketDialog from '@/features/creativeDrawing/PromptMarketDialog.vue'
import { getPromptApplyReferenceImageUrls, type BananaPrompt } from '@/features/creativeDrawing/promptMarket'
import {
  buildConversationTitle,
  buildStoredImageUrl,
  createId,
  dataUrlToFile,
  loadActiveCreativeConversationId,
  loadCreativeConversations,
  readFileAsDataUrl,
  resultToReferenceImage,
  saveActiveCreativeConversationId,
  saveCreativeConversations,
  type CreativeConversation,
  type CreativeReferenceImage,
  type CreativeStoredImage,
  type CreativeTurn
} from '@/features/creativeDrawing/conversations'

const appStore = useAppStore()

const conversations = ref<CreativeConversation[]>([])
const activeConversationId = ref('')
const prompt = ref('')
const fixedCreativeImageModel: CreativeImageModel = 'gpt-image-2'
const outputFormat = ref<CreativeOutputFormat>('png')
const imageCount = ref(1)
const paramsOpen = ref(false)
const marketOpen = ref(false)
const isSubmitting = ref(false)
const previewImage = ref('')
const referenceImages = ref<CreativeReferenceImage[]>([])
const apiKeys = ref<ApiKey[]>([])
const selectedApiKeyId = ref(0)
const fileInputRef = ref<HTMLInputElement | null>(null)

const sizeSelection = reactive<ImageSizeSelection>({
  mode: 'auto',
  aspectRatio: '',
  resolution: 'auto',
  customRatio: DEFAULT_IMAGE_CUSTOM_RATIO,
  customWidth: DEFAULT_IMAGE_CUSTOM_WIDTH,
  customHeight: DEFAULT_IMAGE_CUSTOM_HEIGHT
})

const activeConversation = computed(() => conversations.value.find((item) => item.id === activeConversationId.value) || null)
const drawableKeys = computed(() => {
  return apiKeys.value.filter((key) => {
    return key.status === 'active' &&
      key.group_id &&
      key.group?.platform === 'openai' &&
      key.group.allow_image_generation
  })
})
const selectedApiKey = computed(() => drawableKeys.value.find((key) => key.id === selectedApiKeyId.value) || null)
const creativeSizeOptions = computed(() => IMAGE_SIZE_PRESET_OPTIONS)
const currentImageSize = computed(() => buildImageSize(sizeSelection))
const currentImageBillingTier = computed(() => getImageBillingTier(currentImageSize.value))
const showCurrentSizeOption = computed(() => {
  const size = currentImageSize.value
  if (!size) {
    return false
  }
  return !IMAGE_SIZE_PRESET_OPTIONS.some((option) => option.value === size)
})
const quickSizeValue = computed({
  get: () => currentImageSize.value,
  set: (value: string) => applySizeFromPreset(value)
})
const estimatedConsumptionLabel = computed(() => {
  const count = normalizeImageCount(imageCount.value)
  return `预计消耗 ${count} 张 · ${currentImageBillingTier.value} 图片单位`
})
const hasLoadingReferences = computed(() => referenceImages.value.some((item) => item.loading))

watch(conversations, (items) => saveCreativeConversations(items), { deep: true })
watch(activeConversationId, (id) => saveActiveCreativeConversationId(id))
watch(drawableKeys, (keys) => {
  if (!selectedApiKeyId.value && keys.length > 0) {
    selectedApiKeyId.value = keys[0].id
  }
})

onMounted(async () => {
  conversations.value = loadCreativeConversations()
  activeConversationId.value = loadActiveCreativeConversationId() || conversations.value[0]?.id || ''
  await loadApiKeys()
})

async function loadApiKeys() {
  try {
    const result = await keysAPI.list(1, 100, { status: 'active' })
    apiKeys.value = result.items
  } catch (err) {
    appStore.showError(err instanceof Error ? err.message : '加载 API 密钥失败')
  }
}

function startNewConversation() {
  activeConversationId.value = ''
  prompt.value = ''
  referenceImages.value = []
  imageCount.value = 1
  applySizeFromPreset('')
}

function selectConversation(id: string) {
  activeConversationId.value = id
  prompt.value = ''
  referenceImages.value = []
}

function clearConversations() {
  conversations.value = []
  activeConversationId.value = ''
  prompt.value = ''
  referenceImages.value = []
  imageCount.value = 1
  applySizeFromPreset('')
}

function ensureConversation(seedPrompt: string) {
  if (activeConversation.value) {
    return activeConversation.value
  }
  const now = new Date().toISOString()
  const conversation: CreativeConversation = {
    id: createId(),
    title: buildConversationTitle(seedPrompt),
    createdAt: now,
    updatedAt: now,
    turns: []
  }
  conversations.value = [conversation, ...conversations.value]
  activeConversationId.value = conversation.id
  return conversation
}

function updateConversation(conversation: CreativeConversation, updater: (item: CreativeConversation) => CreativeConversation) {
  conversations.value = conversations.value.map((item) => item.id === conversation.id ? updater(item) : item)
}

function addOrUpdateTurn(conversation: CreativeConversation, turn: CreativeTurn) {
  updateConversation(conversation, (item) => {
    const turns = item.turns.some((existing) => existing.id === turn.id)
      ? item.turns.map((existing) => existing.id === turn.id ? turn : existing)
      : [...item.turns, turn]
    return {
      ...item,
      title: item.turns.length === 0 ? buildConversationTitle(turn.prompt) : item.title,
      updatedAt: new Date().toISOString(),
      turns
    }
  })
}

function applySizeFromPreset(size: string) {
  const selection = getImageSizeSelectionFromSize(size)
  Object.assign(sizeSelection, selection)
}

function guessImageMimeType(name: string) {
  const normalized = name.toLowerCase().split('?')[0]
  if (normalized.endsWith('.jpg') || normalized.endsWith('.jpeg')) return 'image/jpeg'
  if (normalized.endsWith('.webp')) return 'image/webp'
  if (normalized.endsWith('.gif')) return 'image/gif'
  return 'image/png'
}

async function loadReferenceFromURL(url: string, name: string, source: CreativeReferenceImage['source']) {
  const resolvedURL = resolveBrowserReferenceURL(url)
  const response = await fetch(resolvedURL, {
    headers: {
      Accept: 'image/*'
    }
  })
  if (!response.ok) {
    throw new Error(`加载参考图失败：${response.status}`)
  }
  const blob = await response.blob()
  const type = blob.type.startsWith('image/') ? blob.type : guessImageMimeType(name)
  const file = new File([blob], name, { type })
  return {
    id: createId(),
    name,
    type,
    dataUrl: await readFileAsDataUrl(file),
    remoteUrl: resolvedURL,
    source
  }
}

function resolveBrowserReferenceURL(url: string) {
  const trimmed = url.trim()
  if (!trimmed || /^data:image\//i.test(trimmed)) {
    return trimmed
  }
  if (/^\/\//.test(trimmed) && typeof window !== 'undefined') {
    return `${window.location.protocol}${trimmed}`
  }
  if (/^\//.test(trimmed) && typeof window !== 'undefined') {
    return `${window.location.origin}${trimmed}`
  }
  return trimmed
}

function getReferenceFileName(url: string, fallback: string) {
  try {
    const parsed = new URL(resolveBrowserReferenceURL(url), typeof window !== 'undefined' ? window.location.origin : 'http://localhost')
    const name = parsed.pathname.split('/').filter(Boolean).pop()
    return name || fallback
  } catch {
    return fallback
  }
}

function createRemoteReference(url: string, index: number, source: CreativeReferenceImage['source']): CreativeReferenceImage {
  const resolvedURL = resolveBrowserReferenceURL(url)
  const fallback = `${source}-reference-${index + 1}.png`
  const name = getReferenceFileName(resolvedURL, fallback)
  return {
    id: createId(),
    name,
    type: guessImageMimeType(name),
    dataUrl: resolvedURL,
    remoteUrl: resolvedURL,
    loading: true,
    source
  }
}

async function hydrateRemoteReference(reference: CreativeReferenceImage) {
  const url = reference.remoteUrl || reference.dataUrl
  if (!url || isDataUrlReference(reference)) {
    return reference
  }
  try {
    const loaded = await loadReferenceFromURL(url, reference.name, reference.source)
    return {
      ...loaded,
      id: reference.id
    }
  } catch (err) {
    return {
      ...reference,
      loading: false,
      loadError: err instanceof Error ? err.message : '参考图加载失败'
    }
  }
}

async function hydrateReferenceImages(ids: string[]) {
  if (ids.length === 0) {
    return
  }
  const idSet = new Set(ids)
  const loaded = await Promise.all(referenceImages.value
    .filter((reference) => idSet.has(reference.id))
    .map((reference) => hydrateRemoteReference(reference)))
  const loadedById = new Map(loaded.map((reference) => [reference.id, reference]))
  referenceImages.value = referenceImages.value.map((reference) => loadedById.get(reference.id) || reference)
}

async function ensureReferencesReady() {
  const pending = referenceImages.value.filter((reference) => reference.loading || (!isDataUrlReference(reference) && reference.remoteUrl))
  if (pending.length === 0) {
    return true
  }
  referenceImages.value = referenceImages.value.map((reference) => {
    if (!pending.some((item) => item.id === reference.id)) {
      return reference
    }
    return { ...reference, loading: true, loadError: undefined }
  })
  await hydrateReferenceImages(pending.map((reference) => reference.id))
  const failed = referenceImages.value.filter((reference) => reference.loadError)
  if (failed.length > 0) {
    appStore.showWarning('部分参考图加载失败，请移除失败图片或重新套用后再提交')
    return false
  }
  return true
}

async function applyPreset(preset: ImagePromptPreset) {
  prompt.value = preset.prompt
  imageCount.value = preset.count
  applySizeFromPreset(preset.size)
  const reference = createRemoteReference(preset.imageSrc, 0, 'preset')
  referenceImages.value = [reference]
  void hydrateReferenceImages([reference.id])
}

function applyMarketPrompt(marketPrompt: BananaPrompt) {
  prompt.value = marketPrompt.prompt
  const urls = getPromptApplyReferenceImageUrls(marketPrompt)
  const references = urls.map((url, index) => createRemoteReference(url, index, 'market'))
  referenceImages.value = references
  void hydrateReferenceImages(references.map((reference) => reference.id))
}

async function handleFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || []).filter((file) => file.type.startsWith('image/'))
  if (files.length === 0) {
    input.value = ''
    return
  }
  try {
    const refs = await Promise.all(files.map(async (file) => ({
      id: createId(),
      name: file.name,
      type: file.type || 'image/png',
      dataUrl: await readFileAsDataUrl(file),
      source: 'upload' as const
    })))
    referenceImages.value = [...referenceImages.value, ...refs].slice(0, 8)
  } catch (err) {
    appStore.showError(err instanceof Error ? err.message : '读取参考图失败')
  } finally {
    input.value = ''
  }
}

function removeReference(id: string) {
  referenceImages.value = referenceImages.value.filter((item) => item.id !== id)
}

function normalizeImageCount(value: number) {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return 1
  return Math.min(4, Math.max(1, Math.round(numeric)))
}

function isDataUrlReference(reference: CreativeReferenceImage) {
  return /^data:image\//i.test(reference.dataUrl)
}

function buildReferenceFiles(references: CreativeReferenceImage[]) {
  return references.map((reference, index) => {
    const name = reference.name || `reference-${index + 1}.png`
    return dataUrlToFile(reference.dataUrl, name, reference.type)
  })
}

async function submit() {
  const text = prompt.value.trim()
  if (!text) {
    appStore.showWarning('请输入画面描述')
    return
  }
  if (!selectedApiKey.value) {
    paramsOpen.value = true
    appStore.showWarning(drawableKeys.value.length ? '请选择用于作画的 OpenAI 分组密钥' : '请先创建并绑定允许图片生成的 OpenAI 分组 API 密钥')
    return
  }
  if (!(await ensureReferencesReady())) {
    return
  }
  const size = buildImageSize(sizeSelection)
  const count = normalizeImageCount(imageCount.value)
  const conversation = ensureConversation(text)
  const now = new Date().toISOString()
  const references = referenceImages.value.map((item) => ({ ...item }))
  const turn: CreativeTurn = {
    id: createId(),
    prompt: text,
    mode: references.length > 0 ? 'edit' : 'generate',
    model: fixedCreativeImageModel,
    count,
    size,
    outputFormat: outputFormat.value,
    sizeSelection: { ...sizeSelection },
    references,
    images: [],
    status: 'generating',
    createdAt: now
  }
  addOrUpdateTurn(conversation, turn)
  prompt.value = ''
  referenceImages.value = []
  isSubmitting.value = true

  try {
    const request: CreativeImageRequest = {
      apiKey: selectedApiKey.value.key,
      prompt: text,
      model: fixedCreativeImageModel,
      count,
      size,
      outputFormat: outputFormat.value
    }
    if (references.length > 0) {
      const inlineReferences = references.filter(isDataUrlReference)
      if (inlineReferences.length === references.length) {
        request.files = buildReferenceFiles(inlineReferences)
      } else {
        request.imageUrls = references.map((item) => item.dataUrl)
      }
    }
    const images: CreativeImageResult[] = references.length > 0
      ? await createCreativeImageEdit(request)
      : await createCreativeImageGeneration(request)
    const finishedTurn: CreativeTurn = {
      ...turn,
      status: 'success',
      images: images.map((item, index): CreativeStoredImage => ({
        id: item.id || createId(),
        url: item.url,
        b64_json: item.b64_json,
        revised_prompt: item.revised_prompt,
        output_format: item.output_format,
        size: item.size || size,
        created_at: item.created_at || Date.now() + index
      }))
    }
    addOrUpdateTurn(conversation, finishedTurn)
    appStore.showSuccess('图片生成完成')
  } catch (err) {
    const failedTurn: CreativeTurn = {
      ...turn,
      status: 'error',
      error: err instanceof Error ? err.message : '图片生成失败'
    }
    addOrUpdateTurn(conversation, failedTurn)
    appStore.showError(failedTurn.error || '图片生成失败')
  } finally {
    isSubmitting.value = false
  }
}

function useResultAsReference(image: CreativeStoredImage, index: number) {
  const reference = resultToReferenceImage(image, index)
  if (!reference) {
    return
  }
  referenceImages.value = [reference]
  prompt.value = '参考这张图，生成一张同风格的新图。'
}

function maskKey(key: string) {
  if (!key) return ''
  if (key.length <= 10) return '***'
  return `${key.slice(0, 6)}...${key.slice(-4)}`
}

function formatConversationTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}
</script>

<style scoped>
.creative-shell {
  display: grid;
  min-height: calc(100vh - 8rem);
  grid-template-columns: minmax(220px, 280px) minmax(0, 1fr);
  gap: 1rem;
}

.creative-history {
  position: sticky;
  top: 5.5rem;
  height: calc(100vh - 7.5rem);
  overflow-y: auto;
  border-right: 1px solid rgb(226 232 240);
  padding-right: 1rem;
}

.dark .creative-history {
  border-right-color: rgb(31 41 55);
}

.creative-main {
  position: relative;
  display: flex;
  min-height: calc(100vh - 8rem);
  flex-direction: column;
  padding-bottom: 21rem;
}

.creative-empty {
  display: flex;
  flex: 1 1 auto;
  min-height: calc(100vh - 26rem);
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 2rem 0 1rem;
}

.creative-new-button,
.creative-send,
.creative-icon-button,
.creative-pill,
.creative-mini-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  transition: all 0.15s ease;
}

.creative-new-button {
  min-height: 2.75rem;
  flex: 1 1 auto;
  border-radius: 9999px;
  background: rgb(15 23 42);
  padding: 0 1rem;
  font-size: 0.875rem;
  font-weight: 700;
  color: white;
  box-shadow: 0 10px 25px rgb(15 23 42 / 0.16);
}

.creative-icon-button {
  height: 2.75rem;
  width: 2.75rem;
  border-radius: 9999px;
  border: 1px solid rgb(226 232 240);
  background: white;
  color: rgb(71 85 105);
  box-shadow: 0 8px 18px rgb(15 23 42 / 0.08);
}

.dark .creative-icon-button {
  border-color: rgb(55 65 81);
  background: rgb(17 24 39);
  color: rgb(203 213 225);
}

.creative-history-item {
  display: flex;
  width: 100%;
  flex-direction: column;
  align-items: flex-start;
  border-radius: 1rem;
  padding: 0.75rem 1rem;
  text-align: left;
  transition: background 0.15s ease;
}

.creative-history-item:hover,
.creative-history-item-active {
  background: rgb(241 245 249);
}

.creative-history-list {
  margin-top: 1.5rem;
  display: grid;
  gap: 0.5rem;
}

.dark .creative-history-item:hover,
.dark .creative-history-item-active {
  background: rgb(17 24 39);
}

.creative-preset {
  display: flex;
  min-height: 340px;
  flex-direction: column;
  overflow: hidden;
  border-radius: 1.375rem;
  border: 1px solid rgb(241 245 249);
  background: white;
  box-shadow: 0 4px 14px rgb(15 23 42 / 0.06);
}

.dark .creative-preset {
  border-color: rgb(31 41 55);
  background: rgb(17 24 39);
}

.creative-composer-wrap {
  position: fixed;
  bottom: max(1.5rem, env(safe-area-inset-bottom));
  left: calc(16rem + 2rem);
  right: 2rem;
  z-index: 80;
  width: auto;
  max-height: calc(100dvh - 2rem);
  pointer-events: none;
}

.creative-composer-wrap-collapsed {
  left: calc(72px + 2rem);
}

.creative-composer-wrap > * {
  pointer-events: auto;
  margin-left: auto;
  margin-right: auto;
  max-width: 980px;
}

.creative-reference-strip {
  margin-bottom: 0.75rem;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.creative-reference-loading,
.creative-reference-error {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 1rem;
  background: rgb(15 23 42 / 0.5);
  color: white;
}

.creative-reference-error {
  background: rgb(220 38 38 / 0.72);
}

.creative-composer {
  border-radius: 1.75rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255 / 0.96);
  padding: 1rem;
  box-shadow: 0 24px 70px rgb(15 23 42 / 0.18);
  backdrop-filter: blur(14px);
}

.dark .creative-composer {
  border-color: rgb(31 41 55);
  background: rgb(15 23 42 / 0.96);
}

.creative-textarea {
  min-height: 7rem;
  max-height: 15rem;
  width: 100%;
  resize: none;
  border: 0;
  background: transparent;
  padding: 0.5rem 0.75rem;
  color: rgb(15 23 42);
  overflow-y: auto;
  outline: none;
}

.dark .creative-textarea {
  color: white;
}

.creative-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border-top: 1px solid rgb(241 245 249);
  padding-top: 0.75rem;
}

.creative-toolbar-main,
.creative-toolbar-actions {
  min-width: 0;
}

.dark .creative-toolbar {
  border-top-color: rgb(31 41 55);
}

.creative-pill,
.creative-select {
  min-height: 2.25rem;
  border-radius: 9999px;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0 0.875rem;
  font-size: 0.8125rem;
  font-weight: 700;
  color: rgb(71 85 105);
}

.creative-pill-active {
  border-color: rgb(191 219 254);
  background: rgb(239 246 255);
  color: rgb(37 99 235);
}

.creative-size-select {
  min-width: 12.25rem;
}

.dark .creative-pill,
.dark .creative-select {
  border-color: rgb(55 65 81);
  background: rgb(17 24 39);
  color: rgb(226 232 240);
}

.creative-send {
  height: 2.75rem;
  width: 2.75rem;
  border-radius: 9999px;
  background: rgb(15 23 42);
  color: white;
}

.creative-send:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}

.creative-params {
  margin-top: 0.875rem;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 0.75rem;
  border-top: 1px solid rgb(241 245 249);
  padding-top: 0.875rem;
}

.dark .creative-params {
  border-top-color: rgb(31 41 55);
}

.creative-field {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.375rem;
  font-size: 0.75rem;
  font-weight: 700;
  color: rgb(100 116 139);
}

.creative-field input,
.creative-field select {
  height: 2.5rem;
  min-width: 0;
  border-radius: 0.875rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0 0.75rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: rgb(15 23 42);
  outline: none;
}

.dark .creative-field input,
.dark .creative-field select {
  border-color: rgb(55 65 81);
  background: rgb(10 15 26);
  color: white;
}

.creative-turns {
  margin: 0 auto;
  width: 100%;
  max-width: 980px;
  flex: 1 1 auto;
  padding: 0.5rem 0 2rem;
}

.creative-turn {
  border-bottom: 1px solid rgb(226 232 240);
  padding: 1.25rem 0;
}

.dark .creative-turn {
  border-bottom-color: rgb(31 41 55);
}

.creative-turn-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.75rem;
  color: rgb(100 116 139);
}

.creative-result {
  min-width: 0;
}

.creative-result-image {
  aspect-ratio: 1 / 1;
  max-height: 520px;
  min-height: 220px;
  width: 100%;
  object-fit: contain;
}

.creative-mini-button {
  height: 1.75rem;
  width: 1.75rem;
  border-radius: 9999px;
  background: rgb(241 245 249);
  color: rgb(71 85 105);
}

.dark .creative-mini-button {
  background: rgb(31 41 55);
  color: rgb(203 213 225);
}

.creative-image-skeleton {
  display: flex;
  min-height: 220px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  border-radius: 1.25rem;
  background: rgb(241 245 249);
  font-size: 0.875rem;
  font-weight: 700;
  color: rgb(100 116 139);
}

.dark .creative-image-skeleton {
  background: rgb(17 24 39);
  color: rgb(203 213 225);
}

@media (max-width: 1024px) {
  .creative-shell {
    display: block;
    min-height: auto;
  }

  .creative-history {
    position: relative;
    top: auto;
    height: auto;
    max-height: none;
    overflow: visible;
    border-right: 0;
    border-bottom: 1px solid rgb(226 232 240);
    margin-bottom: 1rem;
    padding-bottom: 1rem;
    padding-right: 0;
  }

  .creative-history-list {
    display: flex;
    gap: 0.5rem;
    overflow-x: auto;
    padding-bottom: 0.25rem;
    scroll-snap-type: x mandatory;
  }

  .creative-history-item {
    min-width: 13rem;
    scroll-snap-align: start;
  }

  .creative-main {
    min-height: auto;
    padding-bottom: 20rem;
  }

  .creative-empty {
    min-height: auto;
    justify-content: flex-start;
    padding-bottom: 18rem;
  }

  .creative-composer-wrap,
  .creative-composer-wrap-collapsed {
    bottom: max(1rem, env(safe-area-inset-bottom));
    left: 1rem;
    right: 1rem;
  }
}

@media (max-width: 768px) {
  .creative-shell {
    gap: 0;
  }

  .creative-main {
    padding-bottom: 18rem;
  }

  .creative-empty {
    align-items: stretch;
    padding: 1rem 0 17rem;
  }

  .creative-empty > * {
    margin-left: auto;
    margin-right: auto;
  }

  .creative-empty h1 {
    font-size: 2rem;
    line-height: 1.15;
    text-align: center;
  }

  .creative-empty p {
    max-width: 24rem;
    font-size: 0.9375rem;
  }

  .creative-preset {
    min-height: auto;
    border-radius: 1rem;
  }

  .creative-turns {
    padding: 0 0 1rem;
  }

  .creative-turn {
    padding: 1rem 0;
  }

  .creative-turn-meta {
    gap: 0.375rem;
    font-size: 0.6875rem;
  }

  .creative-turn p {
    font-size: 0.875rem;
    line-height: 1.75;
  }

  .creative-result-image,
  .creative-image-skeleton {
    min-height: 180px;
    max-height: 70dvh;
  }

  .creative-reference-strip {
    overflow-x: auto;
    flex-wrap: nowrap;
    padding-bottom: 0.25rem;
  }

  .creative-composer-wrap,
  .creative-composer-wrap-collapsed {
    bottom: 0;
    left: 0;
    right: 0;
    max-height: 72dvh;
    overflow-y: auto;
    padding: 0 0.75rem max(0.75rem, env(safe-area-inset-bottom));
    pointer-events: auto;
  }

  .creative-composer-wrap > * {
    max-width: none;
  }

  .creative-composer {
    border-radius: 1.25rem 1.25rem 0 0;
    padding: 0.75rem;
  }

  .creative-textarea {
    min-height: 5.5rem;
    max-height: 9rem;
    padding: 0.375rem 0.5rem;
    font-size: 0.9375rem;
    line-height: 1.65;
  }

  .creative-toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 0.625rem;
  }

  .creative-toolbar-main {
    display: flex;
    width: 100%;
  }

  .creative-toolbar-main > * {
    flex: 1 1 calc(50% - 0.25rem);
  }

  .creative-toolbar-main .creative-pill-active {
    flex: 0 0 auto;
  }

  .creative-size-select {
    min-width: 0;
    flex-basis: 100%;
    width: 100%;
  }

  .creative-toolbar-actions {
    justify-content: flex-end;
  }

  .creative-params {
    max-height: 38dvh;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    overflow-y: auto;
    gap: 0.625rem;
  }

  .creative-field input,
  .creative-field select {
    height: 2.25rem;
    border-radius: 0.75rem;
    font-size: 0.8125rem;
  }
}

@media (max-width: 480px) {
  .creative-main {
    padding-bottom: 17rem;
  }

  .creative-empty {
    padding-bottom: 16rem;
  }

  .creative-history-item {
    min-width: 11.5rem;
  }

  .creative-new-button {
    min-height: 2.5rem;
    font-size: 0.8125rem;
  }

  .creative-pill,
  .creative-select {
    min-height: 2.2rem;
    padding: 0 0.7rem;
    font-size: 0.75rem;
  }

  .creative-icon-button,
  .creative-send {
    height: 2.5rem;
    width: 2.5rem;
  }

  .creative-params {
    grid-template-columns: 1fr;
  }
}
</style>
