import { describe, expect, it } from 'vitest'

import { mergeLocaleMessages } from '../mergeLocaleMessages'
import customEn from '../locales/en.ts'
import splitEn from '../locales/en/index'
import customZh from '../locales/zh.ts'
import splitZh from '../locales/zh/index'

function leafPaths(value: unknown, prefix = ''): string[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [prefix]
  return Object.entries(value).flatMap(([key, child]) =>
    leafPaths(child, prefix ? `${prefix}.${key}` : key)
  )
}

function readPath(source: Record<string, any>, path: string): unknown {
  return path.split('.').reduce<unknown>((value, key) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[key]
  }, source)
}

describe.each([
  ['en', splitEn, customEn],
  ['zh', splitZh, customZh]
])('locale message merge %s', (_, split, custom) => {
  const merged = mergeLocaleMessages(split, custom)

  it('包含拆分语言包与定制语言包的全部路径', () => {
    const mergedPaths = new Set(leafPaths(merged))
    const expectedPaths = new Set([...leafPaths(split), ...leafPaths(custom)])
    expect([...expectedPaths].filter((path) => !mergedPaths.has(path))).toEqual([])
  })

  it('同名路径保留定制文案', () => {
    expect(readPath(merged, 'home.dashboard')).toBe(readPath(custom, 'home.dashboard'))
  })
})

describe('中文运行时关键文案', () => {
  const merged = mergeLocaleMessages(splitZh, customZh)

  it.each([
    ['admin.dashboard.groupPricing', '分组定价'],
    ['admin.dashboard.groupPricingDesc', '设置批量折扣和冻结比例'],
    ['usage.tabs.ranking', '用户排行'],
    ['usage.latencyFirstToken', '首字'],
    ['usage.latencyDuration', '总耗时']
  ])('%s 不直接显示翻译键', (path, expected) => {
    expect(readPath(merged, path)).toBe(expected)
  })
})
