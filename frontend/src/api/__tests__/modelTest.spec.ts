import { afterEach, describe, expect, it, vi } from 'vitest'
import { testImageEdit, testImageGeneration } from '@/api/modelTest'

describe('modelTest api', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('自适应图片尺寸不向网关传 size 字段', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"data":[]}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await testImageGeneration({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '生成 16:9 海报',
      size: '',
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const payload = JSON.parse(String(init.body)) as Record<string, unknown>
    expect(payload).not.toHaveProperty('size')
  })

  it('固定图片尺寸会按选择值传给网关', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"data":[]}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await testImageGeneration({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '生成方图',
      size: ' 1024x1024 ',
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const payload = JSON.parse(String(init.body)) as Record<string, unknown>
    expect(payload.size).toBe('1024x1024')
  })

  it('上传参考图时用 multipart 调用图片编辑接口', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"data":[]}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    const image = new File(['fake-image'], 'source.png', { type: 'image/png' })
    await testImageEdit({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '把背景改成夜景',
      size: '1536x1024',
      images: [image],
    })

    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const form = init.body as FormData
    expect(path).toBe('/v1/images/edits')
    expect(init.headers).toEqual({
      Authorization: 'Bearer sk-test',
      Accept: 'application/json',
    })
    expect(form.get('model')).toBe('gpt-image-2')
    expect(form.get('prompt')).toBe('把背景改成夜景')
    expect(form.get('size')).toBe('1536x1024')
    expect(form.get('n')).toBe('1')
    expect(form.get('response_format')).toBe('b64_json')
    const uploaded = form.get('image') as File | null
    expect(uploaded).toBeInstanceOf(File)
    expect(uploaded?.name).toBe('source.png')
    expect(uploaded?.type).toBe('image/png')
    expect(uploaded?.size).toBe(image.size)
  })
})
