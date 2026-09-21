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
  refreshUser: vi.fn(),
  balance: 10,
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores', () => ({
  useAuthStore: () => ({ user: { get balance() { return state.balance } }, refreshUser: state.refreshUser }),
}))
vi.mock('@/api/playground', () => ({
  playgroundAPI: { streamChat: state.streamChat, generateImages: state.generateImages },
}))
vi.mock('@/api/playgroundHistory', () => ({
  playgroundHistory: { list: state.list, save: state.save, delete: state.remove },
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
  state.list.mockResolvedValue([])
  state.save.mockImplementation(async (value: Conversation) => ({ ...value, revision: value.revision + 1, expiresAt: Date.now() + 3_600_000 }))
  state.remove.mockResolvedValue(undefined)
  state.streamChat.mockImplementation(async (input: { onDelta: (delta: string) => void }) => input.onDelta('chat result'))
  state.generateImages.mockResolvedValue([{ url: 'https://images.example/generated.png', revisedPrompt: 'image result' }])
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

  it('restores workflow result and emits its saved presets', async () => {
    state.list.mockResolvedValue([conversation()])
    const wrapper = mountWorkflow()
    await flushPromises()
    await button(wrapper, 'Saved workflow')!.trigger('click')

    expect(wrapper.text()).toContain('restored output')
    expect(wrapper.emitted('presets')?.[0]).toEqual([{ groupId: 7, chatModel: 'chat-1', imageModel: 'image-1' }])
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
