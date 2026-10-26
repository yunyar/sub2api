export interface GroupPricingEntryLike {
  models: string[]
  billing_mode?: string
}

export interface DomesticTokenPricingRemoval<T extends GroupPricingEntryLike> {
  entries: T[]
  removedCount: number
}

const domesticModelPrefix =
  /^(?:kimi|moonshot|glm|deepseek|minimax|doubao|hunyuan)(?=$|[-_.:*])|^qwen(?:[0-9]+(?:\.[0-9]+)?)?(?=$|[-_.:*])|^hy[34](?=$|[-_.:*])/i

const providerModelPrefixes: Record<string, RegExp> = {
  'bytedance-seed': /^doubao[-_.]/,
  deepseek: /^deepseek[-_.]/,
  minimax: /^minimax[-_.]/,
  moonshot: /^(?:kimi|moonshot)[-_.]/,
  moonshotai: /^(?:kimi|moonshot)[-_.]/,
  kimi: /^kimi[-_.]/,
  qwen: /^qwen(?:[0-9]+(?:\.[0-9]+)?)?[-_.]/,
  dashscope: /^qwen(?:[0-9]+(?:\.[0-9]+)?)?[-_.]/,
  tencent: /^hunyuan[-_.]/,
  'z-ai': /^glm[-_.]/,
  z_ai: /^glm[-_.]/,
  zai: /^glm[-_.]/,
  zhipu: /^glm[-_.]/,
}

export function isDomesticModelId(model: string): boolean {
  const normalized = model.trim().toLowerCase()
  if (!normalized) return false
  if (normalized.includes('/')) {
    const parts = normalized.split('/')
    return parts.length === 2 && providerModelPrefixes[parts[0]]?.test(parts[1]) === true
  }
  return domesticModelPrefix.test(normalized)
}

export function countDomesticTokenModelOverrides<T extends GroupPricingEntryLike>(
  entries: readonly T[],
): number {
  return entries.reduce((count, entry) => {
    if ((entry.billing_mode || 'token') !== 'token') return count
    return count + entry.models.filter(isDomesticModelId).length
  }, 0)
}

export function removeDomesticTokenModelOverrides<T extends GroupPricingEntryLike>(
  entries: readonly T[],
  verifiedModels?: ReadonlySet<string>,
): DomesticTokenPricingRemoval<T> {
  let removedCount = 0
  const nextEntries: T[] = []

  for (const entry of entries) {
    if ((entry.billing_mode || 'token') !== 'token') {
      nextEntries.push(entry)
      continue
    }

    const models = entry.models.filter((model) => {
      const domestic = isDomesticModelId(model) &&
        (!verifiedModels || verifiedModels.has(model.trim().toLowerCase()))
      if (domestic) removedCount += 1
      return !domestic
    })

    if (models.length === entry.models.length) {
      nextEntries.push(entry)
    } else if (models.length > 0) {
      nextEntries.push({ ...entry, models })
    }
  }

  return { entries: nextEntries, removedCount }
}

export async function restoreVerifiedDomesticTokenModelOverrides<T extends GroupPricingEntryLike>(
  entries: readonly T[],
  lookup: (model: string) => Promise<{ found: boolean; official_pricing?: boolean }>,
): Promise<DomesticTokenPricingRemoval<T>> {
  const models = [...new Set(entries
    .filter(entry => (entry.billing_mode || 'token') === 'token')
    .flatMap(entry => entry.models)
    .map(model => model.trim().toLowerCase())
    .filter(model => isDomesticModelId(model) && !model.includes('*')))]
  const verified = new Set<string>()
  for (let offset = 0; offset < models.length; offset += 4) {
    const batch = models.slice(offset, offset + 4)
    const results = await Promise.allSettled(batch.map(model => lookup(model)))
    results.forEach((result, index) => {
      if (result.status === 'fulfilled' && result.value.found && result.value.official_pricing) {
        verified.add(batch[index])
      }
    })
  }
  return removeDomesticTokenModelOverrides(entries, verified)
}
