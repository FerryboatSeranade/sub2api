import { apiClient } from '../client'
export interface VMBridgeJob {
  status: string
  stage: string
  message: string
  vm_id?: string
  target_id?: number
  updated_at?: string
}
export async function getVMBridge(id: number) {
  return (await apiClient.get<{ enabled: boolean; job?: VMBridgeJob }>(`/admin/accounts/${id}/vm2api`)).data
}
export async function syncVMBridge(id: number, inheritGroups: boolean) {
  return (await apiClient.post<VMBridgeJob>(`/admin/accounts/${id}/vm2api`, { confirm: true, inherit_groups: inheritGroups })).data
}
