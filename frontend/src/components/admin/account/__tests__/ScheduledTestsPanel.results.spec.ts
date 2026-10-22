import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

import ScheduledTestsPanel from '../ScheduledTestsPanel.vue'
const { listByAccount, listResults, showError } = vi.hoisted(() => ({
  listByAccount: vi.fn(), listResults: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: { scheduledTests: { listByAccount, listResults } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))

async function openPanel() {
  const wrapper = shallowMount(ScheduledTestsPanel, {
    props: { show: false, accountId: 1, modelOptions: [] },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}
const result = (id: number) => ({ id, latency_ms: id, status: 'success', started_at: '2026-10-03T00:00:00Z' })

describe('scheduled test result selection', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listByAccount.mockResolvedValue([1, 2].map(id => ({ id, model_id: `model-${id}`, enabled: true })))
  })

  it.each(['resolve', 'reject'] as const)('ignores an old plan request that later %ss', async outcome => {
    const old = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockResolvedValueOnce([result(22)])
    const wrapper = await openPanel()
    const headers = wrapper.findAll('.cursor-pointer')
    await headers[0].trigger('click')
    await headers[1].trigger('click')
    await flushPromises()
    const current = wrapper.html()
    if (outcome === 'resolve') old.resolve([result(11)])
    else old.reject(new Error('obsolete error'))
    await flushPromises()
    expect(wrapper.html()).toBe(current)
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps loading the current plan when the previous request completes', async () => {
    const old = deferred<ReturnType<typeof result>[]>()
    const current = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await openPanel()
    await wrapper.findAll('.cursor-pointer')[0].trigger('click')
    await wrapper.findAll('.cursor-pointer')[1].trigger('click')
    old.resolve([result(11)])
    await flushPromises()
    expect(wrapper.text()).toContain('common.loading')
    current.resolve([result(22)])
    await flushPromises()
    expect(wrapper.text()).not.toContain('common.loading')
  })

  it('invalidates a request when the same plan is collapsed and reopened', async () => {
    const old = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockResolvedValueOnce([result(22)])
    const wrapper = await openPanel()
    const header = wrapper.findAll('.cursor-pointer')[0]
    await header.trigger('click')
    await header.trigger('click')
    await header.trigger('click')
    await flushPromises()
    const current = wrapper.html()
    old.resolve([result(11)])
    await flushPromises()
    expect(wrapper.html()).toBe(current)
  })
  it('renders auxiliary SVG as an isolated image without a candy verdict', async () => {
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="20" cy="20" r="10"/></svg>'
    listResults.mockResolvedValue([{ ...result(1), test_kind: 'pelican_svg_v1', pelican: { verdict: 'reference_only', score: 50, checks: [{ name: 'safe_svg', passed: true }], svg } }])
    const wrapper = await openPanel()
    await wrapper.findAll('.cursor-pointer')[0].trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.scheduledTests.pelican_reference_only')
    expect(wrapper.text()).not.toContain('admin.scheduledTests.contains21')
    expect(wrapper.find('img').attributes('src')).toBe('data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg))
    expect(wrapper.find('svg').exists()).toBe(false)
  })

  it('offers Astra with a stale catalog and preserves the saved model while editing', async () => {
    listByAccount.mockResolvedValue([{ id: 1, model_id: 'gpt-6-astra', test_kind: 'pelican_svg_v1', enabled: true, cron_expression: '15 4 * * *', max_results: 30 }])
    const wrapper = await openPanel()
    await wrapper.find('button[title="admin.scheduledTests.editPlan"]').trigger('click')
    await flushPromises()
    const selects = wrapper.findAllComponents({ name: 'Select' })
    const model = selects.find(s => s.props('modelValue') === 'gpt-6-astra')
    expect(model).toBeDefined()
    expect(model!.props('options')).toEqual(expect.arrayContaining([{ value: 'gpt-6-astra', label: 'gpt-6-astra' }, { value: 'gpt-6.1-sol', label: 'gpt-6.1-sol' }]))
    expect(model!.props('creatable')).toBe(true)
  })

})
