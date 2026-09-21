import { describe, expect, it } from 'vitest'
import { isPlaygroundImageModel, isPlaygroundVideoModel } from '../playgroundModel'

describe('isPlaygroundImageModel', () => {
  it.each([
    'gpt-image-2',
    'gemini-3.1-flash-image',
    'dall-e-3',
    'imagen-4',
    'grok-imagine-1.0',
    'flux-1.1-pro',
    'recraft-v3'
  ])('classifies %s as an image model', model => {
    expect(isPlaygroundImageModel(model)).toBe(true)
  })

  it.each(['gpt-5.5', 'claude-sonnet-4-6', 'gemini-3-pro', 'deepseek-v3'])('keeps %s in chat mode', model => {
    expect(isPlaygroundImageModel(model)).toBe(false)
  })

  it.each(['grok-imagine-video', 'doubao-seedance-1-0-pro', 'veo-3.1', 'sora-2'])('excludes video model %s from image requests', model => {
    expect(isPlaygroundVideoModel(model)).toBe(true)
    expect(isPlaygroundImageModel(model)).toBe(false)
  })
})
