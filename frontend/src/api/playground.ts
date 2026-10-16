import { apiClient } from './client'
import { buildApiUrl } from './url'
import { refreshAuthTokens } from './tokenRefresh'
import { playgroundImageDataURL } from '@/utils/playgroundImageFormat'

export type PlaygroundRequestRoute =
  | { mode: 'fixed'; groupId: number }
  | { mode: 'auto'; autoProfileId: number }

export interface PlaygroundModel {
  id: string
  model_id?: string
  object?: string
  created?: number
  platform?: string
  owned_by?: string
  type?: string
  display_name?: string
  source_platforms?: string[]
  upstream_models?: string[]
  supported_endpoints?: PlaygroundEndpoint[]
  account_schedulable?: boolean
}

export type PlaygroundEndpoint =
  | 'chat'
  | 'responses'
  | 'messages'
  | 'images_generation'
  | 'images_edit'

export interface PlaygroundAutoRoutingConfig {
  profile_id: number
  routing_mode: 'auto'
  auto_profile_id: number
  auto_selection_mode: 'inherit' | 'custom'
  group_ids: number[]
  cross_group_retry: false
  routing_revision: number
}

export interface UpdatePlaygroundAutoRoutingRequest {
  auto_selection_mode: 'inherit' | 'custom'
  group_ids: number[]
  routing_revision: number
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
  route?: PlaygroundRequestRoute
  groupId?: number
  model: string
  prompt: string
  size: string
  quality: string
  count: number
  inputImages?: string[]
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
  route?: PlaygroundRequestRoute
  groupId?: number
  model: string
  messages: Array<PlaygroundMessage | PlaygroundMultimodalMessage>
  temperature?: number
  signal?: AbortSignal
  onDelta: (content: string) => void
}

function routeHeaders(route: PlaygroundRequestRoute | number): Record<string, string> {
  if (typeof route === 'number') {
    if (!Number.isInteger(route) || route <= 0) throw new RangeError('A positive group ID is required')
    return { 'X-Playground-Group-ID': String(route) }
  }
  if (route.mode === 'auto') {
    if (!Number.isInteger(route.autoProfileId) || route.autoProfileId <= 0) throw new RangeError('A positive Auto profile ID is required')
    return { 'X-Playground-Auto-Profile-ID': String(route.autoProfileId) }
  }
  if (!Number.isInteger(route.groupId) || route.groupId <= 0) throw new RangeError('A positive group ID is required')
  return { 'X-Playground-Group-ID': String(route.groupId) }
}

function requestRoute(options: { route?: PlaygroundRequestRoute; groupId?: number }): Record<string, string> {
  const route = options.route ?? options.groupId
  if (!route) throw new RangeError('A Playground route is required')
  return routeHeaders(route)
}

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

export function playgroundModelSupportsEndpoint(
  model: PlaygroundModel,
  endpoint: PlaygroundEndpoint,
  fallback: boolean
): boolean {
  if (model.account_schedulable === false) return false
  return Array.isArray(model.supported_endpoints)
    ? model.supported_endpoints.includes(endpoint)
    : fallback
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

export async function listPlaygroundModels(route: PlaygroundRequestRoute | number): Promise<PlaygroundModel[]> {
  const { data } = await apiClient.get('/playground/models', {
    headers: routeHeaders(route)
  })
  const payload = Array.isArray(data)
    ? data
    : Array.isArray(data?.data)
      ? data.data
      : Array.isArray(data?.models)
        ? data.models
        : data && typeof data === 'object' && !('data' in data) && !('models' in data)
          ? data
          : []
  const items = Array.isArray(payload)
    ? payload
    : payload && typeof payload === 'object'
      ? Object.entries(payload).map(([id, item]) => item && typeof item === 'object'
        ? { ...item, id: typeof (item as any).id === 'string' ? (item as any).id : id }
        : { id })
      : []
  return items
    .map((item: any) => {
      if (typeof item === 'string') return { id: item }
      const id = typeof item?.id === 'string' ? item.id : item?.model_id
      return typeof id === 'string' ? { ...item, id } : null
    })
    .filter((item: PlaygroundModel | null): item is PlaygroundModel => Boolean(item?.id?.trim()))
}

export async function getPlaygroundAutoRouting(profileId: number): Promise<PlaygroundAutoRoutingConfig> {
  const { data } = await apiClient.get<PlaygroundAutoRoutingConfig>(`/playground/auto-routing/${profileId}`)
  return data
}

export async function updatePlaygroundAutoRouting(
  profileId: number,
  config: UpdatePlaygroundAutoRoutingRequest
): Promise<PlaygroundAutoRoutingConfig> {
  const { data } = await apiClient.put<PlaygroundAutoRoutingConfig>(
    `/playground/auto-routing/${profileId}`,
    config
  )
  return data
}

export async function streamPlaygroundChat(options: StreamChatOptions): Promise<void> {
  const response = await authorizedStreamFetch(buildApiUrl('/playground/chat/completions'), {
    method: 'POST',
    signal: options.signal,
    headers: requestRoute(options),
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
  const delivered = new Set<string>()
  let imageCallbackQueue = Promise.resolve()
  const endpoint = input.inputImages?.length ? '/playground/images/edits' : '/playground/images/generations'
  const requests = Array.from({ length: count }, async (_, index) => {
    const { data } = await apiClient.post(endpoint, {
      model: input.model,
      prompt: `${input.prompt}\n\nGenerate exactly one final image asset for request ${index + 1} of ${count}. This is one image in a requested batch; follow the original creative composition instructions, including a collage or panels when requested.`,
      size: input.size,
      quality: input.quality,
      n: 1,
      response_format: 'b64_json',
      ...(input.inputImages?.length
        ? { images: input.inputImages.map(image_url => ({ image_url })) }
        : {})
    }, { headers: requestRoute(input), timeout: 180_000, signal: input.signal })

    const image = (Array.isArray(data?.data) ? data.data : [])
      .map((item: any) => ({
        url: typeof item.b64_json === 'string' && item.b64_json.trim()
          ? playgroundImageDataURL(item.b64_json, item.mime_type, item.output_format, data.mime_type, data.output_format)
          : item.url,
        revisedPrompt: item.revised_prompt
      }))
      .find((item: PlaygroundImage) => Boolean(item.url))
    if (!image) throw new Error('Image generation returned no image')

    if (delivered.has(image.url)) return
    delivered.add(image.url)
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
  getAutoRouting: getPlaygroundAutoRouting,
  updateAutoRouting: updatePlaygroundAutoRouting,
  streamChat: streamPlaygroundChat,
  generateImages: generatePlaygroundImages
}
