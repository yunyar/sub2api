import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref, watch } from 'vue'
import { claimBloomTask, getBloomTasks, type BloomTask, type BloomTasks } from '@/api/bloom'
import { useAuthStore } from './auth'

const CACHE_MS = 15_000

export const useBloomStore = defineStore('bloom', () => {
  const auth = useAuthStore()
  const account = computed(() => auth.isAuthenticated ? auth.user?.id : undefined)
  const data = ref<BloomTasks | null>(null)
  const loading = ref(false)
  const loadFailed = ref(false)
  const claiming = ref<BloomTask['id'] | null>(null)
  let session = new AbortController()
  let lastLoaded = -Infinity
  let resetAt = Infinity
  let timer: ReturnType<typeof setTimeout> | undefined

  function clearSnapshot() {
    clearTimeout(timer)
    lastLoaded = -Infinity
    resetAt = Infinity
    data.value = null
    loadFailed.value = false
  }

  function clearSession() {
    session.abort()
    session = new AbortController()
    clearSnapshot()
    loading.value = false
    claiming.value = null
  }

  function applySnapshot(snapshot: BloomTasks) {
    const expiration = Math.min(Date.parse(snapshot.resets_at), Date.parse(snapshot.week_resets_at))
    if (expiration <= Date.now()) return false
    data.value = snapshot
    loadFailed.value = false
    lastLoaded = Date.now()
    clearTimeout(timer)
    resetAt = Number.isFinite(expiration) ? expiration : Infinity
    if (Number.isFinite(resetAt)) {
      timer = setTimeout(() => {
        if (Date.now() >= resetAt) clearSnapshot()
      }, Math.min(resetAt - lastLoaded, 2_147_483_647))
    }
    return true
  }

  async function refresh(force = false, { background = false } = {}) {
    if (Date.now() >= resetAt) clearSnapshot()
    if (background && !data.value) return
    if (account.value === undefined || loading.value || claiming.value !== null ||
      (!force && Date.now() - lastLoaded < CACHE_MS)) return
    const { signal } = session
    loading.value = true
    loadFailed.value = false
    try {
      const snapshot = await getBloomTasks(signal)
      if (!signal.aborted && !applySnapshot(snapshot)) loadFailed.value = true
    } catch {
      if (!signal.aborted) loadFailed.value = true
    } finally {
      if (!signal.aborted) loading.value = false
    }
  }

  async function claim(task: BloomTask): Promise<{ unlocked: boolean; previousFlowers: number } | undefined> {
    if (Date.now() >= resetAt) {
      await refresh(true)
      return
    }
    const current = data.value?.tasks.find(candidate => candidate.id === task.id)
    if (account.value === undefined || claiming.value !== null || loading.value || !current || current.claimed) return
    const { signal } = session
    const previousFlowers = data.value?.collected_flowers ?? 0
    claiming.value = task.id
    try {
      const snapshot = await claimBloomTask(task.id, signal)
      if (signal.aborted) return
      const applied = applySnapshot(snapshot)
      void auth.refreshUser(signal).catch(() => {})
      return { unlocked: applied && snapshot.collected_flowers > previousFlowers, previousFlowers }
    } finally {
      if (!signal.aborted) {
        claiming.value = null
        if (!data.value) void refresh()
      }
    }
  }

  watch(account, clearSession, { flush: 'sync' })
  onScopeDispose(clearSession)
  return { data, loading, loadFailed, claiming, refresh, claim }
})
