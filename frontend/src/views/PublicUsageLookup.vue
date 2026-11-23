<template>
  <main class="min-h-screen bg-gray-50 px-4 py-10 text-gray-900 dark:bg-dark-950 dark:text-gray-100">
    <div class="mx-auto max-w-6xl space-y-6">
      <header><h1 class="text-2xl font-semibold">使用记录查询</h1><p class="mt-1 text-sm text-gray-500">查看此 API 密钥最近 7 天的请求与费用。</p></header>
      <form class="card flex flex-wrap items-end gap-4 p-5" @submit.prevent="query(1, true)">
        <label class="min-w-0 flex-1 text-sm font-medium">API 密钥
          <input v-model="key" type="password" autocomplete="off" spellcheck="false" required placeholder="输入完整 API 密钥" class="input mt-2 w-full" @input="reset">
        </label>
        <button class="btn btn-primary" :disabled="busy || !key.trim()">查询</button>
      </form>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <section class="card overflow-hidden" aria-live="polite">
        <header class="flex flex-wrap items-center justify-between gap-4 p-5"><h2 class="font-semibold">请求明细</h2>
          <label class="flex items-center gap-2 text-sm">模型
            <select v-model="model" class="input" :disabled="!ready || busy" @change="query(1, false)">
              <option value="">全部模型</option><option v-for="m in models" :key="m" :value="m">{{ m }}</option>
            </select>
          </label>
        </header>
        <div class="overflow-x-auto">
          <table class="w-full whitespace-nowrap text-left text-sm"><thead class="bg-gray-50 text-gray-500 dark:bg-dark-800"><tr>
            <th v-for="col in columns" :key="col" class="px-4 py-3">{{ col }}</th>
          </tr></thead><tbody><tr v-for="r in rows" :key="r.id" class="border-t border-gray-100 dark:border-dark-700">
            <td class="px-4 py-3">{{ new Date(r.created_at).toLocaleString() }}</td><td>{{ r.model }}</td><td>{{ r.input_tokens }}</td><td>{{ r.output_tokens }}</td><td>${{ r.actual_cost.toFixed(6) }}</td><td>{{ r.duration_ms ?? '—' }}</td>
          </tr></tbody></table>
        </div>
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
interface Row { id:number; created_at:string; model:string; input_tokens:number; output_tokens:number; actual_cost:number; duration_ms:number|null }
const columns = ['时间','模型','输入 Token','输出 Token','实际费用','耗时 (ms)']
const key = ref(''), model = ref(''), error = ref('')
const rows = ref<Row[]>([]), models = ref<string[]>([])
const busy = ref(false), ready = ref(false), total = ref(0), pages = ref(0), page = ref(1)
let controller: AbortController | undefined
let endTime = ''
function reset() {
  controller?.abort()
  ready.value = busy.value = false
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
  if (refresh) endTime = new Date().toISOString()
  const params = new URLSearchParams({end_time:endTime})
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
    const data = await read('records',params)
    if (signal.aborted) return
    rows.value = data.items
    total.value = data.total
    pages.value = data.pages
    page.value = nextPage
    ready.value = true
  } catch (e) {
    if (!signal.aborted) {
      rows.value = []
      ready.value = false
      error.value = e instanceof Error ? e.message : '查询失败'
    }
  } finally { if (!signal.aborted) busy.value = false }
}
</script>
