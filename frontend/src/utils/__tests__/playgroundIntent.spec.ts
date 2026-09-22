import { describe, expect, it } from 'vitest'
import { resolvePlaygroundIntent } from '../playgroundIntent'

describe('resolvePlaygroundIntent', () => {
  it.each([
    '画一张日落时的海边小屋',
    '可以帮我画一只猫吗？',
    '帮我设计一个咖啡馆 logo',
    '生成一幅水彩画',
    '制作产品宣传海报',
    '来一张雪山照片',
    '帮我生图：沙漠中的城堡',
    '根据上面的描述出图',
    '请画图',
    '画三张猫咪图片',
    '出2张海报',
    'Draw a cat in watercolor',
    'Create an image of a seaside cottage',
    'Design a logo for a coffee shop'
  ])('routes an explicit image request: %s', prompt => {
    expect(resolvePlaygroundIntent(prompt)).toBe('image')
  })

  it.each([
    '你好，帮我规划明天的工作',
    '怎么生成图片？',
    '给我写一个画猫的提示词',
    '不要生成图片，写一段故事',
    '解释这张图片中的内容',
    'Describe this image',
    'Write a prompt to generate an image',
    "Don't draw anything, write a story",
    ''
  ])('keeps text requests in chat: %s', prompt => {
    expect(resolvePlaygroundIntent(prompt, 'image')).toBe('chat')
  })

  it('uses previous image context for a revision, without treating all follow-ups as images', () => {
    expect(resolvePlaygroundIntent('背景换成蓝色，更写实一些', 'image')).toBe('image')
    expect(resolvePlaygroundIntent('背景换成蓝色，更写实一些', 'chat')).toBe('chat')
    expect(resolvePlaygroundIntent('我们接下来聊聊旅行', 'image')).toBe('chat')
  })
})
