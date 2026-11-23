import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { createPinia, type Pinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { claimBloomTask, getBloomTasks, type BloomTasks } from '@/api/bloom'
import { i18n, loadLocaleMessages } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useBloomStore } from '@/stores/bloom'
import type { User } from '@/types'
import BloomGarden from '../BloomGarden.vue'

vi.mock('@/api/bloom', () => ({
  getBloomTasks: vi.fn(),
  claimBloomTask: vi.fn()
}))

const snapshot: BloomTasks = {
  day: '2026-11-23',
  week_start: '2026-11-23',
  resets_at: new Date(Date.now() + 3_600_000).toISOString(),
  week_resets_at: new Date(Date.now() + 86_400_000).toISOString(),
  timezone: 'Asia/Shanghai',
  budget_micros: 5000,
  claimed_micros: 0,
  collected_flowers: 7,
  tasks: [
    { id: 'easy-1', template_id: 'checkin', tier: 'easy', metric: 'checkin', target: 1, progress: 0, reward_micros: 500, claimed: false },
    { id: 'medium-1', template_id: 'requests', tier: 'medium', metric: 'productive_requests', target: 3, progress: 3, reward_micros: 1000, claimed: false },
    { id: 'hard-1', template_id: 'tokens', tier: 'hard', metric: 'output_tokens', target: 1000, progress: 100, reward_micros: 2000, claimed: false },
    { id: 'weekly-1', template_id: 'days', tier: 'weekly', metric: 'active_days', target: 4, progress: 4, reward_micros: 3000, claimed: false }
  ]
}

let wrapper: VueWrapper | undefined
let pinia: Pinia
const plugins = () => [i18n, pinia]
const dialog = () => document.body.querySelector<HTMLElement>('[role="dialog"]')!

function openGarden() {
  wrapper!.get<HTMLButtonElement>('[aria-haspopup="dialog"]').element.click()
  return flushPromises()
}

function buttonFor(template: string) {
  return dialog().querySelector<HTMLButtonElement>(`[data-template-id="${template}"] button`)!
}

beforeEach(async () => {
  vi.resetAllMocks()
  vi.mocked(getBloomTasks).mockResolvedValue(snapshot)
  pinia = createPinia()
  const auth = useAuthStore(pinia)
  auth.token = 'test-token'
  auth.user = { id: 1 } as User
  vi.spyOn(auth, 'refreshUser').mockResolvedValue(auth.user)
  await loadLocaleMessages('en')
  i18n.global.locale.value = 'en'
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  useBloomStore(pinia).$dispose()
  document.body.replaceChildren()
})

describe('Task Garden', () => {
  it('keeps weekly progress separate and restores keyboard focus after closing', async () => {
    wrapper = mount(BloomGarden, { attachTo: document.body, props: { collapsed: true }, global: { plugins: plugins() } })
    await flushPromises()
    expect(wrapper.get('[role="tooltip"]').text()).toContain('1 of 3')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('Next: Daily check-in (0 / 1)')
    await openGarden()
    expect(dialog().querySelector('[aria-current="step"]')?.textContent).toContain('Sprout')
    expect(dialog().textContent).toContain('Flowers collected: 7')
    expect(dialog().textContent).toContain('Your first visit this week fixes the plan for the whole week.')
    expect(dialog().textContent).toContain('Each day still shows that day’s tasks.')
    expect(dialog().textContent).toContain('A new plan is created on your first visit next calendar week')
    expect(dialog().textContent?.match(/Asia\/Shanghai/g)).toHaveLength(2)
    expect(buttonFor('tokens')).toBeNull()
    const closeButton = dialog().querySelector<HTMLButtonElement>('button')!
    expect(document.activeElement).toBe(closeButton)
    closeButton.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(buttonFor('days'))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(document.activeElement).toBe(wrapper.get('[aria-haspopup="dialog"]').element)
  })

  it('records check-in only through claiming and prevents duplicate submits', async () => {
    wrapper = mount(BloomGarden, { attachTo: document.body, props: { collapsed: false }, global: { plugins: plugins() } })
    await openGarden()
    expect(claimBloomTask).not.toHaveBeenCalled()
    const updated = structuredClone(snapshot)
    updated.tasks[0] = { ...updated.tasks[0], claimed: true, progress: 1 }
    updated.claimed_micros = 500
    vi.mocked(useAuthStore(pinia).refreshUser).mockRejectedValue(new Error('balance unavailable'))
    vi.mocked(claimBloomTask).mockResolvedValue(updated)
    const checkin = buttonFor('checkin')
    expect(checkin.textContent).toContain('Check in and claim')
    checkin.focus()
    checkin.click()
    checkin.click()
    await flushPromises()
    expect(claimBloomTask).toHaveBeenCalledTimes(1)
    expect(claimBloomTask).toHaveBeenCalledWith('easy-1', expect.any(AbortSignal))
    expect(dialog().textContent).toContain('2 of 3')
    expect(dialog().textContent).toContain('Reward claimed: $0.000500')
    expect(buttonFor('checkin')).toBeNull()
    expect(useAuthStore(pinia).refreshUser).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(dialog().querySelector('[data-template-id="checkin"]'))
  })

  it('allows a failed load and weekly claim to be retried', async () => {
    vi.mocked(getBloomTasks).mockRejectedValue(new Error('offline'))
    wrapper = mount(BloomGarden, { attachTo: document.body, props: { collapsed: false }, global: { plugins: plugins() } })
    await openGarden()
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('Could not load')
    vi.mocked(getBloomTasks).mockResolvedValue(snapshot)
    Array.from(dialog().querySelectorAll('button')).find(button => button.textContent === 'Retry')!.click()
    await flushPromises()

    const updated = structuredClone(snapshot)
    updated.tasks[3].claimed = true
    updated.collected_flowers = 12
    vi.mocked(claimBloomTask).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(updated)
    buttonFor('days').click()
    await flushPromises()
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('Could not claim')
    buttonFor('days').click()
    await flushPromises()
    expect(dialog().textContent).toContain('Flowers collected: 12')
    expect(dialog().textContent).toContain('Twilight Iris unlocked!')
    expect(dialog().textContent).toContain('All flower colors unlocked')
    expect(dialog().textContent).toContain('1 of 3')
  })

  it('aborts the previous account request and ignores its late response', async () => {
    const auth = useAuthStore(pinia)
    let finishOldRequest!: (result: BloomTasks) => void
    vi.mocked(getBloomTasks).mockImplementationOnce(() => new Promise(resolve => { finishOldRequest = resolve }))
    wrapper = mount(defineComponent({
      setup: () => () => h(BloomGarden, { key: auth.user?.id, collapsed: false })
    }), { attachTo: document.body, global: { plugins: plugins() } })
    const oldSignal = vi.mocked(getBloomTasks).mock.calls[0][0]
    auth.user = { id: 2 } as User
    await nextTick()
    expect(oldSignal.aborted).toBe(true)
    finishOldRequest({ ...snapshot, collected_flowers: 99 })
    await openGarden()
    expect(dialog().textContent).toContain('Flowers collected: 7')
    expect(dialog().textContent).not.toContain('99')
  })

  it('keeps the flower stage and deduplicates loads across sidebar remounts', async () => {
    const route = ref('dashboard')
    wrapper = mount(defineComponent({
      setup: () => () => h(BloomGarden, { key: route.value, collapsed: false })
    }), { attachTo: document.body, global: { plugins: plugins() } })
    route.value = 'playground'
    await flushPromises()
    expect(wrapper.get('[data-stage]').attributes('data-stage')).toBe('sprout')
    route.value = 'usage'
    await nextTick()
    expect(wrapper.get('[data-stage]').attributes('data-stage')).toBe('sprout')
    expect(getBloomTasks).toHaveBeenCalledTimes(1)
    useAuthStore(pinia).token = null
    expect(useBloomStore(pinia).data).toBeNull()
  })

  it.each(['resets_at', 'week_resets_at'] as const)('invalidates %s locally and waits for an active visit', async (boundary) => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date('2026-11-29T15:59:59Z'))
      const store = useBloomStore(pinia)
      const resetsAt = Date.now() + 1_000
      const nextPeriod = {
        ...snapshot,
        day: '2026-11-30',
        week_start: '2026-11-30',
        resets_at: new Date(resetsAt + 86_400_000).toISOString(),
        week_resets_at: new Date(resetsAt + 604_800_000).toISOString()
      }
      vi.mocked(getBloomTasks).mockResolvedValueOnce({
        ...nextPeriod,
        day: '2026-11-29',
        week_start: '2026-11-23',
        [boundary]: new Date(resetsAt).toISOString()
      })
      let finishNewDay!: (result: BloomTasks) => void
      vi.mocked(getBloomTasks).mockImplementationOnce(() => new Promise(resolve => { finishNewDay = resolve }))
      await store.refresh()
      await vi.advanceTimersByTimeAsync(1_000)
      expect(store.data).toBeNull()
      expect(getBloomTasks).toHaveBeenCalledTimes(1)
      await store.refresh(false, { background: true })
      expect(getBloomTasks).toHaveBeenCalledTimes(1)
      const request = store.refresh()
      await store.refresh()
      expect(getBloomTasks).toHaveBeenCalledTimes(2)
      finishNewDay(nextPeriod)
      await request
      expect(store.data?.day).toBe('2026-11-30')
      expect(store.data?.week_start).toBe('2026-11-30')
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps an open panel idle across midnight until the user views current tasks', async () => {
    vi.useFakeTimers()
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    try {
      vi.setSystemTime(new Date('2026-11-29T15:59:15Z'))
      const midnight = Date.now() + 45_000
      const nextPeriod = {
        ...snapshot,
        day: '2026-11-30',
        week_start: '2026-11-30',
        resets_at: new Date(midnight + 86_400_000).toISOString(),
        week_resets_at: new Date(midnight + 604_800_000).toISOString()
      }
      vi.mocked(getBloomTasks).mockResolvedValue(nextPeriod).mockResolvedValueOnce({
        ...snapshot,
        day: '2026-11-29',
        resets_at: new Date(midnight).toISOString(),
        week_resets_at: new Date(midnight).toISOString()
      })
      wrapper = mount(BloomGarden, { attachTo: document.body, props: { collapsed: false }, global: { plugins: plugins() } })
      await openGarden()
      await vi.advanceTimersByTimeAsync(45_000)
      expect(useBloomStore(pinia).data).toBeNull()
      expect(dialog().querySelector('[data-template-id]')).toBeNull()
      expect(dialog().textContent).toContain('A new period has started.')
      expect(getBloomTasks).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(90_000)
      expect(getBloomTasks).toHaveBeenCalledTimes(1)

      Array.from(dialog().querySelectorAll('button')).find(button => button.textContent === 'View current tasks')!.click()
      await flushPromises()
      expect(getBloomTasks).toHaveBeenCalledTimes(2)
      expect(useBloomStore(pinia).data?.day).toBe('2026-11-30')
      expect(dialog().querySelector('[data-template-id="checkin"]')).not.toBeNull()
      expect(dialog().textContent).toContain('Your first visit this week fixes the plan for the whole week.')
    } finally {
      visibility.mockRestore()
      vi.useRealTimers()
    }
  })

  it('checks expiration before a background refresh when boundary timers have not run', async () => {
    vi.useFakeTimers()
    try {
      const store = useBloomStore(pinia)
      const reset = Date.now() + 1_000
      vi.mocked(getBloomTasks).mockResolvedValueOnce({ ...snapshot, resets_at: new Date(reset).toISOString() })
      await store.refresh()
      vi.setSystemTime(reset)
      await store.refresh(false, { background: true })
      expect(store.data).toBeNull()
      expect(getBloomTasks).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('loads current tasks instead of claiming an expired task', async () => {
    vi.useFakeTimers()
    try {
      const store = useBloomStore(pinia)
      const reset = Date.now() + 1_000
      vi.mocked(getBloomTasks).mockResolvedValueOnce({ ...snapshot, resets_at: new Date(reset).toISOString() })
      await store.refresh()
      vi.setSystemTime(reset)
      await store.claim(snapshot.tasks[0])
      expect(claimBloomTask).not.toHaveBeenCalled()
      expect(getBloomTasks).toHaveBeenCalledTimes(2)
      expect(store.data).not.toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows the fixed weekly plan explanation in Chinese', async () => {
    await loadLocaleMessages('zh')
    i18n.global.locale.value = 'zh'
    wrapper = mount(BloomGarden, { attachTo: document.body, props: { collapsed: false }, global: { plugins: plugins() } })
    await openGarden()
    expect(dialog().textContent).toContain('本周首次访问时会固定整周任务计划')
    expect(dialog().textContent).toContain('每天仍展示当天任务')
    expect(dialog().textContent).toContain('下个自然周首次访问时生成新计划')
  })

  it('refreshes an open panel every 45 seconds and pauses while hidden or closed', async () => {
    vi.useFakeTimers()
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    try {
      wrapper = mount(BloomGarden, { attachTo: document.body, props: { collapsed: false }, global: { plugins: plugins() } })
      await openGarden()
      expect(getBloomTasks).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(45_000)
      expect(getBloomTasks).toHaveBeenCalledTimes(2)

      visibility.mockReturnValue('hidden')
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(90_000)
      expect(getBloomTasks).toHaveBeenCalledTimes(2)

      visibility.mockReturnValue('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      await Promise.resolve()
      expect(getBloomTasks).toHaveBeenCalledTimes(3)
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await nextTick()
      await vi.advanceTimersByTimeAsync(45_000)
      window.dispatchEvent(new Event('focus'))
      expect(getBloomTasks).toHaveBeenCalledTimes(3)
    } finally {
      visibility.mockRestore()
      vi.useRealTimers()
    }
  })
})
