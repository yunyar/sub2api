import { afterEach, describe, expect, it, vi } from 'vitest'

async function loadURLModule(hostname: string, apiBaseURL?: string) {
  vi.resetModules()
  if (apiBaseURL === undefined) {
    vi.unstubAllEnvs()
  } else {
    vi.stubEnv('VITE_API_BASE_URL', apiBaseURL)
  }
  vi.stubGlobal('window', { location: { hostname, origin: `http://${hostname}` } })
  return import('../url')
}

afterEach(() => {
  vi.unstubAllEnvs()
  vi.unstubAllGlobals()
})

describe('API URL base', () => {
  it('uses same-origin /api/v1 on an IPv4 host even when an absolute API base is configured', async () => {
    const url = await loadURLModule('43.134.166.100', 'https://api.example.com/api/v1')

    expect(url.getAPIBaseURL()).toBe('/api/v1')
    expect(url.buildApiUrl('/playground/chat/completions')).toBe('/api/v1/playground/chat/completions')
  })

  it('preserves an absolute API base on a hostname', async () => {
    const url = await loadURLModule('console.example.com', 'https://api.example.com/api/v1')

    expect(url.getAPIBaseURL()).toBe('https://api.example.com/api/v1')
  })

  it('normalizes a relative API base to an absolute same-origin path', async () => {
    const url = await loadURLModule('43.134.166.100', 'api/v1')

    expect(url.getAPIBaseURL()).toBe('/api/v1')
  })
})
