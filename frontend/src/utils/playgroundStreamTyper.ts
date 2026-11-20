/**
 * 流式回复的平滑输出：上游的增量常常成批到达（一顿一顿），直接拼接会出现"卡一下、蹦一串"的观感。
 * 这里把增量先放进缓冲区，再按动画帧匀速释放；积压越多释放越快，保证不会明显落后于真实进度。
 */

export interface StreamTyperOptions {
  /** 最低输出速度（字符/秒） */
  minCharsPerSecond?: number
  /** 期望在多长时间内追上当前积压（秒） */
  catchUpSeconds?: number
  /** 流结束后清空剩余缓冲的期望时长（秒） */
  finishSeconds?: number
  /** 流结束后的最长等待，超时直接写完（毫秒；后台标签页 rAF 会暂停） */
  finishTimeoutMs?: number
  requestFrame?: (callback: (now: number) => void) => number
  cancelFrame?: (handle: number) => void
  now?: () => number
}

export interface StreamTyper {
  push(text: string): void
  /** 流正常结束：加速吐完剩余内容，完成后 resolve */
  finish(): Promise<void>
  /** 立即写出全部剩余内容（中止 / 切换会话时） */
  flush(): void
  readonly pending: number
}

const defaultRequestFrame = (callback: (now: number) => void) =>
  typeof requestAnimationFrame === 'function'
    ? requestAnimationFrame(callback)
    : (setTimeout(() => callback(Date.now()), 16) as unknown as number)

const defaultCancelFrame = (handle: number) => {
  if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(handle)
  else clearTimeout(handle)
}

export function createStreamTyper(apply: (chunk: string) => void, options: StreamTyperOptions = {}): StreamTyper {
  const minCps = options.minCharsPerSecond ?? 60
  const catchUp = options.catchUpSeconds ?? 0.35
  const finishSeconds = options.finishSeconds ?? 0.25
  const finishTimeoutMs = options.finishTimeoutMs ?? 1500
  const requestFrame = options.requestFrame ?? defaultRequestFrame
  const cancelFrame = options.cancelFrame ?? defaultCancelFrame

  let buffer = ''
  let frame = 0
  let lastTime = 0
  let budget = 0
  let finishing = false
  let finishTimer: ReturnType<typeof setTimeout> | null = null
  let resolveDrain: (() => void) | null = null

  function settle() {
    if (finishTimer) { clearTimeout(finishTimer); finishTimer = null }
    const resolve = resolveDrain
    resolveDrain = null
    resolve?.()
  }

  function emit(count: number) {
    let n = Math.min(count, buffer.length)
    // 不拆开 UTF-16 代理对（emoji 等）
    const code = buffer.charCodeAt(n - 1)
    if (n < buffer.length && code >= 0xd800 && code <= 0xdbff) n += 1
    const chunk = buffer.slice(0, n)
    buffer = buffer.slice(n)
    apply(chunk)
  }

  function tick(now: number) {
    frame = 0
    if (!buffer) { lastTime = 0; budget = 0; if (finishing) settle(); return }
    const elapsed = lastTime ? Math.min(100, Math.max(0, now - lastTime)) : 16
    lastTime = now
    const target = finishing ? finishSeconds : catchUp
    const cps = Math.max(finishing ? minCps * 4 : minCps, buffer.length / target)
    budget += (cps * elapsed) / 1000
    const count = Math.floor(budget)
    if (count >= 1) {
      budget -= count
      emit(count)
    }
    if (buffer) schedule()
    else { lastTime = 0; budget = 0; if (finishing) settle() }
  }

  function schedule() {
    if (!frame) frame = requestFrame(tick)
  }

  function flush() {
    if (frame) { cancelFrame(frame); frame = 0 }
    lastTime = 0
    budget = 0
    if (buffer) {
      const rest = buffer
      buffer = ''
      apply(rest)
    }
    settle()
  }

  return {
    push(text: string) {
      if (!text) return
      buffer += text
      schedule()
    },
    finish() {
      finishing = true
      if (!buffer) { settle(); return Promise.resolve() }
      return new Promise<void>((resolve) => {
        const previous = resolveDrain
        resolveDrain = () => { previous?.(); resolve() }
        if (!finishTimer) finishTimer = setTimeout(flush, finishTimeoutMs)
        schedule()
      })
    },
    flush,
    get pending() { return buffer.length }
  }
}
