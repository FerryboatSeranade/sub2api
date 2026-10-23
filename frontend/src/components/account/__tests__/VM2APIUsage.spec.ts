import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VM2APIUsage from '../VM2APIUsage.vue'
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: api }))
const snapshot = { vm_id: 'slot-1', plan: 'plus', synced_at: '2026-10-23T01:00:00Z', stale: false, five_hour: { utilization: 70 }, seven_day: { utilization: 11 }, reset_credits: { available_count: 2 } }
const options = { props: { accountId: 42 }, global: { stubs: { UsageProgressBar: { props: ['label','utilization'], template: '<div>{{ label }} {{ utilization }}%</div>' } } } }
beforeEach(() => { vi.clearAllMocks(); api.get.mockResolvedValue({ data: snapshot }); api.post.mockResolvedValue({ data: { ...snapshot, five_hour: { utilization: 80 } } }) })
describe('VM2APIUsage', () => {
  it('shows authoritative percentages and vouchers, and queries without redeeming', async () => {
    const wrapper = mount(VM2APIUsage, options); await flushPromises()
    expect(wrapper.text()).toContain('5h 70%'); expect(wrapper.text()).toContain('7d 11%'); expect(wrapper.text()).toContain('重置券：2 张')
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(api.post).toHaveBeenCalledWith('/admin/accounts/42/vm2api/usage/refresh', {}, { timeout: 65000 })
    expect(wrapper.text()).toContain('5h 80%')
  })
  it('keeps last values on refresh failure and does not retry', async () => {
    const wrapper = mount(VM2APIUsage, options); await flushPromises()
    api.post.mockRejectedValue(new Error('network'))
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('5h 70%'); expect(wrapper.text()).toContain('已有数据保留')
    expect(api.post).toHaveBeenCalledTimes(1)
  })
  it('does not report unknown quota or vouchers as zero', async () => {
    api.get.mockResolvedValue({ data: { ...snapshot, five_hour: { utilization: null }, seven_day: { utilization: null }, reset_credits: null } })
    const wrapper = mount(VM2APIUsage, options); await flushPromises()
    expect(wrapper.text()).toContain('5h：暂无额度数据'); expect(wrapper.text()).toContain('重置券：未知')
  })
})
