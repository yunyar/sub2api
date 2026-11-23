import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import {
  bloomPlannerGroups,
  getBloomPlannerSettings,
  updateBloomPlannerSettings,
  type BloomPlannerSettings as Settings
} from '@/api/admin/bloomPlanner'
import { list as listKeys } from '@/api/keys'
import { useStepUp } from '@/composables/useStepUp'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import type { ApiKey } from '@/types'
import BloomPlannerSettings from '../BloomPlannerSettings.vue'

vi.mock('@/api/admin/bloomPlanner', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/api/admin/bloomPlanner')>(),
  getBloomPlannerSettings: vi.fn(),
  updateBloomPlannerSettings: vi.fn()
}))
vi.mock('@/api/keys', () => ({ list: vi.fn() }))

const defaults: Settings = { enabled: false, key_id: 0, model: '' }

function apiKey(id: number, status: ApiKey['status'] = 'active'): ApiKey {
  return {
    id, name: `Key ${id}`, key: 'private-key-value', status, quota: 10, quota_used: 3
  } as ApiKey
}

function mountPlanner(locale: 'en' | 'zh') {
  return mount(BloomPlannerSettings, {
    props: { stepUp: useStepUp() },
    global: { plugins: [createI18n({ legacy: false, locale, messages: { en, zh } })] }
  })
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(getBloomPlannerSettings).mockResolvedValue(defaults)
  vi.mocked(listKeys).mockImplementation(async (page = 1) => ({
    page, pages: 2, total: 3, page_size: 100,
    items: page === 1 ? [apiKey(1), apiKey(2, 'inactive')] : [apiKey(3)]
  }))
})

describe('BloomPlannerSettings', () => {
  it.each([
    { locale: 'en', title: 'Garden Task Rewards', groups: ['First use', 'Text', 'Image', 'Mixed'], tiers: ['Easy', 'Medium', 'Hard'] },
    { locale: 'zh', title: '花园任务奖励', groups: ['首次使用', '文字', '图像', '混合'], tiers: ['简单', '中等', '困难'] }
  ] as const)('renders static and dynamic messages from the $locale site locale', async ({ locale, title, groups, tiers }) => {
    const wrapper = mountPlanner(locale)
    await flushPromises()
    expect(wrapper.get('h2').text()).toBe(title)
    expect(wrapper.findAll('th[scope=row]').map((cell) => cell.text())).toEqual(groups)
    expect(wrapper.findAll('th[scope=col]').slice(1).map((cell) => cell.text())).toEqual(tiers)
    expect(wrapper.findAll('tbody tr:first-child td').map((cell) => cell.text())).toEqual(['1%', '3%', '6%'])
    wrapper.unmount()
  })

  it('selects a paginated own key, handles save failure, shows saved allocations, and restores defaults', async () => {
    const wrapper = mountPlanner('en')
    await flushPromises()
    await wrapper.get('[role=switch]').trigger('click')
    expect(listKeys).toHaveBeenNthCalledWith(2, 2, 100, { status: 'active' })
    expect(wrapper.findAll('option').map((option) => option.attributes('value'))).toEqual(['0', '1', '3'])
    expect(wrapper.get('select').text()).toContain('7 balance units remaining')
    expect(wrapper.html()).not.toContain('private-key-value')
    expect(wrapper.text()).toContain('billed under that key')
    const save = wrapper.get('button.btn-primary')
    expect(save.attributes('disabled')).toBeDefined()
    await wrapper.get('select').setValue('3')
    await wrapper.get('input').setValue(' demo-model ')
    const payload = { enabled: true, key_id: 3, model: 'demo-model' }
    vi.mocked(updateBloomPlannerSettings).mockRejectedValueOnce({ message: 'Invalid model' })
    await save.trigger('click')
    await flushPromises()
    expect(updateBloomPlannerSettings).toHaveBeenLastCalledWith(payload)
    expect(wrapper.get('[role=alert]').text()).toContain('Invalid model')
    expect(wrapper.get('tbody td').text()).toBe('1%')

    const profiles = Object.fromEntries(
      bloomPlannerGroups.map((group) => [group, { easy: 2, medium: 3, hard: 5 }])
    ) as Settings['profiles']
    vi.mocked(updateBloomPlannerSettings).mockResolvedValueOnce({ ...payload, profiles })
    await save.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role=status]').text()).toBe('Settings saved.')
    expect(wrapper.findAll('tbody td:first-of-type').map((cell) => cell.text())).toEqual(['2%', '2%', '2%', '2%'])

    vi.mocked(updateBloomPlannerSettings).mockResolvedValueOnce(defaults)
    await wrapper.get('[role=switch]').trigger('click')
    await save.trigger('click')
    await flushPromises()
    expect(updateBloomPlannerSettings).toHaveBeenLastCalledWith(defaults)
    expect(wrapper.get('tbody td').text()).toBe('1%')
    wrapper.unmount()
  })
})
