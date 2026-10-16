import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import ReasoningProbeOverview from '../ReasoningProbeOverview.vue'
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (k: string) => k }) }))
const { overview, runNow, update, showError } = vi.hoisted(() => ({ overview: vi.fn(), runNow: vi.fn(), update: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin/scheduledTests', () => ({ scheduledTestsAPI: { reasoningOverview: overview, runNow, update } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
enableAutoUnmount(afterEach)
const row = (id: number, state: string) => ({ account_id: id, account_name: `test-${id}`, plan_id: id, enabled: true, cron_expression: '0 3 * * *', result: { id, started_at: '2026-10-17T00:00:00Z', model_id: 'gpt-6.1-sol', quality: { verdict: state, numbers: state === 'not_degraded' ? ['21'] : ['29'] }, response_text: 'answer' } })
async function open() {
 const w = shallowMount(ReasoningProbeOverview, { props: { show: false }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
 await w.setProps({ show: true }); await flushPromises(); return w
}
describe('reasoning overview', () => {
 beforeEach(() => { vi.resetAllMocks(); overview.mockResolvedValue([row(1, 'not_degraded'), row(2, 'degraded'), row(3, 'inconclusive')]) })
 it('renders all verdicts and retains original text', async () => {
  const w = await open()
  expect(w.text()).toContain('admin.scheduledTests.not_degraded: 1')
  expect(w.text()).toContain('admin.scheduledTests.degraded: 1')
  expect(w.text()).toContain('admin.scheduledTests.inconclusive: 1')
  expect(w.text()).toContain('21')
  expect(w.findAll('details')).toHaveLength(3)
 })
 it('limits full-batch execution to two calls and stops queued work on close', async () => {
  const resolvers: Array<(v: unknown) => void> = []
  runNow.mockImplementation(() => new Promise(resolve => resolvers.push(resolve)))
  const w = await open()
  await w.findAll('button').find(b => b.text() === 'admin.scheduledTests.runAll')!.trigger('click')
  expect(runNow).toHaveBeenCalledTimes(2)
  await w.setProps({ show: false })
  resolvers.forEach(resolve => resolve(row(1,'not_degraded').result))
  await flushPromises()
  expect(runNow).toHaveBeenCalledTimes(2)
 })
 it('does not reclassify failed API calls as degraded', async () => {
  runNow.mockRejectedValue(new Error('timeout'))
  const w = await open()
  await w.findAll('button').find(b => b.text() === 'admin.scheduledTests.runNow')!.trigger('click')
  await flushPromises()
  expect(showError).toHaveBeenCalledWith('#1: timeout')
  expect(w.text()).toContain('admin.scheduledTests.not_degraded: 1')
 })
})
