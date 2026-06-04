import type { CreativeDrawingTask, CreativeImageResult } from '@/api/creativeDrawing'
import { normalizeStoredImageBase64, type CreativeStoredImage, type CreativeTurn } from './conversations'

const RECENT_TASK_DETAIL_HYDRATE_LIMIT = 8

function hasInlineImagePayload(image: Pick<CreativeImageResult | CreativeStoredImage, 'url' | 'b64_json'>) {
  return Boolean(normalizeStoredImageBase64(image.b64_json) || normalizeStoredImageBase64(image.url))
}

export function hasInlineCreativeImagePayload(images: Array<Pick<CreativeImageResult | CreativeStoredImage, 'url' | 'b64_json'>> | undefined) {
  return Array.isArray(images) && images.some(hasInlineImagePayload)
}

function hasRemoteImagePointer(image: Pick<CreativeImageResult, 'url' | 'source_url' | 'b64_json'>) {
  return Boolean((image.url || '').trim() || (image.source_url || '').trim() || (image.b64_json || '').trim())
}

export function shouldFetchFullCreativeTaskResult(task: CreativeDrawingTask, existingTurn?: CreativeTurn | null) {
  if (task.status !== 'success' || !Array.isArray(task.images) || task.images.length === 0) {
    return false
  }
  if (hasInlineCreativeImagePayload(task.images)) {
    return false
  }
  if (existingTurn && hasInlineCreativeImagePayload(existingTurn.images)) {
    return false
  }
  return task.images.some(hasRemoteImagePointer)
}

export function shouldHydrateCreativeTaskFromList(task: CreativeDrawingTask, index: number, existingTurn?: CreativeTurn | null) {
  if (!shouldFetchFullCreativeTaskResult(task, existingTurn)) {
    return false
  }
  if (existingTurn) {
    return true
  }
  return index >= 0 && index < RECENT_TASK_DETAIL_HYDRATE_LIMIT
}
