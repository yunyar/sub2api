import { flushPromises, mount } from '@vue/test-utils'
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
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores', () => ({
  useAuthStore: () => ({ user: { id: 1, get balance() { return state.balance } }, refreshUser: state.refreshUser }),
}))
vi.mock('@/api/playground', () => ({
  playgroundAPI: { streamChat: state.streamChat, generateImages: state.generateImages },
}))
vi.mock('@/api/playgroundHistory', () => ({
  playgroundHistory: { list: state.list, save: state.save, delete: state.remove },
}))
vi.mock('@/utils/playgroundImageCache', () => ({
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
  state.generateImages.mockResolvedValue([{ url: 'https://images.example/generated.png', revisedPrompt: 'image result' }])
  state.cacheList.mockResolvedValue([])
  state.cachePut.mockResolvedValue(undefined)
  state.cacheDeleteStep.mockResolvedValue(undefined)
  state.cacheDeleteExpired.mockResolvedValue(undefined)
  state.refreshUser.mockResolvedValue(undefined)
  state.balance = 10
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

  it('uses the natural-language image count and displays every generated image', async () => {
    state.generateImages.mockResolvedValue([
      { url: 'https://images.example/one.png', revisedPrompt: 'first image' },
      { url: 'https://images.example/two.png', revisedPrompt: 'second image' },
      { url: 'https://images.example/three.png', revisedPrompt: 'third image' },
    ])
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'draw three images of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({ count: 3 }))
    expect(wrapper.findAll('img')).toHaveLength(3)
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

    expect(state.generateImages).toHaveBeenLastCalledWith(expect.objectContaining({ count: 3 }))
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

  it('blocks expired images from later context without submitting base64 data', async () => {
    vi.useFakeTimers()
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper, 'generate an image of a mountain')
    await button(wrapper, 'workflow.run')!.trigger('click')
    await flushPromises()
    await button(wrapper, 'workflow.addStep')!.trigger('click')
    await wrapper.findAll('.workflow-step')[1].find('textarea').setValue('describe it')
    await vi.advanceTimersByTimeAsync(600_001)
    await button(wrapper, 'workflow.next')!.trigger('click')
    await flushPromises()

    expect(state.streamChat).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('workflow.regenerateDependency')
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

  it('locks controls while a save is pending', async () => {
    let resolveSave: ((value: Conversation) => void) | undefined
    state.save.mockImplementationOnce(() => new Promise<Conversation>((resolve) => { resolveSave = resolve }))
    const wrapper = mountWorkflow()
    await flushPromises()
    await enterFirstPrompt(wrapper)
    await button(wrapper, 'workflow.save')!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(button(wrapper, 'workflow.addStep')!.attributes('disabled')).toBeDefined()
    resolveSave!({ ...conversation(), revision: 1, expiresAt: Date.now() + 60_000 })
    await flushPromises()
  })
})
