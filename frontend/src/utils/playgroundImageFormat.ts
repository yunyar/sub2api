const imageFormats: Record<string, string> = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  webp: 'image/webp',
  gif: 'image/gif',
  avif: 'image/avif'
}

function imageMimeType(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const format = value.trim().toLowerCase().split(';')[0]
  return imageFormats[format] ?? Object.values(imageFormats).find(mime => mime === format)
}

export function playgroundImageDataURL(encoded: string, ...formats: unknown[]): string {
  const source = encoded.trim()
  const dataURL = /^data:([^;,]+);base64,([\s\S]*)$/i.exec(source)
  const payload = (dataURL ? dataURL[2] : source).replace(/\s/g, '').replace(/-/g, '+').replace(/_/g, '/')
  const padded = payload.padEnd(Math.ceil(payload.length / 4) * 4, '=')
  let detected: string | undefined
  try {
    const header = atob(padded.slice(0, 32))
    if (header.startsWith('\x89PNG\r\n\x1a\n')) detected = 'image/png'
    else if (header.startsWith('\xff\xd8\xff')) detected = 'image/jpeg'
    else if (header.startsWith('RIFF') && header.slice(8, 12) === 'WEBP') detected = 'image/webp'
    else if (/^GIF8[79]a/.test(header)) detected = 'image/gif'
    else if (header.slice(4, 8) === 'ftyp' && ['avif', 'avis'].includes(header.slice(8, 12))) detected = 'image/avif'
  } catch {
    detected = undefined
  }
  const mime = detected ?? [dataURL?.[1], ...formats].map(imageMimeType).find(Boolean) ?? 'image/png'
  return `data:${mime};base64,${padded}`
}

export function playgroundImageExtension(mime: string): string {
  const normalized = imageMimeType(mime)
  return Object.entries(imageFormats).find(([extension, value]) => extension !== 'jpg' && value === normalized)?.[0] ?? 'png'
}
