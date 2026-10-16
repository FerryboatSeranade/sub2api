<template>
  <BaseDialog :show="show" :title="t('admin.scheduledTests.overview')" width="wide" @close="emit('close')">
    <div class="space-y-4">
      <p class="text-xs text-gray-500">{{ t('admin.scheduledTests.qualityHelp') }}</p>
      <div class="flex flex-wrap gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="load" :title="t('common.refresh')">{{ t('common.refresh') }}</button>
        <button class="btn btn-primary" :disabled="batchRunning || running.size > 0 || !items.length" @click="runAll" :title="t('admin.scheduledTests.runAll')">{{ t('admin.scheduledTests.runAll') }}</button>
        <button v-if="batchRunning" class="btn btn-secondary" @click="stopQueued = true" :title="t('admin.scheduledTests.stopQueue')">{{ t('admin.scheduledTests.stopQueue') }}</button>
        <button class="btn btn-secondary" :disabled="!items.length" @click="exportResults" :title="t('admin.scheduledTests.exportResults')">{{ t('admin.scheduledTests.exportResults') }}</button>
      </div>
      <div class="flex flex-wrap gap-3 text-xs">
        <span v-for="state in states" :key="state">{{ t('admin.scheduledTests.' + state) }}: {{ items.filter(i => verdict(i) === state).length }}</span>
      </div>
      <input v-model="search" class="input w-full" :aria-label="t('admin.scheduledTests.searchProbe')" :placeholder="t('admin.scheduledTests.searchProbe')" />
      <p v-if="loading" class="text-sm">{{ t('common.loading') }}</p>
      <div v-else class="max-h-[60vh] space-y-3 overflow-y-auto">
        <div v-for="item in filtered" :key="item.plan_id" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex flex-wrap items-center justify-between gap-2 text-sm">
            <strong>#{{ item.account_id }} {{ item.account_name }}</strong>
            <span :class="verdict(item) === 'not_degraded' ? 'text-green-600' : verdict(item) === 'degraded' ? 'text-red-600' : 'text-gray-500'">{{ t('admin.scheduledTests.' + verdict(item)) }}</span>
          </div>
          <div class="mt-2 flex flex-wrap items-center gap-3 text-xs text-gray-500">
            <span>{{ item.result?.model_id || 'gpt-6.1-sol' }}</span>
            <span v-if="item.result">{{ formatDateTime(item.result.started_at) }} · {{ item.result.latency_ms }}ms</span>
            <label class="flex items-center gap-1"><input type="checkbox" :checked="item.enabled" :disabled="updating.has(item.plan_id)" @change="toggleSchedule(item)" />{{ t('admin.scheduledTests.schedule') }} {{ item.cron_expression }}</label>
            <button class="text-primary-600 hover:underline disabled:opacity-50" :disabled="batchRunning || running.has(item.plan_id)" @click="run(item)" :title="t('admin.scheduledTests.runNow')">{{ running.has(item.plan_id) ? t('admin.scheduledTests.running') : t('admin.scheduledTests.runNow') }}</button>
          </div>
          <p class="mt-2 break-words text-xs">{{ t('admin.scheduledTests.numbers') }}: {{ item.result?.quality?.numbers.join(', ') || '—' }}</p>
          <details v-if="item.result" class="mt-2 text-xs">
            <summary class="cursor-pointer">{{ t('admin.scheduledTests.answer') }}</summary>
            <pre class="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded bg-gray-50 p-2 dark:bg-dark-900">{{ item.result.error_message || item.result.response_text }}</pre>
          </details>
        </div>
        <p v-if="!filtered.length" class="text-sm text-gray-500">{{ t('admin.scheduledTests.noResults') }}</p>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { scheduledTestsAPI, type ReasoningOverviewItem } from '@/api/admin/scheduledTests'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()
const app = useAppStore()
const items = ref<ReasoningOverviewItem[]>([])
const loading = ref(false)
const search = ref('')
const batchRunning = ref(false)
const stopQueued = ref(false)
const running = reactive(new Set<number>())
const updating = reactive(new Set<number>())
const states = ['not_degraded', 'degraded', 'inconclusive', 'not_tested'] as const
const verdict = (item: ReasoningOverviewItem) => item.result?.quality?.verdict || 'not_tested'
const filtered = computed(() => items.value.filter(i => `${i.account_id} ${i.account_name} ${t('admin.scheduledTests.' + verdict(i))}`.toLowerCase().includes(search.value.toLowerCase())))
let requestId = 0
async function load() {
  const id = ++requestId
  loading.value = true
  try { const data = await scheduledTestsAPI.reasoningOverview(); if (id === requestId) items.value = data }
  catch (e: any) { if (id === requestId) app.showError(e.message) }
  finally { if (id === requestId) loading.value = false }
}
watch(() => props.show, show => {
  if (show) void load()
  else { requestId++; stopQueued.value = true; loading.value = false }
})
onBeforeUnmount(() => { requestId++; stopQueued.value = true })
async function run(item: ReasoningOverviewItem) {
  if (running.has(item.plan_id)) return
  running.add(item.plan_id)
  try {
    const result = await scheduledTestsAPI.runNow(item.plan_id)
    const current = items.value.find(i => i.plan_id === item.plan_id)
    if (current) current.result = result
  } catch (e: any) { app.showError(`#${item.account_id}: ${e.message}`) }
  finally { running.delete(item.plan_id) }
}
async function runAll() {
  if (batchRunning.value || running.size) return
  batchRunning.value = true
  stopQueued.value = false
  const queue = [...items.value]
  const worker = async () => { while (queue.length && !stopQueued.value) { const item = queue.shift(); if (item) await run(item) } }
  try { await Promise.all([worker(), worker()]) }
  finally { batchRunning.value = false; if (props.show) await load() }
}
async function toggleSchedule(item: ReasoningOverviewItem) {
  updating.add(item.plan_id)
  try { const plan = await scheduledTestsAPI.update(item.plan_id, { enabled: !item.enabled }); item.enabled = plan.enabled }
  catch (e: any) { app.showError(e.message); await load() }
  finally { updating.delete(item.plan_id) }
}
function exportResults() {
  // JSON preserves full responses and cannot execute spreadsheet formulas.
  const blob = new Blob([JSON.stringify(items.value, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url; link.download = 'reasoning-probes.json'; link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
</script>
