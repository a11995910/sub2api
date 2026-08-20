import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchVideoContent, testChatCompletion, testImageEdit, testImageGeneration, testVideoGeneration } from '@/api/modelTest'

const refreshAuthTokens = vi.hoisted(() => vi.fn())

vi.mock('@/api/tokenRefresh', () => ({ refreshAuthTokens }))

describe('modelTest api', () => {
  beforeEach(() => {
    refreshAuthTokens.mockReset()
    localStorage.setItem('auth_token', 'panel-test-token')
  })

  afterEach(() => {
    localStorage.clear()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('文本测试同时携带 API Key 与站内会话证明', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"choices":[]}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await testChatCompletion({
      apiKey: 'sk-test',
      model: 'gpt-5.6-sol',
      prompt: '你好',
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.headers).toEqual(expect.objectContaining({
      Authorization: 'Bearer sk-test',
      'X-Sub2API-Model-Test': 'text',
      'X-Sub2API-Model-Test-Authorization': 'Bearer panel-test-token',
    }))
  })

  it('面板令牌临近过期时刷新后再发起测试', async () => {
    localStorage.setItem('refresh_token', 'panel-refresh-token')
    localStorage.setItem('token_expires_at', String(Date.now() + 30_000))
    refreshAuthTokens.mockResolvedValue({
      access_token: 'refreshed-panel-token',
      refresh_token: 'refreshed-panel-refresh-token',
      expires_in: 3600,
      token_type: 'Bearer',
    })
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"choices":[]}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await testChatCompletion({
      apiKey: 'sk-test',
      model: 'gpt-5.6-sol',
      prompt: '你好',
    })

    expect(refreshAuthTokens).toHaveBeenCalledWith({ failedAccessToken: 'panel-test-token' })
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.headers).toEqual(expect.objectContaining({
      Authorization: 'Bearer sk-test',
      'X-Sub2API-Model-Test-Authorization': 'Bearer refreshed-panel-token',
    }))
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
    expect(init.headers).toEqual(expect.objectContaining({
      Authorization: 'Bearer sk-test',
      Accept: 'application/json',
      'X-Sub2API-Model-Test': 'image',
      'X-Sub2API-Model-Test-Authorization': 'Bearer panel-test-token',
    }))
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

  it('Seedance 2.5 固定清晰度模型不发送 resolution 且保留 30 秒', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"task_id":"video-25","status":"queued"}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await testVideoGeneration({
      apiKey: 'sk-test',
      model: 'sd4-seedance-2.5-480p',
      prompt: '长镜头',
      resolution: '480p',
      duration: 30,
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(String(init.body))).toEqual({
      model: 'sd4-seedance-2.5-480p',
      prompt: '长镜头',
      duration: 30,
    })
  })

  it('sd8 Seedance 使用文档允许的离散时长', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve('{"task_id":"video-sd8","status":"queued"}'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await testVideoGeneration({
      apiKey: 'sk-test',
      model: 'sd8-seedance-2.0',
      prompt: '短镜头',
      resolution: '720p',
      duration: 8,
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(String(init.body))).toEqual({
      model: 'sd8-seedance-2.0',
      prompt: '短镜头',
      duration: 5,
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
