import { initialBPSDefaults, type ExcelBPSDefaults } from '@/api/admin/excelBPS'
const prefix = 'openai_excel_bps'
const options = ['omit_unsupported_tools', 'ignore_encrypted_content', 'auto_disable_on_403', 'auto_recover_on_403', 'auto_move_on_403', 'cache_creation_as_input'] as const
export function readBPSExtra(extra: Record<string, unknown> = {}): ExcelBPSDefaults {
  const result = initialBPSDefaults()
  result.all_models = !(prefix + '_models' in extra)
  if (Array.isArray(extra[prefix + '_models'])) result.models = (extra[prefix + '_models'] as unknown[]).filter((v): v is string => typeof v === 'string')
  for (const key of options) result[key] = extra[prefix + '_' + key] === true
  result.target_group_id = typeof extra[prefix + '_403_target_group_id'] === 'number' ? extra[prefix + '_403_target_group_id'] as number : -1
  result.recovery_interval_minutes = typeof extra[prefix + '_403_recovery_interval_minutes'] === 'number' ? extra[prefix + '_403_recovery_interval_minutes'] as number : 60
  return result
}
export function bpsDefaultsError(value: ExcelBPSDefaults): string {
  if (!value.all_models && !value.models.some(model => model.trim())) return '请至少选择一个 BPS 模型。'
  if (!Number.isInteger(value.recovery_interval_minutes) || value.recovery_interval_minutes < 1 || value.recovery_interval_minutes > 10080) return '恢复间隔必须为 1–10080 分钟的整数。'
  if (value.auto_move_on_403 && (!Number.isSafeInteger(value.target_group_id) || value.target_group_id < 0)) return '请选择 403 后的目标分组，或明确选择退出所有分组。'
  return ''
}
// 返回当前表单的完整配置；批量保存通过 null 清除模型范围等可选字段。
export function writeBPSExtra(base: Record<string, unknown>, enabled: boolean, value: ExcelBPSDefaults, bulk = false): Record<string, unknown> {
  const result: Record<string, unknown> = { ...base, [prefix]: enabled }
  for (const key of options) result[prefix + '_' + key] = key === 'auto_recover_on_403' ? value.auto_disable_on_403 && value.auto_recover_on_403 : value[key]
  result[prefix + '_403_recovery_interval_minutes'] = value.recovery_interval_minutes
  const setOptional = (key: string, v: unknown) => { if (v === undefined) { if (bulk) result[key] = null; else delete result[key] } else result[key] = v }
  setOptional(prefix + '_models', value.all_models ? undefined : [...new Set(value.models.map(model => model.trim()).filter(Boolean))])
  setOptional(prefix + '_403_target_group_id', value.auto_move_on_403 ? value.target_group_id : undefined)
  return result
}
