<template>
  <div class="space-y-5" data-testid="bps-options">
    <div>
      <label class="flex items-center gap-2 text-sm font-medium"><input v-model="model.all_models" type="checkbox" />全部模型使用 BPS</label>
      <div v-if="!model.all_models" class="mt-3"><ModelWhitelistSelector v-model="model.models" platform="openai" /></div>
      <p class="mt-2 text-xs text-gray-500">按账号模型映射后的名称匹配。未选中的模型继续使用原协议；上游权限仍以账号实际能力为准。</p>
    </div>
    <div class="grid gap-3 sm:grid-cols-2">
      <label v-for="item in flags" :key="item.key" class="flex items-start gap-3 rounded-xl border border-gray-200 p-3 dark:border-dark-600">
        <input v-model="model[item.key]" type="checkbox" class="mt-1" />
        <span><span class="block text-sm font-medium">{{ item.label }}</span><span class="mt-1 block text-xs leading-5 text-gray-500">{{ item.hint }}</span></span>
      </label>
    </div>
    <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
      <label class="flex items-center gap-2 text-sm"><input v-model="model.auto_recover_on_403" type="checkbox" :disabled="!model.auto_disable_on_403" />403 自动关闭后定时探测恢复</label>
      <p class="mt-2 text-xs text-gray-500">只恢复因真实 BPS 403 自动关闭的账号。探测会消耗少量上游额度；手动关闭的账号不会自动开启。</p>
      <label v-if="model.auto_recover_on_403 && model.auto_disable_on_403" class="mt-3 block text-sm">恢复探测间隔（分钟）<input v-model.number="model.recovery_interval_minutes" type="number" min="1" max="10080" step="1" required class="input mt-2" /></label>
    </div>
    <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
      <label class="flex items-center gap-2 text-sm"><input v-model="model.auto_move_on_403" type="checkbox" />遇到 BPS 403 时自动调整分组</label>
      <template v-if="model.auto_move_on_403">
        <select v-model.number="model.target_group_id" class="input mt-3" aria-label="403 目标分组"><option :value="-1" disabled>请选择目标分组</option><option :value="0">退出所有分组</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }} #{{ group.id }}</option></select>
        <p class="mt-2 text-xs text-amber-700 dark:text-amber-300">将替换账号全部现有分组；恢复探测成功不会还原分组。</p>
        <p v-if="groupError" role="alert" class="mt-2 text-xs text-red-600">{{ groupError }}<button type="button" class="ml-2 underline" @click="loadGroups">重试</button></p>
      </template>
    </div>
    <p class="text-xs text-gray-500">请求与图片上传沿用该账号的代理配置。</p>
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import { getAll } from '@/api/admin/groups'
import type { ExcelBPSDefaults } from '@/api/admin/excelBPS'
import type { AdminGroup } from '@/types'
const model = defineModel<ExcelBPSDefaults>({ required: true })
const groups = ref<AdminGroup[]>([])
const groupError = ref('')
const flags = [
  { key: 'ignore_encrypted_content', label: '忽略不支持的加密内容', hint: '将无法转发的加密内容替换为省略提示，保留可用的明文历史。' },
  { key: 'auto_disable_on_403', label: '403 时自动关闭协议', hint: '仅实际 BPS 生成接口返回 403 时关闭，不停用账号。' },
  { key: 'cache_creation_as_input', label: '缓存写入按输入计费', hint: '缓存写入计入普通输入，保留缓存读取计费和原始使用量。' },
  { key: 'omit_unsupported_tools', label: '保持 BPS，省略不支持的托管工具', hint: '省略联网搜索、图片生成等不支持的声明并提示模型；强制指定仍报错。' }
] as const
watch(() => model.value.auto_disable_on_403, enabled => { if (!enabled) model.value.auto_recover_on_403 = false })
async function loadGroups() { try { groups.value = (await getAll()).filter(g => g.platform === 'openai' || g.platform === 'composite'); groupError.value = '' } catch { groupError.value = '分组加载失败，无法选择新的目标分组。' } }
onMounted(loadGroups)
</script>
