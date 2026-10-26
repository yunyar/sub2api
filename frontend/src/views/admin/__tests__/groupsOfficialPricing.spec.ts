import { describe, expect, it } from 'vitest'

import {
  countDomesticTokenModelOverrides,
  isDomesticModelId,
  removeDomesticTokenModelOverrides,
  restoreVerifiedDomesticTokenModelOverrides,
} from '../groupsOfficialPricing'

describe('group domestic official pricing action', () => {
  it('recognizes explicit domestic model families and exact provider-qualified IDs', () => {
    for (const model of [
      'kimi-k2',
      'moonshot-v1-128k',
      'glm-4.5',
      'deepseek-r1',
      'minimax-m2',
      'qwen3-235b-a22b',
      'doubao-pro-32k',
      'hunyuan-t1-latest',
      'hy3',
      'hy4-preview',
      'deepseek/deepseek-chat',
      'moonshotai/kimi-k2',
      'moonshotai/kimi-k3.1-pro',
      'minimax/minimax-m3',
      'z_ai/glm-5.3',
    ]) {
      expect(isDomesticModelId(model)).toBe(true)
    }
  })

  it('does not match accidental prefixes or broad and unrelated wildcards', () => {
    for (const model of [
      'hybrid-model',
      'hy5-preview',
      'qwenish-large',
      'deep*',
      '*',
      '*deepseek*',
      'other-provider/deepseek-chat',
      'deepseek/custom-model',
    ]) {
      expect(isDomesticModelId(model)).toBe(false)
    }
    expect(isDomesticModelId('qwen3-*')).toBe(true)
    expect(isDomesticModelId('deepseek-*')).toBe(true)
  })

  it('removes only domestic models from token cards and preserves mixed-card fields', () => {
    const entries = [
      {
        models: ['gpt-5', 'deepseek-chat', 'claude-sonnet-4'],
        billing_mode: 'token',
        input_price: 2,
        output_price: 8,
        custom: { keep: true },
      },
    ]

    const result = removeDomesticTokenModelOverrides(entries)

    expect(result.removedCount).toBe(1)
    expect(result.entries).toEqual([
      {
        models: ['gpt-5', 'claude-sonnet-4'],
        billing_mode: 'token',
        input_price: 2,
        output_price: 8,
        custom: { keep: true },
      },
    ])
    expect(entries[0].models).toEqual([
      'gpt-5',
      'deepseek-chat',
      'claude-sonnet-4',
    ])
  })

  it('removes domestic-only token cards but leaves non-token cards unchanged', () => {
    const mediaCard = {
      models: ['kimi-k2', 'gpt-image-1'],
      billing_mode: 'image',
      per_request_price: 0.04,
      image_input_price: 3,
    }
    const videoCard = {
      models: ['deepseek-chat'],
      billing_mode: 'video',
      per_request_price: 0.2,
    }
    const requestCard = {
      models: ['qwen-plus'],
      billing_mode: 'per_request',
      per_request_price: 0.01,
    }
    const entries = [
      { models: ['kimi-k2'], billing_mode: 'token', input_price: 1 },
      mediaCard,
      videoCard,
      requestCard,
    ]

    const result = removeDomesticTokenModelOverrides(entries)

    expect(result.removedCount).toBe(1)
    expect(result.entries).toEqual([mediaCard, videoCard, requestCard])
    expect(result.entries[0]).toBe(mediaCard)
    expect(result.entries[1]).toBe(videoCard)
    expect(result.entries[2]).toBe(requestCard)
  })

  it('counts only domestic models in token cards and leaves inputs intact when no matches exist', () => {
    const entries = [
      { models: ['qwen-plus', 'gpt-5'], billing_mode: 'token' },
      { models: ['deepseek-chat'], billing_mode: 'per_request' },
      { models: ['*'], billing_mode: 'token' },
    ]

    expect(countDomesticTokenModelOverrides(entries)).toBe(1)
    expect(removeDomesticTokenModelOverrides(entries)).toEqual({
      entries: [
        { models: ['gpt-5'], billing_mode: 'token' },
        entries[1],
        entries[2],
      ],
      removedCount: 1,
    })
    expect(entries[0].models).toEqual(['qwen-plus', 'gpt-5'])

    const noMatches = [{ models: ['gpt-5'], billing_mode: 'token' }]
    const result = removeDomesticTokenModelOverrides(noMatches)
    expect(result.removedCount).toBe(0)
    expect(result.entries).toEqual(noMatches)
    expect(result.entries[0]).toBe(noMatches[0])
  })

  it('treats omitted token mode consistently with the backend default', () => {
    expect(removeDomesticTokenModelOverrides([{ models: ['kimi-k3.1-pro'] }])).toEqual({
      entries: [],
      removedCount: 1,
    })
  })

  it('keeps manual prices when official coverage is missing or the lookup fails', async () => {
    const entries = [{
      models: ['kimi-k3.1-pro', 'hy4', 'qwen3.8-max', 'deepseek-*', 'glm-5.3', 'gpt-5'],
      billing_mode: 'token',
      input_price: 10,
    }]
    const queried: string[] = []
    const result = await restoreVerifiedDomesticTokenModelOverrides(entries, async model => {
      queried.push(model)
      if (model === 'glm-5.3') throw new Error('offline')
      return { found: true, official_pricing: model === 'kimi-k3.1-pro' }
    })
    expect(result.removedCount).toBe(1)
    expect(result.entries[0].models).toEqual(['hy4', 'qwen3.8-max', 'deepseek-*', 'glm-5.3', 'gpt-5'])
    expect(result.entries[0].input_price).toBe(10)
    expect(entries[0].models).toContain('kimi-k3.1-pro')
    expect(queried).not.toContain('deepseek-*')
    expect(queried).not.toContain('gpt-5')
  })
})
