import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export const DEFAULT_CANDY_PROMPT = `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）
苹果味 桃子味 西瓜味
圆形 7 9 8
五角星形 7 6 4`

export interface CandyTestSettingsInput {
  prompt: string
  expected_answer: string
  model_id: string
  reasoning_effort: string
  schedule_enabled: boolean
  interval_hours: number
  auto_group_enabled: boolean
  auto_group_excluded_account_ids: number[]
  normal_add_group_ids: number[]
  normal_remove_group_ids: number[]
  degraded_add_group_ids: number[]
  degraded_remove_group_ids: number[]
}

export interface CandyTestSettings extends CandyTestSettingsInput {
  last_run_at?: string | null
  next_run_at?: string | null
  running_until?: string | null
  running_run_id: string
}

export interface CandyTestAccount {
  id: number
  name: string
}

export interface CandyTestResult {
  id: number
  account_id: number
  account_name: string
  run_id: string
  status: 'running' | 'normal' | 'degraded' | 'error' | 'skipped'
  answer: string
  response_text?: string
  error_message: string
  model_id: string
  reasoning_effort: string
  started_at: string
  finished_at?: string | null
  running_until?: string | null
  settings_snapshot?: CandyTestSettingsInput
  group_action?: { added_group_ids: number[]; removed_group_ids: number[]; skipped_reason?: 'account_excluded' } | null
  group_error: string
}

export interface CandyTestBatch {
  run_id: string
  settings: CandyTestSettingsInput
  results: CandyTestResult[]
}

export const candyTestsAPI = {
  async getAccounts(signal?: AbortSignal): Promise<CandyTestAccount[]> {
    const { data } = await apiClient.get<CandyTestAccount[]>('/admin/candy-tests/accounts', { signal })
    return data
  },
  async getSettings(signal?: AbortSignal): Promise<CandyTestSettings> {
    const { data } = await apiClient.get<CandyTestSettings>('/admin/candy-tests/settings', { signal })
    return data
  },
  async updateSettings(input: CandyTestSettingsInput): Promise<CandyTestSettings> {
    const { data } = await apiClient.put<CandyTestSettings>('/admin/candy-tests/settings', input)
    return data
  },
  async run(input: { all: boolean; account_ids?: number[] }): Promise<CandyTestBatch> {
    const { data } = await apiClient.post<CandyTestBatch>('/admin/candy-tests/run', input)
    return data
  },
  async stop(runId: string): Promise<void> {
    await apiClient.post('/admin/candy-tests/stop', { run_id: runId })
  },
  async listResults(page = 1, pageSize = 20, signal?: AbortSignal): Promise<PaginatedResponse<CandyTestResult>> {
    const { data } = await apiClient.get<PaginatedResponse<CandyTestResult>>('/admin/candy-tests/results', {
      params: { page, page_size: pageSize }, signal,
    })
    return data
  },
  async getResult(id: number): Promise<CandyTestResult> {
    const { data } = await apiClient.get<CandyTestResult>(`/admin/candy-tests/results/${id}`)
    return data
  },
}
