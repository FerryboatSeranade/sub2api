<template>
  <div class="mb-2 min-w-[180px] space-y-1 text-xs" aria-live="polite">
    <div class="flex items-center gap-2 text-gray-500">
      <span :title="`容器：${status?.vm_id || ''}；额度以 vm2api 为准，本地费用统计单独显示`">vm2api <span v-if="status?.plan">· {{ status.plan.toUpperCase() }}</span></span>
      <button type="button" class="text-primary-600 hover:underline focus-visible:outline focus-visible:outline-2 disabled:opacity-50" :disabled="loading" title="向 vm2api 查询最新额度和重置券，不消耗券；重复点击会合并，失败不自动重试" @click="load(true)">
        {{ loading ? '刷新中…' : '强制刷新状态' }}
      </button>
    </div>
    <template v-if="status">
      <UsageProgressBar v-if="status.five_hour.utilization != null" label="5h" :utilization="status.five_hour.utilization" :resets-at="status.five_hour.resets_at" color="indigo" />
      <span v-else class="block text-gray-400">5h：暂无额度数据</span>
      <UsageProgressBar v-if="status.seven_day.utilization != null" label="7d" :utilization="status.seven_day.utilization" :resets-at="status.seven_day.resets_at" color="emerald" />
      <span v-else class="block text-gray-400">7d：暂无额度数据</span>
      <div :title="creditDetails">重置券：{{ status.reset_credits?.available_count ?? '未知' }}<span v-if="status.reset_credits?.available_count != null"> 张</span></div>
      <div class="text-[10px] text-gray-400" :title="'同步时间表示从 vm2api 读取缓存；强制刷新会重新查询上游。'">
        <span v-if="status.stale">旧数据 · </span>{{ status.synced_at && !status.synced_at.startsWith('0001') ? `同步 ${formatTime(status.synced_at)}` : '尚未同步' }}
        <span v-if="status.refreshed_at"> · 主动查询 {{ formatTime(status.refreshed_at) }}</span>
      </div>
    </template>
    <div v-if="error || status?.error" class="max-w-[260px] text-amber-600" role="status">{{ error || status?.error }}</div>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { apiClient } from '@/api/client'
import UsageProgressBar from './UsageProgressBar.vue'
interface Quota {
  vm_id: string; plan?: string; synced_at: string; refreshed_at?: string; stale: boolean; error?: string
  five_hour: { utilization: number | null; resets_at?: string }
  seven_day: { utilization: number | null; resets_at?: string }
  reset_credits?: { available_count: number | null; fetched_at?: string; credits?: { expires_at: string }[] }
}
const props = defineProps<{ accountId: number; refreshToken?: number }>()
const status = ref<Quota | null>(null)
const error = ref('')
const loading = ref(false)
const formatTime = (value: string) => new Date(value).toLocaleString()
const creditDetails = computed(() => {
  const credits = status.value?.reset_credits
  return [credits?.fetched_at ? `券信息查询时间：${formatTime(credits.fetched_at)}` : '', ...(credits?.credits || []).map(c => `到期：${formatTime(c.expires_at)}`), '仅显示和查询，不自动兑换重置券'].filter(Boolean).join('\n')
})
let generation = 0
async function load(force = false) {
  if (loading.value) return
  const current = ++generation
  const id = props.accountId
  loading.value = true; error.value = ''
  try {
    const url = `/admin/accounts/${id}/vm2api/usage`
    const response = force ? await apiClient.post<Quota>(`${url}/refresh`, {}, { timeout: 65000 }) : await apiClient.get<Quota>(url)
    if (current === generation && id === props.accountId) status.value = response.data
  } catch {
    if (current === generation && id === props.accountId) error.value = 'vm2api 状态查询失败，已有数据保留；请手动刷新重试'
  } finally { if (current === generation) loading.value = false }
}
onMounted(() => load())
watch(() => props.accountId, () => { generation++; loading.value = false; status.value = null; void load() })
watch(() => props.refreshToken, () => load())
</script>
