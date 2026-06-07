/**
 * 热门模板解析器。
 *
 * 远程数据源、字段规范与解析思路改写自 ZyphrZero/chatgpt2api
 * （MIT License，Copyright (c) 2026 kunkun）。
 */

export type BananaPromptMode = 'generate' | 'edit'
export type PromptMarketSourceId = 'library-a' | 'library-b'
export type PromptMarketLanguage = 'zh-CN' | 'en'

export type PromptMarketLocalization = {
  title: string
  prompt: string
  category: string
  subCategory?: string
}

export type BananaPrompt = {
  id: string
  title: string
  preview: string
  referenceImageUrls: string[]
  prompt: string
  author: string
  link?: string
  mode: BananaPromptMode
  category: string
  subCategory?: string
  created?: string
  source: PromptMarketSourceId
  sourceLabel: string
  isNsfw: boolean
  localizations?: Partial<Record<PromptMarketLanguage, PromptMarketLocalization>>
}

export const PROMPT_MARKET_API_URL = '/api/v1/creative-drawing/prompt-market'

const PROMPT_MARKET_ASSET_BASE_URL = `${PROMPT_MARKET_API_URL}/assets/`

const PROMPT_MARKET_DISPLAY_LABELS: Record<PromptMarketSourceId, string> = {
  'library-a': '精选模板库 A',
  'library-b': '精选模板库 B'
}

const LEGACY_PROMPT_MARKET_SOURCE_MAP: Record<string, PromptMarketSourceId> = {
  [`banana-${'prompt'}-quicker`]: 'library-a',
  [`awesome-${'gpt'}-image-2-prompts`]: 'library-b',
  'library-a': 'library-a',
  'library-b': 'library-b'
}

export function normalizePromptMarketSourceId(source: unknown): PromptMarketSourceId {
  return typeof source === 'string' && LEGACY_PROMPT_MARKET_SOURCE_MAP[source]
    ? LEGACY_PROMPT_MARKET_SOURCE_MAP[source]
    : 'library-a'
}

export function getPromptMarketSourceLabel(source: unknown) {
  return PROMPT_MARKET_DISPLAY_LABELS[normalizePromptMarketSourceId(source)]
}

export const PROMPT_MARKET_SOURCE_OPTIONS: {
  value: PromptMarketSourceId
  label: string
}[] = [
  {
    value: 'library-a',
    label: PROMPT_MARKET_DISPLAY_LABELS['library-a']
  },
  {
    value: 'library-b',
    label: PROMPT_MARKET_DISPLAY_LABELS['library-b']
  }
]

type BananaPromptSourceItem = {
  title?: unknown
  preview?: unknown
  reference_image_urls?: unknown
  prompt?: unknown
  author?: unknown
  link?: unknown
  mode?: unknown
  category?: unknown
  sub_category?: unknown
  created?: unknown
}

type AwesomePromptDraft = BananaPrompt & {
  language: PromptMarketLanguage
  mergeKey: string
}

const MARKDOWN_CASE_HEADING_PATTERN =
  /^### Case\s+(\d+):\s+\[([^\]]+)]\(([^)]+)\)\s+\(by\s+\[([^\]]+)]\(([^)]+)\)\)/
const MARKDOWN_IMAGE_PATTERN = /<img\s+[^>]*src=["']([^"']+)["'][^>]*>/i
const MARKDOWN_PROMPT_PATTERN =
  /\*{2,}\s*(?:Prompt|提示词)\s*[:：]\s*\*{2,}\s*\n\s*```(?:\w+)?\n([\s\S]*?)\n```/i
const IGNORED_MARKET_README_HEADINGS = new Set(['简介', '最新动态', 'Menu', '致谢', 'Star History'])
const NSFW_TEXT_PATTERN =
  /\b(nsfw|nude|naked|lingerie|erotic|seductive|sexy|cleavage|underwear|panties|bra|bikini|ahegao|explicit|sensual|fetish|nipples?|genitals?|buttocks?|thong|topless)\b|裸|色情|情色|性感|诱惑|内衣|内裤|乳|胸|臀|私处|泳衣|比基尼|情趣|丁字裤|翻白眼|吐舌|妩媚|暧昧/i

function normalizePromptMode(value: unknown): BananaPromptMode {
  return value === 'edit' ? 'edit' : 'generate'
}

function buildPromptId(item: BananaPromptSourceItem, index: number) {
  return [item.title, item.author, index]
    .map((part) => String(part || '').trim())
    .filter(Boolean)
    .join(':')
}

function normalizeReferenceImageUrls(value: unknown) {
  if (!Array.isArray(value)) {
    return []
  }
  return value.filter((url): url is string => typeof url === 'string' && url.trim().length > 0)
}

function proxyPromptMarketAssetURL(source: PromptMarketSourceId, value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }
  if (trimmed.startsWith(PROMPT_MARKET_ASSET_BASE_URL) || trimmed.startsWith('/api/v1/creative-drawing/prompt-market/assets/')) {
    return trimmed
  }
  const normalized = trimmed
    .split(/[?#]/, 1)[0]
    .replace(/^\.\//, '')
    .split('/')
    .map((part) => part.trim())
    .filter((part) => part && part !== '.' && part !== '..')
    .join('/')
  return normalized ? `${PROMPT_MARKET_ASSET_BASE_URL}${source}/${normalized}` : ''
}

function isNsfwPrompt(category: string, title: string, prompt: string) {
  return category === 'NSFW' || NSFW_TEXT_PATTERN.test(`${category}\n${title}\n${prompt}`)
}

function normalizePrompt(item: BananaPromptSourceItem, index: number): BananaPrompt | null {
  if (
    typeof item.title !== 'string' ||
    typeof item.preview !== 'string' ||
    typeof item.prompt !== 'string' ||
    typeof item.author !== 'string'
  ) {
    return null
  }

  const title = item.title.trim()
  const preview = item.preview.trim()
  const prompt = item.prompt.trim()
  const author = item.author.trim()
  const category =
    typeof item.category === 'string' && item.category.trim() ? item.category.trim() : '未分类'
  if (!title || !preview || !prompt || !author) {
    return null
  }

  return {
    id: `library-a:${buildPromptId(item, index)}`,
    title,
    preview: proxyPromptMarketAssetURL('library-a', preview),
    prompt,
    author,
    referenceImageUrls: normalizeReferenceImageUrls(item.reference_image_urls)
      .map((url) => proxyPromptMarketAssetURL('library-a', url)),
    link: typeof item.link === 'string' && item.link.trim() ? item.link.trim() : undefined,
    mode: normalizePromptMode(item.mode),
    category,
    subCategory: typeof item.sub_category === 'string' && item.sub_category.trim() ? item.sub_category.trim() : undefined,
    created: typeof item.created === 'string' && item.created.trim() ? item.created.trim() : undefined,
    source: 'library-a',
    sourceLabel: PROMPT_MARKET_DISPLAY_LABELS['library-a'],
    isNsfw: category === 'NSFW'
  }
}

function normalizeMarkdownImageUrl(value: string) {
  const imageUrl = value.trim()
  if (!imageUrl) {
    return ''
  }
  return proxyPromptMarketAssetURL('library-b', imageUrl)
}

function buildAwesomePromptMergeKey(link: string, preview: string) {
  return `${link.trim()}|${preview.trim()}`
}

function cleanMarkdownHeading(value: string) {
  return value
    .replace(/^#+\s*/, '')
    .replace(/^[^\p{Letter}\p{Number}]+/u, '')
    .trim()
}

function normalizeAwesomePromptSection(
  section: string,
  category: string,
  language: PromptMarketLanguage
): AwesomePromptDraft | null {
  const heading = section.match(MARKDOWN_CASE_HEADING_PATTERN)
  const image = section.match(MARKDOWN_IMAGE_PATTERN)
  const promptBlock = section.match(MARKDOWN_PROMPT_PATTERN)
  if (!heading || !image || !promptBlock) {
    return null
  }

  const caseNumber = heading[1].trim()
  const title = heading[2].trim()
  const link = heading[3].trim()
  const author = heading[4].trim()
  const preview = normalizeMarkdownImageUrl(image[1])
  const prompt = promptBlock[1].trim()
  if (!caseNumber || !title || !preview || !prompt || !author) {
    return null
  }

  return {
    id: `library-b:${buildAwesomePromptMergeKey(link, preview)}`,
    title,
    preview,
    referenceImageUrls: [],
    prompt,
    author,
    link,
    mode: 'generate',
    category,
    subCategory: `Case ${caseNumber}`,
    source: 'library-b',
    sourceLabel: PROMPT_MARKET_DISPLAY_LABELS['library-b'],
    isNsfw: isNsfwPrompt(category, title, prompt),
    language,
    mergeKey: buildAwesomePromptMergeKey(link, preview),
    localizations: {
      [language]: {
        title,
        prompt,
        category,
        subCategory: `Case ${caseNumber}`
      }
    }
  }
}

function parseAwesomePrompts(markdown: string, language: PromptMarketLanguage) {
  const lines = markdown.split(/\r?\n/)
  const prompts: AwesomePromptDraft[] = []
  let activeCategory = '未分类'

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index]
    if (line.startsWith('## ')) {
      const heading = cleanMarkdownHeading(line)
      if (heading && !IGNORED_MARKET_README_HEADINGS.has(heading)) {
        activeCategory = heading
      }
      continue
    }
    if (!line.startsWith('### Case ')) {
      continue
    }

    const sectionStart = index
    let sectionEnd = lines.length
    for (let nextIndex = index + 1; nextIndex < lines.length; nextIndex += 1) {
      if (lines[nextIndex].startsWith('### Case ') || lines[nextIndex].startsWith('## ')) {
        sectionEnd = nextIndex
        break
      }
    }

    const prompt = normalizeAwesomePromptSection(
      lines.slice(sectionStart, sectionEnd).join('\n'),
      activeCategory,
      language
    )
    if (prompt) {
      prompts.push(prompt)
    }
    index = sectionEnd - 1
  }

  return prompts
}

function mergeAwesomePrompts(...groups: AwesomePromptDraft[][]) {
  const promptsByKey = new Map<string, AwesomePromptDraft>()

  groups.flat().forEach((prompt) => {
    const current = promptsByKey.get(prompt.mergeKey)
    if (!current) {
      promptsByKey.set(prompt.mergeKey, prompt)
      return
    }

    current.localizations = {
      ...current.localizations,
      ...prompt.localizations
    }
    current.isNsfw = current.isNsfw || prompt.isNsfw
    if (current.language !== 'zh-CN' && prompt.language === 'zh-CN') {
      current.title = prompt.title
      current.prompt = prompt.prompt
      current.category = prompt.category
      current.subCategory = prompt.subCategory
      current.language = prompt.language
    }
  })

  return [...promptsByKey.values()].map(({ language: _language, mergeKey: _mergeKey, ...prompt }) => prompt)
}

export async function fetchBananaPrompts(signal?: AbortSignal) {
  const response = await fetch(`${PROMPT_MARKET_API_URL}/libraries/a/prompts`, {
    signal,
    headers: {
      Accept: 'application/json'
    }
  })
  if (!response.ok) {
    throw new Error(`读取热门模板失败：${response.status}`)
  }

  const data: unknown = await response.json()
  if (!Array.isArray(data)) {
    throw new Error('热门模板数据格式无效')
  }

  return data.flatMap((item, index) => {
    const prompt = normalizePrompt(item as BananaPromptSourceItem, index)
    return prompt ? [prompt] : []
  })
}

export async function fetchAwesomeGptImage2Prompts(signal?: AbortSignal) {
  const [zhResponse, enResponse] = await Promise.all([
    fetch(`${PROMPT_MARKET_API_URL}/libraries/b/prompts/zh-CN`, {
      signal,
      headers: {
        Accept: 'text/markdown,text/plain'
      }
    }),
    fetch(`${PROMPT_MARKET_API_URL}/libraries/b/prompts/en`, {
      signal,
      headers: {
        Accept: 'text/markdown,text/plain'
      }
    })
  ])
  if (!zhResponse.ok) {
    throw new Error(`读取精选模板库 B 中文提示词失败：${zhResponse.status}`)
  }
  if (!enResponse.ok) {
    throw new Error(`读取精选模板库 B 英文提示词失败：${enResponse.status}`)
  }

  const [zhMarkdown, enMarkdown] = await Promise.all([zhResponse.text(), enResponse.text()])
  return mergeAwesomePrompts(
    parseAwesomePrompts(zhMarkdown, 'zh-CN'),
    parseAwesomePrompts(enMarkdown, 'en')
  )
}

export async function fetchPromptMarketPrompts(signal?: AbortSignal) {
  const [bananaPrompts, awesomePrompts] = await Promise.all([
    fetchBananaPrompts(signal),
    fetchAwesomeGptImage2Prompts(signal)
  ])

  return [...bananaPrompts, ...awesomePrompts]
}

export function getPromptReferenceImageUrls(prompt: BananaPrompt) {
  const urls = prompt.referenceImageUrls.length > 0 ? prompt.referenceImageUrls : [prompt.preview]
  return Array.from(new Set(urls.map((url) => url.trim()).filter(Boolean)))
}

export function getPromptApplyReferenceImageUrls(prompt: BananaPrompt) {
  const referenceUrls = Array.from(new Set(prompt.referenceImageUrls.map((url) => url.trim()).filter(Boolean)))
  if (referenceUrls.length > 0) {
    return referenceUrls
  }
  if (prompt.mode !== 'edit') {
    return []
  }
  const preview = prompt.preview.trim()
  return preview ? [preview] : []
}

export function getLocalizedPrompt(prompt: BananaPrompt, language: PromptMarketLanguage): BananaPrompt {
  const localization = prompt.localizations?.[language] ?? prompt.localizations?.['zh-CN'] ?? prompt.localizations?.en
  if (!localization) {
    return prompt
  }

  return {
    ...prompt,
    title: localization.title,
    prompt: localization.prompt,
    category: localization.category,
    subCategory: localization.subCategory
  }
}

export function buildPromptJSON(prompt: BananaPrompt) {
  return JSON.stringify(
    {
      title: prompt.title,
      prompt: prompt.prompt,
      mode: prompt.mode,
      category: prompt.category,
      sub_category: prompt.subCategory || undefined,
      reference_image_urls: getPromptReferenceImageUrls(prompt),
      preview: prompt.preview,
      author: prompt.author || undefined
    },
    null,
    2
  )
}
