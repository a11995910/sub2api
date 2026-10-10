<template>
  <AppLayout><div class="mx-auto max-w-5xl space-y-5">
    <div><h1 class="text-2xl font-semibold">自动配置</h1><p class="mt-2 text-sm text-gray-500">管理账号可复用的配置模板。</p></div>
    <section class="card space-y-5 p-5 sm:p-6">
      <div class="flex flex-wrap items-start justify-between gap-3"><div><h2 class="text-lg font-semibold">Excel / BPS 默认配置</h2><p class="mt-2 text-sm text-gray-500">仅在账号页面点击“套用默认配置”时填入当前表单。保存模板不会批量修改或启用现有账号。</p></div><span class="rounded-full bg-primary-50 px-3 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">OpenAI OAuth</span></div>
      <p v-if="loading" class="text-sm text-gray-500">正在加载…</p>
      <form v-else-if="ready" class="space-y-5" @submit.prevent="save"><ExcelBPSOptions v-model="draft" /><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="saving" @click="draft = recommendedBPSDefaults()">恢复推荐配置</button><button class="btn btn-primary" :disabled="saving">{{ saving ? '保存中…' : '保存默认配置' }}</button></div></form>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}<button v-if="!ready" type="button" class="ml-2 underline" @click="load">重试</button></p>
      <p v-if="saved" role="status" class="text-sm text-primary-600">默认配置已保存。</p>
    </section>
  </div></AppLayout>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import ExcelBPSOptions from '@/components/admin/excelbps/ExcelBPSOptions.vue'
import { getBPSDefaults, saveBPSDefaults, recommendedBPSDefaults } from '@/api/admin/excelBPS'
import { bpsDefaultsError } from '@/utils/excelBPS'
import { extractApiErrorMessage } from '@/utils/apiError'
const draft = ref(recommendedBPSDefaults()), loading = ref(false), ready = ref(false), saving = ref(false), saved = ref(false), error = ref('')
async function load() { loading.value = true; error.value = ''; try { draft.value = await getBPSDefaults(); ready.value = true } catch { error.value = '默认配置加载失败。' } finally { loading.value = false } }
async function save() { error.value = bpsDefaultsError(draft.value); saved.value = false; if (error.value) return; saving.value = true; try { draft.value = await saveBPSDefaults(draft.value); saved.value = true } catch (e) { error.value = extractApiErrorMessage(e, '保存失败。') } finally { saving.value = false } }
onMounted(load)
</script>
