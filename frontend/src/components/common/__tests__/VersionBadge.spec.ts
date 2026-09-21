import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '../VersionBadge.vue'

const state = vi.hoisted(() => ({
  authStore: { isAdmin: true },
  appStore: null as any,
  performUpdate: vi.fn(),
  restartService: vi.fn(),
  getRollbackVersions: vi.fn(),
  rollback: vi.fn()
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => state.authStore,
  useAppStore: () => state.appStore
}))

vi.mock('@/api/admin/system', () => ({
  performUpdate: state.performUpdate,
  restartService: state.restartService,
  getRollbackVersions: state.getRollbackVersions,
  rollback: state.rollback
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

function createAppStore() {
  return reactive({
    versionLoading: false,
    currentVersion: '0.1.0',
    latestVersion: 'abcdef123456',
    hasUpdate: true,
    buildType: 'release',
    updateMode: 'docker',
    currentCommit: '123456789abc',
    latestCommit: 'abcdef123456',
    stagedUpdate: false,
    versionWarning: '',
    releaseInfo: {
      name: 'Update message',
      body: 'Update details',
      published_at: '2026-09-21T00:00:00Z',
      html_url: 'https://github.com/yunyar/sub2api/commit/abcdef123456'
    },
    fetchVersion: vi.fn(async () => null),
    clearVersionCache: vi.fn()
  })
}

function mountBadge() {
  return mount(VersionBadge, {
    global: {
      stubs: { Icon: { template: '<i />' } }
    }
  })
}

async function openBadge(wrapper: ReturnType<typeof mountBadge>) {
  await wrapper.get('button').trigger('click')
}

describe('VersionBadge Docker updates', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    state.appStore = createAppStore()
    state.performUpdate.mockReset()
    state.restartService.mockReset()
    state.getRollbackVersions.mockReset()
    state.rollback.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the restart action after refresh reports a staged Docker update', async () => {
    state.appStore.fetchVersion.mockImplementation(async () => {
      state.appStore.stagedUpdate = true
      return null
    })
    const wrapper = mountBadge()

    await openBadge(wrapper)
    await wrapper.get('[title="version.refresh"]').trigger('click')

    expect(wrapper.text()).toContain('version.dockerUpdateDownloaded')
    expect(wrapper.text()).toContain('version.restartNow')
    expect(state.performUpdate).not.toHaveBeenCalled()
    expect(state.restartService).not.toHaveBeenCalled()
  })

  it('does not download or restart while mounted or during periodic checks', async () => {
    const wrapper = mountBadge()

    await vi.advanceTimersByTimeAsync(20 * 60 * 1000)

    expect(state.appStore.fetchVersion).toHaveBeenCalledTimes(2)
    expect(state.performUpdate).not.toHaveBeenCalled()
    expect(state.restartService).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows the restart error and keeps the staged restart action available', async () => {
    state.appStore.stagedUpdate = true
    state.restartService.mockRejectedValue(new Error('Docker helper unavailable'))
    const wrapper = mountBadge()

    await openBadge(wrapper)
    const restartButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.restartNow'))
    await restartButton!.trigger('click')

    expect(wrapper.text()).toContain('Docker helper unavailable')
    expect(wrapper.text()).toContain('version.restartNow')
  })

  it('shows an update-check warning instead of claiming the version is current', async () => {
    state.appStore.hasUpdate = false
    state.appStore.versionWarning = 'GitHub temporarily unavailable'
    const wrapper = mountBadge()

    await openBadge(wrapper)

    expect(wrapper.text()).toContain('version.updateCheckWarning')
    expect(wrapper.text()).toContain('GitHub temporarily unavailable')
    expect(wrapper.text()).not.toContain('version.upToDate')
  })

  it('keeps the staged Docker restart action available alongside a check warning', async () => {
    state.appStore.stagedUpdate = true
    state.appStore.versionWarning = 'The newest image is still building'
    const wrapper = mountBadge()

    await openBadge(wrapper)

    expect(wrapper.text()).toContain('The newest image is still building')
    expect(wrapper.text()).toContain('version.dockerUpdateDownloaded')
    expect(wrapper.text()).toContain('version.restartNow')
  })

  it('clears the periodic update check when unmounted', async () => {
    const wrapper = mountBadge()
    expect(state.appStore.fetchVersion).toHaveBeenCalledTimes(1)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(20 * 60 * 1000)

    expect(state.appStore.fetchVersion).toHaveBeenCalledTimes(1)
  })
})
