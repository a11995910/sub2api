import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../DeveloperDocsView.vue'),
  'utf8'
)

describe('DeveloperDocsView 开发文档契约', () => {
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
})
