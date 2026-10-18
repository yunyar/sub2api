import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AutoGroupsView from '../AutoGroupsView.vue'

const mocks = vi.hoisted(() => ({
  listProfiles: vi.fn(),
  getProfile: vi.fn(),
  createProfile: vi.fn(),
  updateProfile: vi.fn(),
  deleteProfile: vi.fn(),
  getGroups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

const localeState = vi.hoisted(() => ({ value: 'en' }))

vi.mock('@/api/admin/autoGroups', () => ({
  autoGroupsAPI: {
    list: mocks.listProfiles,
    getById: mocks.getProfile,
    create: mocks.createProfile,
    update: mocks.updateProfile,
    delete: mocks.deleteProfile
  }
}))

vi.mock('@/api', () => ({
  adminAPI: {
    groups: {
      getAllIncludingInactive: mocks.getGroups
    }
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: mocks.showError,
    showSuccess: mocks.showSuccess
  })
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ locale: localeState })
}))

enableAutoUnmount(afterEach)

const group = {
  id: 11,
  name: 'OpenAI Main',
  platform: 'openai',
  status: 'active'
}

const profile = {
  id: 3,
  name: 'Primary Auto',
  status: 'active' as const,
  revision: 4,
  max_candidates: 3,
  allow_custom_order: true,
  cross_group_retry_allowed: false,
  group_ids: [11]
}

function mountView() {
  return mount(AutoGroupsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: {
          props: ['show', 'title'],
          template: '<div v-if="show" data-testid="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>'
        },
        Icon: true
      }
    }
  })
}

describe('AutoGroupsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.listProfiles.mockResolvedValue([])
    mocks.getProfile.mockResolvedValue(profile)
    mocks.createProfile.mockResolvedValue({ ...profile, id: 9, name: 'New Auto', status: 'disabled', revision: 1 })
    mocks.updateProfile.mockResolvedValue({ ...profile, revision: 5 })
    mocks.deleteProfile.mockResolvedValue(undefined)
    mocks.getGroups.mockResolvedValue([group])
    localeState.value = 'en'
  })

  it('creates a disabled profile with conservative defaults and ordered group IDs', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text().includes('New profile'))!.trigger('click')
    await wrapper.get('#auto-profile-name').setValue('New Auto')
    await wrapper.findAll('label')
      .find(label => label.text().includes('OpenAI Main'))!
      .get('input[type="checkbox"]')
      .setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.createProfile).toHaveBeenCalledWith({
      name: 'New Auto',
      status: 'disabled',
      max_candidates: 1,
      allow_custom_order: false,
      cross_group_retry_allowed: false,
      model_catalog_filter: {
        enabled: false,
        models: []
      },
      group_ids: [11]
    })
    expect(mocks.showSuccess).toHaveBeenCalledWith('Auto profile created.')
  })

  it('treats a legacy missing filter as disabled and saves normalized patterns', async () => {
    mocks.listProfiles.mockResolvedValue([profile])
    mocks.getProfile.mockResolvedValue(profile)
    mocks.updateProfile.mockResolvedValue({
      ...profile,
      model_catalog_filter: { enabled: true, models: ['gpt-*', 'claude-*'] }
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[aria-label="Edit Primary Auto"]').trigger('click')
    await flushPromises()

    const filterToggle = wrapper.get('[data-testid="model-catalog-filter-toggle"]')
    expect((filterToggle.element as HTMLInputElement).checked).toBe(false)
    await filterToggle.setValue(true)
    await wrapper.get('[data-testid="model-catalog-filter-models"]').setValue(
      '\n gpt-* \nclaude-*\ngpt-*\n '
    )
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.updateProfile).toHaveBeenCalledWith(3, expect.objectContaining({
      model_catalog_filter: {
        enabled: true,
        models: ['gpt-*', 'claude-*']
      },
      revision: 4
    }))
  })

  it('requires at least one pattern when filtering is enabled', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('New profile'))!.trigger('click')
    await wrapper.get('#auto-profile-name').setValue('Filtered Auto')
    await wrapper.get('[data-testid="model-catalog-filter-toggle"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="model-catalog-filter-error"]').text())
      .toContain('Add at least one model name or pattern')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(mocks.createProfile).not.toHaveBeenCalled()
  })

  it('appends common model family patterns without replacing existing entries', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('New profile'))!.trigger('click')
    await wrapper.get('[data-testid="model-catalog-filter-models"]').setValue('custom-*\ngpt-*')
    await wrapper.get('[data-testid="append-common-model-patterns"]').trigger('click')

    const patterns = (wrapper.get('[data-testid="model-catalog-filter-models"]').element as HTMLTextAreaElement)
      .value.split('\n')
    expect(patterns.slice(0, 2)).toEqual(['custom-*', 'gpt-*'])
    expect(patterns.filter(pattern => pattern === 'gpt-*')).toHaveLength(1)
    expect(patterns).toContain('claude-*')
    expect(patterns).toContain('gemini-*')
  })

  it('shows the model list scope explanation in Chinese', async () => {
    localeState.value = 'zh-CN'
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('新建配置'))!.trigger('click')

    expect(wrapper.text()).toContain('只控制客户端和游乐场拉取/展示的模型列表')
    expect(wrapper.text()).toContain('不影响直接请求路由、分组选择或计费')
    expect(wrapper.get('[data-testid="model-catalog-filter-toggle"]').exists()).toBe(true)
  })

  it('loads and saves an enabled cross-group retry setting', async () => {
    const enabledProfile = { ...profile, cross_group_retry_allowed: true }
    mocks.listProfiles.mockResolvedValue([enabledProfile])
    mocks.getProfile.mockResolvedValue(enabledProfile)
    mocks.updateProfile.mockResolvedValue({ ...enabledProfile, revision: 5 })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[aria-label="Edit Primary Auto"]').trigger('click')
    await flushPromises()

    const retryInput = wrapper.get('[data-testid="auto-profile-cross-group-retry"]')
    expect(retryInput.attributes('disabled')).toBeUndefined()
    expect((retryInput.element as HTMLInputElement).checked).toBe(true)

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.updateProfile).toHaveBeenCalledWith(3, {
      name: 'Primary Auto',
      status: 'active',
      max_candidates: 3,
      allow_custom_order: true,
      cross_group_retry_allowed: true,
      model_catalog_filter: {
        enabled: false,
        models: []
      },
      group_ids: [11],
      revision: 4
    })
  })

  it('preserves a draft after revision conflict and reloads the latest profile on request', async () => {
    const latest = {
      ...profile,
      name: 'Updated on server',
      revision: 5,
      model_catalog_filter: { enabled: true, models: ['latest-*'] }
    }
    mocks.listProfiles.mockResolvedValue([profile])
    mocks.getProfile
      .mockResolvedValueOnce(profile)
      .mockResolvedValueOnce(latest)
    mocks.updateProfile.mockRejectedValueOnce({ status: 409, message: 'revision conflict' })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[aria-label="Edit Primary Auto"]').trigger('click')
    await flushPromises()
    await wrapper.get('#auto-profile-name').setValue('My draft')
    await wrapper.get('[data-testid="model-catalog-filter-toggle"]').setValue(true)
    await wrapper.get('[data-testid="model-catalog-filter-models"]').setValue('draft-*')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.updateProfile).toHaveBeenCalledWith(3, {
      name: 'My draft',
      status: 'active',
      max_candidates: 3,
      allow_custom_order: true,
      cross_group_retry_allowed: false,
      model_catalog_filter: {
        enabled: true,
        models: ['draft-*']
      },
      group_ids: [11],
      revision: 4
    })
    expect(wrapper.get('[role="alert"]').text()).toContain('Your draft is preserved')
    expect((wrapper.get('#auto-profile-name').element as HTMLInputElement).value).toBe('My draft')
    expect((wrapper.get('[data-testid="model-catalog-filter-models"]').element as HTMLTextAreaElement).value)
      .toBe('draft-*')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()

    await wrapper.findAll('button').find(button => button.text().includes('Discard draft and reload'))!.trigger('click')
    await flushPromises()

    expect((wrapper.get('#auto-profile-name').element as HTMLInputElement).value).toBe('Updated on server')
    expect((wrapper.get('[data-testid="model-catalog-filter-models"]').element as HTMLTextAreaElement).value)
      .toBe('latest-*')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
