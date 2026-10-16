import { describe, expect, it } from 'vitest'
import { playgroundImageDataURL, playgroundImageExtension } from '../playgroundImageFormat'

describe('playground image format', () => {
  it.each([
    ['image/jpeg', 'jpeg'],
    ['image/webp', 'webp'],
    ['image/png', 'png'],
    ['image/gif', 'gif'],
    ['image/avif', 'avif'],
    ['text/html', 'png']
  ])('uses the matching download extension for %s', (mime, extension) => {
    expect(playgroundImageExtension(mime)).toBe(extension)
  })

  it('unwraps data URLs and normalizes URL-safe unpadded payloads', () => {
    expect(playgroundImageDataURL('data:image/jpeg;base64,-_8')).toBe('data:image/jpeg;base64,+/8=')
  })

  it.each([
    ['\x89PNG\r\n\x1a\n', 'image/png'],
    ['\xff\xd8\xff', 'image/jpeg'],
    ['RIFF0000WEBP', 'image/webp'],
    ['GIF89a', 'image/gif'],
    ['\0\0\0\x20ftypavif', 'image/avif']
  ])('identifies image bytes even when metadata is incorrect', (bytes, mime) => {
    expect(playgroundImageDataURL(btoa(bytes), 'image/jpeg', 'png')).toBe(`data:${mime};base64,${btoa(bytes)}`)
  })

  it('uses supported metadata and ignores arbitrary MIME types', () => {
    expect(playgroundImageDataURL('aW1hZ2U=', 'webp')).toBe('data:image/webp;base64,aW1hZ2U=')
    expect(playgroundImageDataURL('aW1hZ2U=', 'text/html')).toBe('data:image/png;base64,aW1hZ2U=')
  })
})
