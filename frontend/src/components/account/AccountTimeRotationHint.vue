<template>
  <div v-if="slot" class="mt-2 rounded-lg bg-amber-50 p-2 text-xs leading-5 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200" data-testid="time-rotation-hint">
    <p class="font-medium">{{ t('timeRotation.configured') }}<span v-if="!enabled"> · {{ t('timeRotation.paused') }}</span></p>
    <p>{{ slot.start }}–{{ slot.end }} · {{ t('timeRotation.timezone') }}</p>
    <p>{{ t('timeRotation.priorities', { active: slot.active_priority, inactive: slot.inactive_priority }) }}</p>
    <router-link to="/admin/intelligent-ops/time-rotation" class="underline" @click="$emit('navigate')">{{ t('timeRotation.manage') }}</router-link>
  </div>
  <p v-else-if="failed" role="alert" class="mt-2 text-xs text-amber-700">{{ t('timeRotation.hintFailed') }}</p>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { timeRotationAPI, type AccountRotationSlot } from '@/api/admin/timeRotation'
const props = defineProps<{ accountId: number }>()
const emit = defineEmits<{ managed: [value: boolean]; navigate: [] }>()
const { t } = useI18n()
const slot = ref<AccountRotationSlot | null>(null)
const enabled = ref(false)
const failed = ref(false)
watch(() => props.accountId, async (id, _, onCleanup) => {
  let cancelled = false
  onCleanup(() => { cancelled = true })
  slot.value = null
  enabled.value = false
  failed.value = false
  // 查询完成前也不回写打开弹窗时的旧优先级。
  emit('managed', true)
  try {
    const config = await timeRotationAPI.get()
    if (cancelled) return
    slot.value = config.slots.find(item => item.account_ids.includes(id)) || null
    enabled.value = config.enabled
    emit('managed', Boolean(slot.value && enabled.value))
  } catch {
    if (!cancelled) failed.value = true
  }
}, { immediate: true })
</script>
