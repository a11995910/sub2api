import { describe, expect, it } from 'vitest'
import { buildPromptJSON, getPromptApplyReferenceImageUrls, type BananaPrompt } from '../promptMarket'

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
})
