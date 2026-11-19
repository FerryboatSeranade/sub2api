<template>
<AppLayout><div class="mx-auto max-w-6xl space-y-6 pb-20">
 <header class="flex flex-wrap items-start justify-between gap-4"><div><h1 class="text-2xl font-semibold">分层调度</h1><p class="mt-2 text-gray-500">用一个 Key 按来源与检测质量选择账号；从上到下寻找可用层。</p></div><span class="rounded-full px-3 py-1 text-sm" :class="savedEnabled?'bg-green-100 text-green-800':'bg-gray-100 text-gray-600'">{{savedEnabled?'已启用':'未启用'}}</span></header>
 <p v-if="loading" role="status">正在读取配置…</p><p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-red-700">{{error}} <span v-if="!config.layers.length" class="tip"><button type="button" class="btn btn-secondary" @click="load">重新加载</button><span role="tooltip">重新读取配置与分组。</span></span></p>
 <template v-if="!loading">
 <section class="card space-y-4 p-6"><h2 class="text-lg font-semibold">来源与生效范围</h2>
 <p class="text-sm text-gray-500">默认对所有 OpenAI 分组的 Key 生效，也可限定或排除分组。来源分组提供候选账号，扣费仍按 Key 的生效分组规则。仅支持 OpenAI 分组及下面填写的精确模型名。</p>
 <div class="grid gap-6 md:grid-cols-3">
 <fieldset class="min-w-0 space-y-3 rounded-xl border p-4"><legend class="px-1 font-medium">使用策略的 Key 分组</legend>
 <label class="block text-sm">生效范围<select v-model="config.entry_scope" class="input mt-2 w-full" aria-label="生效范围" :disabled="busy"><option value="all">所有 OpenAI 分组（默认）</option><option value="selected">仅指定分组</option><option value="exclude">排除指定分组</option></select></label>
 <p class="text-xs text-gray-500">{{config.entry_scope==='all'?'所有现有及以后新增的 OpenAI 分组自动适用，无需逐个勾选。':config.entry_scope==='selected'?'只有勾选分组的 Key 使用此策略。':'勾选的分组沿用原有调度，其他现有及新增 OpenAI 分组使用此策略。'}}</p>
 <div v-if="config.entry_scope!=='all'" class="max-h-56 space-y-2 overflow-y-auto"><label v-for="g in groups" :key="g.id" class="flex items-center gap-2 text-sm"><input v-model="config[scopeGroupKey]" type="checkbox" :value="g.id" :disabled="busy" :aria-label="(config.entry_scope==='exclude'?'排除分组：':'生效分组：')+g.name" /><span>{{g.name}} <span class="text-gray-400">#{{g.id}}</span></span></label></div>
 <p class="text-xs text-gray-500">仅在总开关开启并保存后生效；仍受下方模型范围及原有 Key 权限限制。</p>
 </fieldset>
 <fieldset v-for="field in groupFields" :key="field.key" class="min-w-0 rounded-xl border p-4"><legend class="px-1 font-medium">{{field.label}}</legend><p class="mb-3 text-xs text-gray-500">{{field.help}}</p>
 <div class="max-h-56 space-y-2 overflow-y-auto"><label v-for="g in groups" :key="g.id" class="flex items-center gap-2 text-sm"><input v-model="config[field.key]" type="checkbox" :value="g.id" :aria-label="field.label+'：'+g.name" /><span class="break-all">{{g.name}} <span class="text-gray-400">#{{g.id}}</span></span></label><p v-if="!groups.length" class="text-sm text-gray-500">暂无可用 OpenAI 分组。</p></div>
 </fieldset></div>
 <p v-if="overlap" class="text-sm text-red-600" role="alert">自建与外接分组不能重叠，请取消重复选择。</p>
 <div class="grid gap-4 md:grid-cols-2"><label class="space-y-2"><span class="text-sm font-medium">应用模型（每行一个）</span><textarea v-model="modelText" class="input w-full" rows="3" aria-label="应用模型" placeholder="gpt-6.1-sol"></textarea></label><label class="space-y-2"><span class="text-sm font-medium">有效检测结果保留时间（小时）</span><input v-model.number="config.max_age_hours" class="input w-full" type="number" min="1" max="720" aria-label="检测有效期" /><p class="text-xs text-gray-500">按请求模型匹配糖果检测的最近成功结果；调用异常不覆盖它。过期、未检测或无法判定均为质量未知。</p></label></div>
 </section>
 <section class="space-y-4"><div class="flex flex-wrap items-center justify-between gap-3"><div><h2 class="text-lg font-semibold">调度顺序</h2><p class="mt-1 text-sm text-gray-500">上移、下移调整优先级。高层无可用账号或并发已满时尝试下一层；原有健康、配额和权限限制仍有效。</p></div>
 <span class="tip"><button type="button" class="btn btn-secondary" :disabled="config.layers.length>=6" @click="addLayer" aria-describedby="add-help">添加层级</button><span id="add-help" role="tooltip">最多配置六种来源与质量组合，不能重复。</span></span></div>
 <article v-for="(layer,index) in config.layers" :key="index" class="card p-5" :class="!layer.enabled?'opacity-60':''">
 <div class="flex flex-wrap items-center gap-3"><span class="flex h-9 w-9 items-center justify-center rounded-full bg-primary-100 font-semibold text-primary-700">{{index+1}}</span>
 <select v-model="layer.source" class="input w-full sm:!w-40" :aria-label="'第'+(index+1)+'层来源'"><option value="self">自建</option><option value="external">外接</option></select>
 <select v-model="layer.quality" class="input w-full sm:!w-40" :aria-label="'第'+(index+1)+'层质量'"><option value="not_degraded">不降智</option><option value="degraded">降智</option><option value="unknown">质量未知</option></select>
 <label class="ml-auto flex items-center gap-2 text-sm"><input type="checkbox" v-model="layer.enabled" />参与调度</label>
 <span v-for="action in ['up','down','remove'] as const" :key="action" class="tip"><button type="button" class="btn btn-secondary px-3" :disabled="action==='up'?index===0:action==='down'?index===config.layers.length-1:config.layers.length===1" :aria-label="actionLabel(action,index)" @click="changeLayer(action,index)">{{action==='up'?'↑':action==='down'?'↓':'移除'}}</button><span role="tooltip">{{actionLabel(action,index)}}</span></span></div>
 <div v-if="health && !healthStale" class="mt-4">
 <div class="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4"><div>匹配账号 <b>{{health.layers[index]?.total||0}}</b></div><div class="text-green-700">可用 <b>{{health.layers[index]?.available||0}}</b></div><div class="text-amber-700">并发忙 <b>{{health.layers[index]?.busy||0}}</b></div><div class="text-red-700">受限 <b>{{health.layers[index]?.blocked||0}}</b></div></div>
 <div class="mt-3 h-2 overflow-hidden rounded-full bg-gray-100"><div class="h-full rounded-full bg-green-500" :style="{width:percent(index)+'%'}"></div></div>
 <details class="mt-3 text-sm"><summary class="cursor-pointer">查看账号与原因</summary><ul class="mt-2 max-h-56 space-y-2 overflow-y-auto"><li v-for="a in health.layers[index]?.accounts||[]" :key="a.id" class="flex flex-wrap justify-between gap-2 border-b py-2"><span>#{{a.id}} {{a.name}}</span><span>{{reasonLabel(a.reason)}}</span></li></ul><p v-if="!health.layers[index]?.total" class="mt-2 text-gray-500">此层暂无匹配账号，检查来源分组、模型检测结果及有效期。</p></details>
 </div><p v-else class="mt-4 text-sm text-gray-500">{{healthStale?'配置已修改，请重新预览健康度。':'预览后显示此层的账号健康度。'}}</p>
 </article>
 </section>
 <section class="card space-y-3 p-6"><div class="flex flex-wrap items-center gap-3"><h2 class="text-lg font-semibold">健康度预览</h2><label class="ml-auto text-sm">请求模型 <select v-model="healthModel" class="input" aria-label="健康度模型"><option v-for="m in models" :key="m">{{m}}</option></select></label><span class="tip"><button class="btn btn-secondary" type="button" :disabled="busy||!healthModel||overlap" @click="preview">预览健康度</button><span role="tooltip">按当前未保存配置查询健康状态，不发送推理请求。</span></span></div>
 <template v-if="health"><p class="text-sm text-gray-500">快照时间：{{new Date(health.checked_at).toLocaleString()}} · 质量未知 {{health.unknown_accounts}} 个账号</p><p class="text-sm text-gray-500">{{health.note}}</p></template>
 <p class="text-sm text-gray-500">健康度是快照，并非成功率。来源重叠账号会被排除；上游要求固定账号的续接请求保留所属账号，不强行切换。未列入任何启用层的账号不会兜底。</p></section>
 <footer class="card flex flex-wrap items-center gap-4 p-5"><label class="flex items-center gap-2 font-medium"><input type="checkbox" v-model="config.enabled" />启用分层调度</label><p class="text-sm text-gray-500">{{dirty?'有未保存修改':'配置已保存'}} · 保存后约 3 秒生效</p><span class="tip ml-auto"><button class="btn btn-primary" type="button" :disabled="busy||overlap" @click="save">{{busy?'处理中…':'保存配置'}}</button><span role="tooltip">保存来源、顺序和生效范围；关闭后恢复原有调度。</span></span></footer>
 </template>
</div></AppLayout>
</template>
<script setup lang="ts">
import {ref,computed,onMounted,watch} from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import {apiClient} from '@/api/client'
import {getAll} from '@/api/admin/groups'
import type {AdminGroup} from '@/types'
import {useAppStore} from '@/stores/app'
type Layer={source:'self'|'external';quality:'not_degraded'|'degraded'|'unknown';enabled:boolean}
type Config={enabled:boolean;entry_scope:'all'|'selected'|'exclude';excluded_entry_groups:number[];entry_groups:number[];self_groups:number[];external_groups:number[];models:string[];max_age_hours:number;layers:Layer[]}
type Health={checked_at:string;unknown_accounts:number;note:string;layers:{index:number;total:number;available:number;busy:number;blocked:number;accounts:{id:number;name:string;reason:string}[]}[]}
const endpoint='/admin/scheduled-test-plans/source-tiers'
const app=useAppStore(),loading=ref(true),busy=ref(false),error=ref(''),groups=ref<AdminGroup[]>([])
const config=ref<Config>({enabled:false,entry_scope:'all',excluded_entry_groups:[],entry_groups:[],self_groups:[],external_groups:[],models:['gpt-6.1-sol'],max_age_hours:48,layers:[]})
const modelText=ref(''),healthModel=ref(''),health=ref<Health|null>(null),saved=ref(''),previewKey=ref(''),savedEnabled=ref(false)
const groupFields=[{key:'self_groups',label:'自建来源分组',help:'选择原生直连或自建 VM2API 所属分组。'},{key:'external_groups',label:'外接来源分组',help:'选择第三方中转、外部号池所属分组。'}] as const
const scopeGroupKey=computed(()=>config.value.entry_scope==='exclude'?'excluded_entry_groups':'entry_groups')
const models=computed(()=>modelText.value.split('\n').map(v=>v.trim()).filter(Boolean))
const payload=computed(()=>({...config.value,models:models.value}))
const dirty=computed(()=>JSON.stringify(payload.value)!==saved.value)
const overlap=computed(()=>config.value.self_groups.some(id=>config.value.external_groups.includes(id)))
const healthStale=computed(()=>health.value!==null&&previewKey.value!==JSON.stringify([payload.value,healthModel.value]))
watch(models,v=>{if(!v.includes(healthModel.value))healthModel.value=v[0]||''})
const message=(e:any)=>e.response?.data?.message||e.message||'操作失败，请重试'
function actionLabel(action:string,index:number){return action==='up'?'将第 '+(index+1)+' 层上移，提高优先级':action==='down'?'将第 '+(index+1)+' 层下移，降低优先级':'移除第 '+(index+1)+' 层，不删除账号'}
function changeLayer(action:string,index:number){const layers=config.value.layers;if(action==='remove')layers.splice(index,1);else {const to=index+(action==='up'?-1:1);[layers[index],layers[to]]=[layers[to]!,layers[index]!]}}
function addLayer(){for(const source of ['self','external'] as const)for(const quality of ['not_degraded','degraded','unknown'] as const)if(!config.value.layers.some(l=>l.source===source&&l.quality===quality)){config.value.layers.push({source,quality,enabled:true});return}}
function percent(i:number){const row=health.value?.layers[i];return row?.total?100*row.available/row.total:0}
function reasonLabel(reason:string){return ({ready:'可用',busy:'并发已满',runtime_blocked:'运行时熔断',disabled_or_cooling:'禁用、冷却、额度或严格质量限制',model_incompatible:'模型或请求条件不兼容',source_conflict:'同时属于自建和外接来源',load_unknown:'暂无法读取并发'} as Record<string,string>)[reason]||reason}
async function preview(){busy.value=true;error.value='';const key=JSON.stringify([payload.value,healthModel.value]);try{const {data}=await apiClient.post<Health>(endpoint+'/health',{config:payload.value,model:healthModel.value});health.value=data;previewKey.value=key}catch(e){error.value=message(e)}finally{busy.value=false}}
async function save(){busy.value=true;error.value='';try{const {data}=await apiClient.put<Config>(endpoint,payload.value);config.value=data;modelText.value=data.models.join('\n');saved.value=JSON.stringify(data);savedEnabled.value=data.enabled;app.showSuccess('分层调度配置已保存')}catch(e){error.value=message(e)}finally{busy.value=false}}
async function load(){loading.value=true;error.value='';try{const [res,g]=await Promise.all([apiClient.get<Config>(endpoint),getAll('openai')]);config.value=res.data;modelText.value=res.data.models.join('\n');groups.value=g;saved.value=JSON.stringify(res.data);savedEnabled.value=res.data.enabled}catch(e){error.value=message(e)}finally{loading.value=false}}
onMounted(load)
</script>
<style scoped>
.tip{position:relative;display:inline-flex}
.tip>[role=tooltip]{pointer-events:none;position:absolute;bottom:calc(100% + 8px);right:0;width:220px;padding:8px 10px;border-radius:8px;background:#111827;color:white;font-size:12px;z-index:30;opacity:0;transition:opacity .12s}
.tip:hover>[role=tooltip],.tip:focus-within>[role=tooltip]{opacity:1}
</style>
