import { describe, expect, it } from 'vitest'
import { readBPSExtra, writeBPSExtra } from '../excelBPS'
import { initialBPSDefaults } from '@/api/admin/excelBPS'
describe('Excel/BPS extra 保存兼容', () => {
 it('批量全模型和关闭转组显式清除旧范围，同时保留其他设置', () => {
  const value = { ...initialBPSDefaults(), all_models: true }
  const extra = writeBPSExtra({ unrelated: true }, true, value, true)
  expect(extra.openai_excel_bps_models).toBeNull()
  expect(extra.openai_excel_bps_403_target_group_id).toBeNull()
  expect(extra.unrelated).toBe(true)
 })
 it('单账号编辑保持模型范围和恢复配置往返一致', () => {
  const value = { ...initialBPSDefaults(), models: ['one', 'two'], auto_disable_on_403: true, auto_recover_on_403: true, auto_move_on_403: true, target_group_id: 0 }
  expect(readBPSExtra(writeBPSExtra({}, true, value))).toEqual(value)
 })
})
