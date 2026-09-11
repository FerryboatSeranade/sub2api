<template>
  <section class="space-y-3">
    <p v-if="accountName" class="break-all text-sm font-medium">{{ accountName }}</p>
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-sm text-gray-500">{{ t('count', { count: models.length }) }}</span>
      <button type="button" class="btn btn-secondary" :disabled="busy || loading" @click="configure = true">
        <Icon name="edit" size="sm" /> {{ t('configure') }}
      </button>
    </div>
    <p v-if="loading" class="text-sm text-gray-500">{{ t('loading') }}</p>
    <p v-if="error" role="alert" class="break-words text-sm text-red-600">{{ error }}</p>
    <div v-if="run" class="space-y-2">
      <div class="flex flex-wrap gap-2 text-sm text-gray-500">
        <span>{{ t('latest') }}: {{ new Date(run.started_at).toLocaleString() }}</span>
        <span>{{ !busy && run.status === 'running' ? t('stale') : t(run.status) }}</span>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full table-fixed text-left text-sm">
          <thead><tr class="border-b dark:border-dark-500"><th class="w-1/2 py-2">{{ t('model') }}</th><th>{{ t('status') }}</th><th>{{ t('latency') }}</th></tr></thead>
          <tbody>
            <tr v-for="result in run.results" :key="result.model" class="border-b align-top dark:border-dark-500">
              <td class="break-all py-2 pr-2">
                {{ result.model }}
                <div v-if="result.actual_model && result.actual_model !== result.model" class="text-xs text-gray-500">→ {{ result.actual_model }}</div>
                <div v-if="result.error" class="mt-1 whitespace-pre-wrap break-words text-xs text-red-600 [word-break:normal]">{{ result.error }}</div>
              </td>
              <td class="py-2" :class="result.status === 'success' ? 'text-green-600' : result.status === 'failed' ? 'text-red-600' : 'text-gray-500'">{{ t(result.status) }}</td>
              <td class="py-2 text-gray-500">{{ result.latency_ms ? `${(result.latency_ms / 1000).toFixed(1)}s` : '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <template v-else-if="!loading">
      <ol v-if="models.length" class="list-inside list-decimal space-y-1 break-all text-sm text-gray-600 dark:text-gray-300">
        <li v-for="model in models" :key="model">{{ model }}</li>
      </ol>
      <p class="text-sm text-gray-500">{{ t('empty') }}</p>
    </template>
    <div class="flex flex-wrap justify-end gap-2">
      <button v-if="!busy" type="button" class="btn btn-secondary" :disabled="loading" :title="t('refresh')" :aria-label="t('refresh')" @click="load"><Icon name="refresh" size="sm" /></button>
      <button v-if="busy" type="button" class="btn btn-secondary" @click="controller?.abort()">{{ t('stop') }}</button>
      <button v-else type="button" class="btn btn-primary" :disabled="loading || !models.length" @click="start"><Icon name="play" size="sm" />{{ t('start') }}</button>
    </div>
  </section>
  <BaseDialog :show="configure" :title="`${t('configure')} · ${platform}`" width="normal" :z-index="60" @close="configure = false">
    <form class="space-y-3" @submit.prevent="save">
      <label class="input-label" for="serial-model-list">{{ t('models') }} · {{ t('preset') }}</label>
      <textarea id="serial-model-list" v-model="draft" class="input font-mono" rows="10" :disabled="saving" />
      <p v-if="configError" role="alert" class="text-sm text-red-600">{{ configError }}</p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="configure = false">{{ t('cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="saving">{{ t('save') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { Icon } from '@/components/icons'
import { getLatestSerialTest, getSerialPreset, parseModelList, runSerialTest, saveSerialPreset, type SerialTestRun } from '@/api/admin/serialModelTests'
import { serialTestMessages } from './serialTestMessages'

const props = defineProps<{ accountId: number; platform: string; accountName?: string }>()
const emit = defineEmits<{ busy: [value: boolean]; configuring: [value: boolean] }>()
const { t } = useI18n({ useScope: 'local', messages: serialTestMessages })
const models = ref<string[]>([])
const run = ref<SerialTestRun | null>(null)
const draft = ref('')
const configure = ref(false)
const busy = ref(false)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const configError = ref('')
let controller: AbortController | null = null
let generation = 0

async function load() {
  const current = ++generation
  loading.value = true
  error.value = ''
  try {
    const [preset, latest] = await Promise.all([getSerialPreset(props.accountId), getLatestSerialTest(props.accountId)])
    if (current !== generation) return
    models.value = preset
    draft.value = preset.join('\n')
    run.value = latest
  } catch (err) { if (current === generation) error.value = err instanceof Error ? err.message : t('unknown') }
  finally { if (current === generation) loading.value = false }
}

async function save() {
  const parsed = parseModelList(draft.value)
  if (!parsed.length || parsed.length > 30 || parsed.some(m => m.length > 200 || /[\s*]/.test(m))) { configError.value = t('invalid'); return }
  saving.value = true
  configError.value = ''
  try {
    await saveSerialPreset(props.accountId, parsed)
    models.value = parsed
    draft.value = parsed.join('\n')
    configure.value = false
  } catch (err) { configError.value = err instanceof Error ? err.message : t('unknown') }
  finally { saving.value = false }
}

async function start() {
  if (busy.value) return
  const current = ++generation
  controller = new AbortController()
  busy.value = true
  emit('busy', true)
  error.value = ''
  try {
    await runSerialTest(props.accountId, [...models.value], controller.signal, value => { if (current === generation) run.value = value })
  } catch (err) {
    if (current === generation) error.value = controller?.signal.aborted ? t('interrupted') : err instanceof Error ? err.message : t('interrupted')
  } finally {
    if (current === generation) { busy.value = false; controller = null; emit('busy', false) }
  }
}

watch(() => props.accountId, () => { controller?.abort(); busy.value = false; emit('busy', false); run.value = null; models.value = []; void load() }, { immediate: true })
watch(configure, value => emit('configuring', value))
onBeforeUnmount(() => { generation++; controller?.abort(); emit('busy', false); emit('configuring', false) })
</script>
