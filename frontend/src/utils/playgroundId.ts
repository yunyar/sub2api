let fallbackSequence = 0

function fallbackId(): string {
  fallbackSequence = (fallbackSequence + 1) % Number.MAX_SAFE_INTEGER
  return `ui-${Date.now().toString(36)}-${fallbackSequence.toString(36)}-${Math.random().toString(36).slice(2, 14)}`
}

function randomValuesId(): string | null {
  const cryptoApi = globalThis.crypto
  if (!cryptoApi?.getRandomValues) return null

  try {
    const bytes = cryptoApi.getRandomValues(new Uint8Array(16))
    bytes[6] = (bytes[6] & 0x0f) | 0x40
    bytes[8] = (bytes[8] & 0x3f) | 0x80
    const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  } catch {
    return null
  }
}

export function createPlaygroundId(): string {
  try {
    if (typeof globalThis.crypto?.randomUUID === 'function') {
      return globalThis.crypto.randomUUID()
    }
  } catch {
    return randomValuesId() ?? fallbackId()
  }

  return randomValuesId() ?? fallbackId()
}
