import { readonly, ref, type Ref } from 'vue'

/**
 * 响应式的深色模式状态。
 *
 * 直接 `computed(() => document.documentElement.classList.contains('dark'))` 不会响应：
 * DOM 的 class 不是 Vue 的响应式数据，computed 第一次求值后就被缓存，
 * 切换主题后图表等仍然沿用旧颜色（例如深色背景上的深灰文字）。
 * 这里用一个全局共享的 MutationObserver 监听 <html> 的 class 变化。
 */
const isDark = ref(readDark())
let observer: MutationObserver | null = null

function readDark(): boolean {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
}

function ensureObserver() {
  if (observer || typeof document === 'undefined' || typeof MutationObserver === 'undefined') return
  observer = new MutationObserver(() => {
    const next = readDark()
    if (next !== isDark.value) isDark.value = next
  })
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
}

export function useIsDark(): Readonly<Ref<boolean>> {
  ensureObserver()
  isDark.value = readDark()
  return readonly(isDark)
}
