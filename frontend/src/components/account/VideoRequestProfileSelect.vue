<template>
  <div>
    <label for="video-request-profile" class="input-label">{{ t('admin.accounts.videoRequestProfile.title') }}</label>
    <Select
      id="video-request-profile"
      data-testid="video-request-profile"
      :aria-label="t('admin.accounts.videoRequestProfile.title')"
      :model-value="modelValue"
      :options="options"
      @update:model-value="$emit('update:modelValue', String($event))"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'

const props = defineProps<{ modelValue: string }>()
defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()

const profiles = computed(() => [
  { value: 'auto', label: t('admin.accounts.videoRequestProfile.auto') },
  { value: 'unified_json', label: t('admin.accounts.videoRequestProfile.unified') },
  { value: 'legacy', label: t('admin.accounts.videoRequestProfile.legacy') },
  { value: 'zyca', label: 'ZYCA' },
])
const options = computed(() => profiles.value.some(({ value }) => value === props.modelValue)
  ? profiles.value
  : [...profiles.value, { value: props.modelValue, label: props.modelValue }])
</script>
