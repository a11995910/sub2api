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
  source_url?: string
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

function firstString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value !== 'string') {
      continue
    }
    const trimmed = value.trim()
    if (trimmed) {
      return trimmed
    }
  }
  return ''
}

function normalizeBase64ImageSource(value: string, outputFormat?: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }
  const markdownImage = trimmed.match(/!\[[^\]]*]\((data:image\/[^)\s]+)\)/i)
  if (markdownImage?.[1]) {
    return markdownImage[1]
  }
  const htmlImage = trimmed.match(/<img\b[^>]*\bsrc=["'](data:image\/[^"']+)["'][^>]*>/i)
  if (htmlImage?.[1]) {
    return htmlImage[1]
  }
  if (/^data:image\//i.test(trimmed)) {
    return trimmed
  }
  const normalized = normalizeBase64ImagePayload(trimmed)
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(normalized)) {
    return ''
  }
  return `data:${imageMimeType(outputFormat)};base64,${normalized}`
}

function normalizeBase64ImagePayload(value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }
  const markdownImage = trimmed.match(/!\[[^\]]*]\((data:image\/[^)\s]+)\)/i)
  if (markdownImage?.[1]) {
    return normalizeBase64ImagePayload(markdownImage[1])
  }
  const htmlImage = trimmed.match(/<img\b[^>]*\bsrc=["'](data:image\/[^"']+)["'][^>]*>/i)
  if (htmlImage?.[1]) {
    return normalizeBase64ImagePayload(htmlImage[1])
  }
  if (/^data:image\//i.test(trimmed)) {
    const [, content = ''] = trimmed.split(',', 2)
    return content.replace(/\s+/g, '')
  }
  return trimmed.replace(/\s+/g, '')
}

function normalizeDisplayableImageSource(value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }
  if (/^(data:image\/|https?:\/\/|blob:)/i.test(trimmed)) {
    return trimmed
  }
  if (/^\/\//.test(trimmed) && typeof window !== 'undefined') {
    return `${window.location.protocol}${trimmed}`
  }
  if (/^\//.test(trimmed) && typeof window !== 'undefined') {
    return `${window.location.origin}${trimmed}`
  }
  return ''
}

function normalizeGatewayImageItem(
  item: Record<string, unknown>,
  index: number,
  context: { outputFormat?: string; size?: string; createdAt?: number }
): CreativeImageResult {
  const outputFormat = firstString(item.output_format, context.outputFormat)
  const rawB64 = firstString(item.b64_json, item.base64, item.image_base64, item.result)
  const b64 = normalizeBase64ImagePayload(rawB64)
  const itemURL = normalizeDisplayableImageSource(firstString(item.url, item.image_url, item.download_url))
  const url = normalizeBase64ImageSource(rawB64, outputFormat) || itemURL

  return {
    id: typeof item.id === 'string' ? item.id : `${Date.now()}-${index}`,
    url,
    source_url: itemURL && itemURL !== url ? itemURL : undefined,
    b64_json: b64 || undefined,
    revised_prompt: typeof item.revised_prompt === 'string' ? item.revised_prompt : undefined,
    output_format: outputFormat || undefined,
    size: typeof item.size === 'string' ? item.size : context.size,
    created_at: typeof item.created_at === 'number' ? item.created_at : context.createdAt
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

  const responsePayload = payload as Record<string, unknown>
  const data = responsePayload.data
  if (!Array.isArray(data)) {
    throw new Error('图片接口返回结构无效：缺少 data 数组')
  }
  const context = {
    outputFormat: typeof responsePayload.output_format === 'string' ? responsePayload.output_format : undefined,
    size: typeof responsePayload.size === 'string' ? responsePayload.size : undefined,
    createdAt: typeof responsePayload.created === 'number' ? responsePayload.created : undefined
  }

  const images = data
    .filter((item): item is Record<string, unknown> => Boolean(item && typeof item === 'object'))
    .map((item, index) => normalizeGatewayImageItem(item, index, context))
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
