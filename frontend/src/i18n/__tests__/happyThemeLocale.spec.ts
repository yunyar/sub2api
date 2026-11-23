import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { JSDOM } from 'jsdom'
import { afterEach, describe, expect, it, vi } from 'vitest'

const source = readFileSync(resolve(process.cwd(), 'public/happy-static/happy-theme.js'), 'utf8')
let page: JSDOM | undefined

function renderTheme(savedLocale: string | null, htmlLocale = 'en', storageUnavailable = false) {
  page = new JSDOM(`<!doctype html><html lang="${htmlLocale}"><body><div id="app"><main></main></div></body></html>`, {
    url: 'https://example.test/dashboard',
    runScripts: 'outside-only'
  })
  const { window } = page
  Object.defineProperty(window.document, 'readyState', { value: 'complete' })
  Object.defineProperty(window.navigator, 'language', { value: 'en-US' })
  window.matchMedia = vi.fn().mockReturnValue({ matches: true })
  vi.spyOn(window, 'setTimeout').mockReturnValue(0)
  if (savedLocale !== null) window.localStorage.setItem('sub2api_locale', savedLocale)
  if (storageUnavailable) {
    vi.spyOn(window.Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('Storage unavailable') })
  }
  window.eval(source)
  return window.document
}

afterEach(() => {
  page?.window.close()
  page = undefined
  vi.restoreAllMocks()
})

describe('public theme locale', () => {
  it.each([null, 'zh', 'fr'])('defaults to Chinese for preference %s before Vue initializes', (preference) => {
    const document = renderTheme(preference)
    expect(document.querySelector('.hp-hello-links')?.textContent).toContain('管理密钥')
    expect(document.querySelector('.hp-hello-line')?.textContent).toMatch(/[\p{Script=Han}]/u)
    expect(document.querySelector('.hp-hello-eyebrow')?.textContent).toMatch(/月/)
    expect(document.querySelector('.hp-signoff')?.textContent).toContain('善意流动')
  })

  it('honors an English selection despite the initial Chinese HTML language', () => {
    const document = renderTheme('en', 'zh-CN')
    expect(document.querySelector('.hp-hello-links')?.textContent).toContain('Manage keys')
    expect(document.querySelector('.hp-hello-links')?.textContent).toContain('View usage')
    expect(document.querySelector('.hp-hello-links')?.textContent).toContain('Explore models')
    expect(document.querySelector('.hp-hello h2')?.textContent).toContain('Good ')
    expect(document.querySelector('.hp-hello-line')?.textContent).not.toMatch(/[\p{Script=Han}]/u)
    expect(document.querySelector('.hp-hello-eyebrow')?.textContent).not.toMatch(/月/)
    expect(document.querySelector('.hp-signoff')?.textContent).toContain('Kindness flows')
  })

  it('uses Chinese when browser storage is unavailable', () => {
    const document = renderTheme(null, 'en', true)
    expect(document.querySelector('.hp-hello-links')?.textContent).toContain('管理密钥')
    expect(document.querySelector('.hp-signoff')?.textContent).toContain('善意流动')
  })
})
