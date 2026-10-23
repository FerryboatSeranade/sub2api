/**
 * Admin Scheduled Tests API endpoints
 * Handles scheduled test plan management for account connectivity monitoring
 */

import { apiClient } from '../client'
import type {
  ScheduledTestPlan,
  ScheduledTestResult,
  CreateScheduledTestPlanRequest,
  UpdateScheduledTestPlanRequest
} from '@/types'

/**
 * List all scheduled test plans for an account
 * @param accountId - Account ID
 * @returns List of scheduled test plans
 */
export async function listByAccount(accountId: number): Promise<ScheduledTestPlan[]> {
  const { data } = await apiClient.get<ScheduledTestPlan[]>(
    `/admin/accounts/${accountId}/scheduled-test-plans`
  )
  return data ?? []
}

/**
 * Create a new scheduled test plan
 * @param req - Plan creation request
 * @returns Created plan
 */
export async function create(req: CreateScheduledTestPlanRequest): Promise<ScheduledTestPlan> {
  const { data } = await apiClient.post<ScheduledTestPlan>(
    '/admin/scheduled-test-plans',
    req
  )
  return data
}

/**
 * Update an existing scheduled test plan
 * @param id - Plan ID
 * @param req - Fields to update
 * @returns Updated plan
 */
export async function update(id: number, req: UpdateScheduledTestPlanRequest): Promise<ScheduledTestPlan> {
  const { data } = await apiClient.put<ScheduledTestPlan>(
    `/admin/scheduled-test-plans/${id}`,
    req
  )
  return data
}

/**
 * Delete a scheduled test plan
 * @param id - Plan ID
 */
export async function deletePlan(id: number): Promise<void> {
  await apiClient.delete(`/admin/scheduled-test-plans/${id}`)
}

/**
 * List test results for a plan
 * @param planId - Plan ID
 * @param limit - Optional max number of results to return
 * @returns List of test results
 */
export async function listResults(planId: number, limit?: number): Promise<ScheduledTestResult[]> {
  const { data } = await apiClient.get<ScheduledTestResult[]>(
    `/admin/scheduled-test-plans/${planId}/results`,
    {
      params: limit ? { limit } : undefined
    }
  )
  return data ?? []
}

export async function runNow(id: number): Promise<ScheduledTestResult> {
  const { data } = await apiClient.post<ScheduledTestResult>(`/admin/scheduled-test-plans/${id}/run`, {}, { timeout: 210000 })
  return data
}

export interface ReasoningOverviewItem {
  account_id: number
  account_name: string
  plan_id: number
  enabled: boolean
  cron_expression: string
  next_run_at: string | null
  result: ScheduledTestResult | null
}

export async function reasoningOverview(): Promise<ReasoningOverviewItem[]> {
  const { data } = await apiClient.get<ReasoningOverviewItem[]>('/admin/scheduled-test-plans/reasoning-overview')
  return data ?? []
}

export async function setReasoningPolicy(accountId: number, policy: string): Promise<void> {
 await apiClient.put(`/admin/accounts/${accountId}/reasoning-policy`, { policy })
}

export async function getReasoningGlobal(): Promise<boolean> {
 const { data } = await apiClient.get('/admin/scheduled-test-plans/reasoning-policy')
 return data.enabled === true
}
export async function setReasoningGlobal(enabled: boolean): Promise<void> {
 await apiClient.put('/admin/scheduled-test-plans/reasoning-policy', { enabled })
}
export const scheduledTestsAPI = {
 getReasoningGlobal,
 setReasoningGlobal,
 setReasoningPolicy,
  reasoningOverview,
  runNow,
  listByAccount,
  create,
  update,
  delete: deletePlan,
  listResults
}

export default scheduledTestsAPI

export interface ProbeManagementItem {
 account_id: number; account_name: string; account_type: string; deleted: boolean
 vm_state: 'not_linked' | 'linked' | 'derived'; target_id: number
 control: { excluded: boolean; note: string; pause_code: string; pause_until: string | null; failures: number }
 review?: { result_id: number; verdict: string; note: string; at: string }
 plans: ScheduledTestPlan[]; result: ScheduledTestResult | null
 failure?: { code: string; label: string; advice: string; permanent: boolean; delay_seconds: number }
}
export interface ProbeSettings { concurrency: number; exclude_vm: boolean }
export const probeManagementAPI = {
 list: async (kind: string) => (await apiClient.get<ProbeManagementItem[]>('/admin/scheduled-test-plans/management', { params: { kind } })).data,
 settings: async () => (await apiClient.get<ProbeSettings>('/admin/scheduled-test-plans/probe-settings')).data,
 saveSettings: async (value: ProbeSettings) => (await apiClient.put<ProbeSettings>('/admin/scheduled-test-plans/probe-settings', value)).data,
 action: async (value: { ids: number[]; action: string; kind: string; model?: string; cron?: string; note?: string; verdict?: string; result_id?: number }) => (await apiClient.post<{id:number;error?:string}[]>('/admin/scheduled-test-plans/batch-action',value)).data,
 cleanup: async () => (await apiClient.post<{disabled:number}>('/admin/scheduled-test-plans/cleanup-deleted')).data
}
