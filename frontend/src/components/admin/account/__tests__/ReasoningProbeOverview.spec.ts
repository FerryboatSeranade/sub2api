import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Overview from '../ReasoningProbeOverview.vue'
const m=vi.hoisted(()=>({list:vi.fn(),settings:vi.fn(),action:vi.fn(),run:vi.fn(),sync:vi.fn(),error:vi.fn()}))
vi.mock('@/stores/app',()=>({useAppStore:()=>({showError:m.error,showSuccess:vi.fn()})}))
vi.mock('@/api/admin/scheduledTests',()=>({probeManagementAPI:{list:m.list,settings:m.settings,action:m.action},scheduledTestsAPI:{runNow:m.run}}))
vi.mock('@/api/admin/vm2api',()=>({getVMBridge:vi.fn(),syncVMBridge:m.sync}))
const item=(id:number,vm='not_linked',v='not_degraded')=>({account_id:id,account_name:'account'+id,account_type:'oauth',deleted:false,vm_state:vm,target_id:0,control:{excluded:false,note:'',pause_code:'',pause_until:null,failures:0},plans:[{id,enabled:true,cron_expression:'15 4 * * *'}],result:{id,quality:{verdict:v,numbers:['21']},status:'success',finished_at:'2026-10-23T01:00:00Z'}})
async function setup(){const w=mount(Overview,{props:{show:false},global:{stubs:{BaseDialog:{template:'<div><slot /></div>'}}}});await w.setProps({show:true});await flushPromises();return w}
function button(w:any,text:string){return w.findAll('button').find((x:any)=>x.text().includes(text))!}
beforeEach(()=>{vi.clearAllMocks();m.list.mockResolvedValue([item(1),item(2,'derived'),item(3,'not_linked','degraded')]);m.settings.mockResolvedValue({concurrency:10,exclude_vm:false,exclude_vm_source:false});m.action.mockResolvedValue([{id:1}])})
describe('probe management',()=>{
 it('scopes batch selection to filters and previews before VM transfer',async()=>{const w=await setup();await w.find('[aria-label="检测状态筛选"]').setValue('not_degraded');await w.find('[aria-label="VM 关联筛选"]').setValue('not_linked');await button(w,'全选筛选').trigger('click');await button(w,'批量转入').trigger('click');expect(w.text()).toContain('确认操作：转入 VM2API · 1 个账号');expect(m.sync).not.toHaveBeenCalled();expect(w.text()).toContain('检测所选（并发 10）')})
 it('skips excluded VM plans even when enabled',async()=>{m.settings.mockResolvedValue({concurrency:10,exclude_vm:true,exclude_vm_source:false});m.list.mockResolvedValue([item(2,'derived')]);const w=await setup();await button(w,'全选筛选').trigger('click');await button(w,'检测所选').trigger('click');await button(w,'确认执行').trigger('click');await flushPromises();expect(m.run).not.toHaveBeenCalled();expect(w.text()).toContain('跳过')})
 it('skips converted originals with enabled plans independently',async()=>{m.settings.mockResolvedValue({concurrency:10,exclude_vm:false,exclude_vm_source:true});m.list.mockResolvedValue([item(4,'linked')]);const w=await setup();expect(w.text()).toContain('排除已转为 vm2api 的原生账号检测');await button(w,'全选筛选').trigger('click');await button(w,'检测所选').trigger('click');await button(w,'确认执行').trigger('click');await flushPromises();expect(m.run).not.toHaveBeenCalled();expect(w.text()).toContain('跳过')})
 it('requires a persistent exclusion note',async()=>{const w=await setup();await w.find('article input[type="checkbox"]').setValue(true);await button(w,'排除检测').trigger('click');await button(w,'确认执行').trigger('click');expect(m.error).toHaveBeenCalledWith('请填写排除备注');expect(m.action).not.toHaveBeenCalled()})
})

describe('plan and transport dimensions',()=>{
 it('filters VM Pro separately from native Pro and VM Plus',async()=>{
  m.list.mockResolvedValue([{...item(1),plan_type:'pro'},{...item(2,'derived','degraded'),plan_type:'pro',source_id:10,source_name:'source-pro'},{...item(3,'derived'),plan_type:'plus'}])
  const w=await setup()
  await w.find('[aria-label="筛选 VM2API · Pro · 降智"]').trigger('click')
  expect(w.findAll('article')).toHaveLength(1)
  expect(w.find('article').text()).toContain('source-pro')
  expect(w.find('article').text()).toContain('account2')
 })
 it('notifies the account list as soon as a probe finishes',async()=>{
  const row=item(1);row.plans[0]={...row.plans[0],model_id:'gpt-6.1-sol'} as any
  m.list.mockResolvedValue([row]);m.run.mockResolvedValue({...row.result,id:22})
  const w=await setup();await button(w,'立即检测').trigger('click');await button(w,'确认执行').trigger('click');await flushPromises()
  expect(w.emitted('result-updated')).toHaveLength(1)
 })
})
