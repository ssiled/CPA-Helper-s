import { apiClient } from '@/shared/api/apiClient'
import type { AuthPool, AuthPoolAccountsResponse, AuthPoolBinding, AuthPoolBindingPayload, AuthPoolPayload, AuthPoolStatus } from '@/shared/types/api'

export function getAuthPoolStatus(): Promise<AuthPoolStatus> {
  return apiClient.get<AuthPoolStatus>('/auth-pools')
}

export function getAuthPoolProxyConfig(): Promise<AuthPoolProxyConfig> {
  return apiClient.get<AuthPoolProxyConfig>('/auth-pools/proxy-config')
}

export function updateAuthPoolProxyConfig(payload: AuthPoolProxyConfigPayload): Promise<AuthPoolProxyConfig> {
  return apiClient.put<AuthPoolProxyConfig>('/auth-pools/proxy-config', payload)
}

export function addAuthPoolAPIKeyAccount(payload: AuthPoolAPIKeyAccountPayload): Promise<AuthPoolAPIKeyAccountResponse> {
  return apiClient.post<AuthPoolAPIKeyAccountResponse>('/auth-pools/accounts/api-key', payload)
}

export function listAuthPoolAccounts(): Promise<AuthPoolAccountsResponse> {
  return apiClient.get<AuthPoolAccountsResponse>('/auth-pools/accounts')
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

export interface AuthPoolAPIKeyAccountPayload {
  provider: 'gemini' | 'grok' | 'xai' | string
  api_key: string
  prefix?: string
  base_url?: string
  proxy_url?: string
  priority?: number | null
  websockets?: boolean | null
}

export interface AuthPoolAPIKeyAccountResponse {
  provider: string
  account_type: string
  count: number
}

export interface AuthPoolProxyConfig {
  cpa_url: string
  api_key_set: boolean
  api_key_preview: string
}

export interface AuthPoolProxyConfigPayload {
  api_key: string
}
