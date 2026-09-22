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

export type PlaygroundContentPart =
  | { type: 'text'; text: string }
  | { type: 'image_url'; image_url: { url: string } }

export interface PlaygroundMultimodalMessage {
  role: 'system' | 'user' | 'assistant'
  content: PlaygroundContentPart[]
}

export interface PlaygroundImage {
  url: string
  revisedPrompt?: string
}

export const playgroundImageGenerationCount = {
  min: 1,
  max: 10
} as const

export interface PlaygroundImageGenerationRequest {
  groupId: number
  model: string
  prompt: string
  size: string
  quality: string
  count: number
  signal?: AbortSignal
  onImage?: (image: PlaygroundImage) => void | Promise<void>
}

export class PlaygroundImageGenerationError extends Error {
  constructor(
    message: string,
    readonly images: PlaygroundImage[],
    readonly status?: number,
    readonly cause?: unknown,
  ) {
    super(message)
    this.name = 'PlaygroundImageGenerationError'
  }
}

interface StreamChatOptions {
  groupId: number
  model: string
  messages: Array<PlaygroundMessage | PlaygroundMultimodalMessage>
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

      let parsed: Record<string, any>
      try {
        parsed = JSON.parse(data)
      } catch {
        continue
      }
      if (parsed.type === 'error' || parsed.error) {
        throw new Error(extractErrorMessage(parsed, 'Streaming request failed'))
      }
      const content = parsed.choices?.[0]?.delta?.content
      if (typeof content === 'string') onDelta(content)
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
  let completed = false
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) {
        completed = true
        break
      }
      parse(decoder.decode(value, { stream: true }))
    }
    parse(decoder.decode(), true)
  } finally {
    if (!completed) await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

export async function generatePlaygroundImages(input: PlaygroundImageGenerationRequest): Promise<PlaygroundImage[]> {
  const count = input.count ?? 1
  if (!Number.isInteger(count) || count < playgroundImageGenerationCount.min || count > playgroundImageGenerationCount.max) {
    throw new RangeError(`Image count must be between ${playgroundImageGenerationCount.min} and ${playgroundImageGenerationCount.max}`)
  }
  if (input.signal?.aborted) throw new DOMException('The operation was aborted', 'AbortError')

  const images = new Array<PlaygroundImage | undefined>(count)
  let imageCallbackQueue = Promise.resolve()
  const requests = Array.from({ length: count }, async (_, index) => {
    const { data } = await apiClient.post('/playground/images/generations', {
      model: input.model,
      prompt: `${input.prompt}\n\nGenerate exactly one final image asset for request ${index + 1} of ${count}. This is one image in a requested batch; follow the original creative composition instructions, including a collage or panels when requested.`,
      size: input.size,
      quality: input.quality,
      n: 1,
      response_format: 'b64_json'
    }, { headers: groupHeaders(input.groupId), timeout: 180_000, signal: input.signal })

    const image = (Array.isArray(data?.data) ? data.data : [])
      .map((item: any) => ({
        url: item.b64_json ? `data:image/png;base64,${item.b64_json}` : item.url,
        revisedPrompt: item.revised_prompt
      }))
      .find((item: PlaygroundImage) => Boolean(item.url))
    if (!image) throw new Error('Image generation returned no image')

    images[index] = image
    imageCallbackQueue = imageCallbackQueue.then(() => input.onImage?.(image))
    await imageCallbackQueue
  })

  const settled = await Promise.allSettled(requests)
  const aborted = settled.find(result => result.status === 'rejected' && (result.reason as Error)?.name === 'AbortError')
  if (input.signal?.aborted || aborted) {
    throw aborted && aborted.status === 'rejected'
      ? aborted.reason
      : new DOMException('The operation was aborted', 'AbortError')
  }

  const failed = settled.find(result => result.status === 'rejected')
  const completedImages = images.filter((image): image is PlaygroundImage => Boolean(image))
  if (failed && failed.status === 'rejected') {
    const response = failed.reason && typeof failed.reason === 'object'
      ? failed.reason as { message?: unknown; status?: unknown; response?: { status?: unknown } }
      : null
    const message = typeof response?.message === 'string' ? response.message : 'Image generation failed'
    const status = typeof response?.status === 'number'
      ? response.status
      : typeof response?.response?.status === 'number' ? response.response.status : undefined
    throw new PlaygroundImageGenerationError(message, completedImages, status, failed.reason)
  }

  return completedImages
}

export const playgroundAPI = {
  listModels: listPlaygroundModels,
  streamChat: streamPlaygroundChat,
  generateImages: generatePlaygroundImages
}
