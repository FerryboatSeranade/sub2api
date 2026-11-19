import {beforeEach,describe,it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Page from '../SourceTierRoutingView.vue'
const m=vi.hoisted(()=>({get:vi.fn(),put:vi.fn(),post:vi.fn(),groups:vi.fn(),success:vi.fn()}))
vi.mock('@/components/layout/AppLayout.vue',()=>({default:{template:'<div><slot /></div>'}}))
vi.mock('@/api/client',()=>({apiClient:{get:m.get,put:m.put,post:m.post}}))
vi.mock('@/api/admin/groups',()=>({getAll:m.groups}))
vi.mock('@/stores/app',()=>({useAppStore:()=>({showSuccess:m.success})}))
const initial=()=>({enabled:false,entry_scope:'all',excluded_entry_groups:[],entry_groups:[],self_groups:[],external_groups:[],models:['gpt-6.1-sol'],max_age_hours:48,layers:[{source:'self',quality:'not_degraded',enabled:true},{source:'external',quality:'not_degraded',enabled:true},{source:'self',quality:'degraded',enabled:true}]})
const button=(w:any,text:string)=>w.findAll('button').find((b:any)=>b.text().includes(text))!
async function setup(){const w=mount(Page,{global:{stubs:{AppLayout:{template:'<div><slot /></div>'}}}});await flushPromises();return w}
beforeEach(()=>{vi.resetAllMocks();m.get.mockResolvedValue({data:initial()});m.groups.mockResolvedValue([{id:10,name:'own'},{id:20,name:'external'}]);m.put.mockImplementation(async(_url,data)=>({data}))})
describe('source tier settings',()=>{
 it('loads disabled policy without modifying traffic and saves reordered layers',async()=>{
  const w=await setup();expect(m.put).not.toHaveBeenCalled()
  await w.find('[aria-label="将第 2 层上移，提高优先级"]').trigger('click')
  await button(w,'保存配置').trigger('click');await flushPromises()
  expect(m.put.mock.calls[0]![1].layers[0].source).toBe('external')
  expect(m.put.mock.calls[0]![1].enabled).toBe(false)
 })
 it('blocks overlapping source group selection',async()=>{
  const w=await setup()
  await w.find('[aria-label="自建来源分组：own"]').setValue(true)
  await w.find('[aria-label="外接来源分组：own"]').setValue(true)
  expect(button(w,'保存配置').attributes('disabled')).toBeDefined()
  expect(w.text()).toContain('自建与外接分组不能重叠')
 })
 it('previews draft health without saving and invalidates it after editing',async()=>{
  m.post.mockResolvedValue({data:{layers:[{index:0,total:2,available:1,busy:0,blocked:1,accounts:[]}],unknown_accounts:3,checked_at:new Date().toISOString(),note:'snapshot'}})
  const w=await setup();await button(w,'预览健康度').trigger('click');await flushPromises()
  expect(m.put).not.toHaveBeenCalled();expect(w.text()).toContain('质量未知 3 个账号')
  await w.find('[aria-label="检测有效期"]').setValue(24)
  expect(w.text()).toContain('配置已修改，请重新预览健康度')
 })
})

describe('entry group scope',()=>{
 it('defaults to all groups without requiring individual selection',async()=>{
  const w=await setup()
  expect((w.find('[aria-label="生效范围"]').element as HTMLSelectElement).value).toBe('all')
  expect(w.find('[aria-label="生效分组：own"]').exists()).toBe(false)
  expect(w.text()).toContain('以后新增的 OpenAI 分组自动适用')
  await button(w,'保存配置').trigger('click');await flushPromises()
  expect(m.put.mock.calls[0]![1].entry_scope).toBe('all')
  expect(m.put.mock.calls[0]![1].enabled).toBe(false)
 })
 it('saves separate inclusion and exclusion lists',async()=>{
  const w=await setup()
  await w.find('[aria-label="生效范围"]').setValue('selected')
  await w.find('[aria-label="生效分组：own"]').setValue(true)
  await w.find('[aria-label="生效范围"]').setValue('exclude')
  await w.find('[aria-label="排除分组：external"]').setValue(true)
  await button(w,'保存配置').trigger('click');await flushPromises()
  expect(m.put.mock.calls[0]![1].entry_scope).toBe('exclude')
  expect(m.put.mock.calls[0]![1].entry_groups).toEqual([10])
  expect(m.put.mock.calls[0]![1].excluded_entry_groups).toEqual([20])
 })
 it('preserves an existing explicit scope on load',async()=>{
  m.get.mockResolvedValue({data:{...initial(),entry_scope:'selected',entry_groups:[10]}})
  const w=await setup()
  expect((w.find('[aria-label="生效范围"]').element as HTMLSelectElement).value).toBe('selected')
  expect((w.find('[aria-label="生效分组：own"]').element as HTMLInputElement).checked).toBe(true)
 })
})
