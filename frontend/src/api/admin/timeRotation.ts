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
  /** 未设置时兼容旧配置，等同于 manual。 */
  mode?: 'manual' | 'smart'
  smart?: SmartTimeRotationConfig
}

export interface SmartRotationPeriod {
  start: string
  end: string
  primary_count: number
}

export interface SmartTimeRotationConfig {
  account_ids: number[]
  periods: SmartRotationPeriod[]
  rotation_minutes: number
  quota_reserve_percent: number
}

export interface TimeRotationStatusAccount {
  account_id: number
  name: string
  role: 'primary' | 'standby' | 'protected' | 'unavailable'
  reason: string
  quota_7d_remaining?: number | null
  quota_5h_remaining?: number | null
}

export interface TimeRotationStatus {
  mode: 'manual' | 'smart'
  enabled: boolean
  revision: number
  ready: boolean
  updated_at: string | null
  period: SmartRotationPeriod | AccountRotationSlot | null
  next_rotation_at: string | null
  accounts: TimeRotationStatusAccount[]
  scope: 'all' | 'group'
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
  },
  async status(groupId?: number): Promise<TimeRotationStatus> {
    const { data } = await apiClient.get<TimeRotationStatus>(`${endpoint}/status`, {
      params: groupId == null ? undefined : { group_id: groupId }
    })
    return data
  }
}
