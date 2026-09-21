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
    post.mockResolvedValue({ data: { data: [] } })

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
})
