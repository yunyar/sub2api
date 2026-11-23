import { apiClient } from './client'

export interface BloomTask {
  id: string | number
  template_id: string | number
  tier: 'easy' | 'medium' | 'hard' | 'weekly'
  metric: 'checkin' | 'productive_requests' | 'output_tokens' | 'images' | 'active_days'
  target: number
  progress: number
  reward_micros: number
  claimed: boolean
}

export interface BloomTasks {
  day: string
  week_start: string
  resets_at: string
  week_resets_at: string
  timezone: string
  budget_micros: number
  claimed_micros: number
  collected_flowers: number
  tasks: BloomTask[]
}

export async function getBloomTasks(signal: AbortSignal): Promise<BloomTasks> {
  const { data } = await apiClient.get<BloomTasks>('/bloom/tasks', { signal })
  return data
}

export async function claimBloomTask(id: BloomTask['id'], signal: AbortSignal): Promise<BloomTasks> {
  const { data } = await apiClient.post<BloomTasks>(
    `/bloom/tasks/${encodeURIComponent(id)}/claim`, undefined, { signal }
  )
  return data
}
