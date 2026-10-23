<template>
 <BaseDialog :show="show" title="账号检测与 VM2API 总览" width="wide" @close="emit('close')">
  <div class="space-y-4 text-sm">
   <p class="text-xs text-gray-500">糖果检测以答案包含独立数字 21 为通过，仅是单题启发式。鹈鹕自动评分只检查 SVG 结构，需人工看图判断；调用失败单独归为异常，不等同降智。</p>
   <div class="flex flex-wrap items-center gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
    <label>检测并发 <input v-model.number="settings.concurrency" type="number" min="1" max="32" class="input w-20" aria-label="检测并发" :disabled="busy" /></label>
    <label class="flex items-center gap-2"><input v-model="settings.exclude_vm" type="checkbox" :disabled="busy" />排除所有 vm2api 衍生账号检测</label>
    <button class="btn btn-secondary" :disabled="busy" title="保存手动与定时检测共同使用的并发和排除规则" @click="saveSettings">保存检测设置</button>
    <p class="w-full text-xs text-gray-500">默认并发 10，可设 1–32；与单账号 / VM / Key 的 2 并发不同。排除检测会覆盖已有定时计划，计划与历史仍保留；不会停止正常 API 调度。取消排除后按计划继续。</p>
   </div>
   <div class="flex flex-wrap gap-2">
    <select v-model="kind" class="input" aria-label="检测类型" :disabled="busy"><option value="candy_v1">糖果推理检测</option><option value="pelican_svg_v1">鹈鹕骑自行车 SVG</option></select>
    <select v-model="stateFilter" class="input" aria-label="检测状态筛选"><option value="">全部检测状态</option><option v-for="(label,state) in labels" :key="state" :value="state">{{ label }}（{{ items.filter(i => verdict(i) === state).length }}）</option></select>
    <select v-model="vmFilter" class="input" aria-label="VM 关联筛选"><option value="">全部 VM 状态</option><option value="not_linked">尚未转入 VM</option><option value="linked">原账号已关联 VM</option><option value="derived">VM 衍生账号</option></select>
    <select v-model="errorFilter" class="input" aria-label="异常原因筛选"><option value="">全部异常原因</option><option v-for="f in failures" :key="f.code" :value="f.code">{{ f.label }}</option></select>
    <input v-model="search" class="input flex-1" placeholder="搜索账号、ID、错误信息" aria-label="搜索账号" />
    <button class="btn btn-secondary" :disabled="loading || busy" title="重新读取服务器最新结果" @click="load">刷新</button>
    <button class="btn btn-secondary" title="导出当前筛选账号及最新结果，不包含凭证" @click="exportResults">导出筛选结果</button>
   </div>
   <div class="flex flex-wrap gap-3 text-xs"><span v-for="(label,state) in labels" :key="state">{{ label }}：{{ items.filter(i => verdict(i) === state).length }}</span></div>
   <div class="rounded-lg border p-3 dark:border-dark-600 space-y-3">
    <div class="flex flex-wrap gap-2 items-center">
     <button class="btn btn-secondary" :disabled="busy" title="只选择当前筛选结果；后续修改筛选会清空选择" @click="selectFiltered">全选筛选结果（{{ filtered.length }}）</button>
     <button class="btn btn-secondary" :disabled="busy" title="清空所有已选账号" @click="selected.clear()">清空选择</button>
     <span>已选 {{ selected.size }} 个账号</span>
     <button class="btn btn-primary" :disabled="busy || !selected.size" title="跳过已排除、暂停或退避中的账号，不循环重试失败请求" @click="prepare('run')">检测所选（并发 {{ settings.concurrency }}）</button>
     <button class="btn btn-secondary" :disabled="busy || !selected.size" title="仅转入尚未关联的 OAuth 账号；逐个完成，失败不自动重试" @click="prepare('vm')">批量转入 VM2API</button>
     <button v-if="busy" class="btn btn-secondary" title="停止未启动的队列；已在服务器启动的任务继续完成" @click="stopQueued=true">停止后续队列</button>
    </div>
    <div class="flex flex-wrap gap-2 items-center">
     <input v-model="model" class="input w-44" aria-label="计划模型" placeholder="gpt-6.1-sol" :disabled="busy || kind==='candy_v1'" />
     <input v-model="cron" class="input w-36" aria-label="批量计划 Cron" placeholder="15 4 * * *" :disabled="busy" />
     <button class="btn btn-secondary" :disabled="busy || !selected.size" title="为当前检测类型创建计划；已有同类型计划则更新，不重复创建" @click="prepare('schedule')">批量设置计划</button>
     <button class="btn btn-secondary" :disabled="busy || !selected.size" title="禁用所选账号当前类型的全部计划，保留历史" @click="prepare('disable')">禁用计划</button>
     <button class="btn btn-secondary" :disabled="busy || !selected.size" title="排除账号所有检测，优先于计划启用状态" @click="prepare('exclude')">排除检测</button>
     <button class="btn btn-secondary" :disabled="busy || !selected.size" title="取消账号级排除；全局 VM 排除和异常暂停仍有效" @click="prepare('include')">取消账号排除</button>
     <button class="btn btn-secondary" :disabled="busy || !selected.size" title="修复凭证、分组或权限后使用；解除异常暂停和退避，不改变排除设置" @click="prepare('resume')">修复后恢复检测</button>
     <button class="btn btn-secondary" :disabled="busy" title="只禁用已删除账号的残留计划，不删除历史记录" @click="prepare('cleanup')">停用已删除账号计划</button>
    </div>
    <p class="text-xs text-gray-500">批量转入：先筛选“不降智”与“尚未转入 VM”，全选后操作。真实 API 验证通过才关闭原账号直接调度，并迁移计划。检测排除不跳过这次必要的接入验证。永久异常需修复后恢复；临时异常按 5 分钟 / 15 分钟 / 1 小时起步逐次退避，最长 6 小时，不立即重试。</p>
   </div>
   <div v-if="pending" class="rounded-lg border border-amber-400 bg-amber-50 p-3 space-y-2 dark:bg-dark-800" role="alert">
    <strong>确认操作：{{ actionLabels[pending.action] }} · {{ pending.ids.length }} 个账号</strong>
    <p class="break-words text-xs">{{ pending.ids.map(id => '#'+id+' '+items.find(i=>i.account_id===id)?.account_name).join('；') }}</p>
    <p v-if="pending.action==='schedule'">模型 {{ model }}，Cron {{ cron }}；已排除账号仍不会执行。</p>
    <input v-model="note" class="input w-full" aria-label="操作备注" placeholder="备注（排除检测时必填，便于以后查阅）" />
    <button class="btn btn-primary" title="仅对上述已列出的账号执行一次" @click="confirmAction">确认执行</button>
    <button class="btn btn-secondary ml-2" title="取消本次批量操作" @click="pending=null">取消</button>
   </div>
   <p v-if="progress" role="status" aria-live="polite" class="whitespace-pre-wrap text-xs">{{ progress }}</p>
   <p v-if="loading">加载中…</p>
   <div v-else class="max-h-[55vh] overflow-auto space-y-3">
    <article v-for="i in filtered" :key="i.account_id" class="rounded-lg border p-3 dark:border-dark-600" :class="i.vm_state==='derived'?'bg-teal-50/50 dark:bg-teal-950/20':''">
     <div class="flex flex-wrap items-center gap-3">
      <input type="checkbox" :checked="selected.has(i.account_id)" :disabled="busy || i.deleted" :aria-label="'选择 '+i.account_name" @change="toggle(i.account_id)" />
      <strong>#{{ i.account_id }} {{ i.account_name }}</strong>
      <span :class="verdict(i)==='degraded'?'text-red-600':verdict(i)==='not_degraded'?'text-green-600':'text-amber-600'">{{ labels[verdict(i)] || verdict(i) }}</span>
      <span class="rounded bg-gray-100 px-2 dark:bg-dark-700">{{ vmLabels[i.vm_state] }}{{ i.target_id ? ' → #'+i.target_id : '' }}</span>
      <span v-if="i.deleted" class="text-red-600">账号已删除</span>
      <span v-if="i.review" class="text-blue-600">人工审核 · {{ i.review.note }}</span>
     </div>
     <div class="mt-2 flex flex-wrap gap-3 text-xs text-gray-500">
      <span>{{ i.result?.model_id || '尚无结果' }} · {{ i.result ? formatDateTime(i.result.finished_at) : '—' }}</span>
      <span>{{ i.plans.filter(p=>p.enabled).length }} / {{ i.plans.length }} 个计划启用</span>
      <span v-for="p in i.plans" :key="p.id">#{{ p.id }} {{ p.cron_expression }} {{ p.enabled?'启用':'禁用' }} · 下次 {{ p.next_run_at ? formatDateTime(p.next_run_at) : '—' }}</span>
     </div>
     <p v-if="gate(i)" class="mt-2 text-xs text-amber-700">{{ gate(i) }}。计划即使启用也不会自动执行。</p>
     <p v-if="i.failure" class="mt-2 text-sm text-red-600">{{ i.failure.label }}：{{ i.failure.advice }}</p>
     <p v-if="i.result?.quality" class="mt-2 text-xs">自动判定：{{ labels[i.result.quality.verdict] }}；提取数字：{{ i.result.quality.numbers?.join(', ') || '—' }}</p>
     <img v-if="i.result?.pelican?.svg" :src="'data:image/svg+xml;charset=utf-8,'+encodeURIComponent(i.result.pelican.svg)" alt="最新鹈鹕骑车 SVG，供人工审核" class="mt-2 h-40 max-w-full bg-white object-contain" loading="lazy" />
     <details v-if="i.result" class="mt-2 text-xs">
      <summary class="cursor-pointer">查看原始回答 / 错误{{ i.result.pelican ? ' / 鹈鹕图与结构评分 '+i.result.pelican.score : '' }}</summary>
      <pre class="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-words">{{ i.result.error_message || i.result.response_text }}</pre>
     </details>
     <div class="mt-2 flex gap-3 text-xs">
      <button :disabled="busy || !!gate(i) || i.deleted" class="text-primary-600 disabled:opacity-40" title="发起一次检测；不循环重试" @click="prepare('run',[i.account_id])">立即检测</button>
      <button v-if="i.result && !i.deleted" :disabled="busy" class="text-primary-600" title="保留自动判定，添加人工复核标记；新检测结果产生后标记失效" @click="reviewing=i;reviewNote='';reviewVerdict='not_degraded'">人工审核标记</button>
     </div>
     <div v-if="reviewing?.account_id===i.account_id" class="mt-2 rounded border p-2 space-y-2">
      <p class="text-xs">人工标记用于此总览筛选和批量操作，不改变原始结果或账号调度策略。新检测结果会使本次标记失效。</p>
      <select v-model="reviewVerdict" class="input" aria-label="人工审核结果"><option value="not_degraded">不降智</option><option value="degraded">降智</option><option value="inconclusive">无法判定</option><option value="clear">清除人工标记</option></select>
      <input v-model="reviewNote" class="input w-full" aria-label="审核依据" placeholder="填写看图或复核依据" />
      <button class="btn btn-primary" :disabled="busy" title="只标记正在查看的这一条结果" @click="saveReview(i)">保存审核</button>
      <button class="btn btn-secondary ml-2" title="取消审核" @click="reviewing=null">取消</button>
     </div>
    </article>
    <p v-if="!filtered.length">没有符合条件的账号。</p>
   </div>
  </div>
 </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { probeManagementAPI as api, scheduledTestsAPI, type ProbeManagementItem as Item, type ProbeSettings } from '@/api/admin/scheduledTests'
import { getVMBridge, syncVMBridge } from '@/api/admin/vm2api'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
const props=defineProps<{show:boolean}>();const emit=defineEmits<{(e:'close'):void}>();const app=useAppStore()
const items=ref<Item[]>([]),loading=ref(false),busy=ref(false),stopQueued=ref(false),search=ref(''),stateFilter=ref(''),vmFilter=ref(''),errorFilter=ref('')
const kind=ref<'candy_v1'|'pelican_svg_v1'>('candy_v1'),model=ref('gpt-6.1-sol'),cron=ref('15 4 * * *'),note=ref(''),progress=ref('')
const settings=ref<ProbeSettings>({concurrency:10,exclude_vm:false});const selected=reactive(new Set<number>())
const pending=ref<{action:string;ids:number[]}|null>(null),reviewing=ref<Item|null>(null),reviewNote=ref(''),reviewVerdict=ref('not_degraded')
const labels:Record<string,string>={not_degraded:'不降智',degraded:'降智',inconclusive:'检测异常 / 无法判定',not_tested:'尚未检测',structural_pass:'SVG 结构通过 / 待人工审核',invalid_svg:'SVG 结构异常 / 待人工审核'}
const vmLabels={not_linked:'尚未转入 VM',linked:'原账号已关联 VM',derived:'VM 衍生账号'}
const actionLabels:Record<string,string>={run:'检测所选',vm:'转入 VM2API',schedule:'设置定时计划',disable:'禁用计划',exclude:'排除检测',include:'取消账号排除',resume:'修复后恢复检测',cleanup:'停用已删除账号计划'}
function verdict(i:Item){return i.review?.verdict || (i.failure?'inconclusive':i.result?.quality?.verdict || (i.result?.pelican ? (i.result.pelican.verdict==='reference_only' || i.result.pelican.verdict==='structural_pass'?'structural_pass':i.result.pelican.verdict==='inconclusive'?'inconclusive':'invalid_svg') : 'not_tested'))}
const filtered=computed(()=>items.value.filter(i=>(!stateFilter.value||verdict(i)===stateFilter.value)&&(!vmFilter.value||i.vm_state===vmFilter.value)&&(!errorFilter.value||i.failure?.code===errorFilter.value)&&`${i.account_id} ${i.account_name} ${i.result?.error_message||''}`.toLowerCase().includes(search.value.toLowerCase())))
const failures=computed(()=>Array.from(new Map(items.value.flatMap(i=>i.failure?[[i.failure.code,i.failure] as const]:[])).values()))
function gate(i:Item){if(i.deleted)return '账号已删除';if(i.control.excluded)return '账号已排除检测：'+(i.control.note||'未填写备注');if(settings.value.exclude_vm&&i.vm_state==='derived')return '全局设置已排除 VM 衍生账号';if(i.control.pause_code&&(!i.control.pause_until||Date.parse(i.control.pause_until)>Date.now()))return i.control.pause_until?'异常退避至 '+formatDateTime(i.control.pause_until):'异常暂停，修复后需手动恢复检测';return ''}
let generation=0
async function load(){const g=++generation;loading.value=true;try{const [data,config]=await Promise.all([api.list(kind.value),api.settings()]);if(g===generation){items.value=data;settings.value=config}}catch(e:any){app.showError(e.message)}finally{if(g===generation)loading.value=false}}
watch(()=>props.show,v=>{if(v)void load();else{generation++;stopQueued.value=true;pending.value=null}})
watch(kind,()=>{selected.clear();stateFilter.value='';errorFilter.value='';reviewing.value=null;if(kind.value==='candy_v1')model.value='gpt-6.1-sol';if(props.show)void load()})
watch([search,stateFilter,vmFilter,errorFilter],()=>{selected.clear();pending.value=null})
onBeforeUnmount(()=>{generation++;stopQueued.value=true})
function toggle(id:number){selected.has(id)?selected.delete(id):selected.add(id)}
function selectFiltered(){selected.clear();filtered.value.filter(i=>!i.deleted).forEach(i=>selected.add(i.account_id))}
function prepare(action:string,ids?:number[]){pending.value={action,ids:ids||Array.from(selected)};note.value=''}
async function saveSettings(){busy.value=true;try{settings.value=await api.saveSettings(settings.value);app.showSuccess('检测设置已保存')}catch(e:any){app.showError(e.message)}finally{busy.value=false}}
async function runOne(i:Item){if(gate(i)){return '#'+i.account_id+' 跳过：'+gate(i)}let plan=i.plans[0];if(!plan)plan=await scheduledTestsAPI.create({account_id:i.account_id,test_kind:kind.value,model_id:model.value,cron_expression:cron.value,enabled:false,max_results:50});const r=await scheduledTestsAPI.runNow(plan.id);i.result=r;return '#'+i.account_id+' '+(r.status==='success'?'完成':'调用异常（不重试）')}
async function transfer(i:Item){if(i.vm_state!=='not_linked'||i.account_type!=='oauth'||i.deleted)return '#'+i.account_id+' 跳过：已关联或非 OAuth 账号'
 let state=await getVMBridge(i.account_id)
 if(!state.enabled)throw new Error('VM2API 尚未配置')
 if(state.job?.status!=='running')await syncVMBridge(i.account_id,true)
 const deadline=Date.now()+600000
 while(Date.now()<deadline){await new Promise(r=>setTimeout(r,2500));state=await getVMBridge(i.account_id);if(state.job?.status!=='running'){if(state.job?.status!=='completed')throw new Error(state.job?.message||'同步未完成');return '#'+i.account_id+' 转入成功 → #'+state.job.target_id}}
 throw new Error('任务仍在后台运行；已停止队列，请刷新检查，不自动重复提交')
}
async function confirmAction(){const p=pending.value;if(!p)return;if(p.action==='exclude'&&!note.value.trim()){app.showError('请填写排除备注');return}pending.value=null;busy.value=true;stopQueued.value=false;progress.value='执行中…'
 try{if(p.action==='cleanup'){const r=await api.cleanup();progress.value='已停用 '+r.disabled+' 个已删除账号的计划'}
 else if(p.action==='run'||p.action==='vm'){const queue=p.ids.map(id=>items.value.find(i=>i.account_id===id)).filter((i):i is Item=>!!i);const lines:string[]=[]
 const worker=async()=>{while(queue.length&&!stopQueued.value){const i=queue.shift()!;try{lines.push(await(p.action==='run'?runOne(i):transfer(i)))}catch(e:any){lines.push('#'+i.account_id+' 失败：'+e.message);if(p.action==='vm')stopQueued.value=true}progress.value=lines.join('\n')}}
 await Promise.all(Array.from({length:p.action==='vm'?1:Math.max(1,Math.min(32,settings.value.concurrency))},worker));if(stopQueued.value)progress.value+='\n后续队列已停止。'}
 else{const r=await api.action({ids:p.ids,action:p.action,kind:kind.value,model:model.value,cron:cron.value,note:note.value});progress.value=r.map(x=>'#'+x.id+' '+(x.error||'完成')).join('\n')}
 }catch(e:any){app.showError(e.message)}finally{busy.value=false;selected.clear();if(props.show)await load()}}
async function saveReview(i:Item){busy.value=true;try{const r=await api.action({ids:[i.account_id],action:'review',kind:kind.value,result_id:i.result!.id,verdict:reviewVerdict.value,note:reviewNote.value});if(r[0]?.error)throw new Error(r[0].error);reviewing.value=null;await load()}catch(e:any){app.showError(e.message)}finally{busy.value=false}}
function exportResults(){const url=URL.createObjectURL(new Blob([JSON.stringify(filtered.value,null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='account-probes-'+kind.value+'.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
</script>
