import { apiClient } from '../client'

export interface ExcelBPSDefaults {
  all_models: boolean
  models: string[]
  omit_unsupported_tools: boolean
  ignore_encrypted_content: boolean
  auto_disable_on_403: boolean
  cache_creation_as_input: boolean
}
export interface ExcelBPSConfig {
  enabled: boolean
  defaults: ExcelBPSDefaults
  images: {
    enabled: boolean
    mode: 'native' | 'relay'
    base_url: string
    body_limit_mib: number
    budget_mib: number
    max_requests: number
    policy: 'off' | 'warn' | 'auto_compact'
    warning_remaining: number
    compact_reserve: number
    limits: { MaxImageMiB: number; MaxImages: number; MaxTotalMiB: number; StorageMiB: number; StorageEntries: number; TTLMinutes: number }
  }
}
export const excelBpsAPI = {
  async get(): Promise<ExcelBPSConfig> { return (await apiClient.get('/admin/intelligent-ops/excel-bps')).data },
  async save(config: Partial<ExcelBPSConfig>): Promise<ExcelBPSConfig> { return (await apiClient.put('/admin/intelligent-ops/excel-bps', config)).data }
}
export const initialExcelBPSDefaults = (): ExcelBPSDefaults => ({ all_models: false, models: ['gpt-6-astra', 'gpt-5.6-sol', 'gpt-5.6-terra'], omit_unsupported_tools: false, ignore_encrypted_content: false, auto_disable_on_403: false, cache_creation_as_input: false })
export const excelBPSOptionLabels = {
  omit_unsupported_tools: '省略 BPS 不支持的托管工具',
  ignore_encrypted_content: '忽略无法转发的加密历史内容',
  auto_disable_on_403: '收到普通 403 时自动关闭 BPS',
  cache_creation_as_input: '缓存创建 Token 按普通输入计费'
} as const
export type ExcelBPSAccountForm = ExcelBPSDefaults & { enabled: boolean }
export function readExcelBPSAccount(extra?: Record<string, unknown>): ExcelBPSAccountForm {
  const result = { ...initialExcelBPSDefaults(), enabled: extra?.openai_excel_bps === true }
  result.all_models = result.enabled && !Object.prototype.hasOwnProperty.call(extra, 'openai_excel_bps_models')
  if (Array.isArray(extra?.openai_excel_bps_models)) result.models = extra.openai_excel_bps_models.filter((x): x is string => typeof x === 'string')
  for (const key of Object.keys(excelBPSOptionLabels) as (keyof typeof excelBPSOptionLabels)[]) result[key] = extra?.[`openai_excel_bps_${key}`] === true
  return result
}
export function writeExcelBPSAccount(extra: Record<string, unknown> | undefined, form: ExcelBPSAccountForm): Record<string, unknown> {
  const result = { ...extra, openai_excel_bps: form.enabled } as Record<string, unknown>
  if (form.all_models) delete result.openai_excel_bps_models
  else result.openai_excel_bps_models = [...new Set(form.models.map(x => x.trim()).filter(Boolean))]
  for (const key of Object.keys(excelBPSOptionLabels) as (keyof typeof excelBPSOptionLabels)[]) result[`openai_excel_bps_${key}`] = form[key]
  return result
}

export interface ExcelOAuthStatus { authorized: boolean; primary: boolean; expires_at?: number }
export const excelOAuthAPI = {
  async status(id: number): Promise<ExcelOAuthStatus> { return (await apiClient.get(`/admin/openai/accounts/${id}/excel-authorization`)).data },
  async generate(id: number): Promise<{ auth_url: string; session_id: string }> { return (await apiClient.post(`/admin/openai/accounts/${id}/excel-authorization/url`)).data },
  async exchange(id: number, input: { session_id: string; code: string; state: string }): Promise<void> { await apiClient.post(`/admin/openai/accounts/${id}/excel-authorization`, input, { timeout: 60000 }) }
}
