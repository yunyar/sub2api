import { apiClient } from './client'

export interface ConversationMessage {
  id: number
  role: 'user' | 'assistant'
  content: string
  model?: string
  kind?: 'chat' | 'image'
  stepId?: string
}

export interface WorkflowStep { id: string; prompt: string }
export interface WorkflowState { steps: WorkflowStep[]; currentStep: number }

export interface Conversation {
  id: string
  title: string
  groupId: number
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
