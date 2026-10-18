<template>
  <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <div class="flex items-start justify-between gap-3">
      <div><h4 class="text-sm font-medium">自动 Excel 授权</h4><p class="input-hint">保存登录资料后，系统使用账号绑定的代理完成授权，并核对同一用户与工作区。</p></div>
      <Toggle v-model="enabled" aria-label="启用自动 Excel 授权" :disabled="loading || busy" />
    </div>
    <p v-if="status" role="status" class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ stateText }}</p>
    <p v-if="status && !status.worker_ready" class="mt-2 text-sm text-amber-600">自动登录运行时尚未就绪，请检查部署；也可使用下方手动授权。</p>
    <p v-if="status?.last_error" class="mt-2 text-sm text-amber-600">{{ status.last_error }}</p>
    <p v-if="status?.state === 'failed' && status.next_attempt_at" class="input-hint">下次尝试：{{ new Date(status.next_attempt_at).toLocaleString() }}</p>
    <div v-if="enabled" class="mt-3 space-y-3">
      <label class="block"><span class="input-label">账号登录邮箱</span><input v-model="email" type="email" class="input" autocomplete="off" :disabled="loading || busy" /></label>
      <label class="block"><span class="input-label">登录密码</span><input v-model="password" type="password" class="input" autocomplete="new-password" :placeholder="status?.configured ? '已保存，留空保留' : '请输入账号密码'" :disabled="loading || busy" /></label>
      <label class="block"><span class="input-label">长期 TOTP 密钥（可选）</span><input v-model="totp" type="password" class="input" autocomplete="new-password" :placeholder="status?.totp_configured ? '已保存，留空保留' : '开启了两步验证时填写，不是六位验证码'" :disabled="loading || busy" /></label>
      <label v-if="status?.totp_configured" class="flex items-center gap-2 text-xs"><input v-model="clearTotp" type="checkbox" :disabled="busy" />清除已保存的 TOTP 密钥</label>
      <p class="input-hint">登录资料加密保存，接口不会回传密码与密钥。保存后还需保存外层账号的 BPS 开关；队列每分钟检查一次，失败后间隔 30 分钟重试。遇到官方额外验证时可改用手动授权。</p>
    </div>
    <p v-else-if="status?.enabled" class="mt-3 input-hint">保存关闭后会清除登录资料并使正在执行的自动任务失效；已完成的 Excel 授权保留。</p>
    <div class="mt-3 flex items-center gap-2">
      <button type="button" class="btn btn-primary btn-sm" :disabled="loading || busy || !status" @click="save">{{ busy ? '保存中…' : '保存自动授权设置' }}</button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || busy" @click="refresh">刷新状态</button>
    </div>
    <p v-if="error" role="alert" class="mt-2 text-sm text-red-600">{{ error }}</p>
    <p v-if="saved" class="mt-2 text-sm text-green-600">设置已保存。{{ enabled ? '账号 BPS 开关保存开启后将自动处理。' : '登录资料已清除。' }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import { excelAutoAPI, type ExcelAutoStatus } from '@/api/admin/excelBps'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ accountId: number }>()
const emit = defineEmits<{ authorized: [] }>()
const status = ref<ExcelAutoStatus>()
const enabled = ref(false), email = ref(''), password = ref(''), totp = ref(''), clearTotp = ref(false)
const loading = ref(false), busy = ref(false), error = ref(''), saved = ref(false)
let revision = 0
let timer: ReturnType<typeof setTimeout> | undefined
const stateText = computed(() => ({ disabled: '自动授权未开启', queued: '等待处理（账号需启用且允许调度）', running: '正在完成官方 Excel 授权', authorized: '自动授权已完成，Excel 令牌会独立刷新', failed: '自动授权未完成' })[status.value?.state || 'disabled'])
async function load(id: number, current: number, fill: boolean) {
  try {
    const result = await excelAutoAPI.status(id)
    if (current !== revision) return
    if (result.state === 'authorized' && status.value?.state !== 'authorized') emit('authorized')
    status.value = result
    if (fill) { enabled.value = result.enabled; email.value = result.email }
    error.value = ''
  } catch { if (current === revision) error.value = '自动授权状态读取失败，请重试。' }
}
function schedule(current: number) {
  clearTimeout(timer)
  timer = setTimeout(async () => {
    if (current !== revision) return
    if (!busy.value) await load(props.accountId, current, false)
    if (current === revision) schedule(current)
  }, 10000)
}
watch(() => props.accountId, async id => {
  const current = ++revision
  clearTimeout(timer); status.value = undefined; enabled.value = false; email.value = ''; password.value = ''; totp.value = ''; clearTotp.value = false; error.value = ''; saved.value = false; busy.value = false
  loading.value = true
  await load(id, current, true)
  if (current === revision) { loading.value = false; schedule(current) }
}, { immediate: true })
async function refresh() {
  const current = revision
  loading.value = true
  await load(props.accountId, current, false)
  if (current === revision) loading.value = false
}
async function save() {
  const current = revision, id = props.accountId
  busy.value = true; error.value = ''; saved.value = false
  try {
    await excelAutoAPI.save(id, { enabled: enabled.value, email: email.value, password: password.value, totp_secret: totp.value, clear_totp: clearTotp.value })
    if (current !== revision) return
    password.value = ''; totp.value = ''; clearTotp.value = false; saved.value = true
    await load(id, current, false)
  } catch (err) { if (current === revision) error.value = extractApiErrorMessage(err, '保存自动授权设置失败') }
  finally { if (current === revision) busy.value = false }
}
onUnmounted(() => { revision++; clearTimeout(timer); password.value = ''; totp.value = '' })
</script>
