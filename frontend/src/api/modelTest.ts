/**
 * 模型测试台直接调用 OpenAI 兼容网关。
 *
 * 这里不走 apiClient，因为 apiClient 的 baseURL 固定为 /api/v1；
 * 测试请求必须进入真实 /v1 网关，才能复用现有鉴权、路由、用量记录和扣费链路。
 */

export interface GatewayTestOptions {
  apiKey: string
  signal?: AbortSignal
}

export interface ChatCompletionTestRequest extends GatewayTestOptions {
  model: string
  prompt: string
  maxTokens?: number
}

export interface ImageGenerationTestRequest extends GatewayTestOptions {
  model: string
  prompt: string
  size?: string
}

export interface ImageEditTestRequest extends GatewayTestOptions {
  model: string
  prompt: string
  size?: string
  images: File[]
}

export interface VideoGenerationTestRequest extends GatewayTestOptions {
  model: string
  prompt: string
  resolution?: string
  duration?: number
  /** 兼容旧调用方的单张起始图字段。 */
  imageDataUrl?: string
  /** video-1.5 使用单张起始图，标准视频模型使用多张参考图。 */
  startingImageDataUrl?: string
  referenceImageDataUrls?: string[]
  pollIntervalMs?: number
  timeoutMs?: number
}

export interface VideoGenerationTestResult {
  payload: unknown
  requestID: string
}

export class ModelTestError extends Error {
  status: number
  payload: unknown

  constructor(message: string, status: number, payload: unknown) {
    super(message)
    this.name = 'ModelTestError'
    this.status = status
    this.payload = payload
  }
}

async function postGateway<T>(
  path: '/v1/chat/completions' | '/v1/images/generations' | '/v1/videos/generations',
  apiKey: string,
  payload: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify(payload),
    signal,
  })

  const text = await response.text()
  const data = parseMaybeJSON(text)
  if (!response.ok) {
    throw new ModelTestError(extractGatewayErrorMessage(data, text, response.status), response.status, data)
  }
  return data as T
}

async function getGateway<T>(path: string, apiKey: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      Accept: 'application/json',
    },
    signal,
  })
  const text = await response.text()
  const data = parseMaybeJSON(text)
  if (!response.ok) {
    throw new ModelTestError(extractGatewayErrorMessage(data, text, response.status), response.status, data)
  }
  return data as T
}

export async function listGatewayModels(apiKey: string, signal?: AbortSignal): Promise<string[]> {
  const payload = await getGateway<{ data?: Array<{ id?: string }> }>('/v1/models', apiKey, signal)
  return (payload.data || [])
    .map((item) => String(item.id || '').trim())
    .filter(Boolean)
}

async function postGatewayFormData<T>(
  path: '/v1/images/edits',
  apiKey: string,
  payload: FormData,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      Accept: 'application/json',
    },
    body: payload,
    signal,
  })

  const text = await response.text()
  const data = parseMaybeJSON(text)
  if (!response.ok) {
    throw new ModelTestError(extractGatewayErrorMessage(data, text, response.status), response.status, data)
  }
  return data as T
}

function parseMaybeJSON(text: string): unknown {
  if (!text.trim()) return null
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

function extractGatewayErrorMessage(payload: unknown, fallbackText: string, status: number): string {
  if (payload && typeof payload === 'object') {
    const obj = payload as Record<string, any>
    const error = obj.error
    if (error && typeof error === 'object') {
      const message = String(error.message || error.detail || '').trim()
      if (message) return message
    }
    const message = String(obj.message || obj.detail || '').trim()
    if (message) return message
  }
  if (fallbackText.trim()) return fallbackText.trim()
  return `Gateway request failed (${status})`
}

export async function testChatCompletion(req: ChatCompletionTestRequest): Promise<unknown> {
  return postGateway(
    '/v1/chat/completions',
    req.apiKey,
    {
      model: req.model,
      messages: [{ role: 'user', content: req.prompt }],
      stream: false,
      max_tokens: req.maxTokens ?? 256,
    },
    req.signal,
  )
}

export async function testImageGeneration(req: ImageGenerationTestRequest): Promise<unknown> {
  const payload: Record<string, unknown> = {
    model: req.model,
    prompt: req.prompt,
    n: 1,
  }
  const size = req.size?.trim()
  if (size) {
    payload.size = size
  }

  return postGateway(
    '/v1/images/generations',
    req.apiKey,
    payload,
    req.signal,
  )
}

export async function testImageEdit(req: ImageEditTestRequest): Promise<unknown> {
  const form = new FormData()
  form.set('model', req.model)
  form.set('prompt', req.prompt)
  form.set('n', '1')

  const size = req.size?.trim()
  if (size) {
    form.set('size', size)
  }

  req.images.forEach((image, index) => {
    form.append(req.images.length === 1 ? 'image' : `image[${index}]`, image, image.name)
  })

  return postGatewayFormData(
    '/v1/images/edits',
    req.apiKey,
    form,
    req.signal,
  )
}

export async function testVideoGeneration(req: VideoGenerationTestRequest): Promise<VideoGenerationTestResult> {
  const payload: Record<string, unknown> = {
    model: req.model,
    prompt: req.prompt,
  }
  if (req.resolution?.trim()) payload.resolution = req.resolution.trim()
  if (Number.isFinite(req.duration)) payload.duration = Math.max(1, Math.min(15, Math.floor(Number(req.duration))))
  const startingImageDataUrl = req.startingImageDataUrl?.trim() || req.imageDataUrl?.trim()
  if (startingImageDataUrl) payload.image = { url: startingImageDataUrl }
  const referenceImageDataUrls = (req.referenceImageDataUrls || [])
    .map((url) => url.trim())
    .filter(Boolean)
  if (referenceImageDataUrls.length > 0) {
    payload.reference_images = referenceImageDataUrls.map((url) => ({ url }))
  }

  const created = await postGateway<unknown>('/v1/videos/generations', req.apiKey, payload, req.signal)
  const requestID = extractVideoRequestID(created)
  if (extractVideoURL(created)) return { payload: created, requestID }
  if (!requestID) {
    throw new ModelTestError('Video generation response did not include request_id', 502, created)
  }

  const pollIntervalMs = Math.max(0, req.pollIntervalMs ?? 2000)
  const timeoutMs = Math.max(1000, req.timeoutMs ?? 5 * 60 * 1000)
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    await waitForPoll(pollIntervalMs, req.signal)
    const statusPayload = await getGateway<unknown>(`/v1/videos/${encodeURIComponent(requestID)}`, req.apiKey, req.signal)
    const status = extractVideoStatus(statusPayload)
    if (extractVideoURL(statusPayload) || ['completed', 'succeeded', 'success', 'done'].includes(status)) {
      return { payload: statusPayload, requestID }
    }
    if (['failed', 'error', 'cancelled', 'canceled'].includes(status)) {
      throw new ModelTestError(extractGatewayErrorMessage(statusPayload, '', 502), 502, statusPayload)
    }
  }
  throw new ModelTestError('Video generation timed out', 408, created)
}

export async function fetchVideoContent(
  requestID: string,
  apiKey: string,
  signal?: AbortSignal,
): Promise<Blob> {
  const response = await fetch(`/v1/videos/${encodeURIComponent(requestID)}/content`, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      Accept: 'video/mp4,video/*',
    },
    signal,
  })
  if (!response.ok) {
    const text = await response.text()
    const data = parseMaybeJSON(text)
    throw new ModelTestError(extractGatewayErrorMessage(data, text, response.status), response.status, data)
  }
  const blob = await response.blob()
  if (blob.size === 0) {
    throw new ModelTestError('Generated video content is empty', 502, null)
  }
  return blob
}

function extractVideoRequestID(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const obj = payload as Record<string, any>
  return String(obj.request_id || obj.id || obj.data?.request_id || obj.data?.id || obj.video?.request_id || obj.video?.id || '').trim()
}

export function extractVideoURL(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const obj = payload as Record<string, any>
  return String(obj.url || obj.video_url || obj.data?.url || obj.data?.video_url || obj.video?.url || obj.video?.video_url || '').trim()
}

function extractVideoStatus(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const obj = payload as Record<string, any>
  return String(obj.status || obj.data?.status || obj.video?.status || '').trim().toLowerCase()
}

function waitForPoll(ms: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) return Promise.reject(new DOMException('Aborted', 'AbortError'))
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms)
    signal?.addEventListener('abort', () => {
      window.clearTimeout(timer)
      reject(new DOMException('Aborted', 'AbortError'))
    }, { once: true })
  })
}

export default {
  testChatCompletion,
  testImageGeneration,
  testImageEdit,
  testVideoGeneration,
  fetchVideoContent,
}
