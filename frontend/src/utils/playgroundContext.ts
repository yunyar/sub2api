import type { PlaygroundMessage, PlaygroundMultimodalMessage, PlaygroundModel } from '@/api/playground'

type ContextMessage = PlaygroundMessage | PlaygroundMultimodalMessage

function estimateTextTokens(text: string): number {
  let asciiLength = 0
  for (let index = 0; index < text.length; index += 1) {
    if (text.charCodeAt(index) < 128) asciiLength += 1
  }
  return Math.ceil(asciiLength / 3) + text.length - asciiLength
}

function estimateMessageTokens(message: ContextMessage): number {
  if (typeof message.content === 'string') return 8 + estimateTextTokens(message.content)
  return 8 + message.content.reduce((total, part) =>
    total + (part.type === 'text' ? estimateTextTokens(part.text) : 4096), 0)
}

export function selectPlaygroundContext(
  history: ContextMessage[],
  systemPrompt: string,
  model?: PlaygroundModel,
): ContextMessage[] | null {
  const windows = [model?.context_window, model?.max_context_window]
    .filter((value): value is number => typeof value === 'number' && Number.isFinite(value) && value > 0)
  const window = Math.min(256_000, ...windows)
  const outputReserve = Math.min(8_192, Math.floor(window / 8))
  const system: PlaygroundMessage = { role: 'system', content: systemPrompt }
  let available = window - outputReserve - estimateMessageTokens(system)
  let start = history.length
  for (let index = history.length - 1; index >= 0; index -= 1) {
    available -= estimateMessageTokens(history[index])
    if (available < 0) break
    if (history[index].role === 'user') start = index
  }
  if (start === history.length) return null
  return [...(systemPrompt ? [system] : []), ...history.slice(start)]
}
