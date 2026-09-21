import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPlaygroundId } from '../playgroundId'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('createPlaygroundId', () => {
  it('uses randomUUID when available', () => {
    vi.stubGlobal('crypto', { randomUUID: () => '11111111-1111-4111-8111-111111111111' })

    expect(createPlaygroundId()).toBe('11111111-1111-4111-8111-111111111111')
  })

  it('creates a UUID from getRandomValues when HTTP does not expose randomUUID', () => {
    vi.stubGlobal('crypto', {
      getRandomValues: (bytes: Uint8Array) => {
        bytes.fill(0xab)
        return bytes
      },
    })

    expect(createPlaygroundId()).toBe('abababab-abab-4bab-abab-abababababab')
  })

  it('creates a valid UI id when crypto is unavailable', () => {
    vi.stubGlobal('crypto', undefined)

    const id = createPlaygroundId()

    expect(id).toMatch(/^[a-zA-Z0-9-]{1,64}$/)
  })
})
