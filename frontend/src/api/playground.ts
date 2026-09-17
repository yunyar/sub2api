import { apiClient } from './client'
import { buildApiUrl } from './url'
import { refreshAuthTokens } from './tokenRefresh'

export interface PlaygroundModel {
  id: string
  owned_by?: string
}

export interface PlaygroundMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
}

export interface PlaygroundImage {
  url: string
  revisedPrompt?: string
}

interface StreamChatOptions {
  groupId: number
  model: string
  messages: PlaygroundMessage[]
  temperature?: number
  signal?: AbortSignal
  onDelta: (content: string) => void
}

const groupHeaders = (groupId: number) => ({
  'X-Playground-Group-ID': String(groupId)
})

function extractErrorMessage(payload: unknown, fallback: string): string {
  if (!payload || typeof payload !== 'object') return fallback
  const value = payload as Record<string, any>
  return value.error?.message || value.message || fallback
}

export function createPlaygroundSSEParser(onDelta: (content: string) => void) {
  let buffer = ''

  return (chunk: string, flush = false) => {
    buffer += chunk.replace(/\r\n/g, '\n')
    const events = buffer.split('\n\n')
    buffer = flush ? '' : (events.pop() ?? '')

    for (const event of events) {
      const data = event
        .split('\n')
        .filter(line => line.startsWith('data:'))
        .map(line => line.slice(5).trimStart())
        .join('\n')
      if (!data || data === '[DONE]') continue

      try {
        const parsed = JSON.parse(data)
        const content = parsed.choices?.[0]?.delta?.content
        if (typeof content === 'string') onDelta(content)
      } catch {
        // Ignore non-JSON keepalive events.
      }
    }
  }
}

async function authorizedStreamFetch(
  url: string,
  init: RequestInit,
  retry = true
): Promise<Response> {
  const failedToken = localStorage.getItem('auth_token')
  const response = await fetch(url, {
    ...init,
    headers: {
      ...init.headers,
      Authorization: `Bearer ${failedToken || ''}`,
      'Content-Type': 'application/json'
    }
  })

  if (response.status === 401 && retry && localStorage.getItem('refresh_token')) {
    await refreshAuthTokens({ failedAccessToken: failedToken })
    return authorizedStreamFetch(url, init, false)
  }
  return response
}

export async function listPlaygroundModels(groupId: number): Promise<PlaygroundModel[]> {
  const { data } = await apiClient.get('/playground/models', {
    headers: groupHeaders(groupId)
  })
  const items = Array.isArray(data) ? data : data?.data
  if (!Array.isArray(items)) return []
  return items
    .map((item: any) => typeof item === 'string' ? { id: item } : item)
    .filter((item: any) => typeof item?.id === 'string' && item.id.trim())
}

export async function streamPlaygroundChat(options: StreamChatOptions): Promise<void> {
  const response = await authorizedStreamFetch(buildApiUrl('/playground/chat/completions'), {
    method: 'POST',
    signal: options.signal,
    headers: groupHeaders(options.groupId),
    body: JSON.stringify({
      model: options.model,
      messages: options.messages,
      temperature: options.temperature ?? 0.7,
      stream: true
    })
  })

  if (!response.ok) {
    let payload: unknown
    try { payload = await response.json() } catch { payload = null }
    throw new Error(extractErrorMessage(payload, `Request failed (${response.status})`))
  }
  if (!response.body) throw new Error('Streaming is not supported by this browser')

  const parse = createPlaygroundSSEParser(options.onDelta)
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    parse(decoder.decode(value, { stream: true }))
  }
  parse(decoder.decode(), true)
}

export async function generatePlaygroundImages(input: {
  groupId: number
  model: string
  prompt: string
  size: string
  quality: string
  count: number
}): Promise<PlaygroundImage[]> {
  const { data } = await apiClient.post('/playground/images/generations', {
    model: input.model,
    prompt: input.prompt,
    size: input.size,
    quality: input.quality,
    n: input.count,
    response_format: 'b64_json'
  }, { headers: groupHeaders(input.groupId), timeout: 180_000 })

  const items = Array.isArray(data?.data) ? data.data : []
  return items.map((item: any) => ({
    url: item.b64_json ? `data:image/png;base64,${item.b64_json}` : item.url,
    revisedPrompt: item.revised_prompt
  })).filter((item: PlaygroundImage) => Boolean(item.url))
}

export const playgroundAPI = {
  listModels: listPlaygroundModels,
  streamChat: streamPlaygroundChat,
  generateImages: generatePlaygroundImages
}
