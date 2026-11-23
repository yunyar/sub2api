import { apiClient } from '../client'

export const bloomPlannerGroups = ['cold_start', 'text', 'image', 'mixed'] as const
export const bloomPlannerTiers = ['easy', 'medium', 'hard'] as const

export type BloomPlannerProfile = Record<(typeof bloomPlannerTiers)[number], number>

export interface BloomPlannerUpdate {
  enabled: boolean
  key_id: number
  model: string
}

export interface BloomPlannerSettings extends BloomPlannerUpdate {
  profiles?: Record<(typeof bloomPlannerGroups)[number], BloomPlannerProfile>
}

const endpoint = '/admin/settings/bloom-planner'

export async function getBloomPlannerSettings(): Promise<BloomPlannerSettings> {
  const { data } = await apiClient.get<BloomPlannerSettings>(endpoint)
  return data
}

export async function updateBloomPlannerSettings(
  payload: BloomPlannerUpdate
): Promise<BloomPlannerSettings> {
  const { data } = await apiClient.put<BloomPlannerSettings>(endpoint, payload)
  return data
}
