import { afterEach, describe, expect, it, vi } from 'vitest'
import { createCreativeImageEdit, createCreativeImageGeneration } from '@/api/creativeDrawing'

describe('creativeDrawing api', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('把 b64_json 图片响应转换为可展示 data URL，并继承顶层输出格式', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({
        created: 1710000007,
        output_format: 'webp',
        size: '1024x1024',
        data: [{ b64_json: 'YWJj', revised_prompt: '画一张海报' }]
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    const images = await createCreativeImageGeneration({
      apiKey: 'sk-test',
      model: 'auto',
      prompt: '画一张海报',
      count: 1,
      outputFormat: 'webp'
    })

    expect(images[0]).toMatchObject({
      url: 'data:image/webp;base64,YWJj',
      b64_json: 'YWJj',
      output_format: 'webp',
      size: '1024x1024',
      created_at: 1710000007
    })
  })

  it('兼容网关返回 image_url 或 download_url 的图片字段', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({
        data: [
          { image_url: 'https://cdn.example.com/image-a.png' },
          { download_url: 'https://cdn.example.com/image-b.png' }
        ]
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    const images = await createCreativeImageGeneration({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '画两张图',
      count: 2,
      outputFormat: 'png'
    })

    expect(images.map((item) => item.url)).toEqual([
      'https://cdn.example.com/image-a.png',
      'https://cdn.example.com/image-b.png'
    ])
  })

  it('网关把 result 写成图片 URL 时按 URL 处理，不写成伪 base64', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({
        data: [{ result: 'http://192.0.2.10:3000/images/generated.png' }]
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    const images = await createCreativeImageGeneration({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '画一张图',
      count: 1,
      outputFormat: 'png'
    })

    expect(images[0].url).toBe('http://192.0.2.10:3000/images/generated.png')
    expect(images[0].b64_json).toBeUndefined()
  })

  it('网关返回不可展示 url 但同时包含 b64_json 时优先展示 base64 图片', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({
        data: [{ url: 'file-service://file_123', b64_json: 'QUJD' }]
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    const images = await createCreativeImageGeneration({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '画一张图',
      count: 1,
      outputFormat: 'png'
    })

    expect(images[0].url).toBe('data:image/png;base64,QUJD')
  })

  it('网关同时返回受保护 url 和 b64_json 时优先展示 base64 图片', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({
        data: [{
          url: 'http://192.0.2.10:3000/images/generated.png',
          b64_json: 'UE5HREFUQQ=='
        }]
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    const images = await createCreativeImageGeneration({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '画一张图',
      count: 1,
      outputFormat: 'png'
    })

    expect(images[0].url).toBe('data:image/png;base64,UE5HREFUQQ==')
    expect(images[0].source_url).toBe('http://192.0.2.10:3000/images/generated.png')
  })

  it('兼容上游把 data URL 包在 markdown 图片里的响应', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({
        data: [{ result: '![image](data:image/webp;base64,V0VCUA==)' }]
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    const images = await createCreativeImageGeneration({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '画一张图',
      count: 1,
      outputFormat: 'webp'
    })

    expect(images[0].url).toBe('data:image/webp;base64,V0VCUA==')
  })

  it('市场远程参考图通过 JSON images[].image_url 调用图片编辑接口', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({
        data: [{ b64_json: 'ZWRpdGVk' }]
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    await createCreativeImageEdit({
      apiKey: 'sk-test',
      model: 'gpt-image-2',
      prompt: '参考这张图生成海报',
      count: 1,
      outputFormat: 'png',
      imageUrls: [' https://cdn.example.com/ref.png ']
    })

    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const payload = JSON.parse(String(init.body)) as Record<string, any>
    expect(path).toBe('/v1/images/edits')
    expect(init.headers).toMatchObject({
      Authorization: 'Bearer sk-test',
      'Content-Type': 'application/json'
    })
    expect(payload.images).toEqual([{ image_url: 'https://cdn.example.com/ref.png' }])
  })
})
