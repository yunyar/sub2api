export const MAX_PLAYGROUND_ARTIFACTS = 8
export const MAX_PLAYGROUND_ARTIFACT_BYTES = 24 * 1024
export const MAX_PLAYGROUND_ARTIFACT_NAME_LENGTH = 120

const supportedExtensions = new Set([
  'txt', 'md', 'csv', 'json', 'html', 'css', 'js', 'ts', 'py', 'yaml', 'yml', 'sql', 'xml', 'svg'
])
const artifactFence = /^```artifact:([^\r\n]+)[ \t]*\r?\n([\s\S]*?)^```[ \t]*(?=\r?$)/gm

export interface PlaygroundArtifact {
  id: string
  name: string
  content: string
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}

function stripControlCharacters(value: string): string {
  return Array.from(value)
    .filter(character => {
      const codePoint = character.codePointAt(0) ?? 0
      return codePoint > 0x1f && (codePoint < 0x7f || codePoint > 0x9f)
    })
    .join('')
}

function safeArtifactName(value: string): string | null {
  const basename = value
    .replace(/\\/g, '/')
    .split('/')
    .pop()
  const sanitizedBasename = basename && stripControlCharacters(basename)
    .replace(/[^a-zA-Z0-9._-]/g, '_')
    .replace(/^\.+/, '')
  if (!sanitizedBasename) return null

  const extension = sanitizedBasename.split('.').pop()?.toLowerCase()
  if (!extension || !supportedExtensions.has(extension)) return null
  const name = sanitizedBasename.slice(0, -(extension.length + 1))
  const maxStemLength = MAX_PLAYGROUND_ARTIFACT_NAME_LENGTH - extension.length - 1
  return `${name.slice(0, maxStemLength) || 'artifact'}.${extension}`
}

export function extractPlaygroundArtifacts(content: string): PlaygroundArtifact[] {
  if (!content) return []

  const artifacts: PlaygroundArtifact[] = []
  let totalBytes = 0
  for (const match of content.matchAll(artifactFence)) {
    if (artifacts.length >= MAX_PLAYGROUND_ARTIFACTS) break

    const name = safeArtifactName(match[1].trim())
    if (!name) continue

    const artifactContent = match[2].replace(/\r\n/g, '\n').replace(/\n$/, '')
    const contentBytes = utf8ByteLength(artifactContent)
    if (totalBytes + contentBytes > MAX_PLAYGROUND_ARTIFACT_BYTES) continue

    artifacts.push({ id: `artifact-${artifacts.length + 1}`, name, content: artifactContent })
    totalBytes += contentBytes
  }
  return artifacts
}

export function downloadPlaygroundArtifact(artifact: PlaygroundArtifact): void {
  const blob = new Blob([artifact.content], { type: 'application/octet-stream' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = safeArtifactName(artifact.name) ?? 'artifact.txt'
  link.style.display = 'none'
  try {
    document.body.appendChild(link)
    link.click()
  } finally {
    link.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
}

export const artifactGenerationInstruction = [
  'When you create a downloadable text artifact, put each complete artifact in its own fenced block using this exact format:',
  '```artifact:filename.ext',
  'artifact contents',
  '```',
  'Use a descriptive filename and one of these extensions: txt, md, csv, json, html, css, js, ts, py, yaml, yml, sql, xml, svg.',
  'Keep all artifact content inside the fence. Artifact files are downloaded as attachments and are never previewed or executed.',
  'Keep the combined file contents within 24 KiB and the complete reply within 32 KiB of UTF-8 text so it can be saved in conversation history.'
].join('\n')
