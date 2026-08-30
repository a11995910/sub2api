import { apiClient } from './client'

export interface OAuthAccountPoolWindow {
  utilization: number
  resets_at: string | null
}

export interface OAuthAccountPoolSummary {
  account_count: number
}

export interface OAuthAccountPoolAccount {
  identifier: string
  plan_type: string
  current_concurrency: number
  concurrency: number
  expires_at: string | null
  usage: {
    five_hour: OAuthAccountPoolWindow | null
    seven_day: OAuthAccountPoolWindow | null
  }
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
