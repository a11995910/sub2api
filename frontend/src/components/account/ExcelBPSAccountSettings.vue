<template>
  <section v-if="config?.enabled" class="border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="excel-bps-account-settings">
    <div class="flex items-start justify-between gap-4">
      <div><h3 class="input-label">Excel / BPS 协议</h3><p class="input-hint">通过 Excel 网关转发当前 OAuth 账号的请求，使用账号绑定的代理。</p></div>
      <Toggle v-model="value.enabled" aria-label="启用 Excel / BPS" />
    </div>
    <div v-if="value.enabled" class="mt-3 space-y-4">
      <div class="flex flex-wrap items-center gap-2">
        <button type="button" class="btn btn-secondary btn-sm" @click="applyDefaults">应用智能运维默认设置</button>
        <button type="button" class="btn btn-secondary btn-sm" @click="Object.assign(value, initialExcelBPSDefaults())">恢复初始选项</button>
        <RouterLink to="/admin/intelligent-ops/excel-bps" target="_blank" class="text-xs text-primary-600">管理默认设置 ↗</RouterLink>
      </div>
      <ExcelBPSOptions v-model="options" />
      <ExcelOAuthAuthorization v-if="accountId" :account-id="accountId" />
      <p v-else class="text-xs text-gray-500">普通 Codex 账号创建后，请在编辑页完成独立 Excel 授权；也可直接选择 Excel OAuth 创建专用账号。BPS 权限由上游决定。</p>
    </div>
  </section>
  <p v-else-if="error" class="text-sm text-red-600">{{ error }} <button type="button" class="underline" @click="load">重试</button></p>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import ExcelBPSOptions from './ExcelBPSOptions.vue'
import ExcelOAuthAuthorization from './ExcelOAuthAuthorization.vue'
defineProps<{ accountId?: number }>()
import { excelBpsAPI, initialExcelBPSDefaults, type ExcelBPSConfig, type ExcelBPSAccountForm } from '@/api/admin/excelBps'
const value = defineModel<ExcelBPSAccountForm>({ required: true })
const config = ref<ExcelBPSConfig>()
const error = ref('')
const options = computed({ get: () => value.value, set: v => { value.value = { ...v, enabled: value.value.enabled } } })
async function load() { try { config.value = await excelBpsAPI.get(); error.value = '' } catch { error.value = 'Excel/BPS 设置读取失败，原有账号选项会保留。' } }
function applyDefaults() { if (config.value) Object.assign(value.value, { ...config.value.defaults, models: [...config.value.defaults.models] }) }
onMounted(load)
</script>
