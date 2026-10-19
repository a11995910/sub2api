import { apiClient } from '../client'

export interface PelicanAccountResult {
  account_id: number
  run_id: string
  status: 'running' | 'success' | 'failed' | 'stopped'
  model_id: string
  reasoning_effort: string
  response_text?: string
  error_message: string
  started_at: string
  finished_at: string | null
  running_until: string
}
export interface PelicanAccountRun { account_id: number; run_id: string }
export interface PelicanAccountStart {
  all?: boolean
  account_ids?: number[]
  models: Record<string, string>
  reasoning_effort: string
}
export const pelicanAccountsAPI = {
  async list(signal?: AbortSignal): Promise<PelicanAccountResult[]> {
    const { data } = await apiClient.get<PelicanAccountResult[]>('/admin/pelican-account-tests', { signal })
    return data
  },
  async get(id: number, signal?: AbortSignal): Promise<PelicanAccountResult> {
    const { data } = await apiClient.get<PelicanAccountResult>(`/admin/pelican-account-tests/${id}`, { signal })
    return data
  },
  async start(input: PelicanAccountStart): Promise<PelicanAccountResult[]> {
    const { data } = await apiClient.post<PelicanAccountResult[]>('/admin/pelican-account-tests/run', input)
    return data
  },
  async stop(runs: PelicanAccountRun[]): Promise<void> {
    await apiClient.post('/admin/pelican-account-tests/stop', { runs })
  },
}
