import { apiClient } from '../client'
import type { LotteryAwardMode, LotteryConfig, LotteryDraw } from '../lottery'

export interface LotteryPrizeInput {
  id?: string
  name: string
  reward_amount: number
  probability_percent: number
}

export interface LotteryConfigInput {
  usage_threshold_m: number
  award_mode: LotteryAwardMode
  prizes: LotteryPrizeInput[]
}

export interface LotteryDrawPage {
  items: LotteryDraw[]
  total: number
  page: number
  page_size: number
}

export async function getConfig(): Promise<LotteryConfig> {
  const { data } = await apiClient.get<LotteryConfig>('/admin/lottery/config')
  return data
}

export async function updateConfig(input: LotteryConfigInput): Promise<LotteryConfig> {
  const { data } = await apiClient.put<LotteryConfig>('/admin/lottery/config', input)
  return data
}

export async function listDraws(page = 1, pageSize = 20): Promise<LotteryDrawPage> {
  const { data } = await apiClient.get<LotteryDrawPage>('/admin/lottery/draws', {
    params: { page, page_size: pageSize }
  })
  return data
}

export const lotteryAdminAPI = { getConfig, updateConfig, listDraws }

export default lotteryAdminAPI
