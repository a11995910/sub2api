<template>
  <section class="card p-6" data-testid="excel-bps-settings">
    <div class="mb-5 flex flex-wrap items-center justify-between gap-3">
      <div><h2 class="text-lg font-semibold text-gray-900 dark:text-white">Excel / BPS {{ defaultsOnly ? '默认设置' : '协议与图片支持' }}</h2><p class="mt-1 text-sm text-gray-500">{{ defaultsOnly ? '账号中点击「应用智能运维默认设置」时复制以下选项，保存默认值不会修改已有账号。' : '保存后即时生效。关闭协议后隐藏账号选项，并停止使用 BPS 和临时图片下载。' }}</p></div>
      <button type="button" class="btn btn-primary" :disabled="!config || saving" @click="save">{{ saving ? '保存中…' : '保存 Excel / BPS 设置' }}</button>
    </div>
    <p v-if="error" role="alert" class="mb-4 text-sm text-red-600">{{ error }} <button v-if="!config" type="button" class="underline" @click="load">重试</button></p>
    <p v-if="success" role="status" class="mb-4 text-sm text-green-600">已保存</p>
    <p v-if="!config" class="text-sm text-gray-500">正在读取设置…</p>
    <fieldset v-else :disabled="saving" class="space-y-6">
      <template v-if="!defaultsOnly">
        <div class="flex items-center justify-between"><label class="font-medium">Excel / BPS 协议</label><Toggle v-model="config.enabled" aria-label="全局 Excel / BPS 协议" /></div>
        <div v-if="config.enabled" class="space-y-5 border-t border-gray-200 pt-5 dark:border-dark-600">
          <div class="flex items-center justify-between"><label class="font-medium">启用图片支持</label><Toggle v-model="config.images.enabled" aria-label="启用 BPS 图片支持" /></div>
          <template v-if="config.images.enabled">
            <label class="block"><span class="input-label">图片传输方式</span><select v-model="config.images.mode" class="input"><option value="native">BPS 原生附件上传</option><option value="relay">临时 HTTPS 图片中转</option></select></label>
            <p class="text-xs text-gray-500">{{ config.images.mode === 'native' ? '通过选中账号的 OAuth 和代理上传至 BPS，无需公网图片域名。上传失败会终止当前请求。' : 'BPS 通过不可猜测的临时 HTTPS 链接读取图片，链接到期后清理。' }}</p>
            <label v-if="config.images.mode === 'relay'" class="block"><span class="input-label">图片公网 HTTPS 域名</span><input v-model.trim="config.images.base_url" class="input" placeholder="https://本站域名" /><span class="input-hint">必须能访问本站的 /api/bps-images/ 路由。</span></label>
            <label class="block"><span class="input-label">图片限额处理策略</span><select v-model="config.images.policy" class="input"><option value="off">保持现状</option><option value="warn">提醒并拦截</option><option value="auto_compact">自动压缩历史图片</option></select></label>
            <p v-if="config.images.policy === 'auto_compact'" class="text-xs text-amber-600">需要稳定的 Codex 会话和 Redis 状态；压缩会产生额外模型用量，客户端需要回传压缩结果。</p>
            <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <label class="block"><span class="input-label">每次请求最多图片数</span><input v-model.number="config.images.limits.MaxImages" class="input" type="number" min="1" max="65536" /></label>
              <template v-if="config.images.policy === 'warn'"><label class="block"><span class="input-label">剩余多少张时提醒</span><input v-model.number="config.images.warning_remaining" class="input" type="number" min="1" /></label><label class="block"><span class="input-label">为压缩保留的图片数</span><input v-model.number="config.images.compact_reserve" class="input" type="number" min="1" /></label></template>
              <label v-for="field in capacityFields" :key="field.key" class="block"><span class="input-label">{{ field.label }}</span><input v-model.number="config.images[field.key]" class="input" type="number" :min="field.min" :max="field.max" /></label>
              <template v-if="config.images.mode === 'relay'"><label v-for="field in relayFields" :key="field.key" class="block"><span class="input-label">{{ field.label }}</span><input v-model.number="config.images.limits[field.key]" class="input" type="number" min="1" :max="field.max" /></label></template>
            </div>
            <p class="text-xs text-gray-500">处理预算至少为请求体上限的 8 倍。原生附件仍限制单图 20 MiB、单次图片合计 32 MiB；网站与反向代理的请求体限制仍然生效。</p>
          </template>
        </div>
        <RouterLink to="/admin/intelligent-ops/excel-bps" class="block text-sm text-primary-600">前往智能运维配置账号默认选项 →</RouterLink>
      </template>
      <ExcelBPSOptions v-else v-model="config.defaults" />
    </fieldset>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import ExcelBPSOptions from '@/components/account/ExcelBPSOptions.vue'
import { excelBpsAPI, type ExcelBPSConfig } from '@/api/admin/excelBps'
const props = defineProps<{ defaultsOnly?: boolean }>()
const config = ref<ExcelBPSConfig>()
const error = ref('')
const success = ref(false)
const saving = ref(false)
const capacityFields = [
  { key: 'body_limit_mib', label: '请求体上限（MiB）', min: 1, max: 1024 },
  { key: 'budget_mib', label: '处理总预算（MiB）', min: 8, max: 65536 },
  { key: 'max_requests', label: '同时处理请求数', min: 1, max: 4096 }
] as const
const relayFields = [
  { key: 'MaxImageMiB', label: '单图上限（MiB）', max: 512 }, { key: 'MaxTotalMiB', label: '单次图片合计（MiB）', max: 512 },
  { key: 'StorageMiB', label: '临时存储上限（MiB）', max: 262144 }, { key: 'StorageEntries', label: '临时图片数量上限', max: 1048576 }, { key: 'TTLMinutes', label: '链接有效期（分钟）', max: 10080 }
] as const
async function load() { error.value = ''; try { config.value = await excelBpsAPI.get() } catch { error.value = '读取失败，请重试。' } }
async function save() { if (!config.value) return; saving.value = true; error.value = ''; success.value = false; try { config.value = await excelBpsAPI.save(props.defaultsOnly ? { defaults: config.value.defaults } : { enabled: config.value.enabled, images: config.value.images }); success.value = true } catch (e: any) { error.value = e?.message || '保存失败，请重试。' } finally { saving.value = false } }
onMounted(load)
</script>
