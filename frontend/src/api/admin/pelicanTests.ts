
import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface PelicanGroupTestConfig {
  question_kind?: string
  prompt: string
  reasoning_effort: string
  parallel_count: number
  model_id?: string
}

export interface PelicanGroupTestAttempt {
  account_id: number
  account_name: string
  error: string
}

export interface PelicanGroupTestResult {
  id: number
  plan_id: number
  group_id: number
  group_name?: string
  account_id: number
  account_name: string
  attempts: PelicanGroupTestAttempt[]
  status: 'success' | 'failed'
  response_text?: string
  error_message: string
  latency_ms: number
  cost_usd: number | null
  cost_incomplete: boolean
  pelican_config?: PelicanGroupTestConfig
  started_at: string
  finished_at: string
  created_at: string
}

export interface PelicanGroupTestPlan {
  id: number
  group_id: number
  group_name: string
  today_cost_usd: number
  total_cost_usd: number
  today_cost_incomplete: boolean
  total_cost_incomplete: boolean
  group_platform: string
  group_status: string
  model_id: string
  cron_expression: string
  enabled: boolean
  pelican_config: PelicanGroupTestConfig
  last_run_at: string | null
  next_run_at: string | null
  running_until?: string | null
  last_result?: PelicanGroupTestResult
  created_at: string
  updated_at: string
}

export interface PelicanGroupTestPlanInput {
  group_id: number
  model_id: string
  cron_expression: string
  enabled: boolean
  prompt: string
  reasoning_effort: string
  parallel_count: number
}

export interface PelicanShowcaseSettings {
  enabled: boolean
  api_enabled: boolean
  max_items: number
  auto_cleanup: boolean
  retention_days: number
}

export async function listPlans(): Promise<PelicanGroupTestPlan[]> {
  const { data } = await apiClient.get<PelicanGroupTestPlan[]>('/admin/pelican-group-tests')
  return data ?? []
}

export async function createPlan(input: PelicanGroupTestPlanInput): Promise<PelicanGroupTestPlan> {
  const { data } = await apiClient.post<PelicanGroupTestPlan>('/admin/pelican-group-tests', input)
  return data
}

export async function updatePlan(id: number, input: PelicanGroupTestPlanInput): Promise<PelicanGroupTestPlan> {
  const { data } = await apiClient.put<PelicanGroupTestPlan>(`/admin/pelican-group-tests/${id}`, input)
  return data
}

export async function deletePlan(id: number): Promise<void> {
  await apiClient.delete(`/admin/pelican-group-tests/${id}`)
}

export async function runPlan(id: number): Promise<void> {
  await apiClient.post(`/admin/pelican-group-tests/${id}/run`)
}

export async function listResults(page = 1, pageSize = 20, planId = 0, signal?: AbortSignal): Promise<PaginatedResponse<PelicanGroupTestResult>> {
  const { data } = await apiClient.get<PaginatedResponse<PelicanGroupTestResult>>('/admin/pelican-group-test-results', {
    params: { page, page_size: pageSize, plan_id: planId || undefined },
    signal,
  })
  return data
}

export async function getResult(id: number): Promise<PelicanGroupTestResult> {
  const { data } = await apiClient.get<PelicanGroupTestResult>(`/admin/pelican-group-test-results/${id}`)
  return data
}

export async function getShowcaseSettings(): Promise<PelicanShowcaseSettings> {
  const { data } = await apiClient.get<PelicanShowcaseSettings>('/admin/pelican-showcase/settings')
  return data
}

export async function updateShowcaseSettings(settings: PelicanShowcaseSettings): Promise<PelicanShowcaseSettings> {
  const { data } = await apiClient.put<PelicanShowcaseSettings>('/admin/pelican-showcase/settings', settings)
  return data
}

export const pelicanTestsAPI = {
  listPlans,
  createPlan,
  updatePlan,
  deletePlan,
  runPlan,
  listResults,
  getResult,
  getShowcaseSettings,
  updateShowcaseSettings,
}

export default pelicanTestsAPI
