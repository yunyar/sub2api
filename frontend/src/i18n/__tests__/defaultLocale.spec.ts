import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/router/title', () => ({
  resolveRouteDocumentTitle: () => 'Updated title'
}))
vi.mock('@/router', () => ({
  default: { currentRoute: { value: { name: 'Dashboard', meta: {} } } }
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ siteName: 'Happy', cachedPublicSettings: null })
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: false })
}))
vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] })
}))

beforeEach(() => {
  vi.resetModules()
  localStorage.clear()
  document.documentElement.lang = 'en'
})

afterEach(() => {
  localStorage.clear()
  document.documentElement.lang = ''
  document.title = ''
  vi.restoreAllMocks()
})

describe('default locale', () => {
  it.each(['en-US', 'zh-CN', 'ja-JP'])('defaults to Chinese with browser language %s', async (language) => {
    vi.spyOn(navigator, 'language', 'get').mockReturnValue(language)
    const { i18n, initI18n, getLocale } = await import('../index')
    await initI18n()
    expect(getLocale()).toBe('zh')
    expect(i18n.global.fallbackLocale.value).toBe('zh')
    expect(i18n.global.t('bloom.title')).toBe('任务花园')
    expect(document.documentElement.lang).toBe('zh')
    expect(localStorage.getItem('sub2api_locale')).toBeNull()
  })

  it.each(['en', 'zh'] as const)('preserves the explicit %s preference', async (locale) => {
    localStorage.setItem('sub2api_locale', locale)
    vi.spyOn(navigator, 'language', 'get').mockReturnValue(locale === 'en' ? 'zh-CN' : 'en-US')
    const { getLocale, initI18n, i18n } = await import('../index')
    await initI18n()
    expect(getLocale()).toBe(locale)
    expect(document.documentElement.lang).toBe(locale)
    expect(i18n.global.t('bloom.title')).toBe(locale === 'en' ? 'Task Garden' : '任务花园')
    expect(localStorage.getItem('sub2api_locale')).toBe(locale)
  })

  it('falls back to Chinese for an unsupported stored language', async () => {
    localStorage.setItem('sub2api_locale', 'fr')
    const { initI18n, getLocale } = await import('../index')
    await initI18n()
    expect(getLocale()).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
  })

  it('keeps a manual English selection across page initialization', async () => {
    const { initI18n, setLocale, getLocale } = await import('../index')
    await initI18n()
    await setLocale('en')
    expect(getLocale()).toBe('en')
    expect(document.documentElement.lang).toBe('en')
    expect(document.title).toBe('Updated title')
    expect(localStorage.getItem('sub2api_locale')).toBe('en')

    vi.resetModules()
    const reloaded = await import('../index')
    await reloaded.initI18n()
    expect(reloaded.getLocale()).toBe('en')
    expect(reloaded.i18n.global.t('bloom.title')).toBe('Task Garden')
    expect(document.documentElement.lang).toBe('en')
  })

  it('ignores unsupported manual selections', async () => {
    const { initI18n, setLocale, getLocale } = await import('../index')
    await initI18n()
    await setLocale('fr')
    expect(getLocale()).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
    expect(localStorage.getItem('sub2api_locale')).toBeNull()
  })
})
