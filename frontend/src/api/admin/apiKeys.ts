/**
 * Admin API Keys API endpoints
 * Handles API key management for administrators
 */

import { apiClient } from '../client'
import type { ApiKey, CreateApiKeyRequest, UpdateApiKeyRequest } from '@/types'

export interface UpdateApiKeyGroupResult {
  api_key: ApiKey
  auto_granted_group_access: boolean
  granted_group_id?: number
  granted_group_name?: string
}

export type AdminCreateApiKeyRequest = CreateApiKeyRequest

export type AdminUpdateApiKeyRequest = UpdateApiKeyRequest

/**
 * Create an API key for a target user.
 * @param userId - Target user ID
 * @param payload - API key fields
 * @returns Created API key
 */
export async function createForUser(userId: number, payload: AdminCreateApiKeyRequest): Promise<ApiKey> {
  const { data } = await apiClient.post<ApiKey>(`/admin/users/${userId}/api-keys`, payload)
  return data
}

/**
 * Update admin-managed API key fields.
 * @param id - API Key ID
 * @param payload - Fields to update
 * @returns Updated API key with auto-grant info
 */
export async function update(id: number, payload: AdminUpdateApiKeyRequest): Promise<UpdateApiKeyGroupResult> {
  const { data } = await apiClient.put<UpdateApiKeyGroupResult>(`/admin/api-keys/${id}`, payload)
  return data
}

/**
 * Update an API key's group binding
 * @param id - API Key ID
 * @param groupId - Group ID (0 to unbind, positive to bind, null/undefined to skip)
 * @returns Updated API key with auto-grant info
 */
export async function updateApiKeyGroup(id: number, groupId: number | null): Promise<UpdateApiKeyGroupResult> {
  return update(id, {
    group_id: groupId === null ? 0 : groupId
  })
}

export const apiKeysAPI = {
  createForUser,
  update,
  updateApiKeyGroup
}

export default apiKeysAPI
