import { afterEach, describe, expect, it, vi } from 'vitest'
import { loadPlaygroundPreferences, savePlaygroundPreferences } from '@/utils/playgroundPreferences'

describe('playgroundPreferences', () => {
  afterEach(() => { vi.restoreAllMocks(); localStorage.clear() })

  it('keeps preferences isolated by account', () => {
    savePlaygroundPreferences(1, { groupId: 2, chatModel: 'chat-a', imageModel: 'image-a' })
    savePlaygroundPreferences(2, { groupId: 3, chatModel: 'chat-b', imageModel: 'image-b' })

    expect(loadPlaygroundPreferences(1)).toEqual({ groupId: 2, chatModel: 'chat-a', imageModel: 'image-a' })
    expect(loadPlaygroundPreferences(2)).toEqual({ groupId: 3, chatModel: 'chat-b', imageModel: 'image-b' })
  })

  it('ignores invalid persisted values', () => {
    localStorage.setItem('playground-preferences:1', '{invalid')
    expect(loadPlaygroundPreferences(1)).toEqual({ groupId: 0, chatModel: '', imageModel: '' })
  })

  it('continues when the browser denies storage access', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('Denied', 'SecurityError') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Denied', 'QuotaExceededError') })
    expect(loadPlaygroundPreferences(1)).toEqual({ groupId: 0, chatModel: '', imageModel: '' })
    expect(() => savePlaygroundPreferences(1, { groupId: 2, chatModel: 'chat', imageModel: 'image' })).not.toThrow()
  })
})
