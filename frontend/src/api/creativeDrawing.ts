export type CreativeImageModel = 'auto' | 'gpt-image-2' | 'gpt-image-1'
export type CreativeOutputFormat = 'png' | 'jpeg' | 'webp'

export const CREATIVE_IMAGE_MODEL_OPTIONS: Array<{ value: CreativeImageModel; label: string }> = [
  { value: 'auto', label: 'Auto' },
  { value: 'gpt-image-2', label: 'gpt-image-2' },
  { value: 'gpt-image-1', label: 'gpt-image-1' }
]

export const CREATIVE_OUTPUT_FORMAT_OPTIONS: Array<{ value: CreativeOutputFormat; label: string }> = [
  { value: 'png', label: 'PNG' },
  { value: 'jpeg', label: 'JPEG' },
  { value: 'webp', label: 'WebP' }
]

export type CreativeImageResult = {
  id: string
  url: string
  b64_json?: string
  revised_prompt?: string
  output_format?: CreativeOutputFormat | string
  size?: string
  created_at?: number
}

export type CreativeImageRequest = {
  apiKey: string
  prompt: string
  model: CreativeImageModel
  size?: string
  count: number
  outputFormat: CreativeOutputFormat
  imageUrls?: string[]
  files?: File[]
  signal?: AbortSignal
}

function resolveGatewayImageModel(model: CreativeImageModel) {
  return model === 'auto' ? 'gpt-image-2' : model
}

function imageMimeType(format?: string) {
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

function normalizeGatewayImageItem(item: Record<string, unknown>, index: number): CreativeImageResult {
  const outputFormat = typeof item.output_format === 'string' ? item.output_format : undefined
  const b64 = typeof item.b64_json === 'string' ? item.b64_json : ''
  const itemURL = typeof item.url === 'string' ? item.url : ''
  const url = itemURL || (b64 ? `data:${imageMimeType(outputFormat)};base64,${b64}` : '')

  return {
    id: typeof item.id === 'string' ? item.id : `${Date.now()}-${index}`,
    url,
    b64_json: b64 || undefined,
    revised_prompt: typeof item.revised_prompt === 'string' ? item.revised_prompt : undefined,
    output_format: outputFormat,
    size: typeof item.size === 'string' ? item.size : undefined,
    created_at: typeof item.created_at === 'number' ? item.created_at : undefined
  }
}

async function parseGatewayResponse(response: Response): Promise<CreativeImageResult[]> {
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    const errorPayload = payload as Record<string, any>
    const message =
      errorPayload?.error?.message ||
      errorPayload?.message ||
      errorPayload?.detail ||
      `图片请求失败：${response.status}`
    throw new Error(message)
  }

  const data = (payload as Record<string, unknown>).data
  if (!Array.isArray(data)) {
    throw new Error('图片接口返回结构无效：缺少 data 数组')
  }

  const images = data
    .filter((item): item is Record<string, unknown> => Boolean(item && typeof item === 'object'))
    .map(normalizeGatewayImageItem)
    .filter((item) => item.url)

  if (images.length === 0) {
    throw new Error('图片接口没有返回可展示的图片')
  }
  return images
}

export async function createCreativeImageGeneration(request: CreativeImageRequest) {
  const body: Record<string, unknown> = {
    model: resolveGatewayImageModel(request.model),
    prompt: request.prompt,
    n: request.count,
    response_format: 'b64_json',
    output_format: request.outputFormat
  }
  if (request.size) {
    body.size = request.size
  }

  const response = await fetch('/v1/images/generations', {
    method: 'POST',
    signal: request.signal,
    headers: {
      Authorization: `Bearer ${request.apiKey}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(body)
  })

  return parseGatewayResponse(response)
}

export async function createCreativeImageEdit(request: CreativeImageRequest) {
  const imageUrls = request.imageUrls?.map((url) => url.trim()).filter(Boolean) || []
  if (imageUrls.length > 0) {
    const body: Record<string, unknown> = {
      model: resolveGatewayImageModel(request.model),
      prompt: request.prompt,
      n: request.count,
      response_format: 'b64_json',
      output_format: request.outputFormat,
      images: imageUrls.map((url) => ({ image_url: url }))
    }
    if (request.size) {
      body.size = request.size
    }

    const response = await fetch('/v1/images/edits', {
      method: 'POST',
      signal: request.signal,
      headers: {
        Authorization: `Bearer ${request.apiKey}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(body)
    })

    return parseGatewayResponse(response)
  }

  if (!request.files?.length) {
    throw new Error('请先上传至少一张参考图')
  }

  const formData = new FormData()
  formData.append('model', resolveGatewayImageModel(request.model))
  formData.append('prompt', request.prompt)
  formData.append('n', String(request.count))
  formData.append('response_format', 'b64_json')
  formData.append('output_format', request.outputFormat)
  if (request.size) {
    formData.append('size', request.size)
  }
  request.files.forEach((file) => {
    formData.append('image', file, file.name || 'reference.png')
  })

  const response = await fetch('/v1/images/edits', {
    method: 'POST',
    signal: request.signal,
    headers: {
      Authorization: `Bearer ${request.apiKey}`
    },
    body: formData
  })

  return parseGatewayResponse(response)
}
