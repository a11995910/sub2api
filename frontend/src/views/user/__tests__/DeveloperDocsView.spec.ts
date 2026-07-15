import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../DeveloperDocsView.vue'),
  'utf8'
)
const markdownSource = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../../../../public/developer-docs.md'),
  'utf8'
)

describe('DeveloperDocsView 开发文档契约', () => {
  it('作为独立公共页面提供 AI 投喂入口', () => {
    expect(source).not.toContain('<AppLayout>')
    expect(source).toContain('可直接把本页 URL 投喂给 AI 进行开发')
    expect(source).toContain('/developer-docs.md')
    expect(markdownSource).toContain('给 AI 的实现要求')
  })

  it('同时说明图片 Base64 与 URL 两种传输方式', () => {
    expect(source).toContain('response_format: b64_json')
    expect(source).toContain('response_format: url')
    expect(source).toContain('data[].b64_json')
    expect(source).toContain('data[].url')
    expect(source).toContain('默认 24 小时后失效')
  })

  it('覆盖文生图、图片编辑和远程参考图', () => {
    expect(source).toContain('/v1/images/generations')
    expect(source).toContain('/v1/images/edits')
    expect(source).toContain('images[].image_url')
    expect(source).toContain('multipart 本地图片')
  })

  it('说明视频创建、轮询和内容下载流程', () => {
    expect(source).toContain('/v1/videos/generations')
    expect(source).toContain('/v1/videos/{request_id}')
    expect(source).toContain('/v1/videos/{request_id}/content')
    expect(source).toContain('reference_images[].url')
    expect(source).toContain('image.url')
  })

  it('纯文本版同步覆盖图片格式和视频三步流程', () => {
    expect(markdownSource).toContain('response_format')
    expect(markdownSource).toContain('b64_json')
    expect(markdownSource).toContain('data[].url')
    expect(markdownSource).toContain('/v1/videos/generations')
    expect(markdownSource).toContain('/v1/videos/{request_id}/content')
  })
})
