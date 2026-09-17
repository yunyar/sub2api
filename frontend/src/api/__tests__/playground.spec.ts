import { describe, expect, it, vi } from 'vitest'
import { createPlaygroundSSEParser } from '../playground'

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
})
