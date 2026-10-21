<template>
  <BaseDialog :show="!!account" :title="t('admin.accounts.vmBridge.title')" @close="emit('close')">
    <div class="space-y-4">
      <p class="font-medium">{{ account?.name }}</p>
      <p class="text-sm text-gray-500">{{ t('admin.accounts.vmBridge.description') }}</p>
      <div class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800">
        {{ t('admin.accounts.vmBridge.limits') }}
      </div>
      <label class="flex items-center gap-2 text-sm" :title="t('admin.accounts.vmBridge.groupsHint')">
        <input v-model="inheritGroups" type="checkbox" :disabled="running || busy" />
        {{ t('admin.accounts.vmBridge.groups') }}
      </label>
      <p class="text-xs text-gray-500">{{ t('admin.accounts.vmBridge.groupsHint') }}</p>
      <div v-if="job?.status" class="space-y-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600" role="status" aria-live="polite">
        <strong>{{ t('admin.accounts.vmBridge.' + job.status) }}</strong>
        <p>{{ job.message }}</p>
        <p v-if="job.vm_id">{{ t('admin.accounts.vmBridge.container') }}: {{ job.vm_id }}</p>
        <p v-if="job.target_id">{{ t('admin.accounts.vmBridge.target') }}: #{{ job.target_id }}</p>
        <p v-if="job.updated_at" class="text-xs text-gray-500">{{ formatDateTime(job.updated_at) }}</p>
      </div>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <p v-if="!loading && !enabled" class="text-sm text-amber-600">{{ t('admin.accounts.vmBridge.unconfigured') }}</p>
      <p class="text-xs text-gray-500">{{ t('admin.accounts.vmBridge.background') }}</p>
    </div>
    <template #footer>
      <button class="btn btn-secondary" :title="t('common.refresh')" :disabled="loading || busy" @click="load">{{ t('common.refresh') }}</button>
      <button class="btn btn-primary" :title="t('admin.accounts.vmBridge.description')" :disabled="!enabled || running || busy || loading" @click="start">
        {{ running || busy ? t('admin.accounts.vmBridge.running') : t('admin.accounts.vmBridge.start') }}
      </button>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { getVMBridge, syncVMBridge, type VMBridgeJob } from '@/api/admin/vm2api'
import type { AccountListItem } from '@/types'
import { formatDateTime } from '@/utils/format'
const props = defineProps<{ account: AccountListItem | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'updated'): void }>()
const { t } = useI18n()
const job = ref<VMBridgeJob>()
const enabled = ref(false)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const inheritGroups = ref(true)
const running = computed(() => job.value?.status === 'running')
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0
function stop() { if (timer) clearTimeout(timer); timer = undefined }
async function load() {
  const id = props.account?.id
  if (!id) return
  const current = generation
  stop(); loading.value = true
  try {
    const result = await getVMBridge(id)
    if (generation !== current) return
    const wasRunning = running.value
    enabled.value = result.enabled; job.value = result.job; error.value = ''
    if (wasRunning && !running.value) emit('updated')
    if (running.value) timer = setTimeout(load, 2500)
  } catch {
    if (generation === current) error.value = t('admin.accounts.vmBridge.loadFailed')
  } finally { if (generation === current) loading.value = false }
}
async function start() {
  const id = props.account?.id
  if (!id || busy.value || running.value) return
  const current = generation
  busy.value = true; error.value = ''
  try {
    const result = await syncVMBridge(id, inheritGroups.value)
    if (generation !== current) return
    job.value = result
    timer = setTimeout(load, 1000)
  } catch (e: unknown) {
    if (generation !== current) return
    const message = (e as { response?: { data?: { message?: string } } }).response?.data?.message
    error.value = message || t('admin.accounts.vmBridge.startFailed')
    // A lost HTTP response can still mean the job was accepted; refresh before retrying.
    timer = setTimeout(load, 2000)
  } finally { if (generation === current) busy.value = false }
}
watch(() => props.account?.id, () => {
  generation++; stop(); job.value = undefined; enabled.value = false; busy.value = false; loading.value = false; error.value = ''; inheritGroups.value = true
  if (props.account) void load()
}, { immediate: true })
onBeforeUnmount(() => { generation++; stop() })
</script>
