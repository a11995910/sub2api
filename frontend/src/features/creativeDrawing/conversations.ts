import type { CreativeImageModel, CreativeOutputFormat } from '@/api/creativeDrawing'
import type { ImageSizeSelection } from './imageOptions'

export type CreativeReferenceImage = {
  id: string
  name: string
  type: string
  dataUrl: string
  remoteUrl?: string
  loading?: boolean
  loadError?: string
  source: 'upload' | 'market' | 'preset' | 'result'
}

export type CreativeStoredImage = {
  id: string
  url: string
  source_url?: string
  image_store_id?: string
  b64_json?: string
  revised_prompt?: string
  output_format?: string
  size?: string
  created_at?: number
}

export type CreativeTurn = {
  id: string
  taskId?: string
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
const IMAGE_STORE_DB_NAME = 'sub2api-creative-drawing'
const IMAGE_STORE_DB_VERSION = 1
const IMAGE_STORE_NAME = 'images'

type CreativeStoredImageRecord = {
  id: string
  b64_json: string
  output_format?: string
  source_url?: string
  updated_at: string
}

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
  if (url && /^data:image\//i.test(url)) {
    return url
  }
  const b64 = normalizeStoredImageBase64(image.b64_json)
  if (b64) {
    return `data:${storedImageMimeType(image.output_format)};base64,${b64}`
  }
  if (url && isDisplayableStoredImageUrl(url) && !/^blob:/i.test(url)) {
    return url
  }
  return url
}

function hydrateStoredImage(image: CreativeStoredImage): CreativeStoredImage {
  const b64 = normalizeStoredImageBase64(image.b64_json)
  const hydrated = {
    ...image,
    url: typeof image.url === 'string' ? image.url : '',
    source_url: typeof image.source_url === 'string' ? image.source_url : undefined,
    image_store_id: typeof image.image_store_id === 'string' ? image.image_store_id : undefined,
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
  const sourceURL = (image.source_url || '').trim()
  const imageStoreID = (image.image_store_id || '').trim()
  const persistedURL = b64 && /^data:image\//i.test(url) ? sourceURL : url
  return {
    ...image,
    url: persistedURL,
    source_url: sourceURL || undefined,
    image_store_id: imageStoreID || undefined,
    b64_json: imageStoreID ? undefined : b64 || undefined
  }
}

function interruptPersistedGeneratingTurn(turn: CreativeTurn): CreativeTurn {
  if (turn.status !== 'generating') {
    return turn
  }
  if (turn.taskId) {
    return turn
  }
  return {
    ...turn,
    status: 'error',
    error: '上次创作在页面刷新或浏览器中断后未能完成状态回写，请重新发起创作。'
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
    turns: item.turns.map((turn) => {
      const normalizedTurn = interruptPersistedGeneratingTurn(turn)
      return {
        ...normalizedTurn,
        references: Array.isArray(normalizedTurn.references) ? normalizedTurn.references : [],
        images: Array.isArray(normalizedTurn.images) ? normalizedTurn.images.map(serializeStoredImage) : []
      }
    })
  }
}

function openCreativeImageStore() {
  return new Promise<IDBDatabase>((resolve, reject) => {
    if (typeof indexedDB === 'undefined') {
      reject(new Error('当前浏览器不支持 IndexedDB'))
      return
    }
    const request = indexedDB.open(IMAGE_STORE_DB_NAME, IMAGE_STORE_DB_VERSION)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(IMAGE_STORE_NAME)) {
        db.createObjectStore(IMAGE_STORE_NAME, { keyPath: 'id' })
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('打开图片本地存储失败'))
  })
}

function waitForTransaction(transaction: IDBTransaction) {
  return new Promise<void>((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onerror = () => reject(transaction.error || new Error('图片本地存储事务失败'))
    transaction.onabort = () => reject(transaction.error || new Error('图片本地存储事务中止'))
  })
}

function getImageRecord(store: IDBObjectStore, id: string) {
  return new Promise<CreativeStoredImageRecord | undefined>((resolve, reject) => {
    const request = store.get(id)
    request.onsuccess = () => resolve(request.result as CreativeStoredImageRecord | undefined)
    request.onerror = () => reject(request.error || new Error('读取图片本地缓存失败'))
  })
}

export async function persistCreativeStoredImages(images: CreativeStoredImage[]) {
  const pendingRecords: Array<{ image: CreativeStoredImage; record: CreativeStoredImageRecord }> = []
  images.forEach((image) => {
    const b64 = normalizeStoredImageBase64(image.b64_json || image.url)
    if (!b64) {
      return
    }
    pendingRecords.push({
      image,
      record: {
        id: image.image_store_id || createId(),
        b64_json: b64,
        output_format: image.output_format,
        source_url: image.source_url,
        updated_at: new Date().toISOString()
      }
    })
  })

  if (pendingRecords.length === 0) {
    return images
  }

  let db: IDBDatabase | null = null
  try {
    db = await openCreativeImageStore()
    const transaction = db.transaction(IMAGE_STORE_NAME, 'readwrite')
    const store = transaction.objectStore(IMAGE_STORE_NAME)
    pendingRecords.forEach(({ record }) => store.put(record))
    await waitForTransaction(transaction)
    pendingRecords.forEach(({ image, record }) => {
      image.image_store_id = record.id
      image.b64_json = record.b64_json
    })
  } catch {
    // IndexedDB 不可用时保留内联 base64，当前创作仍可立即展示。
  } finally {
    db?.close()
  }
  return images
}

export async function hydrateCreativeConversationImages(conversations: CreativeConversation[]) {
  const imageStoreIDs = Array.from(new Set(conversations
    .flatMap((conversation) => conversation.turns)
    .flatMap((turn) => turn.images || [])
    .map((image) => image.image_store_id || '')
    .filter(Boolean)))

  if (imageStoreIDs.length === 0) {
    return conversations
  }

  let db: IDBDatabase | null = null
  try {
    db = await openCreativeImageStore()
    const transaction = db.transaction(IMAGE_STORE_NAME, 'readonly')
    const store = transaction.objectStore(IMAGE_STORE_NAME)
    const records = await Promise.all(imageStoreIDs.map(async (id) => [id, await getImageRecord(store, id)] as const))
    const recordByID = new Map(records.filter((item): item is readonly [string, CreativeStoredImageRecord] => Boolean(item[1])))

    return conversations.map((conversation) => ({
      ...conversation,
      turns: conversation.turns.map((turn) => ({
        ...turn,
        images: (turn.images || []).map((image) => {
          if (normalizeStoredImageBase64(image.b64_json)) {
            return image
          }
          const record = image.image_store_id ? recordByID.get(image.image_store_id) : undefined
          if (!record?.b64_json) {
            return image
          }
          const hydrated = {
            ...image,
            b64_json: normalizeStoredImageBase64(record.b64_json) || undefined,
            output_format: image.output_format || record.output_format,
            source_url: image.source_url || record.source_url
          }
          return {
            ...hydrated,
            url: buildStoredImageUrl(hydrated)
          }
        })
      }))
    }))
  } catch {
    return conversations
  } finally {
    db?.close()
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
      .map((conversation) => ({
        ...conversation,
        turns: conversation.turns.map(interruptPersistedGeneratingTurn)
      }))
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
          url: image.source_url || (/^https?:\/\//i.test(image.url) ? image.url : ''),
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
