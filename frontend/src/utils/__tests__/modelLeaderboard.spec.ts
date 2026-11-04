import { describe, expect, it } from 'vitest'
import type { LeaderboardModel } from '@/api/modelLeaderboard'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import {
  matchLeaderboardGroups,
  normalizeLeaderboardModel,
  rankLeaderboard
} from '../modelLeaderboard'

describe('模型榜单的真实型号匹配', () => {
  it('AA 评测配置可关联基础型号，但不能移除日期版本', () => {
    const groups = [
      { id: 1, name: '基础型号', models: [{ name: 'gpt-6.1-sol' }] }
    ] as ModelPlazaGroup[]
    expect(
      matchLeaderboardGroups({ name: 'GPT-6.1 Sol (Max)', slug: 'gpt-6-1-sol-max' }, groups)
    ).toHaveLength(1)
    expect(
      matchLeaderboardGroups(
        { name: 'GPT-6.1 Sol (20261001)', slug: 'gpt-6-1-sol-20261001' },
        groups
      )
    ).toHaveLength(0)
  })
  it('延迟升序，不把缺失值排在第一', () => {
    const models = [
      { id: 'slow', scores: { latency: 3 } },
      { id: 'fast', scores: { latency: 1 } },
      { id: 'missing', scores: { latency: null } }
    ] as LeaderboardModel[]
    expect(rankLeaderboard(models, 'latency', true).map((model) => model.id)).toEqual([
      'fast',
      'slow'
    ])
  })
  it('统一命名空间与分隔符，同时保留版本和推理后缀', () => {
    expect(normalizeLeaderboardModel('OpenAI/GPT-6.1-Sol')).toBe('gpt-6-1-sol')
    const groups = [
      { id: 1, name: '可见分组', models: [{ name: 'openai/gpt-6.1-sol' }] },
      { id: 2, name: '另一版本', models: [{ name: 'gpt-6-sol' }] },
      { id: 3, name: '推理变体', models: [{ name: 'gpt-6.1-sol-high' }] }
    ] as ModelPlazaGroup[]
    expect(matchLeaderboardGroups('gpt-6.1-sol', groups).map((group) => group.id)).toEqual([1])
    expect(matchLeaderboardGroups('gpt-6.1-sol-20261001', groups)).toEqual([])
    expect(matchLeaderboardGroups('gpt-6.1-sol', [])).toEqual([])
  })
  it('同分并列，零分保留，缺测不冒充零分', () => {
    const models = [
      { id: 'a', overall: 80, scores: { coding: 90 } },
      { id: 'b', overall: 80, scores: { coding: null } },
      { id: 'c', overall: 70, scores: { coding: 0 } }
    ] as LeaderboardModel[]
    expect(rankLeaderboard(models, 'overall').map((model) => model.rank)).toEqual([1, 1, 3])
    expect(rankLeaderboard(models, 'coding').map((model) => [model.id, model.score])).toEqual([
      ['a', 90],
      ['c', 0]
    ])
  })
})
