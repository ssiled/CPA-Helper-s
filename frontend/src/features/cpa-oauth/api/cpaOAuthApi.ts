import { apiClient } from '@/shared/api/apiClient'
import type {
  CPAOAuthAuthURLPayload,
  CPAOAuthAuthURLResponse,
  CPAOAuthCallbackPayload,
  CPAOAuthProvidersResponse,
  CPAOAuthStatusResponse,
} from '@/shared/types/api'

export function listCPAOAuthProviders(): Promise<CPAOAuthProvidersResponse> {
  return apiClient.get<CPAOAuthProvidersResponse>('/cpa-oauth/providers')
}

export function createCPAOAuthURL(payload: CPAOAuthAuthURLPayload): Promise<CPAOAuthAuthURLResponse> {
  return apiClient.post<CPAOAuthAuthURLResponse>('/cpa-oauth/auth-url', payload)
}

export function getCPAOAuthStatus(state: string): Promise<CPAOAuthStatusResponse> {
  return apiClient.get<CPAOAuthStatusResponse>('/cpa-oauth/status', { state })
}

export function submitCPAOAuthCallback(payload: CPAOAuthCallbackPayload): Promise<CPAOAuthStatusResponse> {
  return apiClient.post<CPAOAuthStatusResponse>('/cpa-oauth/callback', payload)
}
