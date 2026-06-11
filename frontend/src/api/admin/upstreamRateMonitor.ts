/**
 * 管理端上游倍率监控 API。
 * 用于保存同类 Sub2API 上游站点，并读取其分组倍率快照。
 */

import { apiClient } from '../client'
import type { BasePaginationResponse } from '@/types'

export type UpstreamRateMonitorStatus = 'unknown' | 'success' | 'failed'

export interface UpstreamRateGroupSnapshot {
  id: number
  name: string
  description?: string
  platform?: string
  rate_multiplier: number
  image_rate_multiplier?: number | null
  image_rate_independent?: boolean
  subscription_type?: string
  is_exclusive: boolean
  status?: string
  rpm_limit?: number
  allow_image_generation?: boolean
  image_super_resolution_enabled?: boolean
  sort_order?: number
}

export interface UpstreamRateMonitor {
  id: number
  name: string
  base_url: string
  username: string
  password_masked: string
  password_decrypt_failed: boolean
  enabled: boolean
  last_checked_at: string | null
  last_status: UpstreamRateMonitorStatus
  last_error: string
  last_group_count: number
  last_snapshot: UpstreamRateGroupSnapshot[]
  created_by: number
  created_at: string
  updated_at: string
}

export interface ListParams {
  page?: number
  page_size?: number
  enabled?: boolean
  search?: string
}

export type ListResponse = BasePaginationResponse<UpstreamRateMonitor>

export interface CreateParams {
  name: string
  base_url: string
  username: string
  password: string
  enabled?: boolean
}

export type UpdateParams = Partial<CreateParams>

export async function list(
  params: ListParams = {},
  options?: { signal?: AbortSignal }
): Promise<ListResponse> {
  const { data } = await apiClient.get<ListResponse>('/admin/upstream-rate-monitors', {
    params,
    signal: options?.signal,
  })
  return data
}

export async function get(id: number): Promise<UpstreamRateMonitor> {
  const { data } = await apiClient.get<UpstreamRateMonitor>(`/admin/upstream-rate-monitors/${id}`)
  return data
}

export async function create(params: CreateParams): Promise<UpstreamRateMonitor> {
  const { data } = await apiClient.post<UpstreamRateMonitor>('/admin/upstream-rate-monitors', params)
  return data
}

export async function update(id: number, params: UpdateParams): Promise<UpstreamRateMonitor> {
  const { data } = await apiClient.put<UpstreamRateMonitor>(`/admin/upstream-rate-monitors/${id}`, params)
  return data
}

export async function del(id: number): Promise<void> {
  await apiClient.delete(`/admin/upstream-rate-monitors/${id}`)
}

export async function refresh(id: number): Promise<UpstreamRateMonitor> {
  const { data } = await apiClient.post<UpstreamRateMonitor>(`/admin/upstream-rate-monitors/${id}/refresh`)
  return data
}

export const upstreamRateMonitorAPI = {
  list,
  get,
  create,
  update,
  del,
  refresh,
}

export default upstreamRateMonitorAPI
