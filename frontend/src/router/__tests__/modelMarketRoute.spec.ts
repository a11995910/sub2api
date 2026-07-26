import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const routerPath = resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts')
const routerSource = readFileSync(routerPath, 'utf8')

describe('模型广场路由', () => {
  it('使用不与模型请求接口冲突的独立路径', () => {
    expect(routerSource).toContain("path: '/model-market'")
    expect(routerSource).not.toContain("path: '/models'")
  })
})
