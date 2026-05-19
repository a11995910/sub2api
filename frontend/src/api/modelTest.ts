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
  path: '/v1/chat/completions' | '/v1/images/generations',
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
    response_format: 'b64_json',
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
  form.set('response_format', 'b64_json')

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

export default {
  testChatCompletion,
  testImageGeneration,
  testImageEdit,
}
