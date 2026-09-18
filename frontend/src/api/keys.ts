/**
 * API Keys management endpoints
 * Handles CRUD operations for user API keys
 */

import { apiClient } from './client'
import type { ApiKey, BindableUserSubscription, CreateApiKeyRequest, UpdateApiKeyRequest, PaginatedResponse } from '@/types'
import type { OrganizationSubscription } from '@/types/organization'

/**
 * List all API keys for current user
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 10)
 * @param filters - Optional filter parameters
 * @param options - Optional request options
 * @returns Paginated list of API keys
 */
export async function list(
  page: number = 1,
  pageSize: number = 10,
  filters?: {
    search?: string
    status?: string
    group_id?: number | string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<ApiKey>> {
  const { data } = await apiClient.get<PaginatedResponse<ApiKey>>('/keys', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

/**
 * Get API key by ID
 * @param id - API key ID
 * @returns API key details
 */
export async function getById(id: number): Promise<ApiKey> {
  const { data } = await apiClient.get<ApiKey>(`/keys/${id}`)
  return data
}

/**
 * Create new API key
 * @param name - Key name
 * @param groupId - Optional group ID
 * @param customKey - Optional custom key value
 * @param ipWhitelist - Optional IP whitelist
 * @param ipBlacklist - Optional IP blacklist
 * @param quota - Optional quota limit in USD (0 = unlimited)
 * @param expiresInDays - Optional days until expiry (undefined = never expires)
 * @param rateLimitData - Optional rate limit fields
 * @param organizationSubscriptionId - Optional company subscription to bind (enterprise API key)
 * @param fallbackGroupIds - Ordered fallback group IDs
 * @param preferCompanyBalance - Prefer company balance for enterprise keys
 * @param options.userSubscriptionId - Optional personal subscription (plan) to bind. When set,
 *   the key's routable groups come from that subscription's covered groups and all consumption
 *   draws from its single shared quota pool, so `groupId` and fallback groups are ignored.
 * @returns Created API key
 */
export async function create(
  name: string,
  groupId?: number | null,
  customKey?: string,
  ipWhitelist?: string[],
  ipBlacklist?: string[],
  quota?: number,
  expiresInDays?: number,
  rateLimitData?: { rate_limit_5h?: number; rate_limit_1d?: number; rate_limit_7d?: number },
  organizationSubscriptionId?: number | null,
  fallbackGroupIds?: number[],
  preferCompanyBalance = false,
  options?: { userSubscriptionId?: number | null }
): Promise<ApiKey> {
  const payload: CreateApiKeyRequest = { name }
  const userSubscriptionId = options?.userSubscriptionId
  // 优先级与后端及编辑表单保持一致：企业订阅 > 个人订阅（套餐）> 单个分组。
  if (organizationSubscriptionId !== undefined && organizationSubscriptionId !== null) {
    payload.organization_subscription_id = organizationSubscriptionId
    payload.fallback_group_ids = fallbackGroupIds ?? []
  } else if (userSubscriptionId !== undefined && userSubscriptionId !== null) {
    // 绑定订阅时不再发送 group_id / fallback：候选分组完全由订阅的覆盖集合决定，
    // 同时发送会产生两套来源。
    payload.user_subscription_id = userSubscriptionId
  } else if (groupId !== undefined) {
    payload.group_id = groupId
    payload.fallback_group_ids = fallbackGroupIds ?? []
  }
  if (preferCompanyBalance) {
    payload.prefer_company_balance = true
  }
  if (customKey) {
    payload.custom_key = customKey
  }
  if (ipWhitelist && ipWhitelist.length > 0) {
    payload.ip_whitelist = ipWhitelist
  }
  if (ipBlacklist && ipBlacklist.length > 0) {
    payload.ip_blacklist = ipBlacklist
  }
  if (quota !== undefined && quota > 0) {
    payload.quota = quota
  }
  if (expiresInDays !== undefined && expiresInDays > 0) {
    payload.expires_in_days = expiresInDays
  }
  if (rateLimitData?.rate_limit_5h && rateLimitData.rate_limit_5h > 0) {
    payload.rate_limit_5h = rateLimitData.rate_limit_5h
  }
  if (rateLimitData?.rate_limit_1d && rateLimitData.rate_limit_1d > 0) {
    payload.rate_limit_1d = rateLimitData.rate_limit_1d
  }
  if (rateLimitData?.rate_limit_7d && rateLimitData.rate_limit_7d > 0) {
    payload.rate_limit_7d = rateLimitData.rate_limit_7d
  }

  const { data } = await apiClient.post<ApiKey>('/keys', payload)
  return data
}

/**
 * Update API key
 * @param id - API key ID
 * @param updates - Fields to update
 * @returns Updated API key
 */
export async function update(id: number, updates: UpdateApiKeyRequest): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/keys/${id}`, updates)
  return data
}

/**
 * Delete API key
 * @param id - API key ID
 * @returns Success confirmation
 */
export async function deleteKey(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/keys/${id}`)
  return data
}

/**
 * Toggle API key status (active/inactive)
 * @param id - API key ID
 * @param status - New status
 * @returns Updated API key
 */
export async function toggleStatus(id: number, status: 'active' | 'inactive'): Promise<ApiKey> {
  return update(id, { status })
}

/**
 * List company subscriptions the current user can bind to a new enterprise API key.
 * @returns Bindable organization subscriptions
 */
export async function listOrganizationSubscriptions(): Promise<OrganizationSubscription[]> {
  const { data } = await apiClient.get<{ subscriptions: OrganizationSubscription[] }>(
    '/keys/organization-subscriptions'
  )
  return data.subscriptions ?? []
}

/**
 * List the current user's personal subscriptions (plans) that can be bound to an API key.
 *
 * A subscription is the quota pool: `group_ids` are the groups sharing it, and binding a key
 * to it removes the ambiguity of which pool to charge when two plans cover the same group.
 * @returns Bindable personal subscriptions
 */
export async function listUserSubscriptions(): Promise<BindableUserSubscription[]> {
  const { data } = await apiClient.get<{ subscriptions: BindableUserSubscription[] }>(
    '/keys/user-subscriptions'
  )
  return data.subscriptions ?? []
}

export const keysAPI = {
  list,
  getById,
  create,
  update,
  delete: deleteKey,
  toggleStatus,
  listOrganizationSubscriptions,
  listUserSubscriptions
}

export default keysAPI
