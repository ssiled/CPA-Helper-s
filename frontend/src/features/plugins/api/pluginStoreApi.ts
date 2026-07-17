import { apiClient } from '@/shared/api/apiClient'
import type { PluginStoreInstallPayload, PluginStoreInstallResponse, PluginStoreResponse } from '@/shared/types/api'

export function getPluginStore(): Promise<PluginStoreResponse> {
  return apiClient.get<PluginStoreResponse>('/plugin-store')
}

export function installPluginFromStore(id: string, payload: PluginStoreInstallPayload = {}): Promise<PluginStoreInstallResponse> {
  return apiClient.post<PluginStoreInstallResponse>(`/plugin-store/${encodeURIComponent(id)}/install`, payload)
}
