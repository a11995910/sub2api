import { apiClient } from '../client'

export interface AccountRotationSlot {
  start: string
  end: string
  account_ids: number[]
  active_priority: number
  inactive_priority: number
}

export interface AccountRandomRestConfig {
  enabled: boolean
  account_ids: number[]
  check_interval_minutes: number
  probability_percent: number
  min_rest_minutes: number
  max_rest_minutes: number
  max_work_minutes: number
}

export interface AccountTimeRotationConfig {
  paced?: AccountPacedConfig
  random_rest?: AccountRandomRestConfig
  enabled: boolean
  revision: number
  strict_rest?: boolean
  request_limits?: { account_id: number; rpm: number }[]
  slots: AccountRotationSlot[]
  /** 未设置时兼容旧配置，等同于 manual。 */
  mode?: 'manual' | 'smart' | 'paced'
  smart?: SmartTimeRotationConfig
}

export interface AccountPacedConfig {
  account_id: number
  start: string
  end: string
  rpm: number
  sustained_rpm: number
  burst_capacity: number
  max_concurrency: number
  max_wait_seconds: number
}

export interface AccountPacedStatus {
  available_tokens: number
  sustained_rpm: number
  burst_capacity: number
  max_concurrency: number
  waiting: number
  blocked_by: string
  retry_after: number
  next_change_at: string
  schedule_open: boolean
}

export interface SmartRotationPeriod {
  start: string
  end: string
  primary_count: number
}

export interface SmartTimeRotationConfig {
  account_ids: number[]
  ttft_threshold_seconds?: number
  slow_request_count?: number
  healthy_request_count?: number
  sample_window_minutes?: number
  cooldown_minutes?: number
  waiting_priority?: number
  probe_interval_seconds?: number
  /** 兼容历史日程配置，智能模式不再使用。 */
  periods?: SmartRotationPeriod[]
  rotation_minutes?: number
  quota_reserve_percent?: number
}

export interface TimeRotationStatusAccount {
  account_id: number
  name: string
  role: 'normal' | 'cooling' | 'recovering' | 'unavailable'
  reason: string
  original_priority: number
  effective_priority: number
  slow_streak: number
  healthy_streak: number
  last_ttft_ms?: number | null
  last_duration_ms?: number | null
  last_sample_at?: string | null
  cooldown_until?: string | null
  next_probe_at?: string | null
}

export interface TimeRotationStatus {
  protection?: { account_id: number; state: string; rpm: number; used: number | null; random_state?: string; random_rest_until?: string; in_flight?: number; paced?: AccountPacedStatus }[]
  mode: 'manual' | 'smart' | 'paced'
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
