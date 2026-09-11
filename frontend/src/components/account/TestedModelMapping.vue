<template>
  <details class="border-y border-gray-200 py-3 dark:border-dark-500" @toggle="opened = ($event.target as HTMLDetailsElement).open">
    <summary class="cursor-pointer text-sm font-medium">{{ t('mapping') }}</summary>
    <div class="mt-3 space-y-3">
      <p v-if="loading" class="text-sm text-gray-500">{{ t('loading') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <template v-if="run">
        <p class="text-xs text-gray-500">{{ t('latest') }}: {{ new Date(run.started_at).toLocaleString() }} · {{ t(run.status) }}</p>
        <label class="input-label" for="tested-model-target">{{ t('target') }}</label>
        <select id="tested-model-target" v-model="target" class="input" :disabled="!targets.length">
          <option value="" disabled>{{ t('target') }}</option>
          <option v-for="model in targets" :key="model" :value="model">{{ model }}</option>
        </select>
        <label class="input-label" for="tested-model-sources">{{ t('sources') }}</label>
        <textarea id="tested-model-sources" v-model="sourceText" class="input font-mono" rows="4" />
        <button type="button" class="btn btn-secondary" :disabled="!target || !sources.length || loading" @click="apply">{{ t('apply') }}</button>
        <p v-if="applied" role="status" class="text-sm text-green-600">{{ t('applied') }}</p>
      </template>
      <p v-if="!loading && !targets.length" class="text-sm text-gray-500">{{ t('noTargets') }}</p>
    </div>
  </details>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLatestSerialTest, parseModelList, type SerialTestRun } from '@/api/admin/serialModelTests'
import { serialTestMessages } from './serialTestMessages'

const props = defineProps<{ accountId: number }>()
const emit = defineEmits<{ apply: [sources: string[], target: string] }>()
const { t } = useI18n({ useScope: 'local', messages: serialTestMessages })
const opened = ref(false)
const loading = ref(false)
const error = ref('')
const run = ref<SerialTestRun | null>(null)
const target = ref('')
const sourceText = ref('')
const applied = ref(false)
const sources = computed(() => parseModelList(sourceText.value))
const targets = computed(() => [...new Set(run.value?.results.filter(r => r.status === 'success').map(r => r.actual_model || r.model) ?? [])])

watch([() => props.accountId, opened], async ([id, open], _, onCleanup) => {
  let active = true
  onCleanup(() => { active = false })
  run.value = null
  target.value = ''
  sourceText.value = ''
  applied.value = false
  if (!open) return
  loading.value = true
  error.value = ''
  try {
    const latest = await getLatestSerialTest(id)
    if (!active) return
    run.value = latest
    sourceText.value = latest?.results.filter(r => r.status === 'failed').map(r => r.model).join('\n') ?? ''
  } catch (err) { if (active) error.value = err instanceof Error ? err.message : t('unknown') }
  finally { if (active) loading.value = false }
})

function apply() {
  if (!targets.value.includes(target.value) || !sources.value.length) return
  emit('apply', sources.value, target.value)
  applied.value = true
}
</script>
