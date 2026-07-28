import { apiClient } from './client'

export interface OAuthAccountPoolWindow {
  utilization: number
  resets_at: string | null
}

export interface OAuthAccountPoolAccount {
  name: string
  usage: {
    five_hour: OAuthAccountPoolWindow | null
    seven_day: OAuthAccountPoolWindow | null
  }
}

export interface OAuthAccountPoolGroup {
  name: string
  accounts: OAuthAccountPoolAccount[]
}

export interface OAuthAccountPoolResponse {
  groups: OAuthAccountPoolGroup[]
}

export async function getOAuthAccountPool(): Promise<OAuthAccountPoolResponse> {
  const { data } = await apiClient.get<OAuthAccountPoolResponse>('/oauth-account-pool')
  return data
}

export const oauthAccountPoolAPI = {
  get: getOAuthAccountPool
}
