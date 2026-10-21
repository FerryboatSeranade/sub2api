import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import VM2APISyncDialog from '../VM2APISyncDialog.vue'
const api = vi.hoisted(() => ({ get: vi.fn(), sync: vi.fn() }))
vi.mock('@/api/admin/vm2api', () => ({ getVMBridge: api.get, syncVMBridge: api.sync }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (v: string) => v }))
const account = { id: 47, name: 'source' } as never
const mountDialog = () => mount(VM2APISyncDialog, { props: { account }, global: { stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' } } } })
beforeEach(() => { vi.useFakeTimers(); api.get.mockReset(); api.sync.mockReset() })
afterEach(() => { vi.useRealTimers() })
describe('VM2API sync workflow', () => {
 it('blocks submission when integration is unconfigured', async () => {
  api.get.mockResolvedValue({ enabled: false })
  const w = mountDialog(); await flushPromises()
  expect(w.findAll('button')[1].attributes('disabled')).toBeDefined()
  expect(api.sync).not.toHaveBeenCalled(); w.unmount()
 })
 it('submits once and polls completion without allowing duplicate clicks', async () => {
  api.get.mockResolvedValueOnce({ enabled: true, job: {} }).mockResolvedValue({ enabled: true, job: { status: 'completed', target_id: 90, message: 'done' } })
  api.sync.mockResolvedValue({ status: 'running', message: 'testing' })
  const w = mountDialog(); await flushPromises()
  await w.findAll('button')[1].trigger('click'); await flushPromises()
  expect(api.sync).toHaveBeenCalledTimes(1)
  expect(api.sync).toHaveBeenCalledWith(47, true)
  expect(w.findAll('button')[1].attributes('disabled')).toBeDefined()
  await vi.advanceTimersByTimeAsync(1100); await flushPromises()
  expect(w.emitted('updated')).toHaveLength(1)
  expect(w.text()).toContain('#90'); w.unmount()
 })
 it('stops polling when dialog closes', async () => {
  api.get.mockResolvedValue({ enabled: true, job: { status: 'running', message: 'testing' } })
  const w = mountDialog(); await flushPromises()
  await w.setProps({ account: null }); await vi.advanceTimersByTimeAsync(5000)
  expect(api.get).toHaveBeenCalledTimes(1); w.unmount()
 })
})
