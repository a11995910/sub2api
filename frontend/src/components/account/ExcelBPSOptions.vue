<template>
  <div class="space-y-3">
    <label class="flex items-center gap-2 text-sm"><input v-model="value.all_models" type="checkbox" class="rounded" />所有模型使用 BPS</label>
    <div v-if="!value.all_models">
      <label class="input-label">使用 BPS 的模型</label>
      <ModelWhitelistSelector v-model="value.models" platform="openai" />
      <p class="input-hint">按账号映射后的模型名选择；其他模型继续使用原有协议。</p>
    </div>
    <label v-for="(label, key) in excelBPSOptionLabels" :key="key" class="flex items-center gap-2 text-sm">
      <input v-model="value[key]" type="checkbox" class="rounded" />{{ label }}
    </label>
    <p class="text-xs leading-5 text-amber-600 dark:text-amber-400">忽略加密历史会丢失该部分上下文；省略托管工具会降低能力。模型无权限的 403 不会关闭账号协议。</p>
  </div>
</template>
<script setup lang="ts">
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'
import { excelBPSOptionLabels, type ExcelBPSDefaults } from '@/api/admin/excelBps'
const value = defineModel<ExcelBPSDefaults>({ required: true })
</script>
