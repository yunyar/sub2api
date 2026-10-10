import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  artifactGenerationInstruction,
  downloadPlaygroundArtifact,
  extractPlaygroundArtifacts,
  MAX_PLAYGROUND_ARTIFACTS,
  MAX_PLAYGROUND_ARTIFACT_BYTES,
  MAX_PLAYGROUND_ARTIFACT_NAME_LENGTH
} from '../playgroundArtifacts'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('extractPlaygroundArtifacts', () => {
  it('extracts named artifacts without consuming surrounding prose', () => {
    const result = extractPlaygroundArtifacts([
      'Here is the result.',
      '```artifact:notes.md',
      '# Notes',
      'Done.',
      '```',
      'And a second file:',
      '```artifact:data.json',
      '{"ok":true}',
      '```'
    ].join('\n'))

    expect(result).toHaveLength(2)
    expect(result.map(({ name, content }) => ({ name, content }))).toEqual([
      { name: 'notes.md', content: '# Notes\nDone.' },
      { name: 'data.json', content: '{"ok":true}' }
    ])
    expect(result[0].id).toBeTruthy()
    expect(result[1].id).not.toBe(result[0].id)
  })

  it('sanitizes paths and control characters and rejects unsupported extensions', () => {
    const result = extractPlaygroundArtifacts([
      '```artifact:../../bad\u0000 name.html',
      '<script>still a downloadable attachment</script>',
      '```',
      '```artifact:run.exe',
      'not allowed',
      '```',
      '```artifact:.hidden.py',
      'print("ok")',
      '```'
    ].join('\n'))

    expect(result.map(({ name }) => name)).toEqual(['bad_name.html', 'hidden.py'])
  })

  it('caps extracted artifacts at eight', () => {
    const content = Array.from({ length: MAX_PLAYGROUND_ARTIFACTS + 2 }, (_, index) =>
      `\`\`\`artifact:${index}.txt\n${index}\n\`\`\``
    ).join('\n')

    expect(extractPlaygroundArtifacts(content)).toHaveLength(MAX_PLAYGROUND_ARTIFACTS)
  })

  it('enforces the aggregate UTF-8 byte limit', () => {
    const oversized = 'é'.repeat(MAX_PLAYGROUND_ARTIFACT_BYTES / 2 + 1)
    const content = [
      '```artifact:first.txt',
      'ok',
      '```',
      '```artifact:too-large.txt',
      oversized,
      '```',
      '```artifact:after.txt',
      'not included',
      '```'
    ].join('\n')

    expect(extractPlaygroundArtifacts(content).map(({ name }) => name)).toEqual(['first.txt', 'after.txt'])
  })

  it('caps sanitized filenames at 120 characters while preserving the safe extension', () => {
    const longName = `${'a'.repeat(150)}.md`
    const [artifact] = extractPlaygroundArtifacts(`\`\`\`artifact:${longName}\ncontent\n\`\`\``)

    expect(artifact.name).toHaveLength(MAX_PLAYGROUND_ARTIFACT_NAME_LENGTH)
    expect(artifact.name.endsWith('.md')).toBe(true)
  })

  it('documents the artifact fence protocol for model instructions', () => {
    expect(artifactGenerationInstruction).toContain('```artifact:filename.ext')
    expect(artifactGenerationInstruction).toContain('never previewed or executed')
  })
})

describe('downloadPlaygroundArtifact', () => {
  it('downloads an octet-stream Blob with a safe filename and revokes its URL later', () => {
    const objectURL = 'blob:test-artifact'
    const createObjectURL = vi.fn(() => objectURL)
    const revokeObjectURL = vi.fn()
    const clickedLinks: HTMLAnchorElement[] = []
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clickedLinks.push(this)
    })
    vi.stubGlobal('URL', { ...URL, createObjectURL, revokeObjectURL })
    const setTimeout = vi.spyOn(window, 'setTimeout')

    downloadPlaygroundArtifact({ id: 'test-id', name: '../report.html', content: '<h1>file</h1>' })

    expect(createObjectURL).toHaveBeenCalledOnce()
    const blob = createObjectURL.mock.calls[0][0]
    expect(blob).toBeInstanceOf(Blob)
    expect(blob.type).toBe('application/octet-stream')
    expect(click).toHaveBeenCalledOnce()
    expect(clickedLinks[0].download).toBe('report.html')
    expect(clickedLinks[0].href).toBe(objectURL)
    expect(document.querySelector('a[download="report.html"]')).toBeNull()

    const cleanup = setTimeout.mock.calls.find(([callback]) => typeof callback === 'function')?.[0]
    expect(cleanup).toBeTypeOf('function')
    ;(cleanup as () => void)()
    expect(revokeObjectURL).toHaveBeenCalledWith(objectURL)
  })
})
