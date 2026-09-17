import { apiClient } from './client'
import type { CommunityQRCode } from '@/types'

export async function list(): Promise<CommunityQRCode[]> {
  const { data } = await apiClient.get<CommunityQRCode[]>('/community-qrcodes')
  return data
}

export const communityQRCodesAPI = { list }
