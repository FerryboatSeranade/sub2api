import { describe, it, expect, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import AccountReasoningCell from '../AccountReasoningCell.vue'
import type { AccountListItem } from '@/types'
import type { ReasoningOverviewItem } from '@/api/admin/scheduledTests'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const mount = (verdict: string | null, enabled = false) => shallowMount(AccountReasoningCell, {
 props: { account: { id: 1, platform: 'openai', extra: { reasoning_auto_exclude: enabled } } as AccountListItem,
 result: verdict ? { quality: { verdict }, finished_at: '2026-10-19T00:00:00Z' } as ReasoningOverviewItem['result'] : null, loadError: false, busy: false },
 global: { stubs: { HelpTooltip: { template: '<div><slot name="trigger" /></div>' } } }
})
describe('account reasoning policy', () => {
 it('distinguishes untested and failed from degraded', () => {
  expect(mount(null).text()).toContain('reasoning.untested')
  expect(mount('inconclusive', true).text()).toContain('reasoning.excludedInconclusive')
  expect(mount('inconclusive', false).text()).not.toContain('reasoning.excludedInconclusive')
  expect(mount('degraded', true).text()).toContain('reasoning.excluded')
 })
 it('emits opt-in without changing the manual scheduling flag', async () => {
  const w=mount('degraded');await w.find('select').setValue('enabled')
  expect(w.emitted('policy')).toEqual([['enabled']])
 })
 it('inherits global and allows an explicit opt-out', async () => {
  const w=mount('inconclusive'); await w.setProps({globalEnabled:true})
  expect(w.text()).toContain('reasoning.excluded')
  await w.setProps({account:{id:1,platform:'openai',extra:{reasoning_exclusion_policy:'disabled'}} as AccountListItem})
  expect(w.text()).not.toContain('reasoning.excluded')
 })
 it('shows a fetch error rather than falsely displaying untested', async () => {
  const w=mount(null);await w.setProps({loadError:true})
  expect(w.text()).toContain('reasoning.loadError')
 })
})
