import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  VIDEO_REFERENCE_IMAGE_MAX_BYTES,
  compressVideoReferenceImage,
  supportsVideoStartingImage,
} from '@/utils/videoReferenceImage'

describe('supportsVideoStartingImage', () => {
  it.each([
    'grok-imagine-video-1.5',
    'Grok-Imagine-Video-1.5-Preview',
    'grok-imagine-video-1.5-2026-05-30',
  ])('允许 %s 使用起始图', (model) => {
    expect(supportsVideoStartingImage(model)).toBe(true)
  })

  it.each([
    'grok-imagine-video',
    'custom-video',
    '',
  ])('禁止 %s 使用起始图', (model) => {
    expect(supportsVideoStartingImage(model)).toBe(false)
  })
})

describe('compressVideoReferenceImage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('未超过限制时保留原文件', async () => {
    const file = new File(['small'], 'reference.png', { type: 'image/png' })

    await expect(compressVideoReferenceImage(file)).resolves.toEqual({
      file,
      compressed: false,
      originalSize: file.size,
    })
  })

  it('超过限制时缩小尺寸并转换为限制内的 JPEG', async () => {
    const close = vi.fn()
    const drawImage = vi.fn()
    const canvas = {
      width: 0,
      height: 0,
      getContext: vi.fn(() => ({
        fillStyle: '',
        fillRect: vi.fn(),
        drawImage,
      })),
      toBlob: vi.fn((callback: BlobCallback) => {
        callback(new Blob([new Uint8Array(800 * 1024)], { type: 'image/jpeg' }))
      }),
    }
    vi.stubGlobal('createImageBitmap', vi.fn(async () => ({ width: 4096, height: 2048, close })))
    vi.spyOn(document, 'createElement').mockImplementation(((tagName: string) => {
      if (tagName === 'canvas') return canvas as unknown as HTMLCanvasElement
      return document.createElementNS('http://www.w3.org/1999/xhtml', tagName) as HTMLElement
    }) as typeof document.createElement)
    const source = new File([new Uint8Array(VIDEO_REFERENCE_IMAGE_MAX_BYTES + 1)], 'reference.png', {
      type: 'image/png',
      lastModified: 123,
    })

    const result = await compressVideoReferenceImage(source)

    expect(result.compressed).toBe(true)
    expect(result.originalSize).toBe(source.size)
    expect(result.file.name).toBe('reference.jpg')
    expect(result.file.type).toBe('image/jpeg')
    expect(result.file.size).toBeLessThanOrEqual(VIDEO_REFERENCE_IMAGE_MAX_BYTES)
    expect(canvas.width).toBe(2048)
    expect(canvas.height).toBe(1024)
    expect(drawImage).toHaveBeenCalled()
    expect(close).toHaveBeenCalled()
  })

  it('浏览器无法生成压缩文件时返回错误', async () => {
    const canvas = {
      width: 0,
      height: 0,
      getContext: vi.fn(() => ({ fillStyle: '', fillRect: vi.fn(), drawImage: vi.fn() })),
      toBlob: vi.fn((callback: BlobCallback) => callback(null)),
    }
    vi.stubGlobal('createImageBitmap', vi.fn(async () => ({ width: 1000, height: 1000, close: vi.fn() })))
    vi.spyOn(document, 'createElement').mockImplementation(((tagName: string) => {
      if (tagName === 'canvas') return canvas as unknown as HTMLCanvasElement
      return document.createElementNS('http://www.w3.org/1999/xhtml', tagName) as HTMLElement
    }) as typeof document.createElement)
    const source = new File([new Uint8Array(VIDEO_REFERENCE_IMAGE_MAX_BYTES + 1)], 'reference.png', { type: 'image/png' })

    await expect(compressVideoReferenceImage(source)).rejects.toThrow('image compression failed')
  })
})
