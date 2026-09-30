import { apiClient } from '../client'

export interface AccountRotationSlot {
  start: string
  end: string
  account_ids: number[]
  active_priority: number
  inactive_priority: number
}

export interface AccountTimeRotationConfig {
  enabled: boolean
  revision: number
  slots: AccountRotationSlot[]
}

const endpoint = '/admin/intelligent-ops/time-rotation'

export const timeRotationAPI = {
  async get(): Promise<AccountTimeRotationConfig> {
    const { data } = await apiClient.get<AccountTimeRotationConfig>(endpoint)
    return data
  },
  async save(config: AccountTimeRotationConfig): Promise<AccountTimeRotationConfig> {
    const { data } = await apiClient.put<AccountTimeRotationConfig>(endpoint, config)
    return data
  }
}
