import { apiClient } from '../client'

export type AutoGroupProfileStatus = 'disabled' | 'active'

export interface AutoGroupProfile {
  id: number
  name: string
  status: AutoGroupProfileStatus
  revision: number
  max_candidates: number
  allow_custom_order: boolean
  cross_group_retry_allowed: boolean
  group_ids: number[]
}

export interface CreateAutoGroupProfileRequest {
  name: string
  status: AutoGroupProfileStatus
  max_candidates: number
  allow_custom_order: boolean
  cross_group_retry_allowed: boolean
  group_ids: number[]
}

export interface UpdateAutoGroupProfileRequest extends CreateAutoGroupProfileRequest {
  revision: number
}

export async function list(): Promise<AutoGroupProfile[]> {
  const { data } = await apiClient.get<AutoGroupProfile[]>('/admin/auto-group-profiles')
  return data
}

export async function getById(id: number): Promise<AutoGroupProfile> {
  const { data } = await apiClient.get<AutoGroupProfile>(`/admin/auto-group-profiles/${id}`)
  return data
}

export async function create(
  profile: CreateAutoGroupProfileRequest
): Promise<AutoGroupProfile> {
  const { data } = await apiClient.post<AutoGroupProfile>('/admin/auto-group-profiles', profile)
  return data
}

export async function update(
  id: number,
  profile: UpdateAutoGroupProfileRequest
): Promise<AutoGroupProfile> {
  const { data } = await apiClient.put<AutoGroupProfile>(
    `/admin/auto-group-profiles/${id}`,
    profile
  )
  return data
}

export async function deleteProfile(id: number, revision: number): Promise<void> {
  await apiClient.delete(`/admin/auto-group-profiles/${id}`, { params: { revision } })
}

export const autoGroupsAPI = {
  list,
  getById,
  create,
  update,
  delete: deleteProfile
}

export default autoGroupsAPI
