import type {
  BananaPrompt,
  BananaPromptMode,
  PromptMarketLanguage,
  PromptMarketLocalization,
  PromptMarketSourceId
} from './promptMarket'
import { getPromptMarketSourceLabel, normalizePromptMarketSourceId } from './promptMarket'

export type PromptFavorite = {
  id: string
  prompt_id: string
  source: PromptMarketSourceId
  title: string
  preview: string
  reference_image_urls: string[]
  prompt: string
  author: string
  link?: string
  mode: BananaPromptMode
  category: string
  sub_category?: string
  created?: string
  source_label: string
  is_nsfw: boolean
  localizations?: Partial<
    Record<
      PromptMarketLanguage,
      PromptMarketLocalization & {
        sub_category?: string
      }
    >
  >
  favorited_at: string
  updated_at?: string
}

const FAVORITES_STORAGE_KEY = 'sub2api:creative-drawing:prompt-favorites'

export function promptFavoriteKey(prompt: Pick<BananaPrompt, 'id' | 'source'>) {
  return `${prompt.source}:${prompt.id}`
}

export function promptFavoriteRecordKey(favorite: Pick<PromptFavorite, 'prompt_id' | 'source'>) {
  return `${favorite.source}:${favorite.prompt_id}`
}

function normalizeFavoriteLocalizations(value: PromptFavorite['localizations']): BananaPrompt['localizations'] {
  if (!value) {
    return undefined
  }
  const localizations: BananaPrompt['localizations'] = {}
  for (const language of ['zh-CN', 'en'] satisfies PromptMarketLanguage[]) {
    const item = value[language]
    if (!item) {
      continue
    }
    localizations[language] = {
      title: item.title,
      prompt: item.prompt,
      category: item.category,
      subCategory: item.subCategory ?? item.sub_category
    }
  }
  return Object.keys(localizations).length > 0 ? localizations : undefined
}

function localizationsToPayload(localizations: NonNullable<BananaPrompt['localizations']>) {
  const payload: Record<string, PromptMarketLocalization & { sub_category?: string }> = {}
  for (const [language, item] of Object.entries(localizations)) {
    if (!item) {
      continue
    }
    payload[language] = {
      title: item.title,
      prompt: item.prompt,
      category: item.category,
      sub_category: item.subCategory
    }
  }
  return payload
}

export function promptFavoriteToBananaPrompt(favorite: PromptFavorite): BananaPrompt {
  return {
    id: favorite.prompt_id,
    title: favorite.title,
    preview: favorite.preview,
    referenceImageUrls: favorite.reference_image_urls,
    prompt: favorite.prompt,
    author: favorite.author,
    link: favorite.link,
    mode: favorite.mode,
    category: favorite.category,
    subCategory: favorite.sub_category,
    created: favorite.created,
    source: normalizePromptMarketSourceId(favorite.source),
    sourceLabel: getPromptMarketSourceLabel(favorite.source),
    isNsfw: favorite.is_nsfw,
    localizations: normalizeFavoriteLocalizations(favorite.localizations)
  }
}

export function bananaPromptToFavorite(prompt: BananaPrompt): PromptFavorite {
  const now = new Date().toISOString()
  return {
    id: promptFavoriteKey(prompt),
    prompt_id: prompt.id,
    source: normalizePromptMarketSourceId(prompt.source),
    title: prompt.title,
    preview: prompt.preview,
    reference_image_urls: prompt.referenceImageUrls,
    prompt: prompt.prompt,
    author: prompt.author,
    link: prompt.link,
    mode: prompt.mode,
    category: prompt.category,
    sub_category: prompt.subCategory,
    created: prompt.created,
    source_label: getPromptMarketSourceLabel(prompt.source),
    is_nsfw: prompt.isNsfw,
    localizations: prompt.localizations ? localizationsToPayload(prompt.localizations) : undefined,
    favorited_at: now,
    updated_at: now
  }
}

export function loadPromptFavorites(): PromptFavorite[] {
  if (typeof localStorage === 'undefined') {
    return []
  }
  try {
    const raw = localStorage.getItem(FAVORITES_STORAGE_KEY)
    if (!raw) {
      return []
    }
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) {
      return []
    }
    return parsed.filter((item): item is PromptFavorite => {
      return Boolean(item && typeof item === 'object' && typeof item.id === 'string' && typeof item.prompt === 'string')
    })
  } catch {
    return []
  }
}

export function savePromptFavorites(items: PromptFavorite[]) {
  if (typeof localStorage === 'undefined') {
    return
  }
  localStorage.setItem(FAVORITES_STORAGE_KEY, JSON.stringify(items))
}

export function togglePromptFavorite(prompt: BananaPrompt) {
  const items = loadPromptFavorites()
  const key = promptFavoriteKey(prompt)
  const existingIndex = items.findIndex((item) => promptFavoriteRecordKey(item) === key)
  if (existingIndex >= 0) {
    items.splice(existingIndex, 1)
    savePromptFavorites(items)
    return { items, favorited: false }
  }
  const next = [bananaPromptToFavorite(prompt), ...items]
  savePromptFavorites(next)
  return { items: next, favorited: true }
}
