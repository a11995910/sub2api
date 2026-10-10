<template>
  <section v-if="available" class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="excel-bps-account">
    <div class="flex items-center justify-between gap-3"><div><h3 class="font-medium">Excel / BPS 协议</h3><p class="mt-1 text-xs text-gray-500">使用当前 OAuth 账号。保存后生效，建议切换后新建会话。</p></div><Toggle v-model="enabled" :disabled="free && !enabled" aria-label="启用 Excel/BPS 协议" /></div>
    <p v-if="free" class="text-sm text-amber-700">免费套餐不能启用 BPS；可关闭已有协议配置。</p>
    <div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || free" @click="applyDefaults">开启并套用默认配置</button><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || free" @click="applyInitial">开启初始配置</button><RouterLink to="/admin/auto-config" target="_blank" class="self-center text-xs text-primary-600">管理默认配置 ↗</RouterLink></div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="disabledAt" class="text-xs text-amber-700">上游 403 后已自动关闭：{{ disabledAt }}。可在下方调整恢复策略。</p>
    <ExcelBPSOptions v-if="enabled || disabledAt" v-model="options" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import ExcelBPSOptions from '@/components/admin/excelbps/ExcelBPSOptions.vue'
import { getBPSSettings, getBPSDefaults, initialBPSDefaults, type ExcelBPSDefaults } from '@/api/admin/excelBPS'
withDefaults(defineProps<{ free?: boolean; disabledAt?: string }>(), { free: false, disabledAt: '' })
const emit = defineEmits<{ change: []; available: [value: boolean] }>()
const enabled = defineModel<boolean>('enabled', { required: true })
const options = defineModel<ExcelBPSDefaults>('options', { required: true })
watch(options, () => emit('change'), { deep: true })
const available = ref(false), loading = ref(false), error = ref('')
let alive = true
onBeforeUnmount(() => { alive = false })
onMounted(async () => { try { const s = await getBPSSettings(); if (alive) { available.value = s.excel_bps_enabled; emit('available', available.value) } } catch { /* 全局状态未知时不显示协议选项。 */ } })
async function applyDefaults() { loading.value = true; error.value = ''; try { const value = await getBPSDefaults(); if (alive) { options.value = value; enabled.value = true } } catch { if (alive) error.value = '默认配置加载失败，账号表单未改变。' } finally { if (alive) loading.value = false } }
function applyInitial() { options.value = initialBPSDefaults(); enabled.value = true; error.value = '' }
</script>
