<template>
  <div>
    <label class="input-label" for="user-preferred-account">{{ t('admin.users.preferredAccount.label') }}</label>
    <Select
      id="user-preferred-account"
      :model-value="modelValue"
      :options="options"
      :loading="loading"
      :search-placeholder="t('admin.users.preferredAccount.search')"
      remote
      @search="search"
      @update:model-value="select"
    />
    <p class="input-hint">{{ t('admin.users.preferredAccount.hint') }}</p>
    <p v-if="loadError" class="mt-1 text-sm text-red-500" role="alert">{{ t('admin.users.preferredAccount.loadError') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Select from '@/components/common/Select.vue'

type AccountOption = { id: number; name: string; platform: string }
const props = defineProps<{ modelValue: number }>()
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
const { t } = useI18n()
const results = ref<AccountOption[]>([])
const selected = ref<AccountOption | null>(null)
const loading = ref(false)
const loadError = ref(false)
let searchSequence = 0
let selectedSequence = 0
const label = (account: AccountOption) => `${account.name} · ${account.platform} · #${account.id}`
const options = computed(() => {
  const items = results.value.map(account => ({ value: account.id, label: label(account) }))
  if (props.modelValue > 0 && !items.some(item => item.value === props.modelValue)) {
    items.unshift({ value: props.modelValue, label: selected.value?.id === props.modelValue
      ? label(selected.value) : t('admin.users.preferredAccount.idFallback', { id: props.modelValue }) })
  }
  return [{ value: 0, label: t('admin.users.preferredAccount.none') }, ...items]
})
async function search(query = '') {
  const sequence = ++searchSequence
  loading.value = true
  loadError.value = false
  try {
    const response = await adminAPI.accounts.list(1, 50, { search: query.trim(), lite: 'true' })
    if (sequence === searchSequence) results.value = response.items
  } catch {
    if (sequence === searchSequence) { results.value = []; loadError.value = true }
  } finally {
    if (sequence === searchSequence) loading.value = false
  }
}
function select(value: string | number | boolean | null) {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) return
  selected.value = results.value.find(account => account.id === value) ?? selected.value
  emit('update:modelValue', value)
}
watch(() => props.modelValue, async id => {
  const sequence = ++selectedSequence
  selected.value = null
  if (id <= 0) return
  try {
    const account = await adminAPI.accounts.getById(id)
    if (sequence === selectedSequence) selected.value = account
  } catch {
    // 加载失败或账号已删除时保留 ID，不能静默清除原配置。
  }
}, { immediate: true })
void search()
onBeforeUnmount(() => { searchSequence++; selectedSequence++ })
</script>
