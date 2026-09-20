import type { Account, OpenAITurnStateMode } from '@/types'

export function resolveOpenAITurnStateMode(extra: Account['extra']): OpenAITurnStateMode {
  const mode = extra?.openai_turn_state_mode
  if (mode === 'off' || mode === 'healthy_retry' || mode === 'healthy_preflight' || mode === 'codex_ticket') return mode
  return extra?.openai_healthy_turn_state_replace === true ? 'healthy_retry' : 'off'
}

export function isHealthyTurnStateMode(mode: OpenAITurnStateMode): boolean {
  return mode === 'healthy_retry' || mode === 'healthy_preflight'
}
