import { afterEach, describe, expect, it, vi } from 'vitest'

const get = vi.hoisted(() => vi.fn())
const post = vi.hoisted(() => vi.fn())
const put = vi.hoisted(() => vi.fn())

vi.mock('../client', () => ({
  apiClient: { get, post, put },
}))

import {
  createPlaygroundSSEParser,
  generatePlaygroundImages,
  getPlaygroundAutoRouting,
  listPlaygroundModels,
  playgroundModelSupportsEndpoint,
  streamPlaygroundChat,
  updatePlaygroundAutoRouting
} from '../playground'

afterEach(() => {
  get.mockReset()
  post.mockReset()
  put.mockReset()
  vi.unstubAllGlobals()
})

describe('Playground routing and model catalog', () => {
  it('normalizes object and list model payloads while retaining capability fields', async () => {
    get.mockResolvedValueOnce({
      data: {
        object: 'list',
        data: [{
          id: 'image-v2',
          object: 'model',
          owned_by: 'openai',
          source_platforms: ['openai'],
          supported_endpoints: ['images_generation', 'images_edit']
        }]
      }
    })

    await expect(listPlaygroundModels({ mode: 'auto', autoProfileId: 12 })).resolves.toEqual([{
      id: 'image-v2',
      object: 'model',
      owned_by: 'openai',
      source_platforms: ['openai'],
      supported_endpoints: ['images_generation', 'images_edit']
    }])
    expect(get).toHaveBeenCalledWith('/playground/models', {
      headers: { 'X-Playground-Auto-Profile-ID': '12' }
    })

    get.mockResolvedValueOnce({ data: { 'chat-v2': { object: 'model', owned_by: 'anthropic', supported_endpoints: ['chat'] } } })
    await expect(listPlaygroundModels(7)).resolves.toEqual([
      { id: 'chat-v2', object: 'model', owned_by: 'anthropic', supported_endpoints: ['chat'] }
    ])
    expect(get).toHaveBeenLastCalledWith('/playground/models', {
      headers: { 'X-Playground-Group-ID': '7' }
    })
  })

  it('uses explicit endpoint and schedulability capabilities when provided', () => {
    expect(playgroundModelSupportsEndpoint({
      id: 'image-v2',
      supported_endpoints: ['images_generation'],
      account_schedulable: true
    }, 'images_generation', false)).toBe(true)
    expect(playgroundModelSupportsEndpoint({
      id: 'image-v2',
      supported_endpoints: ['images_generation'],
      account_schedulable: true
    }, 'chat', true)).toBe(false)
    expect(playgroundModelSupportsEndpoint({
      id: 'chat-v2',
      supported_endpoints: ['chat'],
      account_schedulable: false
    }, 'chat', true)).toBe(false)
    expect(playgroundModelSupportsEndpoint({ id: 'legacy-image' }, 'images_generation', true)).toBe(true)
  })

  it('loads and saves profile-scoped Playground routing with revision control', async () => {
    const config = {
      profile_id: 12,
      routing_mode: 'auto' as const,
      auto_profile_id: 12,
      auto_selection_mode: 'custom' as const,
      group_ids: [8, 4],
      cross_group_retry: false as const,
      routing_revision: 3
    }
    get.mockResolvedValue({ data: config })
    put.mockResolvedValue({ data: { ...config, routing_revision: 4 } })

    await expect(getPlaygroundAutoRouting(12)).resolves.toEqual(config)
    await expect(updatePlaygroundAutoRouting(12, {
      auto_selection_mode: 'custom',
      group_ids: [8, 4],
      routing_revision: 3
    })).resolves.toEqual({ ...config, routing_revision: 4 })

    expect(get).toHaveBeenCalledWith('/playground/auto-routing/12')
    expect(put).toHaveBeenCalledWith('/playground/auto-routing/12', {
      auto_selection_mode: 'custom',
      group_ids: [8, 4],
      routing_revision: 3
    })
  })
})

describe('createPlaygroundSSEParser', () => {
  it('handles JSON events split across transport chunks', () => {
    const onDelta = vi.fn()
    const parse = createPlaygroundSSEParser(onDelta)

    parse('data: {"choices":[{"delta":{"content":"Hel')
    parse('lo"}}]}\n\ndata: {"choices":[{"delta":{"content":" world"}}]}\n\n')

    expect(onDelta.mock.calls.flat()).toEqual(['Hello', ' world'])
  })

  it('ignores done markers, comments, and malformed keepalives', () => {
    const onDelta = vi.fn()
    const parse = createPlaygroundSSEParser(onDelta)

    parse(': ping\n\ndata: not-json\n\ndata: [DONE]\n\n', true)

    expect(onDelta).not.toHaveBeenCalled()
  })

  it('throws valid OpenAI error events after delivering partial content', () => {
    const onDelta = vi.fn()
    const parse = createPlaygroundSSEParser(onDelta)

    expect(() => parse('data: {"choices":[{"delta":{"content":"partial"}}]}\n\ndata: {"error":{"message":"quota exceeded"}}\n\n')).toThrow('quota exceeded')
    expect(onDelta).toHaveBeenCalledWith('partial')
  })
})

describe('streamPlaygroundChat', () => {
  it('rejects an SSE error after partial tokens while ignoring keepalives', async () => {
    const encoder = new TextEncoder()
    let canceled = false
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoder.encode(': ping\n\ndata: {"choices":[{"delta":{"content":"partial"}}]}\n\n'))
        controller.enqueue(encoder.encode('data: {"type":"error","message":"upstream failed"}\n\n'))
      },
      cancel() {
        canceled = true
      },
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body, { status: 200 })))
    const onDelta = vi.fn()

    await expect(streamPlaygroundChat({
      route: { mode: 'auto', autoProfileId: 12 },
      model: 'chat-model',
      messages: [{ role: 'user', content: 'hello' }],
      onDelta,
    })).rejects.toThrow('upstream failed')

    expect(onDelta).toHaveBeenCalledTimes(1)
    expect(onDelta).toHaveBeenCalledWith('partial')
    expect(canceled).toBe(true)
    const requestHeaders = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][1].headers
    expect(requestHeaders['X-Playground-Auto-Profile-ID']).toBe('12')
    expect(requestHeaders).not.toHaveProperty('X-Playground-Group-ID')
  })
})

describe('generatePlaygroundImages', () => {
  it.each([
    [{ b64_json: '/9j/', mime_type: 'image/jpeg' }, {}, 'data:image/jpeg;base64,/9j/'],
    [{ b64_json: 'aW1hZ2U=', output_format: 'webp' }, {}, 'data:image/webp;base64,aW1hZ2U='],
    [{ b64_json: 'aW1hZ2U=' }, { output_format: 'jpeg' }, 'data:image/jpeg;base64,aW1hZ2U='],
    [{ b64_json: 'data:image/webp;base64,aW1hZ2U=' }, {}, 'data:image/webp;base64,aW1hZ2U='],
    [{ url: 'https://images.example/one.png' }, {}, 'https://images.example/one.png']
  ])('preserves the response image format and source', async (item, metadata, url) => {
    post.mockResolvedValue({ data: { ...metadata, data: [item] } })
    const onImage = vi.fn()

    await expect(generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: 'sunset',
      size: '1024x1024', quality: 'auto', count: 1, onImage
    })).resolves.toEqual([{ url, revisedPrompt: undefined }])

    expect(onImage).toHaveBeenCalledWith({ url, revisedPrompt: undefined })
  })

  it('routes reference images to edits with group attribution and one output per request', async () => {
    post.mockResolvedValue({ data: { data: [{ b64_json: 'generated' }] } })
    const references = ['data:image/png;base64,reference']

    await generatePlaygroundImages({
      groupId: 7, model: 'gpt-image-1', prompt: 'change the background',
      size: '1024x1024', quality: 'auto', count: 2, inputImages: references
    })

    expect(post).toHaveBeenCalledTimes(2)
    for (const [endpoint, body, config] of post.mock.calls) {
      expect(endpoint).toBe('/playground/images/edits')
      expect(body).toMatchObject({ n: 1, images: [{ image_url: references[0] }] })
      expect(config.headers).toEqual({ 'X-Playground-Group-ID': '7' })
    }
  })

  it('does not silently retry unsupported image edits as text-to-image', async () => {
    post.mockRejectedValue({ response: { status: 404 }, message: 'Image edits are not supported' })

    await expect(generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: 'edit this',
      size: '1024x1024', quality: 'auto', count: 1,
      inputImages: ['data:image/png;base64,reference']
    })).rejects.toMatchObject({ status: 404, images: [] })
    expect(post).toHaveBeenCalledOnce()
    expect(post.mock.calls[0][0]).toBe('/playground/images/edits')
  })

  function deferred<T>() {
    let resolve!: (value: T) => void
    let reject!: (reason?: unknown) => void
    const promise = new Promise<T>((resolvePromise, rejectPromise) => {
      resolve = resolvePromise
      reject = rejectPromise
    })
    return { promise, resolve, reject }
  }

  it('passes an optional cancellation signal to axios', async () => {
    const controller = new AbortController()
    post.mockResolvedValue({ data: { data: [{ url: 'https://images.example/one.png' }] } })

    await generatePlaygroundImages({
      groupId: 7,
      model: 'image-model',
      prompt: 'sunset',
      size: '1024x1024',
      quality: 'auto',
      count: 1,
      signal: controller.signal,
    })

    expect(post).toHaveBeenCalledWith(
      '/playground/images/generations',
      expect.objectContaining({ model: 'image-model' }),
      expect.objectContaining({ signal: controller.signal }),
    )
  })

  it('starts all one-image requests concurrently and preserves slot order', async () => {
    const onImage = vi.fn()
    const first = deferred<{ data: { data: Array<{ url: string }> } }>()
    const second = deferred<{ data: { data: Array<{ url: string }> } }>()
    const third = deferred<{ data: { data: Array<{ url: string }> } }>()
    post.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise).mockReturnValueOnce(third.promise)

    const generated = generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: '生成3张城市夜景', size: '1024x1024', quality: 'auto', count: 3, onImage,
    })

    expect(post).toHaveBeenCalledTimes(3)
    expect(post.mock.calls.every(([, body]) => body.n === 1)).toBe(true)
    third.resolve({ data: { data: [{ url: 'https://images.example/three.png' }] } })
    await vi.waitFor(() => expect(onImage).toHaveBeenCalledWith({ url: 'https://images.example/three.png', revisedPrompt: undefined }))
    second.resolve({ data: { data: [{ url: 'https://images.example/two.png' }] } })
    first.resolve({ data: { data: [{ url: 'https://images.example/one.png' }] } })

    await expect(generated).resolves.toEqual([
      { url: 'https://images.example/one.png', revisedPrompt: undefined },
      { url: 'https://images.example/two.png', revisedPrompt: undefined },
      { url: 'https://images.example/three.png', revisedPrompt: undefined },
    ])
  })

  it('serializes image callbacks while keeping generation requests concurrent', async () => {
    const callbackGate = deferred<void>()
    let activeCallbacks = 0
    let maxActiveCallbacks = 0
    const onImage = vi.fn(async () => {
      activeCallbacks += 1
      maxActiveCallbacks = Math.max(maxActiveCallbacks, activeCallbacks)
      await callbackGate.promise
      activeCallbacks -= 1
    })
    post.mockResolvedValue({ data: { data: [{ url: 'https://images.example/generated.png' }] } })

    const generated = generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: '生成3张', size: '1024x1024', quality: 'auto', count: 3, onImage,
    })

    expect(post).toHaveBeenCalledTimes(3)
    await vi.waitFor(() => expect(onImage).toHaveBeenCalledTimes(1))
    callbackGate.resolve()
    await generated
    expect(onImage).toHaveBeenCalledTimes(1)
    expect(maxActiveCallbacks).toBe(1)
  })

  it('returns a useful 402 aggregate error with successful concurrent slots', async () => {
    const onImage = vi.fn()
    const balanceError = { status: 402, message: 'Insufficient balance' }
    post
      .mockResolvedValueOnce({ data: { data: [{ url: 'https://images.example/one.png' }] } })
      .mockRejectedValueOnce(balanceError)
      .mockResolvedValueOnce({ data: { data: [{ url: 'https://images.example/three.png' }] } })

    await expect(generatePlaygroundImages({
      groupId: 7,
      model: 'image-model',
      prompt: '生成3张城市夜景',
      size: '1024x1024',
      quality: 'auto',
      count: 3,
      onImage,
    })).rejects.toMatchObject({
      name: 'PlaygroundImageGenerationError',
      message: 'Insufficient balance',
      status: 402,
      cause: balanceError,
      images: [
        { url: 'https://images.example/one.png', revisedPrompt: undefined },
        { url: 'https://images.example/three.png', revisedPrompt: undefined },
      ],
    })

    expect(post).toHaveBeenCalledTimes(3)
    expect(post).toHaveBeenNthCalledWith(1, '/playground/images/generations', expect.objectContaining({ n: 1, prompt: expect.stringContaining('request 1 of 3') }), expect.any(Object))
    expect(post).toHaveBeenNthCalledWith(2, '/playground/images/generations', expect.objectContaining({ n: 1, prompt: expect.stringContaining('request 2 of 3') }), expect.any(Object))
    expect(onImage).toHaveBeenCalledWith({ url: 'https://images.example/one.png', revisedPrompt: undefined })
    expect(onImage).toHaveBeenCalledWith({ url: 'https://images.example/three.png', revisedPrompt: undefined })
  })

  it('makes exactly one n=1 request per requested image and deduplicates provider over-returns', async () => {
    post.mockResolvedValue({ data: { data: [
      { url: 'https://images.example/first.png' },
      { url: 'https://images.example/unexpected.png' },
    ] } })

    const images = await generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: '生成3张', size: '1024x1024', quality: 'auto', count: 3,
    })

    expect(post).toHaveBeenCalledTimes(3)
    expect(post.mock.calls.every(([, body]) => body.n === 1)).toBe(true)
    expect(images).toEqual([{ url: 'https://images.example/first.png', revisedPrompt: undefined }])
  })

  it('propagates cancellation after launching the concurrent batch', async () => {
    const controller = new AbortController()
    post.mockImplementation(async () => {
      controller.abort()
      throw new DOMException('The operation was aborted', 'AbortError')
    })

    await expect(generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: '生成3张', size: '1024x1024', quality: 'auto', count: 3, signal: controller.signal,
    })).rejects.toMatchObject({ name: 'AbortError' })
    expect(post).toHaveBeenCalledTimes(3)
  })
})
