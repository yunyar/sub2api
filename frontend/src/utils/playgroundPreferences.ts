export interface PlaygroundPreferences {
  groupId: number
  chatModel: string
  imageModel: string
}

const STORAGE_PREFIX = 'playground-preferences:'

const emptyPreferences = (): PlaygroundPreferences => ({ groupId: 0, chatModel: '', imageModel: '' })

export function loadPlaygroundPreferences(accountId: number | string | null | undefined): PlaygroundPreferences {
  if (accountId === null || accountId === undefined || typeof window === 'undefined') return emptyPreferences()
  try {
    const saved = JSON.parse(window.localStorage.getItem(`${STORAGE_PREFIX}${accountId}`) || '{}')
    return {
      groupId: Number.isInteger(saved.groupId) && saved.groupId > 0 ? saved.groupId : 0,
      chatModel: typeof saved.chatModel === 'string' ? saved.chatModel : '',
      imageModel: typeof saved.imageModel === 'string' ? saved.imageModel : ''
    }
  } catch {
    return emptyPreferences()
  }
}

export function savePlaygroundPreferences(accountId: number | string | null | undefined, preferences: PlaygroundPreferences) {
  if (accountId === null || accountId === undefined || typeof window === 'undefined') return
  try {
    window.localStorage.setItem(`${STORAGE_PREFIX}${accountId}`, JSON.stringify(preferences))
  } catch {
    return
  }
}
