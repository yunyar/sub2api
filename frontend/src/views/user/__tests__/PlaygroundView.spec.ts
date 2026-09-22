import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PlaygroundView from '../PlaygroundView.vue'

const state = vi.hoisted(() => ({
  getAvailable: vi.fn(),
  listModels: vi.fn(),
  generateImages: vi.fn(),
  streamChat: vi.fn(),
  list: vi.fn(),
  save: vi.fn(),
  remove: vi.fn(),
  refreshUser: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  balance: 10,
  cachePut: vi.fn(),
  cacheList: vi.fn(),
  cacheDeleteConversation: vi.fn(),
  cacheDeleteExpired: vi.fn(),
  accountId: undefined as number | undefined,
  PartialImageError: class PartialImageError extends Error {
    constructor(message: string, readonly images: unknown[], readonly status?: number) { super(message) }
  },
  nextId: 0
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/playground/WorkflowPlayground.vue', () => ({
  default: {
    emits: ['presets'],
    template: '<button data-testid="workflow-presets" @click="$emit(\'presets\', { groupId: 7, chatModel: \'workflow-chat\', imageModel: \'workflow-image\' })" />'
  }
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => ({ user: { get id() { return state.accountId }, get balance() { return state.balance } }, refreshUser: state.refreshUser }),
  useAppStore: () => ({ showError: state.showError, showSuccess: state.showSuccess })
}))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getAvailable: state.getAvailable } }))
vi.mock('@/api/playground', () => ({
  PlaygroundImageGenerationError: state.PartialImageError,
  playgroundAPI: {
    listModels: state.listModels,
    generateImages: state.generateImages,
    streamChat: state.streamChat
  }
}))
vi.mock('@/api/playgroundHistory', () => ({
  playgroundHistory: { list: state.list, save: state.save, delete: state.remove }
}))
vi.mock('@/utils/playgroundModel', () => ({
  isPlaygroundImageModel: (model: string) => model.includes('image'),
  isPlaygroundVideoModel: (model: string) => model === 'video-model'
}))
vi.mock('@/utils/playgroundIntent', () => ({
  resolvePlaygroundIntent: (prompt: string, previous?: string) => prompt.startsWith('draw') || (previous === 'image' && prompt === 'brighter') ? 'image' : 'chat'
}))
vi.mock('@/utils/playgroundId', () => ({ createPlaygroundId: () => `id-${++state.nextId}` }))
vi.mock('@/utils/playgroundImageCache', () => ({
  playgroundImageBlob: async (url: string) => new Blob([url]),
  playgroundImageCache: {
    put: state.cachePut,
    listConversation: state.cacheList,
    deleteConversation: state.cacheDeleteConversation,
    deleteExpired: state.cacheDeleteExpired
  }
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function mountView() {
  return mount(PlaygroundView)
}

describe('PlaygroundView model and intent routing', () => {
  beforeEach(() => {
    state.nextId = 0
    state.balance = 10
    state.accountId = undefined
    state.getAvailable.mockResolvedValue([{ id: 7, name: 'Default' }])
    state.listModels.mockResolvedValue([
      { id: 'chat-model' },
      { id: 'image-model' },
      { id: 'workflow-chat' },
      { id: 'workflow-image' },
      { id: 'video-model' }
    ])
    state.list.mockResolvedValue([])
    state.save.mockReset()
    state.save.mockImplementation(async (conversation: any) => ({ ...conversation, revision: 1, expiresAt: Date.now() + 604800000 }))
    state.generateImages.mockReset()
    state.streamChat.mockReset()
    state.cachePut.mockResolvedValue('image')
    state.cacheList.mockResolvedValue([])
    state.cacheDeleteConversation.mockResolvedValue(undefined)
    state.cacheDeleteExpired.mockResolvedValue(undefined)
    state.refreshUser.mockResolvedValue(undefined)
  })

  it('keeps video models out of chat and image presets', async () => {
    const wrapper = mountView()
    await flushPromises()

    const selects = wrapper.findAll('select')
    expect(selects[1].text()).toContain('chat-model')
    expect(selects[1].text()).not.toContain('video-model')
    expect(selects[2].text()).toContain('image-model')
    expect(selects[2].text()).not.toContain('video-model')
  })

  it('uses prompt intent and the image preset instead of the selected chat model', async () => {
    state.generateImages.mockResolvedValue([{ url: 'data:image/png;base64,image' }])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({
      groupId: 7,
      model: 'image-model',
      prompt: 'draw a lighthouse'
    }))
    expect(state.streamChat).not.toHaveBeenCalled()
  })

  it('shows the existing insufficient-balance warning and blocks requests', async () => {
    state.balance = 0
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')

    expect(wrapper.get('[role="alert"]').text()).toBe('playground.insufficientBalance')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(state.generateImages).not.toHaveBeenCalled()
    expect(state.streamChat).not.toHaveBeenCalled()
  })

  it('uses an explicit natural-language image count over the manual count', async () => {
    state.generateImages.mockResolvedValue([{ url: 'data:image/png;base64,image' }])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw 3 pictures of a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({ count: 3 }))
  })

  it('shows insufficient balance when the server rejects a multi-image request', async () => {
    state.generateImages.mockRejectedValue({ status: 402, message: 'Insufficient balance for image generation' })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('textarea.composer-input').setValue('draw 3 pictures')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('playground.insufficientBalance')
    expect(state.generateImages).toHaveBeenCalledTimes(1)
  })

  it('keeps completed images and shows insufficient balance after a partial plain-object 402', async () => {
    state.generateImages.mockImplementation(async (input: any) => {
      const image = { url: 'data:image/png;base64,completed' }
      await input.onImage?.(image)
      throw new state.PartialImageError('Insufficient balance for image generation', [image], 402)
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('textarea.composer-input').setValue('draw 3 pictures')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.findAll('img')).toHaveLength(1)
    expect(wrapper.text()).toContain('playground.insufficientBalance')
  })

  it('downloads an expired locally cached image', async () => {
    state.accountId = 1
    state.list.mockResolvedValue([{
      id: 'cached-conversation', title: 'Cached image', groupId: 7, model: 'chat-model', imageModel: 'image-model', systemPrompt: '', temperature: 0.7,
      messages: [
        { id: 1, role: 'user', content: 'draw a lighthouse', model: 'image-model', kind: 'image' },
        { id: 2, role: 'assistant', content: 'lighthouse', model: 'image-model', kind: 'image' },
      ], revision: 1, expiresAt: Date.now() + 604800000,
    }])
    state.cacheList.mockResolvedValue([{ id: 'cached-image', accountId: '1', conversationId: 'cached-conversation', messageId: 2, prompt: 'draw a lighthouse', expiresAt: Date.now() - 600_001, blob: new Blob(['image']) }])
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:cached-image') })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
    const fetch = vi.fn().mockResolvedValue(new Response(new Blob(['image'])))
    vi.stubGlobal('fetch', fetch)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.history-item button').trigger('click')
    await flushPromises()
    await wrapper.get('button.text-sm.font-medium').trigger('click')
    await flushPromises()

    expect(fetch).toHaveBeenCalledWith('blob:cached-image')
    expect(click).toHaveBeenCalled()
  })

  it('applies workflow presets without changing the standard conversation presets', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[role="tab"][aria-selected="false"]').trigger('click')
    await wrapper.get('[data-testid="workflow-presets"]').trigger('click')
    await flushPromises()

    const workflowSelects = wrapper.findAll('select')
    expect((workflowSelects[1].element as HTMLSelectElement).value).toBe('workflow-chat')
    expect((workflowSelects[2].element as HTMLSelectElement).value).toBe('workflow-image')

    await wrapper.get('[role="tab"][aria-selected="false"]').trigger('click')
    await flushPromises()

    const standardSelects = wrapper.findAll('select')
    expect((standardSelects[1].element as HTMLSelectElement).value).toBe('chat-model')
    expect((standardSelects[2].element as HTMLSelectElement).value).toBe('image-model')
  })

  it('blocks duplicate sends while the initial history save is pending', async () => {
    let resolveSave!: () => void
    state.save.mockImplementation((conversation: any) => new Promise((resolve) => {
      resolveSave = () => resolve({ ...conversation, revision: 1, expiresAt: Date.now() + 604800000 })
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('form').trigger('submit')

    expect(state.save).toHaveBeenCalledTimes(1)
    expect(wrapper.get('textarea.composer-input').attributes('disabled')).toBeDefined()
    resolveSave()
    await flushPromises()
  })

  it('accepts another saved image request while a multi-image batch is pending', async () => {
    let resolveFirst!: () => void
    state.generateImages.mockImplementation(async (input: any) => {
      if (input.prompt.includes('first')) {
        await new Promise<void>(resolve => { resolveFirst = resolve })
      }
      const image = { url: `data:image/png;base64,${input.prompt}` }
      await input.onImage?.(image)
      return [image]
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw first 3 pictures')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('textarea.composer-input').setValue('draw second picture')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledTimes(2)
    expect(state.save.mock.calls.some(call => call[0].messages.some((message: any) => message.content === 'draw first 3 pictures'))).toBe(true)
    expect(state.save.mock.calls.some(call => call[0].messages.some((message: any) => message.content === 'draw second picture'))).toBe(true)
    resolveFirst()
    await flushPromises()
  })

  it('reseeds numeric message IDs when opening history from a later clock', async () => {
    const futureMessageId = Date.now() + 60_000
    state.list.mockResolvedValue([{
      id: 'history-1',
      title: 'Saved image prompt',
      groupId: 7,
      model: 'chat-model',
      imageModel: 'image-model',
      systemPrompt: '',
      temperature: 0.7,
      messages: [{ id: futureMessageId, role: 'user', content: 'draw a tower' }],
      revision: 4,
      expiresAt: Date.now() + 604800000
    }])
    state.generateImages.mockResolvedValue([{ url: 'data:image/png;base64,image' }])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('.history-item button').trigger('click')
    await wrapper.get('textarea.composer-input').setValue('draw a bridge')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const savedIds = state.save.mock.calls[0][0].messages.map((message: any) => message.id)
    expect(Math.max(...savedIds)).toBeGreaterThan(futureMessageId)
  })

  it('keeps the composer available while image generation is pending', async () => {
    let resolveGeneration!: (value: Array<{ url: string }>) => void
    state.generateImages.mockReturnValue(new Promise((resolve) => { resolveGeneration = resolve }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('textarea.composer-input').attributes('disabled')).toBeUndefined()
    await wrapper.get('textarea.composer-input').setValue('draw another lighthouse')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    resolveGeneration([{ url: 'data:image/png;base64,image' }])
  })

  it('combines the prior image prompt with a follow-up generation request', async () => {
    state.generateImages.mockResolvedValue([{ url: 'data:image/png;base64,image' }])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('textarea.composer-input').setValue('brighter')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenLastCalledWith(expect.objectContaining({
      prompt: 'draw a lighthouse\n\nbrighter'
    }))
  })

  it('keeps the original image subject through two consecutive revisions', async () => {
    state.generateImages.mockResolvedValue([{ url: 'data:image/png;base64,image' }])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('textarea.composer-input').setValue('brighter')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('textarea.composer-input').setValue('brighter')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenLastCalledWith(expect.objectContaining({
      prompt: 'draw a lighthouse\n\nbrighter\n\nbrighter'
    }))
  })

  it('uses numeric message IDs and advances revisions for concurrent image saves', async () => {
    const resolvers: Array<(value: Array<{ url: string }>) => void> = []
    let acceptedRevision = 0
    state.save.mockImplementation(async (conversation: any) => {
      if (conversation.revision !== acceptedRevision) throw new Error('409 revision conflict')
      acceptedRevision += 1
      return { ...conversation, revision: acceptedRevision, expiresAt: Date.now() + 604800000 }
    })
    state.generateImages.mockImplementation(() => new Promise((resolve) => { resolvers.push(resolve) }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('textarea.composer-input').setValue('draw a bridge')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    resolvers[0]([{ url: 'data:image/png;base64,first' }])
    resolvers[1]([{ url: 'data:image/png;base64,second' }])
    await flushPromises()
    await flushPromises()

    expect(state.save.mock.calls.map(([conversation]: [any]) => conversation.revision)).toEqual([0, 1, 2, 3])
    expect(state.save.mock.calls.flatMap(([conversation]: [any]) => conversation.messages).every((message: any) => typeof message.id === 'number')).toBe(true)
    expect(wrapper.text()).not.toContain('playground.saveFailed')
  })
})
