import { apiClient } from '@/shared/api/apiClient'
import type { AuthPool, AuthPoolBinding, AuthPoolBindingPayload, AuthPoolPayload, AuthPoolStatus } from '@/shared/types/api'

export function getAuthPoolStatus(): Promise<AuthPoolStatus> {
  return apiClient.get<AuthPoolStatus>('/auth-pools')
}

export function saveAuthPool(payload: AuthPoolPayload): Promise<AuthPool> {
  return apiClient.post<AuthPool>('/auth-pools', payload)
}

export function deleteAuthPool(id: string): Promise<void> {
  return apiClient.delete(`/auth-pools/${encodeURIComponent(id)}`)
}

export function bindApiKeyToAuthPool(payload: AuthPoolBindingPayload): Promise<AuthPoolBinding> {
  return apiClient.post<AuthPoolBinding>('/auth-pools/bindings', payload)
}

export function unbindApiKeyFromAuthPool(apiKeyHash: string): Promise<void> {
  return apiClient.delete(`/auth-pools/bindings/${encodeURIComponent(apiKeyHash)}`)
}
