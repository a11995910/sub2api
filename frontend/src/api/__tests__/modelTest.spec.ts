import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchVideoContent, testImageEdit, testImageGeneration, testVideoGeneration } from '@/api/modelTest'

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
    expect(payload).not.toHaveProperty('response_format')
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
    expect(form.has('response_format')).toBe(false)
    const uploaded = form.get('image') as File | null
    expect(uploaded).toBeInstanceOf(File)
    expect(uploaded?.name).toBe('source.png')
    expect(uploaded?.type).toBe('image/png')
    expect(uploaded?.size).toBe(image.size)
  })

  it('视频生成创建任务后立即返回并标记为测试台请求', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"request_id":"video-123","status":"queued"}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await testVideoGeneration({
      apiKey: 'sk-test',
      model: 'grok-imagine-video-1.5',
      prompt: '海浪慢镜头',
      resolution: '720p',
      duration: 10,
      startingImageDataUrl: 'data:image/png;base64,aW1n',
    })

    const [createPath, createInit] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(createPath).toBe('/v1/videos')
    expect(JSON.parse(String(createInit.body))).toEqual({
      model: 'grok-imagine-video-1.5',
      prompt: '海浪慢镜头',
      resolution: '720p',
      duration: 10,
      image: { url: 'data:image/png;base64,aW1n' },
    })
    expect(createInit.headers).toEqual(expect.objectContaining({ 'X-Sub2API-Model-Test': 'video' }))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(result).toEqual({
      payload: expect.objectContaining({ status: 'queued' }),
      requestID: 'video-123',
    })
  })

  it('视频生成不再使用客户端总超时或发起状态轮询', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"task_id":"video-slow","status":"in_progress"}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await testVideoGeneration({
      apiKey: 'sk-test',
      model: 'jing-video-2-pro',
      prompt: '慢速视频任务',
      pollIntervalMs: 0,
      timeoutMs: 1,
    })

    expect(result.requestID).toBe('video-slow')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('标准视频模型会把多张参考图传给 reference_images', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"task_id":"video-456","status":"queued"}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await testVideoGeneration({
      apiKey: 'sk-test',
      model: 'grok-imagine-video',
      prompt: '展示产品细节',
      referenceImageDataUrls: [
        'data:image/jpeg;base64,one',
        'data:image/jpeg;base64,two',
      ],
      pollIntervalMs: 0,
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(String(init.body))).toEqual({
      model: 'grok-imagine-video',
      prompt: '展示产品细节',
      reference_images: [
        { url: 'data:image/jpeg;base64,one' },
        { url: 'data:image/jpeg;base64,two' },
      ],
    })
  })

  it('视频内容通过带 API Key 的受限网关接口下载', async () => {
    const videoBlob = new Blob(['video-content'], { type: 'video/mp4' })
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      blob: () => Promise.resolve(videoBlob),
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchVideoContent('video /123', 'sk-test')

    expect(result).toBe(videoBlob)
    expect(fetchMock).toHaveBeenCalledWith('/v1/videos/video%20%2F123/content', expect.objectContaining({
      method: 'GET',
      headers: {
        Authorization: 'Bearer sk-test',
        Accept: 'video/mp4,video/*',
      },
    }))
  })
})
