import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  buildPromptJSON,
  fetchAwesomeGptImage2Prompts,
  fetchBananaPrompts,
  getPromptApplyReferenceImageUrls,
  type BananaPrompt
} from '../promptMarket'

function buildPrompt(overrides: Partial<BananaPrompt>): BananaPrompt {
  return {
    id: 'prompt-1',
    title: '测试案例',
    preview: 'https://example.com/preview.png',
    referenceImageUrls: [],
    prompt: '画一张海报',
    author: 'tester',
    mode: 'generate',
    category: '海报',
    source: 'library-a',
    sourceLabel: '精选模板库 A',
    isNsfw: false,
    ...overrides
  }
}

describe('promptMarket apply references', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('市场案例带 reference_image_urls 时即使是文生图模式也加载参考图', () => {
    const urls = getPromptApplyReferenceImageUrls(buildPrompt({
      mode: 'generate',
      referenceImageUrls: [
        ' https://cdn.example.com/ref-1.png ',
        'https://cdn.example.com/ref-1.png',
        'https://cdn.example.com/ref-2.png'
      ]
    }))

    expect(urls).toEqual([
      'https://cdn.example.com/ref-1.png',
      'https://cdn.example.com/ref-2.png'
    ])
  })

  it('编辑案例没有显式参考图时才回退到预览图', () => {
    const urls = getPromptApplyReferenceImageUrls(buildPrompt({
      mode: 'edit',
      preview: 'https://example.com/edit-preview.png'
    }))

    expect(urls).toEqual(['https://example.com/edit-preview.png'])
  })

  it('普通文生图案例没有显式参考图时不把成品预览误当参考图', () => {
    const urls = getPromptApplyReferenceImageUrls(buildPrompt({
      mode: 'generate',
      preview: 'https://example.com/result-preview.png'
    }))

    expect(urls).toEqual([])
  })

  it('复制 JSON 不暴露模板来源链接和内部来源标识', () => {
    const json = buildPromptJSON(buildPrompt({
      link: 'https://github.com/example/repo',
      sourceLabel: '精选模板库 A'
    }))

    expect(json).not.toContain('source')
    expect(json).not.toContain('source_label')
    expect(json).not.toContain('link')
    expect(json).not.toContain('github.com')
  })

  it('读取模板库 A 时使用公开库别名路径', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve([
        {
          title: '海报模板',
          preview: 'images/poster.png',
          prompt: '画一张海报',
          author: 'tester',
          category: '海报'
        }
      ])
    })
    vi.stubGlobal('fetch', fetchMock)

    const prompts = await fetchBananaPrompts()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/creative-drawing/prompt-market/libraries/library-a/prompts',
      expect.objectContaining({
        headers: { Accept: 'application/json' }
      })
    )
    expect(prompts[0].preview).toBe('/api/v1/creative-drawing/prompt-market/assets/library-a/images/poster.png')
  })

  it('读取模板库 B 时使用公开库别名路径并解析新仓库图片代理', async () => {
    const markdown = `
## 海报

### Case 151: [商业海报](https://example.com/case-151) (by [@tester](https://example.com/tester))

| Output |
| :----: |
| <img src="/api/v1/creative-drawing/prompt-market/assets/library-b/api/images/poster_case151/output.jpg" width="300" alt="Output image"> |

**Prompt:**

\`\`\`
Create a premium product poster.
\`\`\`
`
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        text: () => Promise.resolve(markdown)
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        text: () => Promise.resolve(markdown)
      })
    vi.stubGlobal('fetch', fetchMock)

    const prompts = await fetchAwesomeGptImage2Prompts()

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/v1/creative-drawing/prompt-market/libraries/library-b/prompts/zh-CN',
      expect.objectContaining({
        headers: { Accept: 'text/markdown,text/plain' }
      })
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/v1/creative-drawing/prompt-market/libraries/library-b/prompts/en',
      expect.objectContaining({
        headers: { Accept: 'text/markdown,text/plain' }
      })
    )
    expect(prompts).toHaveLength(1)
    expect(prompts[0].preview).toBe('/api/v1/creative-drawing/prompt-market/assets/library-b/api/images/poster_case151/output.jpg')
  })
})
