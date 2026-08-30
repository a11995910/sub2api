import { apiClient } from './client'

export type LotteryAwardMode = 'daily_once' | 'per_threshold'

export interface LotteryPrize {
  id: string
  name: string
  reward_amount: number
  probability_percent: number
  is_thanks: boolean
}

export interface LotteryConfig {
  enabled: boolean
  usage_threshold_m: number
  usage_threshold_tokens: number
  award_mode: LotteryAwardMode
  prizes: LotteryPrize[]
  thanks_probability_percent: number
  version: number
  updated_at: string
}

export interface LotteryUserState {
  available_chances: number
  total_earned: number
  total_drawn: number
  today_usage_tokens: number
  today_threshold_tokens: number
  today_award_mode: LotteryAwardMode
  today_awarded_chances: number
  today_next_target_tokens: number
  today_qualified: boolean
}

export interface LotteryDraw {
  id: number
  user_id: number
  user_email?: string
  prize_id: string
  prize_name: string
  reward_amount: number
  probability_percent: number
  config_version: number
  chance_before: number
  chance_after: number
  balance_after: number
  created_at: string
}

export interface LotteryOverview {
  config: LotteryConfig
  state: LotteryUserState
  today_usage_m: number
  today_next_target_m: number
  today_progress_percent: number
  recent_draws: LotteryDraw[]
}

export interface LotteryDrawResult {
  draw: LotteryDraw
  available_chances: number
  new_balance: number
}

export async function getOverview(): Promise<LotteryOverview> {
  const { data } = await apiClient.get<LotteryOverview>('/lottery')
  return data
}

export async function draw(): Promise<LotteryDrawResult> {
  const { data } = await apiClient.post<LotteryDrawResult>('/lottery/draw')
  return data
}

export const lotteryAPI = { getOverview, draw }

export default lotteryAPI
