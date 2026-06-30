import { describe, expect, it } from 'vitest'
import { selectAvailableModelKind } from '../modelKind'

describe('selectAvailableModelKind', () => {
  it('优先保留当前有可用模型的模式', () => {
    const models = [
      { kind: 'token' as const },
      { kind: 'image' as const },
    ]

    expect(selectAvailableModelKind(models, 'token')).toBe('token')
    expect(selectAvailableModelKind(models, 'image')).toBe('image')
  })

  it('当前模式没有模型时自动切到另一个可用模式', () => {
    const models = [
      { kind: 'image' as const },
    ]

    expect(selectAvailableModelKind(models, 'token')).toBe('image')
  })

  it('没有任何模型时保持原模式', () => {
    expect(selectAvailableModelKind([], 'token')).toBe('token')
  })
})
