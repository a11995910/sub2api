<template>
  <section class="card overflow-hidden" data-testid="excel-bps-settings">
    <div class="border-b border-gray-100 p-5 dark:border-dark-700"><h2 class="text-lg font-semibold">协议功能</h2><p class="mt-1 text-sm text-gray-500">关闭后，新请求停止使用对应协议，并隐藏账号编辑中的相关选项。</p></div>
    <p v-if="loading" class="p-5 text-sm text-gray-500">正在加载 Excel/BPS 配置…</p>
    <div v-if="draft" class="space-y-5 p-5">
      <div class="flex items-center justify-between"><span class="font-medium">Excel / BPS 协议</span><Toggle v-model="draft.excel_bps_enabled" aria-label="Excel / BPS 协议" /></div>
      <div class="border-t border-gray-100 pt-5 dark:border-dark-700"><h3 class="font-semibold">Excel / BPS 图片支持</h3><p class="mt-1 text-sm text-gray-500">为 base64 图片和工具截图选择临时 HTTPS 中转或 BPS 原生附件上传。</p></div>
      <div class="flex items-center justify-between"><div><span class="text-sm font-medium">启用图片支持</span><p class="mt-1 text-xs text-gray-500">保存后立即生效。关闭后停止转换，并停止提供临时图片链接。</p></div><Toggle v-model="draft.excel_bps_image_relay_enabled" aria-label="启用图片支持" /></div>
      <label class="block text-sm font-medium">图片传输方式<select v-model="draft.excel_bps_image_mode" class="input mt-2"><option value="native">BPS 原生附件上传</option><option value="relay">临时 HTTPS 中转</option></select></label>
      <p class="text-xs text-gray-500">{{ draft.excel_bps_image_mode === 'native' ? '通过选中账号的 OAuth 和代理直接上传，无需公网图片域名。上传失败会终止本次尝试。' : '图片临时保存在当前实例，链接持有者可在有效期内读取；需要上游能访问的 HTTPS 域名。' }}</p>
      <label v-if="draft.excel_bps_image_mode === 'relay'" class="block text-sm font-medium">公网 HTTPS 访问地址<input v-model.trim="draft.excel_bps_image_base_url" type="url" class="input mt-2" placeholder="https://api.example.com" /><span class="mt-1 block text-xs font-normal text-gray-500">只填写 HTTPS origin，不附加路径、查询参数或账号密码。</span></label>
      <label class="block text-sm font-medium">图片限额处理策略<select v-model="draft.excel_bps_image_limit_policy" class="input mt-2"><option value="off">关闭：超过限额直接报错</option><option value="warn">提前提醒手动 compact</option><option value="auto_compact">自动 compact 历史图片</option></select></label>
      <p class="text-xs text-gray-500">自动 compact 需要稳定会话标识及 Codex 客户端，会产生额外上游用量；新图片超限仍需分批发送。</p>
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3"><label v-for="field in fields" :key="field.key" class="block text-sm">{{ field.label }}<input v-model.number="draft[field.key]" type="number" :min="field.min" :max="field.max" step="1" class="input mt-2" /></label></div>
      <p class="text-xs text-gray-500">资源预算是请求处理估算值，必须至少为请求体上限的 8 倍。原生附件固定受单图 20 MiB、每请求合计 32 MiB 的上游限制。</p>
      <div class="flex flex-wrap items-center justify-between gap-3"><RouterLink to="/admin/auto-config" class="text-sm text-primary-600">配置 Excel / BPS 默认值 →</RouterLink><button type="button" class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存 Excel/BPS 配置' }}</button></div>
    </div>
    <p v-if="error" role="alert" class="px-5 pb-5 text-sm text-red-600">{{ error }}<button v-if="!draft" type="button" class="ml-2 underline" @click="load">重试</button></p>
    <p v-if="saved" role="status" class="px-5 pb-5 text-sm text-primary-600">Excel/BPS 配置已保存。</p>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import { getBPSSettings, saveBPSSettings, type ExcelBPSSettings } from '@/api/admin/excelBPS'
import { extractApiErrorMessage } from '@/utils/apiError'
const draft = ref<ExcelBPSSettings>(), loading = ref(false), saving = ref(false), saved = ref(false), error = ref('')
const fields = [
  { key: 'excel_bps_image_max_images', label: '每请求图片上限（张）', min: 1, max: 65536 },
  { key: 'excel_bps_image_warning_remaining', label: '剩余多少张时提醒', min: 1, max: 65536 },
  { key: 'excel_bps_image_compact_reserve', label: '压缩保留空间（张）', min: 1, max: 65536 },
  { key: 'excel_bps_image_body_limit_mib', label: '请求体上限（MiB）', min: 1, max: 1024 },
  { key: 'excel_bps_image_budget_mib', label: '估算资源预算（MiB）', min: 512, max: 65536 },
  { key: 'excel_bps_image_max_requests', label: '最大在途请求数', min: 1, max: 4096 },
  { key: 'excel_bps_image_max_image_mib', label: '中转单图上限（MiB）', min: 1, max: 512 },
  { key: 'excel_bps_image_max_total_mib', label: '中转请求图片合计（MiB）', min: 1, max: 512 },
  { key: 'excel_bps_image_storage_mib', label: '中转磁盘配额（MiB）', min: 1, max: 262144 },
  { key: 'excel_bps_image_storage_entries', label: '中转文件数量上限', min: 1, max: 1048576 },
  { key: 'excel_bps_image_ttl_minutes', label: '中转图片有效期（分钟）', min: 1, max: 10080 }
] as const
async function load() { loading.value = true; error.value = ''; try { draft.value = await getBPSSettings() } catch { error.value = 'Excel/BPS 配置加载失败。' } finally { loading.value = false } }
async function save() { if (!draft.value) return; error.value = ''; saved.value = false; for (const f of fields) { const n = draft.value[f.key]; if (!Number.isInteger(n) || n < f.min || n > f.max) { error.value = `${f.label}需为 ${f.min}–${f.max} 的整数。`; return } } saving.value = true; try { draft.value = await saveBPSSettings(draft.value); saved.value = true } catch (e) { error.value = extractApiErrorMessage(e, '保存失败。') } finally { saving.value = false } }
onMounted(load)
</script>
