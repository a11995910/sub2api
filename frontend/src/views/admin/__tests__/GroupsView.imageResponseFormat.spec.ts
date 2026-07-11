import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

const viewSource = readFileSync('src/views/admin/GroupsView.vue', 'utf8')
const typesSource = readFileSync('src/types/index.ts', 'utf8')

describe('GroupsView 图片默认传输方式', () => {
  it('为分组类型和请求类型声明图片响应格式', () => {
    expect(typesSource.match(/image_response_format[^\n]+b64_json[^\n]+url/g)?.length).toBeGreaterThanOrEqual(3)
  })

  it('创建默认 Base64，编辑时兼容旧数据并回填 URL', () => {
    expect(viewSource).toContain('image_response_format: "b64_json"')
    expect(viewSource).toContain('group.image_response_format || "b64_json"')
  })

  it('在创建和编辑图片区提供选择控件及客户参数优先说明', () => {
    expect(viewSource.match(/data-testid="image-response-format"/g)).toHaveLength(2)
    expect(viewSource).toContain('admin.groups.imagePricing.responseFormatExplicitHint')
  })
})
