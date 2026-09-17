import { apiClient } from '../client'
import type { CommunityQRCode } from '@/types'

export async function list(): Promise<CommunityQRCode[]> {
  const { data } = await apiClient.get<CommunityQRCode[]>('/admin/community-qrcodes')
  return data
}

export async function update(items: CommunityQRCode[]): Promise<CommunityQRCode[]> {
  const { data } = await apiClient.put<CommunityQRCode[]>('/admin/community-qrcodes', { items })
  return data
}

export const communityQRCodesAdminAPI = { list, update }
export default communityQRCodesAdminAPI
