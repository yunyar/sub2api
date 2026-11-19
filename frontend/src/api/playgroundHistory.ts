import { apiClient } from './client'

export interface ConversationMessage {
  id: number
  role: 'user' | 'assistant'
  content: string
  model?: string
  kind?: 'chat' | 'image'
  stepId?: string
}

export interface ConversationRouting {
  mode: 'fixed' | 'auto'
  autoProfileId?: number
}

export interface WorkflowStep { id: string; prompt: string }
export interface WorkflowState { steps: WorkflowStep[]; currentStep: number }
export interface PlaygroundStorageQuota { limitMB: number; defaultMB: number }

export interface Conversation {
  id: string
  title: string
  groupId: number
  routing?: ConversationRouting
  model: string
  imageModel?: string
  kind?: 'chat' | 'workflow'
  workflow?: WorkflowState
  systemPrompt: string
  temperature: number
  messages: ConversationMessage[]
  revision: number
  expiresAt: number
}

export const playgroundHistory = {
  async quota() {
    return (await apiClient.get<PlaygroundStorageQuota>('/playground/conversations/quota')).data
  },
  async adminQuota(userID: number) {
    return (await apiClient.get<PlaygroundStorageQuota>(`/admin/users/${userID}/playground-storage`)).data
  },
  async setAdminQuota(userID: number, limitMB: number) {
    return (await apiClient.put<PlaygroundStorageQuota>(`/admin/users/${userID}/playground-storage`, { limitMB })).data
  },
  async list() {
    return (await apiClient.get<Conversation[]>('/playground/conversations')).data
  },
  async save(conversation: Conversation) {
    return (await apiClient.put<Conversation>(`/playground/conversations/${conversation.id}`, conversation)).data
  },
  async delete(id: string) {
    await apiClient.delete(`/playground/conversations/${id}`)
  }
}
