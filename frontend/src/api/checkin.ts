import { apiClient } from './client'

export interface CheckinSettings {
  enabled: boolean
  content: string
  daily_reward: number
  extra_reward_4: number
  extra_reward_16: number
}

export interface CheckinRecord {
  id: number
  user_id: number
  checkin_date: string
  daily_reward: number
  extra_reward: number
  month_count: number
  consecutive_count: number
  extra_milestones: number[]
  checked_in_at: string
  created_at: string
  updated_at: string
}

export interface CheckinMonthSummary {
  year: number
  month: number
  today: string
  today_checked: boolean
  month_count: number
  consecutive_count: number
  consecutive_cycle_day: number
  days_in_month: number
  records: CheckinRecord[]
  next_extra_milestone?: number
}

export interface CheckinOverview {
  settings: CheckinSettings
  summary: CheckinMonthSummary
}

export interface CheckinResult {
  record: CheckinRecord
  summary: CheckinMonthSummary
  reward: number
  new_balance: number
}

export async function getOverview(): Promise<CheckinOverview> {
  const { data } = await apiClient.get<CheckinOverview>('/checkin')
  return data
}

export async function checkin(): Promise<CheckinResult> {
  const { data } = await apiClient.post<CheckinResult>('/checkin')
  return data
}

export const checkinAPI = {
  getOverview,
  checkin,
}

export default checkinAPI
