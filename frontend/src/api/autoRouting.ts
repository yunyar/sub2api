import { apiClient } from './client'

export type KeyRoutingMode = 'fixed' | 'auto'
export type AutoSelectionMode = 'inherit' | 'custom'

export interface AvailableAutoGroupProfile {
  id: number
  name: string
  status: 'active' | 'disabled'
  revision: number
  max_candidates: number
  allow_custom_order: boolean
  cross_group_retry_allowed: boolean
  group_ids: number[]
}

export interface KeyAutoRoutingConfig {
  routing_mode: KeyRoutingMode
  auto_profile_id: number | null
  auto_selection_mode: AutoSelectionMode
  group_ids: number[]
  cross_group_retry: boolean
  routing_revision: number
  group_id?: number | null
}

export interface UpdateKeyAutoRoutingRequest extends KeyAutoRoutingConfig {
  group_id: number | null
}

export async function getAvailableProfiles(): Promise<AvailableAutoGroupProfile[]> {
  const { data } = await apiClient.get<AvailableAutoGroupProfile[]>(
    '/auto-group-profiles/available'
  )
  return data ?? []
}

export async function getKeyAutoRouting(keyId: number): Promise<KeyAutoRoutingConfig> {
  const { data } = await apiClient.get<KeyAutoRoutingConfig>(
    `/keys/${keyId}/auto-routing`
  )
  return data
}

export async function updateKeyAutoRouting(
  keyId: number,
  config: UpdateKeyAutoRoutingRequest
): Promise<KeyAutoRoutingConfig> {
  const { data } = await apiClient.put<KeyAutoRoutingConfig>(
    `/keys/${keyId}/auto-routing`,
    config
  )
  return data
}

export const autoRoutingAPI = {
  getAvailableProfiles,
  getKeyAutoRouting,
  updateKeyAutoRouting
}

export default autoRoutingAPI
