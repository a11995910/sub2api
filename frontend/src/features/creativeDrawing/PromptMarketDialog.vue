<template>
  <teleport to="body">
    <transition name="fade">
      <div v-if="open" class="fixed inset-0 z-[1000] bg-black/40 p-3 backdrop-blur-sm sm:p-8" @click.self="emit('update:open', false)">
        <section class="mx-auto flex h-[min(94dvh,860px)] max-w-[1180px] flex-col overflow-hidden rounded-[28px] bg-white shadow-2xl dark:bg-dark-900">
          <header class="border-b border-gray-100 px-5 py-4 sm:px-7">
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0">
                <h2 class="text-2xl font-bold text-gray-950 dark:text-white">热门模板</h2>
                <p class="mt-2 hidden text-sm leading-6 text-gray-600 dark:text-dark-300 sm:block">
                  来自
                  <a :href="BANANA_PROMPTS_SOURCE_URL" target="_blank" rel="noreferrer" class="font-semibold text-blue-600 hover:underline">glidea/banana-prompt-quicker</a>
                  和
                  <a :href="AWESOME_GPT_IMAGE_2_PROMPTS_SOURCE_URL" target="_blank" rel="noreferrer" class="font-semibold text-blue-600 hover:underline">EvoLinkAI/awesome-gpt-image-2-prompts</a>
                  ，可按来源筛选并一键套用到当前生图输入框。
                </p>
              </div>
              <div class="flex shrink-0 items-center gap-2">
                <span class="rounded-full bg-gray-100 px-3 py-1 text-xs font-medium text-gray-500 dark:bg-dark-800 dark:text-dark-300">
                  {{ countLabel }}
                </span>
                <button class="rounded-full p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:hover:bg-dark-800 dark:hover:text-white" title="关闭" @click="emit('update:open', false)">
                  <Icon name="x" size="md" />
                </button>
              </div>
            </div>
          </header>

          <div class="border-b border-gray-100 px-5 py-3 sm:px-7 dark:border-dark-800">
            <div class="grid gap-2 md:grid-cols-[minmax(220px,1fr)_160px]">
              <div class="relative">
                <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
                <input
                  v-model="keyword"
                  class="h-11 w-full rounded-2xl border border-gray-200 bg-white pl-10 pr-3 text-sm outline-none transition focus:border-blue-500 focus:ring-4 focus:ring-blue-100 dark:border-dark-700 dark:bg-dark-950 dark:text-white dark:focus:ring-blue-900/30"
                  placeholder="搜索标题、作者、分类或提示词"
                >
              </div>
              <div class="flex h-11 rounded-full bg-gray-100 p-1 dark:bg-dark-800">
                <button class="market-tab" :class="{ 'market-tab-active': favoriteFilter === 'all' }" @click="favoriteFilter = 'all'">全部</button>
                <button class="market-tab" :class="{ 'market-tab-active': favoriteFilter === 'favorites' }" @click="favoriteFilter = 'favorites'">
                  <Icon name="sparkles" size="xs" />
                  收藏 {{ favoriteItems.length || '' }}
                </button>
              </div>
            </div>

            <div class="mt-2 grid gap-2 md:grid-cols-[minmax(180px,1fr)_120px_minmax(160px,1fr)_130px_140px]">
              <select v-model="source" class="market-select">
                <option value="all">全部来源</option>
                <option v-for="item in PROMPT_MARKET_SOURCE_OPTIONS" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
              <select v-model="promptLanguage" class="market-select">
                <option value="zh-CN">中文</option>
                <option value="en">English</option>
              </select>
              <select v-model="category" class="market-select">
                <option :value="ALL_CATEGORY_VALUE">全部分类</option>
                <option v-for="item in categories" :key="item" :value="item">{{ item }}</option>
              </select>
              <select v-model="mode" class="market-select">
                <option value="all">全部模式</option>
                <option value="generate">文生图</option>
                <option value="edit">编辑</option>
              </select>
              <select v-model="nsfwFilter" class="market-select">
                <option value="safe">隐藏 NSFW</option>
                <option value="include">包含 NSFW</option>
                <option value="only">仅 NSFW</option>
              </select>
            </div>
          </div>

          <div ref="scrollRef" class="min-h-0 flex-1 overflow-y-auto bg-gray-50 px-5 py-4 dark:bg-dark-950 sm:px-7">
            <div v-if="isLoading" class="flex h-full min-h-[320px] flex-col items-center justify-center gap-3 text-gray-500 dark:text-dark-300">
              <Icon name="refresh" size="lg" class="animate-spin text-blue-600" />
              <p class="text-sm">正在读取远程热门模板...</p>
            </div>

            <div v-else-if="error" class="flex h-full min-h-[320px] flex-col items-center justify-center gap-4 text-center">
              <p class="max-w-md text-sm leading-6 text-gray-600 dark:text-dark-300">{{ error }}</p>
              <button class="rounded-full border border-gray-200 bg-white px-4 py-2 text-sm font-semibold text-gray-700 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-white" @click="loadPromptData">
                重新加载
              </button>
            </div>

            <div v-else-if="visiblePrompts.length === 0" class="flex h-full min-h-[320px] items-center justify-center text-sm text-gray-500 dark:text-dark-400">
              {{ favoriteFilter === 'favorites' ? '没有匹配的收藏提示词' : '没有找到匹配的提示词' }}
            </div>

            <div v-else class="space-y-4">
              <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
                <article v-for="prompt in visiblePrompts" :key="prompt.id" class="group flex min-h-[430px] flex-col overflow-hidden rounded-[22px] border border-gray-100 bg-white shadow-sm transition hover:-translate-y-0.5 hover:shadow-lg dark:border-dark-800 dark:bg-dark-900">
                  <div class="relative aspect-[16/10] overflow-hidden bg-gray-100 dark:bg-dark-800">
                    <img :src="prompt.preview" :alt="prompt.title" class="h-full w-full object-cover transition duration-300 group-hover:scale-[1.03]" loading="lazy">
                    <div class="absolute left-3 top-3 max-w-[calc(100%-1.5rem)] rounded-full bg-black/45 px-2 py-1 text-xs font-medium text-white backdrop-blur">
                      {{ prompt.author || prompt.sourceLabel }}
                    </div>
                    <div class="absolute inset-x-0 bottom-0 flex flex-wrap items-center gap-1.5 bg-gradient-to-t from-black/70 via-black/25 to-transparent px-3 pb-2 pt-8">
                      <span class="market-badge bg-white text-gray-900">{{ prompt.mode === 'edit' ? '编辑' : '文生图' }}</span>
                      <span class="market-badge bg-white/20 text-white backdrop-blur">{{ prompt.category }}</span>
                      <span v-if="prompt.isNsfw" class="market-badge bg-white/20 text-white backdrop-blur">NSFW</span>
                      <span v-if="prompt.referenceImageUrls.length" class="market-badge bg-white/20 text-white backdrop-blur">{{ prompt.referenceImageUrls.length }} 张参考图</span>
                    </div>
                  </div>
                  <div class="flex flex-1 flex-col gap-3 p-4">
                    <div class="flex min-w-0 items-start justify-between gap-2">
                      <div class="min-w-0">
                        <h3 class="truncate text-base font-semibold text-gray-950 dark:text-white">{{ prompt.title }}</h3>
                        <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                          {{ [prompt.subCategory, formatPromptDate(prompt.created)].filter(Boolean).join(' / ') || prompt.sourceLabel }}
                        </p>
                      </div>
                      <button
                        class="rounded-full border border-gray-200 p-2 text-gray-500 transition hover:bg-gray-50 hover:text-blue-600 dark:border-dark-700 dark:hover:bg-dark-800"
                        :class="{ 'border-blue-200 bg-blue-50 text-blue-600 dark:border-blue-900/50 dark:bg-blue-950/30': favoriteIds.has(promptFavoriteKey(prompt)) }"
                        :title="favoriteIds.has(promptFavoriteKey(prompt)) ? '取消收藏' : '收藏'"
                        @click="toggleFavorite(prompt)"
                      >
                        <Icon name="sparkles" size="sm" />
                      </button>
                    </div>

                    <p class="line-clamp-4 min-h-[96px] text-sm leading-6 text-gray-600 dark:text-dark-300">{{ prompt.prompt }}</p>

                    <div class="mt-auto flex items-center justify-between gap-2 border-t border-gray-100 pt-3 dark:border-dark-800">
                      <button class="inline-flex items-center gap-1.5 rounded-full border border-gray-200 px-3 py-2 text-xs font-semibold text-gray-600 hover:bg-gray-50 dark:border-dark-700 dark:text-dark-200 dark:hover:bg-dark-800" @click="copyPromptJSON(prompt)">
                        <Icon name="clipboard" size="xs" />
                        复制 JSON
                      </button>
                      <div class="flex items-center gap-2">
                        <a
                          v-if="prompt.link"
                          :href="prompt.link"
                          target="_blank"
                          rel="noreferrer"
                          class="rounded-full border border-gray-200 p-2 text-gray-500 hover:bg-gray-50 hover:text-blue-600 dark:border-dark-700 dark:hover:bg-dark-800"
                          title="打开来源"
                        >
                          <Icon name="externalLink" size="sm" />
                        </a>
                        <button class="rounded-full bg-blue-600 px-4 py-2 text-xs font-semibold text-white shadow-sm hover:bg-blue-700" @click="applyPrompt(prompt)">
                          套用
                        </button>
                      </div>
                    </div>
                  </div>
                </article>
              </div>

              <div v-if="hasMore" class="flex justify-center py-2">
                <button class="rounded-full border border-gray-200 bg-white px-5 py-2 text-sm font-semibold text-gray-700 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-white dark:hover:bg-dark-800" @click="visibleCount += VISIBLE_COUNT_STEP">
                  加载更多
                </button>
              </div>
            </div>
          </div>
        </section>
      </div>
    </transition>
  </teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores'
import {
  AWESOME_GPT_IMAGE_2_PROMPTS_SOURCE_URL,
  BANANA_PROMPTS_SOURCE_URL,
  PROMPT_MARKET_SOURCE_OPTIONS,
  buildPromptJSON,
  fetchPromptMarketPrompts,
  getLocalizedPrompt,
  type BananaPrompt,
  type BananaPromptMode,
  type PromptMarketLanguage,
  type PromptMarketSourceId
} from './promptMarket'
import {
  loadPromptFavorites,
  promptFavoriteKey,
  promptFavoriteRecordKey,
  promptFavoriteToBananaPrompt,
  togglePromptFavorite,
  type PromptFavorite
} from './promptFavorites'

type PromptMarketModeFilter = 'all' | BananaPromptMode
type PromptMarketNsfwFilter = 'safe' | 'include' | 'only'
type PromptMarketSourceFilter = 'all' | PromptMarketSourceId
type PromptMarketFavoriteFilter = 'all' | 'favorites'

defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  apply: [prompt: BananaPrompt]
}>()

const appStore = useAppStore()
const ALL_CATEGORY_VALUE = '__all__'
const INITIAL_VISIBLE_COUNT = 60
const VISIBLE_COUNT_STEP = 60

const prompts = ref<BananaPrompt[]>([])
const favoriteItems = ref<PromptFavorite[]>(loadPromptFavorites())
const isLoading = ref(false)
const error = ref('')
const keyword = ref('')
const favoriteFilter = ref<PromptMarketFavoriteFilter>('all')
const source = ref<PromptMarketSourceFilter>('all')
const promptLanguage = ref<PromptMarketLanguage>('zh-CN')
const category = ref(ALL_CATEGORY_VALUE)
const mode = ref<PromptMarketModeFilter>('all')
const nsfwFilter = ref<PromptMarketNsfwFilter>('safe')
const visibleCount = ref(INITIAL_VISIBLE_COUNT)
const scrollRef = ref<HTMLElement | null>(null)

const favoritePrompts = computed(() => favoriteItems.value.map((item) => promptFavoriteToBananaPrompt(item)))
const favoriteIds = computed(() => new Set(favoriteItems.value.map((item) => promptFavoriteRecordKey(item))))
const promptPool = computed(() => (favoriteFilter.value === 'favorites' ? favoritePrompts.value : prompts.value))

const sourceFilteredPrompts = computed(() => {
  if (source.value === 'all') {
    return promptPool.value
  }
  return promptPool.value.filter((prompt) => prompt.source === source.value)
})

const categories = computed(() => {
  const values = new Set<string>()
  sourceFilteredPrompts.value.forEach((prompt) => {
    values.add(getLocalizedPrompt(prompt, promptLanguage.value).category)
  })
  return [...values].sort((a, b) => a.localeCompare(b, 'zh-CN'))
})

const filteredPrompts = computed(() => {
  const normalizedKeyword = keyword.value.trim().toLowerCase()
  return sourceFilteredPrompts.value
    .map((prompt) => getLocalizedPrompt(prompt, promptLanguage.value))
    .filter((prompt) => {
      if (nsfwFilter.value === 'safe' && prompt.isNsfw) return false
      if (nsfwFilter.value === 'only' && !prompt.isNsfw) return false
      if (category.value !== ALL_CATEGORY_VALUE && prompt.category !== category.value) return false
      if (mode.value !== 'all' && prompt.mode !== mode.value) return false
      if (!normalizedKeyword) return true
      return [prompt.title, prompt.prompt, prompt.author, prompt.category, prompt.subCategory, prompt.sourceLabel]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(normalizedKeyword))
    })
})

const visiblePrompts = computed(() => filteredPrompts.value.slice(0, visibleCount.value))
const hasMore = computed(() => visiblePrompts.value.length < filteredPrompts.value.length)
const countLabel = computed(() => favoriteFilter.value === 'favorites'
  ? `已收藏 ${filteredPrompts.value.length}`
  : prompts.value.length
    ? `${filteredPrompts.value.length} / ${sourceFilteredPrompts.value.length}`
    : '热门模板')

watch([keyword, source, promptLanguage, category, mode, nsfwFilter, favoriteFilter], () => {
  visibleCount.value = INITIAL_VISIBLE_COUNT
  nextTick(() => scrollRef.value?.scrollTo({ top: 0 }))
})

watch(categories, (next) => {
  if (category.value !== ALL_CATEGORY_VALUE && !next.includes(category.value)) {
    category.value = ALL_CATEGORY_VALUE
  }
})

async function loadPromptData() {
  isLoading.value = true
  error.value = ''
  try {
    prompts.value = await fetchPromptMarketPrompts()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取热门模板失败'
  } finally {
    isLoading.value = false
  }
}

function formatPromptDate(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit'
  }).format(date)
}

function toggleFavorite(prompt: BananaPrompt) {
  const result = togglePromptFavorite(prompt)
  favoriteItems.value = result.items
  appStore.showSuccess(result.favorited ? '已收藏提示词' : '已取消收藏')
}

async function copyPromptJSON(prompt: BananaPrompt) {
  try {
    await navigator.clipboard.writeText(buildPromptJSON(prompt))
    appStore.showSuccess('已复制 JSON')
  } catch {
    appStore.showError('复制失败，请手动复制')
  }
}

function applyPrompt(prompt: BananaPrompt) {
  emit('apply', prompt)
  emit('update:open', false)
}

watch(
  () => prompts.value.length,
  (length) => {
    if (length === 0) {
      void loadPromptData()
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.market-select {
  height: 2.5rem;
  min-width: 0;
  border-radius: 0.875rem;
  border: 1px solid rgb(229 231 235);
  background: white;
  padding: 0 0.75rem;
  font-size: 0.875rem;
  color: rgb(31 41 55);
  outline: none;
}

.dark .market-select {
  border-color: rgb(55 65 81);
  background: rgb(10 15 26);
  color: white;
}

.market-tab {
  display: inline-flex;
  min-width: 0;
  flex: 1 1 0%;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  border-radius: 9999px;
  padding: 0 0.75rem;
  font-size: 0.75rem;
  font-weight: 700;
  color: rgb(75 85 99);
  transition: all 0.15s ease;
}

.market-tab-active {
  background: white;
  color: rgb(37 99 235);
  box-shadow: 0 1px 2px rgb(15 23 42 / 0.08);
}

.dark .market-tab-active {
  background: rgb(17 24 39);
  color: rgb(147 197 253);
}

.market-badge {
  display: inline-flex;
  align-items: center;
  border-radius: 9999px;
  padding: 0.25rem 0.5rem;
  font-size: 0.6875rem;
  font-weight: 700;
}
</style>
