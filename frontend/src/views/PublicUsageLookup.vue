<template>
  <main class="min-h-screen bg-gray-50 px-4 py-10 text-gray-900 dark:bg-dark-950 dark:text-gray-100">
    <div class="mx-auto max-w-screen-2xl space-y-6">
      <header><h1 class="text-2xl font-semibold">使用记录查询</h1><p class="mt-1 text-sm text-gray-500">查看此 API 密钥在所选时间范围内的使用情况。</p></header>
      <form class="card flex flex-wrap items-end gap-4 p-5" @submit.prevent="query(1, true)">
        <label class="min-w-0 flex-1 text-sm font-medium">API 密钥
          <input v-model="key" type="password" autocomplete="off" spellcheck="false" required placeholder="输入完整 API 密钥" class="input mt-2 w-full" @input="reset">
        </label>
        <button class="btn btn-primary" :disabled="busy || !key.trim()">查询</button>
      </form>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <UsageStatsCards v-if="overview" :stats="overview.stats" :show-account-cost="false" :strike-standard-cost="true" />

      <div class="space-y-4">
        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-4">
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="rangeChanged"
              />
            </div>
            <div class="ml-auto flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.granularity') }}:</span>
              <div class="w-28">
                <select v-model="granularity" class="input" @change="rangeChanged"><option value="day">按天</option><option value="hour">按小时</option></select>
              </div>
            </div>
          </div>
        </div>

        <div v-if="overview" class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <ModelDistributionChart
            v-model:metric="modelDistributionMetric"
            :model-stats="overview.models || []"
            :loading="busy"
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :start-date="startDate"
            :end-date="endDate"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="overview.groups || []"
            :loading="busy"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :start-date="startDate"
            :end-date="endDate"
          />
        </div>

        <div v-if="overview" class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="overview.stats.endpoints || []"
            :upstream-endpoint-stats="[]"
            :endpoint-path-stats="[]"
            :loading="busy"
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
          />
          <TokenUsageTrend :trend-data="overview.trend || []" :loading="busy" />
        </div>
      </div>

      <section class="card overflow-hidden" aria-live="polite">
        <header class="flex flex-wrap items-center justify-between gap-4 p-5"><h2 class="font-semibold">请求明细</h2>
          <label class="flex items-center gap-2 text-sm">模型
            <select v-model="model" class="input" :disabled="!ready || busy" @change="query(1, false)">
              <option value="">全部模型</option><option v-for="m in models" :key="m" :value="m">{{ m }}</option>
            </select>
          </label>
        </header>
        <UsageTable :columns="columns" :data="rows" :loading="busy" :show-account-billing="false" :show-upstream-endpoint="false" />
        <p v-if="!rows.length" class="p-8 text-center text-sm text-gray-500">{{ busy ? '查询中…' : ready ? '所选条件下没有使用记录' : '输入完整密钥后查询' }}</p>
        <footer v-if="ready" class="flex flex-wrap items-center justify-end gap-3 p-4 text-sm"><span>共 {{ total }} 条 · 第 {{ page }} / {{ Math.max(1,pages) }} 页</span>
          <button class="btn btn-secondary" :disabled="busy || page <= 1" @click="query(page-1,false)">上一页</button>
          <button class="btn btn-secondary" :disabled="busy || page >= pages" @click="query(page+1,false)">下一页</button>
        </footer>
      </section>
    </div>
  </main>
</template>
<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import { useI18n } from 'vue-i18n'
import type { UsageLog, UsageStatsResponse, ModelStat, GroupStat, TrendDataPoint } from '@/types'
const { t } = useI18n()
const columns = [
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: false },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: false },
]
type Metric = 'tokens' | 'actual_cost'
const modelDistributionMetric = ref<Metric>('tokens')
const groupDistributionMetric = ref<Metric>('tokens')
const endpointDistributionMetric = ref<Metric>('tokens')
const endpointDistributionSource = ref<'inbound'|'upstream'|'path'>('inbound')
const overview = ref<{stats:UsageStatsResponse; models:ModelStat[]; groups:GroupStat[]; trend:TrendDataPoint[]} | null>(null)
function localDate(daysAgo:number) {
  const d = new Date()
  d.setDate(d.getDate()-daysAgo)
  return [d.getFullYear(),String(d.getMonth()+1).padStart(2,'0'),String(d.getDate()).padStart(2,'0')].join('-')
}
const startDate = ref(localDate(6)), endDate = ref(localDate(0))
const granularity = ref<'hour'|'day'>('day')
function rangeChanged() {
  if (key.value.trim()) query(1,true)
}
const key = ref(''), model = ref(''), error = ref('')
const rows = ref<UsageLog[]>([]), models = ref<string[]>([])
const busy = ref(false), ready = ref(false), total = ref(0), pages = ref(0), page = ref(1)
let controller: AbortController | undefined
let startTime = ''
let endTime = ''
function reset() {
  controller?.abort()
  ready.value = busy.value = false
  overview.value = null
  models.value = []
  rows.value = []
  model.value = error.value = ''
}
onBeforeUnmount(() => controller?.abort())
async function query(nextPage:number, refresh:boolean) {
  if (!key.value.trim()) return
  controller?.abort()
  controller = new AbortController()
  const signal = controller.signal
  if (refresh) {
    const start = new Date(startDate.value+'T00:00:00')
    const end = new Date(endDate.value+'T00:00:00')
    end.setDate(end.getDate()+1)
    const until = Math.min(end.getTime(), Date.now())
    if (!(until > +start && until - +start <= 31*86400000)) {
      reset()
      error.value = '请选择有效时间范围，每次最多查询 31 天。'
      return
    }
    startTime = start.toISOString()
    endTime = new Date(until).toISOString()
  }
  const params = new URLSearchParams({start_time:startTime,end_time:endTime,granularity:granularity.value})
  const options = {headers:{Authorization:'Bearer '+key.value.trim()}, signal, credentials:'omit' as const, redirect:'error' as const}
  async function read(path:string, filters:URLSearchParams) {
    const res = await fetch('/v1/usage/'+path+'?'+filters, options)
    if (!res.ok) throw new Error(res.status === 401 ? '密钥无效，请输入完整 API 密钥。' : '查询失败（HTTP '+res.status+'）')
    return (await res.json()).data
  }
  busy.value = true
  error.value = ''
  try {
    if (refresh) {
      const data = await read('models',params)
      if (signal.aborted) return
      models.value = data.items.map((item:{model:string}) => item.model)
      if (!models.value.includes(model.value)) model.value = ''
    }
    params.set('page',String(nextPage))
    if (model.value) params.set('model',model.value)
    const [data,summary] = await Promise.all([read('records',params),nextPage === 1 || !overview.value ? read('overview',params) : overview.value])
    if (signal.aborted) return
    overview.value = summary
    rows.value = data.items
    total.value = data.total
    pages.value = data.pages
    page.value = nextPage
    ready.value = true
  } catch (e) {
    if (!signal.aborted) {
      overview.value = null
      rows.value = []
      ready.value = false
      error.value = e instanceof Error ? e.message : '查询失败'
    }
  } finally { if (!signal.aborted) busy.value = false }
}
</script>
