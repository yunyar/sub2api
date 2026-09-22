export interface CachedPlaygroundImage {
  id: string
  accountId: string
  conversationId: string
  messageId: number
  stepId?: string
  prompt: string
  expiresAt?: number
  blob: Blob
}

const DATABASE_NAME = 'sub2api-playground-images'
const STORE_NAME = 'images'
const DATABASE_VERSION = 2

type RestoredPlaygroundImage = CachedPlaygroundImage & { expiresAt: number }
type StoredPlaygroundImage = CachedPlaygroundImage & { cacheKey: string }

function cacheKey(accountId: string, id: string) {
  return `${accountId}:${id}`
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('IndexedDB request failed'))
  })
}

function transactionResult(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onerror = () => reject(transaction.error || new Error('IndexedDB transaction failed'))
    transaction.onabort = () => reject(transaction.error || new Error('IndexedDB transaction aborted'))
  })
}

function decodeBase64(value: string): Uint8Array {
  const normalized = value.replace(/\s/g, '').replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=')
  const decoded = atob(padded)
  const bytes = new Uint8Array(decoded.length)
  for (let index = 0; index < decoded.length; index += 1) bytes[index] = decoded.charCodeAt(index)
  return bytes
}

export async function playgroundImageBlob(url: string): Promise<Blob> {
  if (!url.startsWith('data:')) {
    const response = await fetch(url)
    if (!response.ok) throw new Error('image cache fetch failed')
    return response.blob()
  }

  const separator = url.indexOf(',')
  if (separator < 0) throw new Error('invalid image data URL')
  const metadata = url.slice(5, separator)
  const mimeType = metadata.split(';', 1)[0] || 'text/plain'
  if (!mimeType.toLowerCase().startsWith('image/')) throw new Error('image data URL must have an image MIME type')
  const payload = url.slice(separator + 1)
  const bytes = /;base64(?:;|$)/i.test(metadata)
    ? decodeBase64(payload)
    : new TextEncoder().encode(decodeURIComponent(payload))
  return new Blob([bytes], { type: mimeType })
}

function openDatabase(): Promise<IDBDatabase> {
  if (typeof indexedDB === 'undefined') return Promise.reject(new Error('IndexedDB is unavailable'))
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION)
    request.onupgradeneeded = (event) => {
      const database = request.result
      const resetLegacyStore = event.oldVersion > 0 && event.oldVersion < 2 && database.objectStoreNames.contains(STORE_NAME)
      if (resetLegacyStore) database.deleteObjectStore(STORE_NAME)
      if (resetLegacyStore || !database.objectStoreNames.contains(STORE_NAME)) {
        const store = database.createObjectStore(STORE_NAME, { keyPath: 'cacheKey' })
        store.createIndex('accountConversation', ['accountId', 'conversationId'], { unique: false })
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('IndexedDB open failed'))
    request.onblocked = () => reject(new Error('IndexedDB is blocked'))
  })
}

async function withStore<T>(mode: IDBTransactionMode, callback: (store: IDBObjectStore) => Promise<T>): Promise<T> {
  const database = await openDatabase()
  try {
    const transaction = database.transaction(STORE_NAME, mode)
    const completed = transactionResult(transaction)
    try {
      const result = await callback(transaction.objectStore(STORE_NAME))
      await completed
      return result
    } catch (error) {
      await completed.catch(() => {})
      throw error
    }
  } finally {
    database.close()
  }
}

export const playgroundImageCache = {
  put(image: CachedPlaygroundImage) {
    return withStore('readwrite', async store => requestResult(store.put({ ...image, cacheKey: cacheKey(image.accountId, image.id) })))
  },
  async listConversation(accountId: number | string, conversationId: string) {
    const records = await withStore('readonly', async store => requestResult(store.index('accountConversation').getAll([String(accountId), conversationId])))
    return (records as StoredPlaygroundImage[]).map(({ cacheKey: _cacheKey, expiresAt, ...image }): RestoredPlaygroundImage => ({
      ...image,
      expiresAt: expiresAt ?? 0
    }))
  },
  deleteConversation(accountId: number | string, conversationId: string) {
    return withStore('readwrite', async store => {
      const records = await requestResult(store.index('accountConversation').getAllKeys([String(accountId), conversationId]))
      records.forEach(key => store.delete(key))
    })
  },
  deleteStep(accountId: number | string, conversationId: string, stepId: string) {
    return withStore('readwrite', async store => {
      const records = await requestResult(store.index('accountConversation').getAll([String(accountId), conversationId])) as StoredPlaygroundImage[]
      records.filter(image => image.stepId === stepId).forEach(image => store.delete(image.cacheKey))
    })
  },
  deleteExpired(_now?: number) {
    return Promise.resolve()
  }
}
