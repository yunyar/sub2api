import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AutoRoutingEditor from '../AutoRoutingEditor.vue'
import type { Group } from '@/types'

const mocks = vi.hoisted(() => ({
  getAutoRouting: vi.fn(),
  updateAutoRouting: vi.fn(),
  getAvailableProfiles: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api', () => ({
  keysAPI: {
    getAutoRouting: mocks.getAutoRouting,
    updateAutoRouting: mocks.updateAutoRouting
  }
}))

vi.mock('@/api/autoRouting', () => ({
  autoRoutingAPI: {
    getAvailableProfiles: mocks.getAvailableProfiles
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: mocks.showSuccess })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ locale: { value: 'en' } })
}))

enableAutoUnmount(afterEach)

const routingConfig = {
  routing_mode: 'auto' as const,
  auto_profile_id: 7,
  auto_selection_mode: 'inherit' as const,
  group_ids: [],
  cross_group_retry: true,
  routing_revision: 4,
  group_id: null
}

const profile = {
  id: 7,
  name: 'Primary Auto',
  status: 'active' as const,
  revision: 3,
  max_candidates: 2,
  allow_custom_order: true,
  cross_group_retry_allowed: true,
  group_ids: [11]
}

const groups = [{
  id: 11,
  name: 'OpenAI Main',
  platform: 'openai',
  status: 'active'
}] as Group[]

function mountEditor() {
  return mount(AutoRoutingEditor, {
    props: {
      show: true,
      apiKey: { id: 5, name: 'Test key', group_id: null },
      groups
    },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>'
        },
        Icon: true
      }
    }
  })
}

describe('AutoRoutingEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getAutoRouting.mockResolvedValue(routingConfig)
    mocks.updateAutoRouting.mockResolvedValue(routingConfig)
    mocks.getAvailableProfiles.mockResolvedValue([profile])
  })

  it('loads an enabled retry setting and persists it', async () => {
    const wrapper = mountEditor()
    await flushPromises()

    const retryInput = wrapper.get('[data-testid="key-cross-group-retry"]')
    expect(retryInput.attributes('disabled')).toBeUndefined()
    expect((retryInput.element as HTMLInputElement).checked).toBe(true)

    await wrapper.get('[data-testid="key-auto-routing-save"]').trigger('click')
    await flushPromises()

    expect(mocks.updateAutoRouting).toHaveBeenCalledWith(5, {
      routing_mode: 'auto',
      auto_profile_id: 7,
      auto_selection_mode: 'inherit',
      group_ids: [],
      cross_group_retry: true,
      routing_revision: 4,
      group_id: null
    })
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('disables and clears retry when the selected profile does not allow it', async () => {
    mocks.getAvailableProfiles.mockResolvedValue([{ ...profile, cross_group_retry_allowed: false }])

    const wrapper = mountEditor()
    await flushPromises()

    const retryInput = wrapper.get('[data-testid="key-cross-group-retry"]')
    expect(retryInput.attributes('disabled')).toBeDefined()
    expect((retryInput.element as HTMLInputElement).checked).toBe(false)
  })
})
