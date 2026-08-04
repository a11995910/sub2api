import { apiClient } from './client'

export interface OAuthAccountPoolWindow {
  utilization: number
  resets_at: string | null
}

export interface OAuthAccountPoolRequestTokenStats {
  requests: number
  tokens: number
}

export interface OAuthAccountPoolAccountStats {
  five_hour: OAuthAccountPoolRequestTokenStats
  seven_day: OAuthAccountPoolRequestTokenStats
  total: OAuthAccountPoolRequestTokenStats
}

export interface OAuthAccountPoolSummary {
  account_count: number
  requests?: number
  tokens?: number
}

export interface OAuthAccountPoolAccount {
  identifier: string
  plan_type: string
  current_concurrency: number
  concurrency: number
  usage: {
    five_hour: OAuthAccountPoolWindow | null
    seven_day: OAuthAccountPoolWindow | null
  }
  stats?: OAuthAccountPoolAccountStats
}

export interface OAuthAccountPoolGroup {
  name: string
  accounts: OAuthAccountPoolAccount[]
  summary: OAuthAccountPoolSummary
}

export interface OAuthAccountPoolResponse {
  groups: OAuthAccountPoolGroup[]
  summary: OAuthAccountPoolSummary
}

export async function getOAuthAccountPool(): Promise<OAuthAccountPoolResponse> {
  const { data } = await apiClient.get<OAuthAccountPoolResponse>('/oauth-account-pool')
  return data
}

export const oauthAccountPoolAPI = {
  get: getOAuthAccountPool
}
