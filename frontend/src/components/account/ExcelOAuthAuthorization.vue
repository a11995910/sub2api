<template>
  <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h4 class="text-sm font-medium">Excel 独立授权</h4>
        <p class="input-hint">{{ status?.primary ? '此账号已使用 Excel OAuth。' : status?.authorized ? '已授权，请求时自动刷新 Excel 令牌。' : '使用同一用户、同一工作区完成 Excel 授权后，BPS 请求才能使用此账号。' }}</p>
      </div>
      <button v-if="!status?.primary" type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="generate">{{ status?.authorized ? '重新授权' : '生成授权链接' }}</button>
    </div>
    <p v-if="error" role="alert" class="mt-2 text-sm text-red-600">{{ error }}</p>
    <p v-if="saved" role="status" class="mt-2 text-sm text-green-600">Excel 授权已保存，原 Codex 凭据保持独立。</p>
    <div v-if="authUrl" class="mt-3 space-y-3">
      <a :href="authUrl" target="_blank" rel="noopener noreferrer" class="text-sm text-primary-600 underline">打开官方 Excel 授权页面 ↗</a>
      <input :value="authUrl" readonly class="input" aria-label="Excel 授权链接" @focus="($event.target as HTMLInputElement).select()" />
      <label class="block"><span class="input-label">授权后的完整回调 URL</span><textarea v-model="callback" class="input" rows="3" autocomplete="off" spellcheck="false" placeholder="https://bps.openai.com/basispoints/extension/…/auth/callback?code=…&state=…" /></label>
      <p class="input-hint">完成登录后，复制浏览器地址栏中包含 code 和 state 的完整回调地址。只保存 Excel 授权，账号开关仍需在表单中保存。</p>
      <button type="button" class="btn btn-primary btn-sm" :disabled="busy || !callback.trim()" @click="exchange">{{ busy ? '处理中…' : '保存 Excel 授权' }}</button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { excelOAuthAPI, type ExcelOAuthStatus } from '@/api/admin/excelBps'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ accountId: number }>()
const status = ref<ExcelOAuthStatus>()
const busy = ref(false)
const error = ref('')
const saved = ref(false)
const authUrl = ref('')
const sessionId = ref('')
const callback = ref('')
let revision = 0
watch(() => props.accountId, async id => {
  const current = ++revision
  status.value = undefined; authUrl.value = ''; sessionId.value = ''; callback.value = ''; error.value = ''; saved.value = false
  try { const result = await excelOAuthAPI.status(id); if (current === revision) status.value = result }
  catch { if (current === revision) error.value = '授权状态读取失败，可重新授权或稍后重试。' }
}, { immediate: true })
async function generate() {
  const current = revision
  busy.value = true; error.value = ''; saved.value = false; callback.value = ''
  try { const result = await excelOAuthAPI.generate(props.accountId); if (current === revision) { authUrl.value = result.auth_url; sessionId.value = result.session_id } }
  catch (err) { if (current === revision) error.value = extractApiErrorMessage(err, '生成授权链接失败') }
  finally { busy.value = false }
}
async function exchange() {
  const current = revision
  busy.value = true; error.value = ''
  try {
    let url: URL
    try { url = new URL(callback.value.trim()) } catch { throw new Error('请输入完整回调 URL') }
    const expected = new URL(authUrl.value)
    const code = url.searchParams.get('code'); const state = url.searchParams.get('state')
    if (!code || !state || state !== expected.searchParams.get('state')) throw new Error('回调缺少 code、state，或与本次授权不匹配')
    await excelOAuthAPI.exchange(props.accountId, { session_id: sessionId.value, code, state })
    if (current === revision) { status.value = { authorized: true, primary: false }; saved.value = true; callback.value = ''; authUrl.value = ''; sessionId.value = '' }
  } catch (err) { if (current === revision) error.value = extractApiErrorMessage(err, 'Excel 授权保存失败') }
  finally { busy.value = false }
}
</script>
