import { describe, expect, it } from 'vitest'
import type { UserAvailableGroup } from '@/api/channels'
import { filterGroupsByModelKind, selectAvailableModelKind } from '../modelKind'

describe('selectAvailableModelKind', () => {
  it('优先保留当前有可用模型的模式', () => {
    const models = [
      { kind: 'token' as const },
      { kind: 'image' as const },
      { kind: 'video' as const },
    ]

    expect(selectAvailableModelKind(models, 'token')).toBe('token')
    expect(selectAvailableModelKind(models, 'image')).toBe('image')
    expect(selectAvailableModelKind(models, 'video')).toBe('video')
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

  it('当前模式没有模型时可切换到视频模式', () => {
    expect(selectAvailableModelKind([{ kind: 'video' as const }], 'image')).toBe('video')
  })
})

describe('filterGroupsByModelKind', () => {
  const baseGroup: UserAvailableGroup = {
    id: 1,
    name: '分组',
    platform: 'openai',
    subscription_type: 'standard',
    rate_multiplier: 1,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    allow_image_generation: false,
    image_super_resolution_enabled: false,
    image_rate_independent: false,
    cache_hit_quarter_to_input_enabled: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
  }

  it('图片模式只展示开启图片能力的分组', () => {
    const groups = [
      { ...baseGroup, id: 1, allow_image_generation: false },
      { ...baseGroup, id: 2, allow_image_generation: true },
    ]

    expect(filterGroupsByModelKind(groups, 'image').map((group) => group.id)).toEqual([2])
  })

  it('文本模式保留 Grok 这类原生多模态分组', () => {
    const groups = [
      { ...baseGroup, id: 1, platform: 'openai', allow_image_generation: true },
      { ...baseGroup, id: 2, platform: 'grok', allow_image_generation: true },
      { ...baseGroup, id: 3, platform: 'anthropic', allow_image_generation: false },
    ]

    expect(filterGroupsByModelKind(groups, 'token').map((group) => group.id)).toEqual([2, 3])
  })

  it('视频模式只展示开启生成能力的分组', () => {
    const groups = [
      { ...baseGroup, id: 1, platform: 'grok', allow_image_generation: false },
      { ...baseGroup, id: 2, platform: 'grok', allow_image_generation: true },
    ]
    expect(filterGroupsByModelKind(groups, 'video').map((group) => group.id)).toEqual([2])
  })
})
