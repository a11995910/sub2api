import type { Account, OpenAITurnStateMode } from '@/types'

// 已移除的旧健康头模式按关闭处理，不自动启用门票。
export function resolveOpenAITurnStateMode(extra: Account['extra']): OpenAITurnStateMode {
  return extra?.openai_turn_state_mode === 'codex_ticket' ? 'codex_ticket' : 'off'
}
