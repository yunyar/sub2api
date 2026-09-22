import { afterEach, describe, expect, it, vi } from 'vitest'

const post = vi.hoisted(() => vi.fn())

vi.mock('../client', () => ({
  apiClient: { post },
}))

import { createPlaygroundSSEParser, generatePlaygroundImages, streamPlaygroundChat } from '../playground'

afterEach(() => {
  post.mockReset()
  vi.unstubAllGlobals()
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
      groupId: 7,
      model: 'chat-model',
      messages: [{ role: 'user', content: 'hello' }],
      onDelta,
    })).rejects.toThrow('upstream failed')

    expect(onDelta).toHaveBeenCalledTimes(1)
    expect(onDelta).toHaveBeenCalledWith('partial')
    expect(canceled).toBe(true)
  })
})

describe('generatePlaygroundImages', () => {
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

  it('splits a requested batch into one-image requests and stops after a balance rejection', async () => {
    const onImage = vi.fn()
    const balanceError = { status: 402, message: 'Insufficient balance' }
    post
      .mockResolvedValueOnce({ data: { data: [{ url: 'https://images.example/one.png' }] } })
      .mockRejectedValueOnce(balanceError)

    await expect(generatePlaygroundImages({
      groupId: 7,
      model: 'image-model',
      prompt: '生成3张城市夜景',
      size: '1024x1024',
      quality: 'auto',
      count: 3,
      onImage,
    })).rejects.toMatchObject({ name: 'PlaygroundImageGenerationError', message: 'Insufficient balance', status: 402, cause: balanceError, images: [{ url: 'https://images.example/one.png' }] })

    expect(post).toHaveBeenCalledTimes(2)
    expect(post).toHaveBeenNthCalledWith(1, '/playground/images/generations', expect.objectContaining({ n: 1, prompt: expect.stringContaining('request 1 of 3') }), expect.any(Object))
    expect(post).toHaveBeenNthCalledWith(2, '/playground/images/generations', expect.objectContaining({ n: 1, prompt: expect.stringContaining('request 2 of 3') }), expect.any(Object))
    expect(onImage).toHaveBeenCalledWith({ url: 'https://images.example/one.png', revisedPrompt: undefined })
  })

  it('makes exactly one n=1 request per requested image and caps provider over-returns', async () => {
    post.mockResolvedValue({ data: { data: [
      { url: 'https://images.example/first.png' },
      { url: 'https://images.example/unexpected.png' },
    ] } })

    const images = await generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: '生成3张', size: '1024x1024', quality: 'auto', count: 3,
    })

    expect(post).toHaveBeenCalledTimes(3)
    expect(post.mock.calls.every(([, body]) => body.n === 1)).toBe(true)
    expect(images).toHaveLength(3)
    expect(images.every(image => image.url === 'https://images.example/first.png')).toBe(true)
  })

  it('does not start the next request after cancellation', async () => {
    const controller = new AbortController()
    post.mockImplementationOnce(async () => {
      controller.abort()
      return { data: { data: [{ url: 'https://images.example/first.png' }] } }
    })

    await expect(generatePlaygroundImages({
      groupId: 7, model: 'image-model', prompt: '生成3张', size: '1024x1024', quality: 'auto', count: 3, signal: controller.signal,
    })).rejects.toMatchObject({ name: 'AbortError' })
    expect(post).toHaveBeenCalledTimes(1)
  })
})
