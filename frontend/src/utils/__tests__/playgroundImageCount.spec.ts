import { describe, expect, it } from 'vitest'
import { resolvePlaygroundImageCount } from '../playgroundImageCount'

describe('resolvePlaygroundImageCount', () => {
  it.each([
    ['生成三张海报', 3], ['画2张猫的图片', 2], ['出十张图', 10],
    ['Create four images of a house', 4], ['Draw 3 pictures', 3], ['生成三张，改成两张', 2]
  ])('reads explicit count from %s', (prompt, count) => {
    expect(resolvePlaygroundImageCount(String(prompt), 1)).toEqual({ count, explicit: true, error: null })
  })

  it.each(['画一只猫', '画2026年夏天的风景', '两个人的合照', '画一个有三张桌子的房间'])('retains manual count for %s', prompt => {
    expect(resolvePlaygroundImageCount(prompt, 4)).toEqual({ count: 4, explicit: false, error: null })
  })

  it.each(['生成11张图片', '生成二十张图片', 'Create twelve images'])('rejects excessive count instead of silently charging fewer: %s', prompt => {
    expect(resolvePlaygroundImageCount(prompt).error).toBe('too_many')
  })

  it.each(['生成0张图', '生成-2张图片', '生成1.5张图', 'Create -2 images', 'Create 1.5 images'])('rejects invalid count: %s', prompt => {
    expect(resolvePlaygroundImageCount(prompt).error).toBe('invalid')
  })

  it('validates manual values too', () => {
    expect(resolvePlaygroundImageCount('画猫', 11).error).toBe('too_many')
    expect(resolvePlaygroundImageCount('画猫', Number.NaN).error).toBe('invalid')
    expect(resolvePlaygroundImageCount('画猫', 0).error).toBe('invalid')
  })
})
