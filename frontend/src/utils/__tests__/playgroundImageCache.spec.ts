import { afterEach, describe, expect, it, vi } from 'vitest'
import { playgroundImageBlob, playgroundImageCache } from '../playgroundImageCache'

function request<T>(result: T) {
  const value = { result, error: null, onsuccess: null as null | (() => void), onerror: null }
  queueMicrotask(() => value.onsuccess?.())
  return value as unknown as IDBRequest<T>
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('playgroundImageCache', () => {
  it('converts base64 image data URLs without a network request', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)

    const blob = await playgroundImageBlob('data:image/png;base64,aW1hZ2U=')

    expect(fetch).not.toHaveBeenCalled()
    expect(blob.type).toBe('image/png')
    expect(blob.size).toBe(5)
  })

  it('supports URL-safe unpadded base64 image data URLs', async () => {
    const blob = await playgroundImageBlob('data:image/png;base64,-_8')

    expect(blob.type).toBe('image/png')
    expect(blob.size).toBe(2)
  })

  it('fetches provider URLs only when they are browser-readable', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(new Blob(['image'], { type: 'image/png' })))
    vi.stubGlobal('fetch', fetch)

    await expect(playgroundImageBlob('https://images.example/generated.png')).resolves.toBeInstanceOf(Blob)
    expect(fetch).toHaveBeenCalledWith('https://images.example/generated.png')
  })

  it('restores v2 records regardless of their legacy expiry without recreating the store', async () => {
    const records = [{ cacheKey: '7:image-1', id: 'image-1', accountId: '7', conversationId: 'conversation-1', messageId: 1, prompt: 'sunset', expiresAt: 1, blob: new Blob(['image']) }]
    const store = {
      index: vi.fn(() => ({ getAll: vi.fn(() => request(records)) })),
      createIndex: vi.fn()
    }
    const transaction = { oncomplete: null as null | (() => void), onerror: null, onabort: null, objectStore: vi.fn(() => store) }
    const database = {
      objectStoreNames: { contains: vi.fn((name: string) => name === 'images') },
      deleteObjectStore: vi.fn(),
      createObjectStore: vi.fn(),
      transaction: vi.fn(() => {
        queueMicrotask(() => transaction.oncomplete?.())
        return transaction
      }),
      close: vi.fn()
    }
    const open = vi.fn(() => {
      const value = { result: database, error: null, onupgradeneeded: null as null | ((event: IDBVersionChangeEvent) => void), onsuccess: null as null | (() => void), onerror: null, onblocked: null }
      queueMicrotask(() => {
        value.onupgradeneeded?.({ oldVersion: 2 } as IDBVersionChangeEvent)
        value.onsuccess?.()
      })
      return value as unknown as IDBOpenDBRequest
    })
    vi.stubGlobal('indexedDB', { open })

    await expect(playgroundImageCache.listConversation(7, 'conversation-1')).resolves.toEqual([
      expect.objectContaining({ id: 'image-1', expiresAt: 1 })
    ])

    expect(open).toHaveBeenCalledWith('sub2api-playground-images', 2)
    expect(database.deleteObjectStore).not.toHaveBeenCalled()
    expect(database.createObjectStore).not.toHaveBeenCalled()
    expect(store.index).toHaveBeenCalledWith('accountConversation')
  })

  it('recreates a version 1 store so unscoped records cannot be restored', async () => {
    const legacyStore = {
      createIndex: vi.fn(),
      index: vi.fn(() => ({ getAll: vi.fn(() => request([])) }))
    }
    const transaction = { oncomplete: null as null | (() => void), onerror: null, onabort: null, objectStore: vi.fn(() => legacyStore) }
    const database = {
      objectStoreNames: { contains: vi.fn((name: string) => name === 'images') },
      deleteObjectStore: vi.fn(),
      createObjectStore: vi.fn(() => legacyStore),
      transaction: vi.fn(() => {
        queueMicrotask(() => transaction.oncomplete?.())
        return transaction
      }),
      close: vi.fn()
    }
    const open = vi.fn(() => {
      const value = { result: database, error: null, onupgradeneeded: null as null | ((event: IDBVersionChangeEvent) => void), onsuccess: null as null | (() => void), onerror: null, onblocked: null }
      queueMicrotask(() => {
        value.onupgradeneeded?.({ oldVersion: 1 } as IDBVersionChangeEvent)
        value.onsuccess?.()
      })
      return value as unknown as IDBOpenDBRequest
    })
    vi.stubGlobal('indexedDB', { open })

    await expect(playgroundImageCache.listConversation(7, 'conversation-1')).resolves.toEqual([])

    expect(database.deleteObjectStore).toHaveBeenCalledWith('images')
    expect(database.createObjectStore).toHaveBeenCalledWith('images', { keyPath: 'cacheKey' })
    expect(legacyStore.createIndex).toHaveBeenCalledWith('accountConversation', ['accountId', 'conversationId'], { unique: false })
  })

  it('does not open IndexedDB for timer-driven expiry cleanup', async () => {
    const open = vi.fn()
    vi.stubGlobal('indexedDB', { open })

    await expect(playgroundImageCache.deleteExpired()).resolves.toBeUndefined()
    expect(open).not.toHaveBeenCalled()
  })
})
