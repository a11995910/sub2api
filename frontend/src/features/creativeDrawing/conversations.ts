import type { CreativeImageModel, CreativeOutputFormat } from '@/api/creativeDrawing'
import type { ImageSizeSelection } from './imageOptions'

export type CreativeReferenceImage = {
  id: string
  name: string
  type: string
  dataUrl: string
  source: 'upload' | 'market' | 'preset' | 'result'
}

export type CreativeStoredImage = {
  id: string
  url: string
  b64_json?: string
  revised_prompt?: string
  output_format?: string
  size?: string
  created_at?: number
}

export type CreativeTurn = {
  id: string
  prompt: string
  mode: 'generate' | 'edit'
  model: CreativeImageModel
  count: number
  size: string
  outputFormat: CreativeOutputFormat
  sizeSelection: ImageSizeSelection
  references: CreativeReferenceImage[]
  images: CreativeStoredImage[]
  status: 'success' | 'error' | 'generating'
  error?: string
  createdAt: string
}

export type CreativeConversation = {
  id: string
  title: string
  createdAt: string
  updatedAt: string
  turns: CreativeTurn[]
}

const STORAGE_KEY = 'sub2api:creative-drawing:conversations'
const ACTIVE_STORAGE_KEY = 'sub2api:creative-drawing:active-conversation'
const MAX_CONVERSATIONS = 80

function storedImageMimeType(format?: string) {
  switch ((format || 'png').toLowerCase()) {
    case 'jpeg':
    case 'jpg':
      return 'image/jpeg'
    case 'webp':
      return 'image/webp'
    default:
      return 'image/png'
  }
}

function normalizeStoredImageBase64(value?: string) {
  const trimmed = (value || '').trim()
  if (!trimmed) {
    return ''
  }
  if (/^data:image\//i.test(trimmed)) {
    const [, content = ''] = trimmed.split(',', 2)
    return content.replace(/\s+/g, '')
  }
  return trimmed.replace(/\s+/g, '')
}

function isDisplayableStoredImageUrl(url: string) {
  return /^(data:image\/|https?:\/\/|\/\/|\/)/i.test(url)
}

export function buildStoredImageUrl(image: Pick<CreativeStoredImage, 'url' | 'b64_json' | 'output_format'>) {
  const url = (image.url || '').trim()
  if (url && isDisplayableStoredImageUrl(url) && !/^blob:/i.test(url)) {
    return url
  }
  const b64 = normalizeStoredImageBase64(image.b64_json)
  if (!b64) {
    return url
  }
  return `data:${storedImageMimeType(image.output_format)};base64,${b64}`
}

function hydrateStoredImage(image: CreativeStoredImage): CreativeStoredImage {
  const b64 = normalizeStoredImageBase64(image.b64_json)
  const hydrated = {
    ...image,
    url: typeof image.url === 'string' ? image.url : '',
    b64_json: b64 || undefined
  }
  return {
    ...hydrated,
    url: buildStoredImageUrl(hydrated)
  }
}

function serializeStoredImage(image: CreativeStoredImage): CreativeStoredImage {
  const b64 = normalizeStoredImageBase64(image.b64_json)
  const url = (image.url || '').trim()
  return {
    ...image,
    url: b64 && /^data:image\//i.test(url) ? '' : url,
    b64_json: b64 || undefined
  }
}

function hydrateCreativeConversation(item: CreativeConversation): CreativeConversation {
  return {
    ...item,
    turns: item.turns.map((turn) => ({
      ...turn,
      references: Array.isArray(turn.references) ? turn.references : [],
      images: Array.isArray(turn.images) ? turn.images.map(hydrateStoredImage) : []
    }))
  }
}

function serializeCreativeConversation(item: CreativeConversation): CreativeConversation {
  return {
    ...item,
    turns: item.turns.map((turn) => ({
      ...turn,
      references: Array.isArray(turn.references) ? turn.references : [],
      images: Array.isArray(turn.images) ? turn.images.map(serializeStoredImage) : []
    }))
  }
}

export function createId() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export function buildConversationTitle(prompt: string) {
  const trimmed = prompt.trim().replace(/\s+/g, ' ')
  if (!trimmed) {
    return '未命名创作'
  }
  return trimmed.length <= 18 ? trimmed : `${trimmed.slice(0, 18)}...`
}

export function loadCreativeConversations(): CreativeConversation[] {
  if (typeof localStorage === 'undefined') {
    return []
  }
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) {
      return []
    }
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) {
      return []
    }
    return parsed
      .filter((item): item is CreativeConversation => {
        return Boolean(item && typeof item === 'object' && typeof item.id === 'string' && Array.isArray(item.turns))
      })
      .map(hydrateCreativeConversation)
  } catch {
    return []
  }
}

export function saveCreativeConversations(conversations: CreativeConversation[]) {
  if (typeof localStorage === 'undefined') {
    return
  }
  const normalized = [...conversations]
    .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())
    .slice(0, MAX_CONVERSATIONS)
    .map(serializeCreativeConversation)
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(normalized))
  } catch {
    const compact = normalized.slice(0, 20).map((conversation) => ({
      ...conversation,
      turns: conversation.turns.map((turn) => ({
        ...turn,
        references: [],
        images: turn.images.map((image) => ({
          ...image,
          url: /^https?:\/\//i.test(image.url) ? image.url : '',
          b64_json: undefined
        }))
      }))
    }))
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(compact))
    } catch {
      // 浏览器配额已满时不再阻塞当前创作流程。
    }
  }
}

export function loadActiveCreativeConversationId() {
  if (typeof localStorage === 'undefined') {
    return ''
  }
  return localStorage.getItem(ACTIVE_STORAGE_KEY) || ''
}

export function saveActiveCreativeConversationId(id: string) {
  if (typeof localStorage === 'undefined') {
    return
  }
  if (!id) {
    localStorage.removeItem(ACTIVE_STORAGE_KEY)
    return
  }
  localStorage.setItem(ACTIVE_STORAGE_KEY, id)
}

export function readFileAsDataUrl(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(new Error('读取参考图失败'))
    reader.readAsDataURL(file)
  })
}

export function dataUrlToFile(dataUrl: string, fileName: string, mimeType?: string) {
  const [header, content] = dataUrl.split(',', 2)
  const matchedMimeType = header.match(/data:(.*?);base64/)?.[1]
  const binary = atob(content || '')
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return new File([bytes], fileName, { type: mimeType || matchedMimeType || 'image/png' })
}

export function resultToReferenceImage(image: CreativeStoredImage, index: number): CreativeReferenceImage | null {
  const url = buildStoredImageUrl(image)
  if (!url) {
    return null
  }
  return {
    id: createId(),
    name: `result-${index + 1}.${image.output_format === 'jpeg' ? 'jpg' : image.output_format || 'png'}`,
    type: storedImageMimeType(image.output_format),
    dataUrl: url,
    source: 'result'
  }
}
