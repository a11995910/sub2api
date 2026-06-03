<template>
  <AppLayout>
    <div class="creative-studio">
      <aside class="creative-control-panel">
        <div class="creative-panel-header">
          <div>
            <p class="creative-kicker">gpt-image-2</p>
            <h1>创意绘图</h1>
          </div>
          <button class="creative-icon-button" title="新建创作" @click="startNewConversation">
            <Icon name="plus" size="sm" />
          </button>
        </div>

        <section class="creative-section">
          <div class="creative-section-title">
            <Icon name="edit" size="sm" />
            <span>提示词</span>
          </div>
          <textarea
            v-model="prompt"
            class="creative-prompt"
            :placeholder="referenceImages.length ? '描述参考图要如何变化、保留或重绘...' : '输入你要生成的画面、主体、风格、镜头、材质和用途...'"
            @keydown.meta.enter.prevent="submit"
            @keydown.ctrl.enter.prevent="submit"
          />
          <div class="creative-prompt-actions">
            <button class="creative-secondary-button" @click="marketOpen = true">
              <Icon name="globe" size="xs" />
              提示词市场
            </button>
            <span>{{ prompt.trim().length }} 字</span>
          </div>
        </section>

        <section class="creative-section">
          <div class="creative-section-title">
            <Icon name="key" size="sm" />
            <span>账号与产出</span>
          </div>
          <label class="creative-field">
            <span>API 密钥</span>
            <select v-model.number="selectedApiKeyId">
              <option :value="0">选择 OpenAI 图片分组密钥</option>
              <option v-for="key in drawableKeys" :key="key.id" :value="key.id">
                {{ key.name }} · {{ maskKey(key.key) }}
              </option>
            </select>
          </label>
          <div class="creative-inline-grid">
            <label class="creative-field">
              <span>数量</span>
              <input v-model.number="imageCount" min="1" max="4" type="number">
            </label>
            <label class="creative-field">
              <span>格式</span>
              <select v-model="outputFormat">
                <option v-for="item in CREATIVE_OUTPUT_FORMAT_OPTIONS" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
            </label>
          </div>
        </section>

        <section class="creative-section">
          <div class="creative-section-title">
            <Icon name="grid" size="sm" />
            <span>尺寸与分辨率</span>
          </div>
          <label class="creative-field">
            <span>快速尺寸</span>
            <select v-model="quickSizeValue">
              <option v-for="item in creativeSizeOptions" :key="item.value || 'auto'" :value="item.value">
                {{ item.label }}
              </option>
              <option v-if="showCurrentSizeOption" :value="currentImageSize">
                {{ formatImageSizeDisplay(currentImageSize) }} · {{ currentImageBillingTier }}
              </option>
            </select>
          </label>
          <div class="creative-inline-grid">
            <label class="creative-field">
              <span>尺寸模式</span>
              <select v-model="sizeSelection.mode">
                <option v-for="item in IMAGE_SIZE_MODE_OPTIONS" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
            </label>
            <label v-if="sizeSelection.mode === 'ratio'" class="creative-field">
              <span>分辨率</span>
              <select v-model="sizeSelection.resolution">
                <option v-for="item in IMAGE_RESOLUTION_OPTIONS" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
            </label>
          </div>
          <label v-if="sizeSelection.mode === 'ratio'" class="creative-field">
            <span>画幅比例</span>
            <select v-model="sizeSelection.aspectRatio">
              <option v-for="item in IMAGE_ASPECT_RATIO_OPTIONS" :key="item.value || 'auto'" :value="item.value">{{ item.label }}</option>
            </select>
          </label>
          <label v-if="sizeSelection.mode === 'ratio' && sizeSelection.aspectRatio === CUSTOM_IMAGE_ASPECT_RATIO" class="creative-field">
            <span>自定义比例</span>
            <input v-model="sizeSelection.customRatio" placeholder="16:9">
          </label>
          <div v-if="sizeSelection.mode === 'custom'" class="creative-inline-grid">
            <label class="creative-field">
              <span>宽度</span>
              <input v-model="sizeSelection.customWidth" inputmode="numeric">
            </label>
            <label class="creative-field">
              <span>高度</span>
              <input v-model="sizeSelection.customHeight" inputmode="numeric">
            </label>
          </div>
          <div class="creative-size-summary">
            <span>{{ formatImageSizeDisplay(currentImageSize) }}</span>
            <strong>{{ currentImageBillingTier }}</strong>
          </div>
        </section>

        <section class="creative-section">
          <div class="creative-section-title">
            <Icon name="image" size="sm" />
            <span>参考图</span>
          </div>
          <input ref="fileInputRef" type="file" accept="image/*" multiple class="hidden" @change="handleFileChange">
          <button class="creative-upload" @click="fileInputRef?.click()">
            <Icon name="upload" size="sm" />
            <span>上传参考图</span>
          </button>
          <div v-if="referenceImages.length" class="creative-reference-grid">
            <div v-for="reference in referenceImages" :key="reference.id" class="creative-reference-item">
              <img :src="reference.dataUrl" :alt="reference.name" referrerpolicy="no-referrer">
              <div v-if="reference.loading" class="creative-reference-overlay">
                <Icon name="refresh" size="xs" class="animate-spin" />
              </div>
              <div v-else-if="reference.loadError" class="creative-reference-overlay creative-reference-overlay-error" title="参考图加载失败">
                <Icon name="x" size="xs" />
              </div>
              <button title="移除参考图" @click="removeReference(reference.id)">
                <Icon name="x" size="xs" />
              </button>
            </div>
          </div>
          <p v-else class="creative-muted">无参考图时走文生图；添加参考图后自动走图片编辑接口。</p>
        </section>

        <button class="creative-generate-button" :disabled="isSubmitting || hasLoadingReferences" @click="submit">
          <Icon v-if="isSubmitting || hasLoadingReferences" name="refresh" size="sm" class="animate-spin" />
          <Icon v-else name="sparkles" size="sm" />
          {{ isSubmitting ? '提交中...' : '开始生成' }}
        </button>
        <p class="creative-cost">{{ estimatedConsumptionLabel }}</p>

        <section class="creative-section creative-history-section">
          <div class="creative-section-title creative-history-title">
            <span><Icon name="clock" size="sm" /> 最近任务</span>
            <button title="清空历史" @click="clearConversations">
              <Icon name="trash" size="xs" />
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
              <span>{{ conversation.title }}</span>
              <small>{{ getConversationSummary(conversation) }} · {{ formatConversationTime(conversation.updatedAt) }}</small>
            </button>
            <p v-if="conversations.length === 0" class="creative-muted">提交第一张图后会在这里保存本地历史。</p>
          </div>
        </section>
      </aside>

      <section class="creative-canvas">
        <div class="creative-canvas-topbar">
          <div>
            <p class="creative-kicker">结果展示区</p>
            <h2>{{ activeConversation?.title || '等待生成' }}</h2>
          </div>
          <div class="creative-topbar-actions">
            <button class="creative-secondary-button" @click="syncCreativeDrawingTasks">
              <Icon name="refresh" size="xs" />
              同步任务
            </button>
            <button class="creative-secondary-button" @click="startNewConversation">
              <Icon name="plus" size="xs" />
              新建
            </button>
          </div>
        </div>

        <div v-if="activeConversation?.turns.length" class="creative-turns">
          <article v-for="turn in activeConversation.turns" :key="turn.id" class="creative-turn">
            <header class="creative-turn-header">
              <div class="creative-turn-meta">
                <span>{{ turn.mode === 'edit' ? '参考图作画' : '文生图' }}</span>
                <span>{{ formatImageSizeDisplay(turn.size) }}</span>
                <span>{{ turn.count }} 张</span>
                <span>{{ formatConversationTime(turn.createdAt) }}</span>
              </div>
              <button class="creative-mini-action" title="重试这个提示词" @click="retryTurnPrompt(turn)">
                <Icon name="refresh" size="xs" />
                重试
              </button>
            </header>
            <p class="creative-turn-prompt">{{ turn.prompt }}</p>

            <div v-if="turn.references.length" class="creative-turn-references">
              <img
                v-for="reference in turn.references"
                :key="reference.id"
                :src="reference.dataUrl"
                :alt="reference.name"
                referrerpolicy="no-referrer"
              >
            </div>

            <div v-if="turn.status === 'generating'" class="creative-results-grid">
              <div v-for="index in Math.max(turn.count, 1)" :key="index" class="creative-image-skeleton">
                <Icon name="sparkles" size="lg" class="animate-pulse" />
                <span>生成中</span>
              </div>
            </div>

            <div v-else-if="turn.status === 'error'" class="creative-error">
              {{ turn.error || '生成失败' }}
            </div>

            <div v-else class="creative-results-grid">
              <figure v-for="(image, index) in turn.images" :key="image.id" class="creative-result">
                <button class="creative-result-preview" @click="previewImage = buildStoredImageUrl(image)">
                  <img :src="buildStoredImageUrl(image)" :alt="turn.prompt" referrerpolicy="no-referrer">
                </button>
                <figcaption>
                  <span>{{ image.revised_prompt || image.size || `结果 ${index + 1}` }}</span>
                  <div>
                    <button title="下载图片" @click="downloadImage(image, index)">
                      <Icon name="download" size="xs" />
                    </button>
                    <button title="加入参考图" @click="useResultAsReference(image, index)">
                      <Icon name="image" size="xs" />
                    </button>
                    <button title="重试提示词" @click="retryTurnPrompt(turn)">
                      <Icon name="refresh" size="xs" />
                    </button>
                    <a title="打开图片" :href="buildStoredImageUrl(image)" target="_blank" rel="noreferrer">
                      <Icon name="externalLink" size="xs" />
                    </a>
                  </div>
                </figcaption>
              </figure>
            </div>
          </article>
        </div>

        <div v-else class="creative-empty-state">
          <Icon name="image" size="xl" />
          <h2>右侧会展示生成结果</h2>
          <p>在左侧选择 Key、分辨率、尺寸模式并输入提示词。生成完成后可下载、重试或把结果加入参考图继续创作。</p>
        </div>
      </section>
    </div>

    <PromptMarketDialog v-model:open="marketOpen" @apply="applyMarketPrompt" />

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
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores'
import keysAPI from '@/api/keys'
import type { ApiKey } from '@/types'
import {
  CREATIVE_OUTPUT_FORMAT_OPTIONS,
  createCreativeDrawingTask,
  getCreativeDrawingTask,
  listCreativeDrawingTasks,
  type CreativeDrawingTask,
  type CreativeImageModel,
  type CreativeOutputFormat
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
import PromptMarketDialog from '@/features/creativeDrawing/PromptMarketDialog.vue'
import { getPromptApplyReferenceImageUrls, type BananaPrompt } from '@/features/creativeDrawing/promptMarket'
import {
  buildConversationTitle,
  buildStoredImageUrl,
  createId,
  hydrateCreativeConversationImages,
  loadActiveCreativeConversationId,
  loadCreativeConversations,
  loadHiddenCreativeDrawingBefore,
  loadHiddenCreativeDrawingTaskIds,
  persistCreativeStoredImages,
  readFileAsDataUrl,
  resultToReferenceImage,
  saveActiveCreativeConversationId,
  saveCreativeConversations,
  saveHiddenCreativeDrawingBefore,
  saveHiddenCreativeDrawingTaskIds,
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
const marketOpen = ref(false)
const isSubmitting = ref(false)
const previewImage = ref('')
const referenceImages = ref<CreativeReferenceImage[]>([])
const apiKeys = ref<ApiKey[]>([])
const selectedApiKeyId = ref(0)
const fileInputRef = ref<HTMLInputElement | null>(null)
let taskSyncTimer: number | null = null
let isTaskSyncing = false
const hiddenTaskIds = ref<Set<string>>(new Set())
const hiddenTaskBefore = ref('')
const notifiedTaskIds = new Set<string>()

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
  hiddenTaskIds.value = new Set(loadHiddenCreativeDrawingTaskIds())
  hiddenTaskBefore.value = loadHiddenCreativeDrawingBefore()
  conversations.value = normalizeSingleTurnConversations(loadCreativeConversations())
  activeConversationId.value = loadActiveCreativeConversationId() || conversations.value[0]?.id || ''
  conversations.value = await hydrateCreativeConversationImages(conversations.value)
  await loadApiKeys()
  await syncCreativeDrawingTasks()
  startTaskSyncTimer()
})

onBeforeUnmount(() => {
  if (taskSyncTimer) {
    window.clearInterval(taskSyncTimer)
    taskSyncTimer = null
  }
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

function normalizeSingleTurnConversations(items: CreativeConversation[]) {
  return items.flatMap((conversation) => {
    if (conversation.turns.length <= 1) {
      return [conversation]
    }
    return conversation.turns.map((turn) => ({
      id: turn.taskId || turn.id || createId(),
      title: buildConversationTitle(turn.prompt),
      createdAt: turn.createdAt || conversation.createdAt,
      updatedAt: turn.createdAt || conversation.updatedAt,
      turns: [turn]
    }))
  })
}

function clearConversations() {
  const nextHiddenIds = new Set(hiddenTaskIds.value)
  conversations.value.forEach((conversation) => {
    conversation.turns.forEach((turn) => {
      if (turn.taskId) {
        nextHiddenIds.add(turn.taskId)
      }
    })
  })
  hiddenTaskIds.value = nextHiddenIds
  hiddenTaskBefore.value = new Date().toISOString()
  saveHiddenCreativeDrawingTaskIds(Array.from(nextHiddenIds))
  saveHiddenCreativeDrawingBefore(hiddenTaskBefore.value)
  conversations.value = []
  activeConversationId.value = ''
  prompt.value = ''
  referenceImages.value = []
  imageCount.value = 1
  applySizeFromPreset('')
}

function createTaskConversation(seedPrompt: string) {
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
    const turns = item.turns.some((existing) => existing.id === turn.id || existing.taskId === turn.taskId)
      ? item.turns.map((existing) => (existing.id === turn.id || existing.taskId === turn.taskId) ? turn : existing)
      : [turn]
    return {
      ...item,
      title: buildConversationTitle(turn.prompt),
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

async function submit() {
  const text = prompt.value.trim()
  if (!text) {
    appStore.showWarning('请输入画面描述')
    return
  }
  if (!selectedApiKey.value) {
    appStore.showWarning(drawableKeys.value.length ? '请选择用于作画的 OpenAI 分组密钥' : '请先创建并绑定允许图片生成的 OpenAI 分组 API 密钥')
    return
  }
  if (!(await ensureReferencesReady())) {
    return
  }
  const size = buildImageSize(sizeSelection)
  const count = normalizeImageCount(imageCount.value)
  const conversation = createTaskConversation(text)
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
    const task = await createCreativeDrawingTask({
      api_key_id: selectedApiKey.value.id,
      conversation_id: conversation.id,
      turn_id: turn.id,
      mode: turn.mode,
      prompt: text,
      model: fixedCreativeImageModel,
      count,
      size,
      output_format: outputFormat.value,
      reference_images: references.map((item) => ({
        id: item.id,
        name: item.name,
        type: item.type,
        data_url: item.dataUrl,
        remote_url: item.remoteUrl,
        source: item.source
      }))
    })
    await applyCreativeDrawingTask(task, { notify: false })
    void pollCreativeDrawingTask(task.id, true)
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

function startTaskSyncTimer() {
  if (taskSyncTimer) {
    return
  }
  taskSyncTimer = window.setInterval(() => {
    if (hasGeneratingTasks()) {
      void syncCreativeDrawingTasks()
    }
  }, 3500)
}

function hasGeneratingTasks() {
  return conversations.value.some((conversation) =>
    conversation.turns.some((turn) => turn.status === 'generating' && turn.taskId)
  )
}

async function syncCreativeDrawingTasks() {
  if (isTaskSyncing) {
    return
  }
  isTaskSyncing = true
  try {
    const tasks = await listCreativeDrawingTasks(80)
    for (const task of tasks) {
      if (shouldHideCreativeTask(task)) {
        continue
      }
      await applyCreativeDrawingTask(task, { notify: false })
    }
  } catch {
    // 同步失败不打断页面使用，下一轮轮询会继续尝试。
  } finally {
    isTaskSyncing = false
  }
}

async function pollCreativeDrawingTask(taskId: string, notify = true) {
  if (!taskId) {
    return
  }
  try {
    const task = await getCreativeDrawingTask(taskId)
    await applyCreativeDrawingTask(task, { notify })
    if (task.status === 'queued' || task.status === 'running') {
      window.setTimeout(() => void pollCreativeDrawingTask(taskId, notify), 3500)
    }
  } catch {
    window.setTimeout(() => void pollCreativeDrawingTask(taskId, notify), 5000)
  }
}

async function applyCreativeDrawingTask(task: CreativeDrawingTask, options: { notify: boolean }) {
  if (shouldHideCreativeTask(task)) {
    return
  }
  const conversation = ensureTaskConversation(task)
  const existingTurn = conversation.turns.find((turn) => turn.id === task.turn_id || turn.taskId === task.id)
  const baseTurn = existingTurn || buildTurnFromTask(task)
  const nextTurn: CreativeTurn = {
    ...baseTurn,
    id: task.turn_id || baseTurn.id,
    taskId: task.id,
    status: task.status === 'success' && task.images.length === 0 ? 'generating' : task.status === 'success' ? 'success' : task.status === 'error' ? 'error' : 'generating',
    error: task.status === 'error' ? task.error || '图片生成失败' : undefined
  }
  if (task.status === 'success') {
    const hasResultPayload = task.images.some((item) => item.url || item.source_url || item.b64_json)
    if (!hasResultPayload) {
      void pollCreativeDrawingTask(task.id, options.notify)
      addOrUpdateTurn(conversation, nextTurn)
      return
    }
    const storedImages = task.images.map((item, index): CreativeStoredImage => ({
      id: item.id || createId(),
      url: item.url || '',
      source_url: item.source_url,
      b64_json: item.b64_json,
      revised_prompt: item.revised_prompt,
      output_format: item.output_format,
      size: item.size || task.size || '',
      created_at: item.created_at || Date.now() + index
    }))
    await persistCreativeStoredImages(storedImages)
    nextTurn.images = storedImages
  }
  addOrUpdateTurn(conversation, nextTurn)
  if (options.notify && task.status === 'error' && !notifiedTaskIds.has(task.id)) {
    notifiedTaskIds.add(task.id)
    appStore.showError(nextTurn.error || '图片生成失败')
  }
}

function shouldHideCreativeTask(task: CreativeDrawingTask) {
  if (hiddenTaskIds.value.has(task.id)) {
    return true
  }
  if (!hiddenTaskBefore.value) {
    return false
  }
  const clearTime = new Date(hiddenTaskBefore.value).getTime()
  const taskTime = new Date(task.created_at).getTime()
  return Number.isFinite(clearTime) && Number.isFinite(taskTime) && taskTime <= clearTime
}

function ensureTaskConversation(task: CreativeDrawingTask) {
  const id = task.id
  const existing = conversations.value.find((item) => item.id === id)
  if (existing) {
    return existing
  }
  const createdAt = task.created_at || new Date().toISOString()
  const conversation: CreativeConversation = {
    id,
    title: buildConversationTitle(task.prompt),
    createdAt,
    updatedAt: task.updated_at || createdAt,
    turns: []
  }
  conversations.value = [conversation, ...conversations.value]
  if (!activeConversationId.value) {
    activeConversationId.value = conversation.id
  }
  return conversation
}

function buildTurnFromTask(task: CreativeDrawingTask): CreativeTurn {
  const output = normalizeTaskOutputFormat(task.output_format)
  const size = task.size || ''
  return {
    id: task.turn_id || createId(),
    taskId: task.id,
    prompt: task.prompt,
    mode: task.mode,
    model: task.model,
    count: task.count || Math.max(task.images.length, 1),
    size,
    outputFormat: output,
    sizeSelection: getImageSizeSelectionFromSize(size),
    references: (task.reference_images || []).map((item) => ({
      id: item.id || createId(),
      name: item.name || 'reference.png',
      type: item.type || 'image/png',
      dataUrl: item.data_url,
      remoteUrl: item.remote_url,
      source: normalizeReferenceSource(item.source)
    })),
    images: [],
    status: task.status === 'success' ? 'success' : task.status === 'error' ? 'error' : 'generating',
    error: task.error,
    createdAt: task.created_at || new Date().toISOString()
  }
}

function getConversationSummary(conversation: CreativeConversation) {
  const turn = conversation.turns[0]
  if (!turn) {
    return '空记录'
  }
  const count = Math.max(turn.images.length, turn.count || 1)
  const status = turn.status === 'success' ? '成功' : turn.status === 'error' ? '失败' : '生成中'
  return `${status} · ${count} 张`
}

function normalizeTaskOutputFormat(value?: string): CreativeOutputFormat {
  if (value === 'jpeg' || value === 'webp' || value === 'png') {
    return value
  }
  return 'png'
}

function normalizeReferenceSource(value?: string): CreativeReferenceImage['source'] {
  if (value === 'upload' || value === 'market' || value === 'preset' || value === 'result') {
    return value
  }
  return 'upload'
}

function useResultAsReference(image: CreativeStoredImage, index: number) {
  const reference = resultToReferenceImage(image, index)
  if (!reference) {
    return
  }
  referenceImages.value = [reference]
  prompt.value = '参考这张图，生成一张同风格的新图。'
  appStore.showSuccess('已加入参考图')
}

function retryTurnPrompt(turn: CreativeTurn) {
  prompt.value = turn.prompt
  imageCount.value = normalizeImageCount(turn.count)
  outputFormat.value = turn.outputFormat
  Object.assign(sizeSelection, turn.sizeSelection || getImageSizeSelectionFromSize(turn.size))
  referenceImages.value = turn.references.map((reference) => ({ ...reference }))
}

function getImageDownloadName(image: CreativeStoredImage, index: number) {
  const format = image.output_format === 'jpeg' ? 'jpg' : image.output_format || outputFormat.value || 'png'
  return `creative-drawing-${index + 1}.${format}`
}

async function downloadImage(image: CreativeStoredImage, index: number) {
  const url = buildStoredImageUrl(image)
  if (!url) {
    appStore.showWarning('当前图片没有可下载地址')
    return
  }
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = getImageDownloadName(image, index)
    document.body.appendChild(link)
    link.click()
    link.remove()
  } catch (err) {
    appStore.showError(err instanceof Error ? err.message : '下载图片失败')
  }
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
.creative-studio {
  display: grid;
  min-height: calc(100vh - 7.5rem);
  grid-template-columns: minmax(320px, 380px) minmax(0, 1fr);
  gap: 1rem;
}

.creative-control-panel,
.creative-canvas {
  min-width: 0;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255 / 0.96);
  box-shadow: 0 16px 40px rgb(15 23 42 / 0.06);
}

.dark .creative-control-panel,
.dark .creative-canvas {
  border-color: rgb(31 41 55);
  background: rgb(15 23 42 / 0.96);
}

.creative-control-panel {
  position: sticky;
  top: 5rem;
  display: flex;
  max-height: calc(100vh - 6rem);
  flex-direction: column;
  overflow-y: auto;
  border-radius: 0.5rem;
  padding: 1rem;
}

.creative-canvas {
  display: flex;
  min-height: calc(100vh - 7.5rem);
  flex-direction: column;
  border-radius: 0.5rem;
  padding: 1rem;
}

.creative-panel-header,
.creative-canvas-topbar,
.creative-turn-header,
.creative-history-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.creative-panel-header h1,
.creative-canvas-topbar h2,
.creative-empty-state h2 {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 800;
  color: rgb(15 23 42);
}

.dark .creative-panel-header h1,
.dark .creative-canvas-topbar h2,
.dark .creative-empty-state h2 {
  color: white;
}

.creative-kicker {
  margin: 0 0 0.25rem;
  font-size: 0.72rem;
  font-weight: 800;
  letter-spacing: 0;
  color: rgb(37 99 235);
  text-transform: uppercase;
}

.creative-section {
  margin-top: 0.875rem;
  border-top: 1px solid rgb(241 245 249);
  padding-top: 0.875rem;
}

.dark .creative-section {
  border-top-color: rgb(31 41 55);
}

.creative-section-title,
.creative-section-title span {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
}

.creative-section-title {
  margin-bottom: 0.625rem;
  font-size: 0.83rem;
  font-weight: 800;
  color: rgb(51 65 85);
}

.dark .creative-section-title {
  color: rgb(226 232 240);
}

.creative-icon-button,
.creative-secondary-button,
.creative-generate-button,
.creative-upload,
.creative-mini-action,
.creative-result figcaption button,
.creative-result figcaption a,
.creative-history-title button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  border-radius: 0.5rem;
  transition: background 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}

.creative-icon-button {
  height: 2.25rem;
  width: 2.25rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  color: rgb(71 85 105);
}

.creative-secondary-button,
.creative-mini-action {
  min-height: 2.25rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0 0.75rem;
  font-size: 0.78rem;
  font-weight: 800;
  color: rgb(71 85 105);
}

.creative-secondary-button:hover,
.creative-icon-button:hover,
.creative-mini-action:hover {
  border-color: rgb(191 219 254);
  color: rgb(37 99 235);
}

.dark .creative-icon-button,
.dark .creative-secondary-button,
.dark .creative-mini-action {
  border-color: rgb(55 65 81);
  background: rgb(17 24 39);
  color: rgb(203 213 225);
}

.creative-prompt {
  min-height: 11rem;
  width: 100%;
  resize: vertical;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.875rem;
  font-size: 0.92rem;
  line-height: 1.7;
  color: rgb(15 23 42);
  outline: none;
}

.creative-prompt:focus,
.creative-field input:focus,
.creative-field select:focus {
  border-color: rgb(37 99 235);
  box-shadow: 0 0 0 3px rgb(37 99 235 / 0.12);
}

.dark .creative-prompt {
  border-color: rgb(55 65 81);
  background: rgb(10 15 26);
  color: white;
}

.creative-prompt-actions,
.creative-size-summary,
.creative-cost {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  color: rgb(100 116 139);
}

.creative-prompt-actions {
  margin-top: 0.5rem;
  font-size: 0.75rem;
}

.creative-field {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.375rem;
  font-size: 0.76rem;
  font-weight: 800;
  color: rgb(100 116 139);
}

.creative-field + .creative-field,
.creative-inline-grid + .creative-field,
.creative-field + .creative-inline-grid {
  margin-top: 0.625rem;
}

.creative-field input,
.creative-field select {
  height: 2.45rem;
  min-width: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0 0.7rem;
  font-size: 0.86rem;
  font-weight: 650;
  color: rgb(15 23 42);
  outline: none;
}

.dark .creative-field input,
.dark .creative-field select {
  border-color: rgb(55 65 81);
  background: rgb(10 15 26);
  color: white;
}

.creative-inline-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 0.625rem;
}

.creative-size-summary {
  margin-top: 0.625rem;
  border-radius: 0.5rem;
  background: rgb(239 246 255);
  padding: 0.625rem 0.75rem;
  font-size: 0.82rem;
  font-weight: 800;
  color: rgb(30 64 175);
}

.dark .creative-size-summary {
  background: rgb(30 58 138 / 0.22);
  color: rgb(191 219 254);
}

.creative-upload {
  width: 100%;
  min-height: 3rem;
  border: 1px dashed rgb(148 163 184);
  background: rgb(248 250 252);
  font-size: 0.88rem;
  font-weight: 800;
  color: rgb(71 85 105);
}

.dark .creative-upload {
  border-color: rgb(71 85 105);
  background: rgb(10 15 26);
  color: rgb(203 213 225);
}

.creative-reference-grid,
.creative-turn-references {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.creative-reference-grid {
  margin-top: 0.625rem;
}

.creative-reference-item {
  position: relative;
  height: 4.5rem;
  width: 4.5rem;
  overflow: hidden;
  border-radius: 0.5rem;
  background: rgb(241 245 249);
}

.creative-reference-item img,
.creative-turn-references img {
  height: 100%;
  width: 100%;
  object-fit: cover;
}

.creative-reference-item button {
  position: absolute;
  right: 0.25rem;
  top: 0.25rem;
  border-radius: 9999px;
  background: rgb(15 23 42 / 0.72);
  padding: 0.2rem;
  color: white;
}

.creative-reference-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(15 23 42 / 0.5);
  color: white;
}

.creative-reference-overlay-error {
  background: rgb(220 38 38 / 0.74);
}

.creative-muted {
  margin: 0;
  font-size: 0.8rem;
  line-height: 1.6;
  color: rgb(100 116 139);
}

.creative-generate-button {
  margin-top: 0.875rem;
  min-height: 2.8rem;
  width: 100%;
  background: rgb(15 23 42);
  font-size: 0.95rem;
  font-weight: 900;
  color: white;
}

.creative-generate-button:disabled {
  cursor: not-allowed;
  opacity: 0.62;
}

.dark .creative-generate-button {
  background: rgb(37 99 235);
}

.creative-cost {
  margin: 0.5rem 0 0;
  justify-content: center;
  font-size: 0.75rem;
}

.creative-history-section {
  padding-bottom: 0.25rem;
}

.creative-history-title button {
  height: 1.8rem;
  width: 1.8rem;
  color: rgb(100 116 139);
}

.creative-history-list {
  display: grid;
  gap: 0.5rem;
}

.creative-history-item {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.25rem;
  border-radius: 0.5rem;
  padding: 0.65rem 0.75rem;
  text-align: left;
}

.creative-history-item span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.86rem;
  font-weight: 800;
  color: rgb(51 65 85);
}

.creative-history-item small {
  font-size: 0.72rem;
  color: rgb(100 116 139);
}

.creative-history-item:hover,
.creative-history-item-active {
  background: rgb(241 245 249);
}

.dark .creative-history-item:hover,
.dark .creative-history-item-active {
  background: rgb(17 24 39);
}

.dark .creative-history-item span {
  color: rgb(226 232 240);
}

.creative-canvas-topbar {
  border-bottom: 1px solid rgb(241 245 249);
  padding-bottom: 0.875rem;
}

.dark .creative-canvas-topbar {
  border-bottom-color: rgb(31 41 55);
}

.creative-topbar-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.5rem;
}

.creative-turns {
  display: grid;
  gap: 1rem;
  padding-top: 1rem;
}

.creative-turn {
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  padding: 0.875rem;
}

.dark .creative-turn {
  border-color: rgb(31 41 55);
}

.creative-turn-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}

.creative-turn-meta span {
  border-radius: 9999px;
  background: rgb(241 245 249);
  padding: 0.25rem 0.55rem;
  font-size: 0.72rem;
  font-weight: 800;
  color: rgb(71 85 105);
}

.dark .creative-turn-meta span {
  background: rgb(17 24 39);
  color: rgb(203 213 225);
}

.creative-turn-prompt {
  margin: 0.75rem 0 0;
  white-space: pre-wrap;
  font-size: 0.92rem;
  line-height: 1.8;
  color: rgb(51 65 85);
}

.dark .creative-turn-prompt {
  color: rgb(226 232 240);
}

.creative-turn-references {
  margin-top: 0.75rem;
}

.creative-turn-references img {
  height: 4rem;
  width: 4rem;
  border-radius: 0.5rem;
}

.creative-results-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 0.875rem;
  margin-top: 0.875rem;
}

.creative-image-skeleton,
.creative-result-preview {
  display: flex;
  min-height: 18rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(241 245 249);
}

.dark .creative-image-skeleton,
.dark .creative-result-preview {
  background: rgb(17 24 39);
}

.creative-image-skeleton {
  flex-direction: column;
  gap: 0.75rem;
  font-size: 0.88rem;
  font-weight: 800;
  color: rgb(37 99 235);
}

.creative-result {
  min-width: 0;
}

.creative-result-preview {
  width: 100%;
  overflow: hidden;
}

.creative-result-preview img {
  max-height: 34rem;
  width: 100%;
  object-fit: contain;
}

.creative-result figcaption {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-top: 0.55rem;
  color: rgb(100 116 139);
}

.creative-result figcaption span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.78rem;
}

.creative-result figcaption div {
  display: flex;
  flex-shrink: 0;
  gap: 0.25rem;
}

.creative-result figcaption button,
.creative-result figcaption a {
  height: 1.85rem;
  width: 1.85rem;
  background: rgb(241 245 249);
  color: rgb(71 85 105);
}

.dark .creative-result figcaption button,
.dark .creative-result figcaption a {
  background: rgb(31 41 55);
  color: rgb(203 213 225);
}

.creative-error {
  margin-top: 0.875rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(254 202 202);
  background: rgb(254 242 242);
  padding: 0.875rem;
  font-size: 0.88rem;
  line-height: 1.7;
  color: rgb(185 28 28);
}

.dark .creative-error {
  border-color: rgb(127 29 29);
  background: rgb(127 29 29 / 0.2);
  color: rgb(254 202 202);
}

.creative-empty-state {
  display: flex;
  flex: 1 1 auto;
  min-height: 28rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  text-align: center;
  color: rgb(100 116 139);
}

.creative-empty-state p {
  max-width: 34rem;
  margin: 0;
  font-size: 0.92rem;
  line-height: 1.75;
}

@media (max-width: 1180px) {
  .creative-studio {
    grid-template-columns: minmax(290px, 340px) minmax(0, 1fr);
  }
}

@media (max-width: 980px) {
  .creative-studio {
    display: block;
  }

  .creative-control-panel {
    position: relative;
    top: auto;
    max-height: none;
    margin-bottom: 1rem;
  }

  .creative-canvas {
    min-height: 32rem;
  }
}

@media (max-width: 640px) {
  .creative-control-panel,
  .creative-canvas {
    padding: 0.75rem;
  }

  .creative-panel-header,
  .creative-canvas-topbar,
  .creative-turn-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .creative-topbar-actions,
  .creative-secondary-button,
  .creative-mini-action {
    width: 100%;
  }

  .creative-inline-grid {
    grid-template-columns: 1fr;
  }

  .creative-results-grid {
    grid-template-columns: 1fr;
  }

  .creative-image-skeleton,
  .creative-result-preview {
    min-height: 14rem;
  }

  .creative-result figcaption {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
