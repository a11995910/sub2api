import { apiClient } from '../client'

export interface ExcelBPSDefaults {
  all_models: boolean
  models: string[]
  omit_unsupported_tools: boolean
  ignore_encrypted_content: boolean
  auto_disable_on_403: boolean
  auto_recover_on_403: boolean
  recovery_interval_minutes: number
  auto_move_on_403: boolean
  target_group_id: number
  cache_creation_as_input: boolean
}
export function initialBPSDefaults(): ExcelBPSDefaults {
  return { all_models: false, models: ['gpt-6-astra', 'gpt-5.6-sol', 'gpt-5.6-terra'], omit_unsupported_tools: false,
    ignore_encrypted_content: false, auto_disable_on_403: false, auto_recover_on_403: false,
    recovery_interval_minutes: 60, auto_move_on_403: false, target_group_id: -1, cache_creation_as_input: false }
}
export function recommendedBPSDefaults(): ExcelBPSDefaults {
  return { ...initialBPSDefaults(), ignore_encrypted_content: true, auto_disable_on_403: true, cache_creation_as_input: true }
}
export async function getBPSDefaults() { return (await apiClient.get<{ excel_bps: ExcelBPSDefaults }>('/admin/auto-config')).data.excel_bps }
export async function saveBPSDefaults(excel_bps: ExcelBPSDefaults) { return (await apiClient.put<{ excel_bps: ExcelBPSDefaults }>('/admin/auto-config', { excel_bps })).data.excel_bps }
export async function getBPSSettings() { return (await apiClient.get<ExcelBPSSettings>('/admin/settings/excel-bps')).data }
export async function saveBPSSettings(settings: ExcelBPSSettings) { return (await apiClient.put<ExcelBPSSettings>('/admin/settings/excel-bps', settings)).data }

export interface ExcelBPSSettings {
  excel_bps_enabled: boolean
  excel_bps_image_mode: string
  excel_bps_image_relay_enabled: boolean
  excel_bps_image_base_url: string
  excel_bps_image_body_limit_mib: number
  excel_bps_image_budget_mib: number
  excel_bps_image_max_requests: number
  excel_bps_image_max_image_mib: number
  excel_bps_image_max_images: number
  excel_bps_image_limit_policy: string
  excel_bps_image_warning_remaining: number
  excel_bps_image_compact_reserve: number
  excel_bps_image_max_total_mib: number
  excel_bps_image_storage_mib: number
  excel_bps_image_storage_entries: number
  excel_bps_image_ttl_minutes: number
}
