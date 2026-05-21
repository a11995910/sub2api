import type { CreativeImageModel, CreativeOutputFormat } from '@/api/creativeDrawing'
import type { ImageSizeSelection } from './imageOptions'

export type CreativeReferenceImage = {
  id: string
  name: string
  type: string
  dataUrl: string
  source: 'upload' | 'market' | 'result'
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
    return parsed.filter((item): item is CreativeConversation => {
      return Boolean(item && typeof item === 'object' && typeof item.id === 'string' && Array.isArray(item.turns))
    })
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
  localStorage.setItem(STORAGE_KEY, JSON.stringify(normalized))
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
  if (!image.url) {
    return null
  }
  return {
    id: createId(),
    name: `result-${index + 1}.${image.output_format === 'jpeg' ? 'jpg' : image.output_format || 'png'}`,
    type: image.output_format === 'jpeg' ? 'image/jpeg' : `image/${image.output_format || 'png'}`,
    dataUrl: image.url,
    source: 'result'
  }
}
