import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { LeaderboardModel } from '@/api/modelLeaderboard'

// 统一展示名/API 名的分隔符；型号日期和预览后缀保持不变。
export function normalizeLeaderboardModel(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/^(?:openai|anthropic|google|deepseek|qwen|moonshotai|x-ai)\//, '')
    .replace(/[\s_.-]+/g, '-')
}

// AA 的推理档位属于评测配置。仅移除明确的档位括号，匹配基础型号后仍提示配置差异。
export function leaderboardModelAliases(model: Pick<LeaderboardModel, 'name' | 'slug'>): string[] {
  const baseName = model.name.replace(
    /\s*\((?:max|xhigh|high|medium|low|non-reasoning)(?:(?:,?\s+default)?\s+fallback|\s+with\s+fallback)?\)\s*$/i,
    ''
  )
  return [...new Set([model.slug, baseName].map(normalizeLeaderboardModel))]
}

export function matchLeaderboardGroups(
  model: string | Pick<LeaderboardModel, 'name' | 'slug'>,
  groups: ModelPlazaGroup[]
) {
  const aliases =
    typeof model === 'string' ? [normalizeLeaderboardModel(model)] : leaderboardModelAliases(model)
  return groups.flatMap((group) => {
    const matched = group.models.find((item) =>
      aliases.includes(normalizeLeaderboardModel(item.name))
    )
    return matched ? [{ id: group.id, name: group.name, model: matched.name }] : []
  })
}

export function rankLeaderboard(models: LeaderboardModel[], metric: string, ascending = false) {
  const scored = models
    .flatMap((model) => {
      const score = metric === 'overall' ? model.overall : model.scores[metric]
      return score != null && Number.isFinite(score) ? [{ ...model, score }] : []
    })
    .sort((a, b) => (ascending ? a.score - b.score : b.score - a.score) || a.id.localeCompare(b.id))
  let rank = 0
  return scored.map((model, index) => {
    if (index === 0 || model.score !== scored[index - 1]?.score) rank = index + 1
    return { ...model, rank }
  })
}
