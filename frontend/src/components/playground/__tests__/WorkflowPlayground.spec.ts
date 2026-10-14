import { flushPromises, mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Conversation } from '@/api/playgroundHistory'
import WorkflowPlayground from '../WorkflowPlayground.vue'

const state = vi.hoisted(() => ({
  list: vi.fn(),
  save: vi.fn(),
  remove: vi.fn(),
  streamChat: vi.fn(),
  generateImages: vi.fn(),
  cacheList: vi.fn(),
  cachePut: vi.fn(),
  cacheDeleteStep: vi.fn(),
  cacheDeleteExpired: vi.fn(),
  refreshUser: vi.fn(),
  balance: 10,
  account: null as ReturnType<typeof ref<number>> | null,
  PartialImageError: class PartialImageError extends Error {
    constructor(message: string, readonly images: unknown[], readonly status?: number) { super(message) }
  },
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores', () => ({
  useAuthStore: () => ({ user: { get id() { return state.account?.value ?? 1 }, get balance() { return state.balance } }, refreshUser: state.refreshUser }),
}))
vi.mock('@/api/playground', () => ({
  PlaygroundImageGenerationError: state.PartialImageError,
  playgroundAPI: { streamChat: state.streamChat, generateImages: state.generateImages },
}))
vi.mock('@/api/playgroundHistory', () => ({
  playgroundHistory: { list: state.list, save: state.save, delete: state.remove },
}))
vi.mock('@/utils/playgroundImageCache', () => ({
  playgroundImageBlob: async (url: string) => new Blob([url]),
  playgroundImageCache: { listConversation: state.cacheList, put: state.cachePut, deleteStep: state.cacheDeleteStep, deleteExpired: state.cacheDeleteExpired },
}))
vi.mock('@/utils/playgroundId', () => {
  let id = 0
  return { createPlaygroundId: () => `id-${++id}` }
})

const props = { groups: [{ id: 7 }], groupId: 7, chatModel: 'chat-1', imageModel: 'image-1' }

function conversation(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: 'workflow-1',
    title: 'Saved workflow',
    groupId: 7,
    model: 'chat-1',
    imageModel: 'image-1',
    kind: 'workflow',
    workflow: { steps: [{ id: 'step-1', prompt: 'first' }, { id: 'step-2', prompt: 'second' }], currentStep: 1 },
    systemPrompt: '',
    temperature: 0.7,
    messages: [{ id: 1, role: 'assistant', content: 'restored output', model: 'chat-1', kind: 'chat', stepId: 'step-1' }],
    revision: 2,
    expiresAt: Date.now() + 60_000,
    ...overrides,
  }
}

function mountWorkflow(input = props) {
  return mount(WorkflowPlayground, { props: input })
}

function button(wrapper: ReturnType<typeof mount>, label: string) {
  return wrapper.findAll('button').find((item) => item.text().includes(label))
}

async function enterFirstPrompt(wrapper: ReturnType<typeof mount>, value = 'first prompt') {
  if (/^(draw|generate)/i.test(value)) await wrapper.get('select').setValue('image')
  await wrapper.find('textarea').setValue(value)
}

beforeEach(() => {
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'data:image/png;base64,cached') })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(new Blob(['image']))))
  state.list.mockResolvedValue([])
  state.save.mockImplementation(async (value: Conversation) => ({ ...value, revision: value.revision + 1, expiresAt: Date.now() + 3_600_000 }))
  state.remove.mockResolvedValue(undefined)
  state.streamChat.mockImplementation(async (input: { onDelta: (delta: string) => void }) => input.onDelta('chat result'))
  state.generateImages.mockImplementation(async (input: { onImage?: (image: { url: string; revisedPrompt: string }) => void | Promise<void> }) => {
    const image = { url: 'https://images.example/generated.png', revisedPrompt: 'image result' }
    await input.onImage?.(image)
    return [image]
  })
  state.cacheList.mockResolvedValue([])
  state.cachePut.mockResolvedValue(undefined)
  state.cacheDeleteStep.mockResolvedValue(undefined)
  state.cacheDeleteExpired.mockResolvedValue(undefined)
  state.refreshUser.mockResolvedValue(undefined)
  state.balance = 10
  state.account = ref(1)
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('WorkflowPlayground', () => {
  it('uses props that become valid after an initial group zero', async () => {
    const wrapper = mountWorkflow({ ...props, groupId: 0 })
    await flushPromises()
    await wrapper.setProps({ groupId: 7, chatModel: 'chat-7', imageModel: 'image-7' })
    await wrapper.vm.$nextTick()
    await enterFirstPrompt(wrapper)
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(state.streamChat).toHaveBeenCalledWith(expect.objectContaining({ groupId: 7, model: 'chat-7' }))
  })

  it('obeys explicit chat and image modes regardless of prompt wording', async () => {
    const wrapper = mountWorkflow()
    await flushPromises()
    await wrapper.get('select').setValue('chat')
    await wrapper.find('textarea').setValue('draw a lighthouse')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    expect(state.generateImages).not.toHaveBeenCalled()
    expect(state.streamChat).toHaveBeenCalledTimes(1)

    await wrapper.get('select').setValue('image')
    await enterFirstPrompt(wrapper, 'a lighthouse at sunset')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({ count: 1, model: 'image-1' }))
  })

  it('passes multiple text and image references from the shared attachment tray', async () => {
    const attachments = [
      { id: 'text-1', name: 'notes.txt', kind: 'text' as const, text: 'Reference text', size: 14 },
      { id: 'image-1', name: 'scene.png', kind: 'image' as const, dataUrl: 'data:image/png;base64,scene', size: 10 }
    ]
    const wrapper = mountWorkflow({
      ...props,
      attachments
    } as any)
    await flushPromises()
    await button(wrapper, 'playground.attachFiles')!.trigger('click')
    await wrapper.findAll('.workflow-attachment-remove')[0].trigger('click')
    expect(wrapper.emitted('requestAttachments')).toHaveLength(1)
    expect(wrapper.emitted('removeAttachment')?.[0]).toEqual(['text-1'])
    await wrapper.setProps({ ...props, attachments } as any)
    await enterFirstPrompt(wrapper, 'Describe the references')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    const request = state.streamChat.mock.calls[0][0]
    expect(request.messages.at(-1)).toEqual({
      role: 'user',
      content: [
        { type: 'text', text: expect.stringContaining('Reference text') },
        { type: 'image_url', image_url: { url: 'data:image/png;base64,scene' } }
      ]
    })
    expect(wrapper.emitted('consumeAttachments')).toHaveLength(1)
  })

  it('passes multiple image references to image generation and consumes their ids', async () => {
    const attachments = [
      { id: 'reference-1', name: 'one.png', kind: 'image' as const, dataUrl: 'data:image/png;base64,one', size: 10 },
      { id: 'reference-2', name: 'two.png', kind: 'image' as const, dataUrl: 'data:image/png;base64,two', size: 10 },
    ]
    const wrapper = mountWorkflow({ ...props, attachments } as any)
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw a new scene from these references')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({ inputImages: attachments.map(item => item.dataUrl) }))
    expect(wrapper.emitted('consumeAttachments')).toEqual([[['reference-1', 'reference-2']]])
  })

  it('runs separate steps concurrently with partial context and keeps both results', async () => {
    const requests: Array<{ input: any; finish: () => void }> = []
    state.streamChat.mockImplementation((input: any) => new Promise<void>((resolve) => {
      const isFirst = input.messages.at(-1).content === 'first prompt'
      if (isFirst) input.onDelta('partial first result')
      requests.push({
        input,
        finish: () => {
          input.onDelta(isFirst ? ' final first result' : 'second result')
          resolve()
        },
      })
    }))
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper)
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    await button(wrapper, 'workflow.addStep')!.trigger('click')
    await button(wrapper, 'workflow.goTo')!.trigger('click')
    await wrapper.findAll('.workflow-step')[1].find('textarea').setValue('second prompt')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(requests).toHaveLength(2)
    expect(requests[1].input.messages).toEqual([
      { role: 'user', content: 'first prompt' },
      { role: 'assistant', content: 'partial first result' },
      { role: 'user', content: 'second prompt' }
    ])
    requests[1].finish()
    await flushPromises()
    requests[0].finish()
    await flushPromises()

    expect(wrapper.text()).toContain('partial first result final first result')
    expect(wrapper.text()).toContain('second result')
    expect(requests[0].input.signal.aborted).toBe(false)
  })

  it('aborts pending work and ignores its output after an account change', async () => {
    let finish: (() => void) | undefined
    state.streamChat.mockImplementation((input: any) => new Promise<void>((resolve) => {
      finish = () => {
        input.onDelta('private account result')
        resolve()
      }
    }))
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper)
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    const signal = state.streamChat.mock.calls[0][0].signal as AbortSignal

    state.account!.value = 2
    await nextTick()
    finish!()
    await flushPromises()

    expect(signal.aborted).toBe(true)
    expect(wrapper.text()).not.toContain('private account result')
    expect(wrapper.findAll('.workflow-step')).toHaveLength(1)
  })

  it('saves a draft retaining an empty second step', async () => {
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper)
    await button(wrapper, 'workflow.addStep')!.trigger('click')
    await button(wrapper, 'workflow.save')!.trigger('click')
    await flushPromises()

    expect(state.save).toHaveBeenCalledWith(expect.objectContaining({
      workflow: expect.objectContaining({ steps: [
        { id: expect.any(String), prompt: 'first prompt' },
        { id: expect.any(String), prompt: '' },
      ] }),
    }))
  })

  it('does not submit charged work when the initial workflow save fails', async () => {
    state.save.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper)
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(state.streamChat).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('save failed')
  })

  it('uses the manual image count and displays every unique generated image', async () => {
    state.generateImages.mockImplementation(async (input: { onImage?: (image: { url: string; revisedPrompt: string }) => void | Promise<void> }) => {
      const generated = [
        { url: 'https://images.example/one.png', revisedPrompt: 'first image' },
        { url: 'https://images.example/two.png', revisedPrompt: 'second image' },
        { url: 'https://images.example/three.png', revisedPrompt: 'third image' },
      ]
      for (const image of generated) await input.onImage?.(image)
      return generated
    })
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw three images of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({ count: 1 }))
    expect(wrapper.findAll('img')).toHaveLength(3)
  })

  it('replaces a three-image result with the next three-image regeneration', async () => {
    let batch = 0
    state.generateImages.mockImplementation(async (input: { onImage?: (image: { url: string; revisedPrompt: string }) => void | Promise<void> }) => {
      batch += 1
      const generated = Array.from({ length: 3 }, (_, index) => ({
        url: `https://images.example/batch-${batch}-${index}.png`,
        revisedPrompt: `batch ${batch}`,
      }))
      for (const image of generated) await input.onImage?.(image)
      return generated
    })
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw three images of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('textarea')[1].setValue('make them brighter')
    await button(wrapper, 'workflow.revise')!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('img')).toHaveLength(3)
    expect(state.cacheDeleteStep).toHaveBeenCalledTimes(2)
  })

  it('preserves prior images and cached entries when regeneration fails before its first image', async () => {
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw an image of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    state.generateImages.mockRejectedValueOnce(new Error('upstream failed'))
    await wrapper.findAll('textarea')[1].setValue('make it brighter')
    await button(wrapper, 'workflow.revise')!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('img')).toHaveLength(1)
    expect(state.cacheDeleteStep).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('playground.imagesGenerated')
  })

  it('keeps a cache failure visible after the workflow save succeeds', async () => {
    state.cachePut.mockRejectedValue(new Error('storage unavailable'))
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw an image of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(state.save).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('workflow.imageCacheFailed')
  })

  it('shows insufficient balance when the server rejects the selected image count', async () => {
    state.generateImages.mockRejectedValue({ status: 402 })
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw three images')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('workflow.insufficientBalance')
    expect(state.generateImages).toHaveBeenCalledTimes(1)
  })

  it('keeps completed images and shows insufficient balance after a partial plain-object 402', async () => {
    state.generateImages.mockImplementation(async (input: any) => {
      const image = { url: 'https://images.example/completed.png', revisedPrompt: 'completed image' }
      await input.onImage?.(image)
      throw new state.PartialImageError('Insufficient balance for image generation', [image], 402)
    })
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw three images')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('img')).toHaveLength(1)
    expect(wrapper.text()).toContain('workflow.insufficientBalance')
  })

  it('restores workflow result and emits its saved presets', async () => {
    state.list.mockResolvedValue([conversation()])
    const wrapper = mountWorkflow()
    await flushPromises()
    await button(wrapper, 'Saved workflow')!.trigger('click')

    expect(wrapper.text()).toContain('restored output')
    expect(wrapper.emitted('presets')?.[0]).toEqual([{ groupId: 7, chatModel: 'chat-1', imageModel: 'image-1' }])
  })

  it('restores cached images only for the active account and workflow', async () => {
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:restored') })
    state.cacheList.mockResolvedValue([{ id: 'cached-1', accountId: '1', conversationId: 'workflow-1', messageId: 1, stepId: 'step-1', prompt: 'first', expiresAt: Date.now() + 60_000, blob: new Blob(['image']) }])
    state.list.mockResolvedValue([conversation()])
    const wrapper = mountWorkflow()
    await flushPromises()
    await button(wrapper, 'Saved workflow')!.trigger('click')
    await flushPromises()

    expect(state.cacheList).toHaveBeenCalledWith('1', 'workflow-1')
    expect(wrapper.findAll('img')).toHaveLength(1)
  })

  it('uses guidance image counts when regenerating', async () => {
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw an image of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('textarea')[1].setValue('draw three images instead')
    await button(wrapper, 'workflow.revise')!.trigger('click')
    await flushPromises()

    expect(state.generateImages).toHaveBeenLastCalledWith(expect.objectContaining({ count: 1 }))
    expect(state.cacheDeleteStep).toHaveBeenCalled()
  })

  it('uses the latest saved result when regenerating with guidance', async () => {
    state.list.mockResolvedValue([conversation({ workflow: { steps: [{ id: 'step-1', prompt: 'first' }], currentStep: 0 }, messages: [{ id: 1, role: 'assistant', content: 'latest result', model: 'chat-1', kind: 'chat', stepId: 'step-1' }] })])
    const wrapper = mountWorkflow()
    await flushPromises()
    await button(wrapper, 'Saved workflow')!.trigger('click')
    await wrapper.findAll('textarea')[1].setValue('make it shorter')
    await button(wrapper, 'workflow.revise')!.trigger('click')
    await flushPromises()

    expect(state.streamChat).toHaveBeenCalledWith(expect.objectContaining({
      messages: expect.arrayContaining([expect.objectContaining({ content: expect.stringContaining('Previous result:\nlatest result') })]),
    }))
  })

  it('keeps the current step after a failed next-step request', async () => {
    state.list.mockResolvedValue([conversation()])
    state.streamChat.mockRejectedValueOnce(new Error('next failed'))
    const wrapper = mountWorkflow()
    await flushPromises()
    await button(wrapper, 'Saved workflow')!.trigger('click')
    await button(wrapper, 'workflow.goTo')!.trigger('click')
    await button(wrapper, 'workflow.next')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('next failed')
    expect(wrapper.findAll('.workflow-step.current')[0].text()).toContain('workflow.step')
  })

  it('preserves a successful result and guidance after regeneration fails', async () => {
    state.list.mockResolvedValue([conversation({ workflow: { steps: [{ id: 'step-1', prompt: 'first' }], currentStep: 0 }, messages: [{ id: 1, role: 'assistant', content: 'good result', model: 'chat-1', kind: 'chat', stepId: 'step-1' }] })])
    state.streamChat.mockRejectedValueOnce(new Error('regenerate failed'))
    const wrapper = mountWorkflow()
    await flushPromises()
    await button(wrapper, 'Saved workflow')!.trigger('click')
    await wrapper.findAll('textarea')[1].setValue('keep tone')
    await button(wrapper, 'workflow.revise')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('good result')
    expect((wrapper.findAll('textarea')[1].element as HTMLTextAreaElement).value).toBe('keep tone')
  })

  it('passes a live image as a multimodal user message to the next chat step', async () => {
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'generate an image of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    await button(wrapper, 'workflow.addStep')!.trigger('click')
    await wrapper.findAll('.workflow-step')[1].find('textarea').setValue('describe it')
    await button(wrapper, 'workflow.next')!.trigger('click')
    await new Promise(resolve => setTimeout(resolve, 0))
    await flushPromises()

    expect(state.streamChat).toHaveBeenCalledWith(expect.objectContaining({
      messages: expect.arrayContaining([expect.objectContaining({ role: 'user', content: expect.arrayContaining([expect.objectContaining({ type: 'image_url' })]) })]),
    }))
  })

  it('keeps available text context when prior image references expire', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    state.cachePut.mockRejectedValueOnce(new Error('storage unavailable'))
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'generate an image of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    await button(wrapper, 'workflow.addStep')!.trigger('click')
    await wrapper.findAll('.workflow-step')[1].find('textarea').setValue('describe it')
    vi.setSystemTime(Date.now() + 600_001)
    await button(wrapper, 'workflow.next')!.trigger('click')
    await flushPromises()

    expect(state.streamChat).toHaveBeenCalledWith(expect.objectContaining({
      messages: expect.arrayContaining([
        expect.objectContaining({ role: 'user', content: expect.stringContaining('generate an image of a mountain') }),
        expect.objectContaining({ role: 'user', content: 'describe it' })
      ])
    }))
    expect(JSON.stringify(state.streamChat.mock.calls[0][0].messages)).not.toContain('base64')
    expect(wrapper.text()).toContain('workflow.imageExpired')
    vi.useRealTimers()
  })

  it('shows the balance hint and does not submit with no balance', async () => {
    state.balance = 0
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper)
    await button(wrapper, 'workflow.run')!.trigger('click')

    expect(wrapper.text()).toContain('workflow.insufficientBalance')
    expect(state.streamChat).not.toHaveBeenCalled()
  })

  it('keeps step navigation available while a save is pending', async () => {
    let resolveSave: (() => void) | undefined
    state.save.mockImplementationOnce((value: Conversation) => new Promise<Conversation>((resolve) => {
      resolveSave = () => resolve({ ...value, revision: value.revision + 1, expiresAt: Date.now() + 60_000 })
    }))
    const wrapper = mountWorkflow()
    await flushPromises()
    await button(wrapper, 'workflow.save')!.trigger('click')
    await wrapper.vm.$nextTick()

    await button(wrapper, 'workflow.new')!.trigger('click')
    await enterFirstPrompt(wrapper, 'new workflow prompt')
    await button(wrapper, 'workflow.save')!.trigger('click')
    resolveSave!()
    await flushPromises()

    expect(state.save).toHaveBeenCalledTimes(2)
    expect(state.save.mock.calls[1][0]).toEqual(expect.objectContaining({
      title: 'new workflow prompt',
      revision: 0,
      workflow: expect.objectContaining({ steps: [{ id: expect.any(String), prompt: 'new workflow prompt' }] }),
    }))
  })
})
