export const MAX_PLAYGROUND_IMAGE_COUNT = 10

export interface PlaygroundImageCount {
  count: number
  explicit: boolean
  error: 'invalid' | 'too_many' | null
}

function parseCount(value: string): number {
  if (/^\d+$/.test(value)) return Number(value)
  const words: Record<string, number> = {
    零: 0, 一: 1, 二: 2, 两: 2, 三: 3, 四: 4, 五: 5, 六: 6, 七: 7, 八: 8, 九: 9,
    one: 1, two: 2, three: 3, four: 4, five: 5, six: 6, seven: 7, eight: 8,
    nine: 9, ten: 10, eleven: 11, twelve: 12
  }
  if (value in words) return words[value]
  const tens = value.split('十')
  if (tens.length === 2) return (tens[0] ? words[tens[0]] : 1) * 10 + (tens[1] ? words[tens[1]] : 0)
  return Number.NaN
}

export function resolvePlaygroundImageCount(prompt: string, manualCount = 1): PlaygroundImageCount {
  const matches = [...prompt.toLowerCase().matchAll(/(-?\d+(?:\.\d+)?|[零一二两三四五六七八九十百千]+)\s*(?:张|幅)(?!嘴|脸|桌)|(?<![\w.])(-?\d+(?:\.\d+)?|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(?:images?|pictures?|photos?|illustrations?|posters?|variations?)\b/g)]
  const counts = matches.map(match => parseCount(match[1] || match[2]))
  const explicit = counts.length > 0
  const count = explicit ? counts[counts.length - 1] : Number(manualCount)
  const invalid = !Number.isInteger(count) || count < 1
  return {
    count,
    explicit,
    error: invalid ? 'invalid' : count > MAX_PLAYGROUND_IMAGE_COUNT ? 'too_many' : null
  }
}
