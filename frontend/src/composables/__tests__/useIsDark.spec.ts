import { afterEach, describe, expect, it } from 'vitest'
import { computed } from 'vue'
import { useIsDark } from '../useIsDark'

const flushObserver = () => new Promise(resolve => setTimeout(resolve, 0))

afterEach(() => {
  document.documentElement.classList.remove('dark')
})

describe('useIsDark', () => {
  it('reflects the current <html> class', () => {
    document.documentElement.classList.add('dark')
    expect(useIsDark().value).toBe(true)
  })

  it('updates dependent computeds when the theme is toggled later', async () => {
    document.documentElement.classList.remove('dark')
    const isDark = useIsDark()
    const textColor = computed(() => (isDark.value ? '#e5e7eb' : '#374151'))
    expect(textColor.value).toBe('#374151')

    document.documentElement.classList.add('dark')
    await flushObserver()
    expect(textColor.value).toBe('#e5e7eb')

    document.documentElement.classList.remove('dark')
    await flushObserver()
    expect(textColor.value).toBe('#374151')
  })
})
