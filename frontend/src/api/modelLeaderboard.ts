import { apiClient } from './client'

export interface LeaderboardCategory {
  key: string
  name: string
  unit: string
  ascending: boolean
}

export interface LeaderboardModel {
  id: string
  name: string
  slug: string
  creator: string
  release_date?: string
  overall: number | null
  scores: Record<string, number | null>
  input_price: number | null
  output_price: number | null
}

export interface ModelLeaderboardResponse {
  source: string
  source_url: string
  methodology_url: string
  data_url: string
  synced_at: string
  refresh_interval_hours: number
  sync_enabled: boolean
  stale: boolean
  refreshing: boolean
  last_attempt_failed: boolean
  categories: LeaderboardCategory[]
  models: LeaderboardModel[]
}

export async function getModelLeaderboard(signal?: AbortSignal): Promise<ModelLeaderboardResponse> {
  const { data } = await apiClient.get<ModelLeaderboardResponse>('/model-leaderboard', { signal })
  return data
}
