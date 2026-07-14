export const VIDEO_REFERENCE_IMAGE_MAX_BYTES = 1024 * 1024
export const VIDEO_REFERENCE_IMAGE_MAX_DIMENSION = 2048

export interface VideoReferenceImageCompressionResult {
  file: File
  compressed: boolean
  originalSize: number
}

interface DecodedImage {
  width: number
  height: number
  draw(ctx: CanvasRenderingContext2D, width: number, height: number): void
  close(): void
}

/** 起始图生视频是 video-1.5 的能力，标准版参考图属于另一种 reference-to-video 工作流。 */
export function supportsVideoStartingImage(modelName: string): boolean {
  return modelName.trim().toLowerCase().startsWith('grok-imagine-video-1.5')
}

export async function compressVideoReferenceImage(
  file: File,
  maxBytes = VIDEO_REFERENCE_IMAGE_MAX_BYTES,
): Promise<VideoReferenceImageCompressionResult> {
  if (file.size <= maxBytes) {
    return { file, compressed: false, originalSize: file.size }
  }

  const decoded = await decodeImage(file)
  try {
    let { width, height } = fitWithin(decoded.width, decoded.height, VIDEO_REFERENCE_IMAGE_MAX_DIMENSION)
    const qualityLevels = [0.9, 0.82, 0.74, 0.66, 0.58, 0.5, 0.42]

    for (let resizeAttempt = 0; resizeAttempt < 4; resizeAttempt += 1) {
      const canvas = document.createElement('canvas')
      canvas.width = width
      canvas.height = height
      const ctx = canvas.getContext('2d')
      if (!ctx) throw new Error('canvas unavailable')

      // JPEG 不支持透明通道，使用白色背景避免透明 PNG 被转换为黑底。
      ctx.fillStyle = '#ffffff'
      ctx.fillRect(0, 0, width, height)
      decoded.draw(ctx, width, height)

      for (const quality of qualityLevels) {
        const blob = await canvasToBlob(canvas, 'image/jpeg', quality)
        if (blob.size <= maxBytes) {
          return {
            file: new File([blob], jpegFileName(file.name), {
              type: 'image/jpeg',
              lastModified: file.lastModified,
            }),
            compressed: true,
            originalSize: file.size,
          }
        }
      }

      width = Math.max(320, Math.round(width * 0.8))
      height = Math.max(320, Math.round(height * 0.8))
    }
  } finally {
    decoded.close()
  }

  throw new Error('compressed image still exceeds the upload limit')
}

function fitWithin(width: number, height: number, maxDimension: number): { width: number; height: number } {
  if (width <= 0 || height <= 0) throw new Error('invalid image dimensions')
  const scale = Math.min(1, maxDimension / Math.max(width, height))
  return {
    width: Math.max(1, Math.round(width * scale)),
    height: Math.max(1, Math.round(height * scale)),
  }
}

function jpegFileName(name: string): string {
  const base = name.replace(/\.[^.]+$/, '').trim() || 'reference-image'
  return `${base}.jpg`
}

function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob)
      else reject(new Error('image compression failed'))
    }, type, quality)
  })
}

async function decodeImage(file: File): Promise<DecodedImage> {
  if (typeof window.createImageBitmap === 'function') {
    const bitmap = await window.createImageBitmap(file)
    return {
      width: bitmap.width,
      height: bitmap.height,
      draw: (ctx, width, height) => ctx.drawImage(bitmap, 0, 0, width, height),
      close: () => bitmap.close(),
    }
  }

  const objectURL = URL.createObjectURL(file)
  try {
    const image = await loadHTMLImage(objectURL)
    return {
      width: image.naturalWidth || image.width,
      height: image.naturalHeight || image.height,
      draw: (ctx, width, height) => ctx.drawImage(image, 0, 0, width, height),
      close: () => undefined,
    }
  } finally {
    URL.revokeObjectURL(objectURL)
  }
}

function loadHTMLImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('failed to decode image'))
    image.src = src
  })
}
