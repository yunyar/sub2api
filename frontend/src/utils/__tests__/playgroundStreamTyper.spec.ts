import { afterEach, describe, expect, it, vi } from 'vitest'
import { createStreamTyper } from '../playgroundStreamTyper'

function manualFrames() {
  const queue: Array<(now: number) => void> = []
  let now = 0
  return {
    requestFrame: (callback: (now: number) => void) => { queue.push(callback); return queue.length },
    cancelFrame: () => { queue.length = 0 },
    step(ms = 16) {
      now += ms
      const callbacks = queue.splice(0)
      callbacks.forEach(callback => callback(now))
      return callbacks.length
    },
    get size() { return queue.length }
  }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('createStreamTyper', () => {
  it('reveals a burst gradually instead of all at once', () => {
    const frames = manualFrames()
    let out = ''
    const typer = createStreamTyper(chunk => { out += chunk }, frames)
    typer.push('a'.repeat(200))
    frames.step()
    expect(out.length).toBeGreaterThan(0)
    expect(out.length).toBeLessThan(200)
    for (let i = 0; i < 300 && typer.pending; i += 1) frames.step()
    expect(out).toBe('a'.repeat(200))
  })

  it('keeps order across pushes and never splits surrogate pairs', () => {
    const frames = manualFrames()
    const chunks: string[] = []
    const typer = createStreamTyper(chunk => chunks.push(chunk), { ...frames, minCharsPerSecond: 62 })
    typer.push('你好😀')
    typer.push('世界😀!')
    for (let i = 0; i < 100 && typer.pending; i += 1) frames.step()
    expect(chunks.join('')).toBe('你好😀世界😀!')
    for (const chunk of chunks) {
      const last = chunk.charCodeAt(chunk.length - 1)
      expect(last >= 0xd800 && last <= 0xdbff).toBe(false)
    }
  })

  it('finish resolves after the buffer drains', async () => {
    const frames = manualFrames()
    let out = ''
    const typer = createStreamTyper(chunk => { out += chunk }, frames)
    typer.push('x'.repeat(50))
    let done = false
    const finished = typer.finish().then(() => { done = true })
    for (let i = 0; i < 300 && typer.pending; i += 1) frames.step()
    frames.step()
    await finished
    expect(done).toBe(true)
    expect(out).toBe('x'.repeat(50))
  })

  it('finish falls back to flushing when frames are paused', async () => {
    vi.useFakeTimers()
    const frames = manualFrames()
    let out = ''
    const typer = createStreamTyper(chunk => { out += chunk }, { ...frames, finishTimeoutMs: 500 })
    typer.push('hidden tab')
    const finished = typer.finish()
    vi.advanceTimersByTime(500)
    await finished
    expect(out).toBe('hidden tab')
  })

  it('flush writes everything immediately', () => {
    const frames = manualFrames()
    let out = ''
    const typer = createStreamTyper(chunk => { out += chunk }, frames)
    typer.push('abc')
    typer.flush()
    expect(out).toBe('abc')
    expect(typer.pending).toBe(0)
  })
})
