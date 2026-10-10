import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
  preferenceMode: 'auto',
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
vi.mock('@/stores', async () => {
  const { ref } = await import('vue')
  const accountId = ref<number | undefined>(state.accountId)
  Object.defineProperty(state, 'accountId', {
    configurable: true,
    get: () => accountId.value,
    set: (value: number | undefined) => { accountId.value = value }
  })
  return {
    useAuthStore: () => ({ user: { get id() { return accountId.value }, get balance() { return state.balance } }, refreshUser: state.refreshUser }),
    useAppStore: () => ({ showError: state.showError, showSuccess: state.showSuccess })
  }
})
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
vi.mock('@/utils/playgroundPreferences', () => ({
  loadPlaygroundPreferences: () => ({ groupId: 0, chatModel: '', imageModel: '', imageMode: state.preferenceMode }),
  savePlaygroundPreferences: vi.fn()
}))
vi.mock('@/utils/playgroundModel', () => ({
  isPlaygroundImageModel: (model: string) => model.includes('image'),
  isPlaygroundVideoModel: (model: string) => model === 'video-model'
}))
vi.mock('@/utils/playgroundIntent', () => ({
  resolvePlaygroundIntent: (prompt: string, previous?: string, mode = 'auto') =>
    mode !== 'auto' ? mode : prompt.startsWith('draw') || (previous === 'image' && prompt === 'brighter') ? 'image' : 'chat'
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
  const wrapper = mount(PlaygroundView)
  mountedWrappers.push(wrapper)
  return wrapper
}

const mountedWrappers: Array<ReturnType<typeof mount>> = []

function createFile(name: string, bytes: number[] | string, type = '') {
  const content = typeof bytes === 'string' ? new TextEncoder().encode(bytes) : Uint8Array.from(bytes)
  const file = new File([content], name, { type })
  Object.defineProperty(file, 'arrayBuffer', {
    configurable: true,
    value: async () => Uint8Array.from(content).buffer
  })
  return file
}

async function selectFiles(wrapper: ReturnType<typeof mountView>, files: File[]) {
  const input = wrapper.get('.composer input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: files })
  await input.trigger('change')
  await flushPromises()
}

afterEach(() => {
  mountedWrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('PlaygroundView model and intent routing', () => {
  beforeEach(() => {
    state.nextId = 0
    state.balance = 10
    state.accountId = undefined
    state.preferenceMode = 'auto'
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
    localStorage.clear()
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

  it('persists text attachment labels and content while keeping artifact instructions request-only', async () => {
    state.streamChat.mockImplementation(async (input: any) => {
      expect(input.messages.at(-1)).toEqual(expect.objectContaining({
        role: 'user',
        content: expect.stringContaining('--- notes.md ---\nHello from the file')
      }))
      expect(input.messages[0].content).toContain('```artifact:filename.ext')
      input.onDelta('```artifact:hello.txt\nHello download\n```')
    })
    const wrapper = mountView()
    await flushPromises()
    await selectFiles(wrapper, [createFile('notes.md', 'Hello from the file', 'text/markdown')])
    await wrapper.get('textarea.composer-input').setValue('Summarize this.')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.save.mock.calls[0][0].messages[0].content).toBe('Summarize this.\n\n--- notes.md ---\nHello from the file')
    expect(state.save.mock.calls[0][0].systemPrompt).toBe('')
    expect(wrapper.find('.attachment-item').exists()).toBe(false)
    const artifactButton = wrapper.findAll('button').find(button => button.text() === 'playground.downloadArtifact')
    expect(artifactButton).toBeTruthy()

    const createObjectURL = vi.fn(() => 'blob:playground-artifact')
    vi.stubGlobal('URL', { ...URL, createObjectURL, revokeObjectURL: vi.fn() })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await artifactButton!.trigger('click')
    expect(createObjectURL).toHaveBeenCalledOnce()
    expect(click).toHaveBeenCalledOnce()
  })

  it('sends image attachments to image edits and stores only their filenames', async () => {
    state.generateImages.mockResolvedValue([{ url: 'data:image/png;base64,generated' }])
    const wrapper = mountView()
    await flushPromises()
    const pngHeader = [137, 80, 78, 71, 13, 10, 26, 10]
    await selectFiles(wrapper, [createFile('source.png', pngHeader, 'image/png')])
    await wrapper.get('.mode-select').setValue('image')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({
      inputImages: [expect.stringMatching(/^data:image\/png;base64,/)]
    }))
    const storedContent = state.save.mock.calls[0][0].messages[0].content
    expect(storedContent).toContain('[Image: source.png]')
    expect(storedContent).not.toContain('data:image/png;base64')
    expect(state.streamChat).not.toHaveBeenCalled()
  })

  it('sends chat image attachments as multimodal image_url parts', async () => {
    state.streamChat.mockImplementation(async (input: any) => input.onDelta('I see a landscape.'))
    const wrapper = mountView()
    await flushPromises()
    const webpHeader = [82, 73, 70, 70, 0, 0, 0, 0, 87, 69, 66, 80]
    await selectFiles(wrapper, [createFile('scene.webp', webpHeader, 'image/webp')])
    await wrapper.get('textarea.composer-input').setValue('Describe this image.')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const userMessage = state.streamChat.mock.calls[0][0].messages.find((message: any) => message.role === 'user')
    expect(userMessage.content).toEqual([
      { type: 'text', text: expect.stringContaining('[Image: scene.webp]') },
      { type: 'image_url', image_url: { url: expect.stringMatching(/^data:image\/webp;base64,/) } }
    ])
    expect(state.save.mock.calls[0][0].messages[0].content).not.toContain('data:image/webp;base64')
  })

  it('keeps attachments when the initial conversation save fails', async () => {
    state.save.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountView()
    await flushPromises()
    await selectFiles(wrapper, [createFile('notes.txt', 'keep me')])
    await wrapper.get('textarea.composer-input').setValue('Read the file.')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.attachment-item').exists()).toBe(true)
    expect(wrapper.get('textarea.composer-input').element.value).toBe('Read the file.')
    expect(state.streamChat).not.toHaveBeenCalled()
  })

  it('rejects invalid UTF-8 and oversized image attachments', async () => {
    const wrapper = mountView()
    await flushPromises()
    await selectFiles(wrapper, [createFile('invalid.txt', [0xff], 'text/plain')])
    expect(wrapper.text()).toContain('playground.attachmentInvalidText')
    expect(wrapper.find('.attachment-item').exists()).toBe(false)

    await selectFiles(wrapper, [new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'large.png', { type: 'image/png' })])
    expect(wrapper.text()).toContain('playground.attachmentImageTooLarge')
    expect(wrapper.find('.attachment-item').exists()).toBe(false)
  })

  it('rejects expanded messages over the UTF-8 history limit before saving', async () => {
    const wrapper = mountView()
    await flushPromises()
    await selectFiles(wrapper, [createFile('large.txt', 'x'.repeat(24 * 1024), 'text/plain')])
    await wrapper.get('textarea.composer-input').setValue('p'.repeat(10 * 1024))
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('playground.historyMessageTooLarge')
    expect(state.save).not.toHaveBeenCalled()
    expect(wrapper.find('.attachment-item').exists()).toBe(true)
  })

  it('clears attachments when opening history or starting a new conversation', async () => {
    state.list.mockResolvedValue([{
      id: 'history-clear',
      title: 'Saved conversation',
      groupId: 7,
      model: 'chat-model',
      imageModel: 'image-model',
      systemPrompt: '',
      temperature: 0.7,
      messages: [{ id: 1, role: 'user', content: 'Saved prompt' }],
      revision: 1,
      expiresAt: Date.now() + 604800000
    }])
    const wrapper = mountView()
    await flushPromises()
    await selectFiles(wrapper, [createFile('first.txt', 'first')])
    await wrapper.get('.history-item > button').trigger('click')
    expect(wrapper.find('.attachment-item').exists()).toBe(false)

    await selectFiles(wrapper, [createFile('second.txt', 'second')])
    await wrapper.get('.history-panel > .btn-primary').trigger('click')
    expect(wrapper.find('.attachment-item').exists()).toBe(false)
  })

  it('clears conversation and attachment state when the account changes', async () => {
    state.accountId = 1
    const wrapper = mountView()
    await flushPromises()
    await selectFiles(wrapper, [createFile('private.txt', 'private attachment')])
    expect(wrapper.find('.attachment-item').exists()).toBe(true)

    state.accountId = 2
    await flushPromises()
    expect(wrapper.find('.attachment-item').exists()).toBe(false)
    expect(wrapper.get('textarea.composer-input').element.value).toBe('')
  })

  it('never generates when chat-only mode is selected, even for an image prompt', async () => {
    state.preferenceMode = 'chat'
    state.streamChat.mockImplementation(async (input: { onDelta: (value: string) => void }) => input.onDelta('text reply'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.mode-select').setValue('chat')
    await wrapper.get('textarea.composer-input').setValue('draw a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).not.toHaveBeenCalled()
    expect(state.streamChat).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('text reply')
  })

  it('generates one image only when image mode is selected for an ordinary prompt', async () => {
    state.generateImages.mockImplementation(async (input: { onImage: (image: { url: string }) => Promise<void> }) => {
      const image = { url: 'data:image/png;base64,first' }
      await input.onImage(image)
      await input.onImage(image)
      return [image]
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.mode-select').setValue('image')
    await wrapper.get('textarea.composer-input').setValue('a lighthouse at sunset')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({ count: 1, model: 'image-model' }))
    expect(state.streamChat).not.toHaveBeenCalled()
    expect(wrapper.findAll('img')).toHaveLength(1)
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

  it('uses the manual image count instead of guessing from natural language', async () => {
    state.generateImages.mockResolvedValue([{ url: 'data:image/png;base64,image' }])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('textarea.composer-input').setValue('draw 3 pictures of a lighthouse')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(state.generateImages).toHaveBeenCalledWith(expect.objectContaining({ count: 1 }))
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
