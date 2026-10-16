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
  useI18n: () => ({ locale: { value: 'en' } })
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
      group_ids: [11]
    })
    expect(mocks.showSuccess).toHaveBeenCalledWith('Auto profile created.')
  })

  it('preserves a draft after revision conflict and reloads the latest profile on request', async () => {
    const latest = { ...profile, name: 'Updated on server', revision: 5 }
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
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.updateProfile).toHaveBeenCalledWith(3, {
      name: 'My draft',
      status: 'active',
      max_candidates: 3,
      allow_custom_order: true,
      cross_group_retry_allowed: false,
      group_ids: [11],
      revision: 4
    })
    expect(wrapper.get('[role="alert"]').text()).toContain('Your draft is preserved')
    expect((wrapper.get('#auto-profile-name').element as HTMLInputElement).value).toBe('My draft')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()

    await wrapper.findAll('button').find(button => button.text().includes('Discard draft and reload'))!.trigger('click')
    await flushPromises()

    expect((wrapper.get('#auto-profile-name').element as HTMLInputElement).value).toBe('Updated on server')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
