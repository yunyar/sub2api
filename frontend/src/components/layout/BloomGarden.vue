<template>
  <div class="bloom-garden group relative mb-2" :class="{ 'bloom-garden-collapsed': collapsed }">
    <button
      type="button"
      class="flex min-h-11 w-full items-center gap-2 rounded-lg bg-emerald-50 px-2 py-1 text-start text-sm font-medium text-emerald-900 hover:bg-emerald-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-600 dark:bg-emerald-950/50 dark:text-emerald-200 dark:hover:bg-emerald-900/60"
      :class="{ 'justify-center': collapsed }"
      :aria-label="t('bloom.title')"
      :aria-describedby="show ? undefined : previewId"
      aria-haspopup="dialog"
      :aria-expanded="show"
      @pointerenter="previewOnPointer"
      @focus="refresh()"
      @click="open"
    >
      <BloomFlower :stage="stage" class="h-8 w-8 shrink-0" />
      <span v-if="!collapsed" class="min-w-0 truncate">{{ t('bloom.title') }}</span>
    </button>
    <div
      v-if="!show"
      :id="previewId"
      role="tooltip"
      class="bloom-preview invisible absolute bottom-full left-0 z-10 mb-2 w-max max-w-60 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 shadow-lg group-focus-within:visible dark:border-dark-600 dark:text-dark-200"
    >
      <template v-if="data">
        <p class="font-semibold">{{ t(`bloom.stage.${stage}`) }}</p>
        <p>{{ t('bloom.todayProgress', { complete: completedDaily, total: dailyTasks.length }) }}</p>
        <p v-if="nextTask" class="mt-1 border-t border-gray-100 pt-1 dark:border-dark-600">
          {{ t('bloom.nextTask', {
            metric: t(`bloom.metric.${nextTask.metric}`),
            current: localeNumber.format(nextTask.progress),
            target: localeNumber.format(nextTask.target)
          }) }}
        </p>
      </template>
      <p v-else>{{ t(loadFailed ? 'bloom.loadFailed' : loading ? 'bloom.loading' : 'bloom.periodEnded') }}</p>
    </div>
    <BaseDialog :show="show" :title="t('bloom.title')" close-on-click-outside @close="show = false">
      <div ref="dialogBody" :aria-busy="loading && !data">
        <p v-if="loading && !data" role="status" class="py-6 text-center text-sm text-gray-500 dark:text-dark-300">
          {{ t('bloom.loading') }}
        </p>
        <div v-if="loadFailed" class="mb-4 flex flex-wrap items-center justify-between gap-2">
          <p role="alert" class="text-sm text-red-700 dark:text-red-300">{{ t('bloom.loadFailed') }}</p>
          <button type="button" class="btn btn-secondary btn-sm" @click="refresh(true)">{{ t('bloom.retry') }}</button>
        </div>
        <p class="mb-4 text-sm text-gray-600 dark:text-dark-300">{{ t('bloom.planSchedule') }}</p>
        <div v-if="!data && !loading && !loadFailed" class="flex flex-wrap items-center justify-between gap-2">
          <p role="status" class="text-sm text-gray-600 dark:text-dark-300">{{ t('bloom.periodEnded') }}</p>
          <button type="button" class="btn btn-secondary btn-sm" @click="refresh(true)">{{ t('bloom.viewCurrent') }}</button>
        </div>
        <template v-if="data">
          <div class="flex flex-wrap items-center justify-between gap-4 rounded-lg bg-emerald-50 p-4 dark:bg-emerald-950/40">
            <div>
              <p class="font-semibold text-emerald-900 dark:text-emerald-200">{{ t(`bloom.stage.${stage}`) }}</p>
              <p class="mt-1 text-sm text-emerald-800 dark:text-emerald-300">
                {{ t('bloom.todayProgress', { complete: completedDaily, total: dailyTasks.length }) }}
              </p>
            </div>
            <ol class="flex items-center gap-2">
              <li
                v-for="growth in stages"
                :key="growth"
                class="flex flex-col items-center gap-1 text-xs text-emerald-800 dark:text-emerald-300"
                :aria-current="stage === growth ? 'step' : undefined"
              >
                <BloomFlower
                  :stage="growth"
                  class="h-9 w-9 rounded-full"
                  :class="stage === growth ? 'ring-2 ring-emerald-600 dark:ring-emerald-400' : 'opacity-50'"
                />
                {{ t(`bloom.stage.${growth}`) }}
              </li>
            </ol>
          </div>
          <section v-for="section in sections" :key="section.name" class="mt-5">
            <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
              <h4 class="font-semibold text-gray-900 dark:text-white">{{ t(`bloom.${section.name}`) }}</h4>
              <p v-if="section.name === 'weekly'" class="flex items-center gap-1 text-sm font-medium text-pink-700 dark:text-pink-300">
                <BloomFlower stage="flower" class="h-5 w-5" />
                {{ t('bloom.collection', { count: data.collected_flowers }) }}
              </p>
            </div>
            <p v-if="section.name === 'weekly'" class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-gray-600 dark:text-dark-300">
              <span v-if="currentBadge" class="inline-flex items-center gap-1 font-medium text-gray-900 dark:text-white">
                <BloomFlower stage="flower" :petal-color="currentBadge.color" class="h-6 w-6" />
                {{ t(`bloom.badge.${currentBadge.name}`) }}
              </span>
              <span>{{ nextBadge ? t('bloom.badgeNext', { count: nextBadge.count - data.collected_flowers, badge: t(`bloom.badge.${nextBadge.name}`) }) : t('bloom.badgeComplete') }}</span>
            </p>
            <p class="mt-1 text-xs text-gray-600 dark:text-dark-300">
              {{ t('bloom.resets', { time: resetTime(section.reset, data.timezone), timezone: data.timezone }) }}
            </p>
            <p v-if="section.name === 'weekly'" class="mt-1 text-xs text-gray-600 dark:text-dark-300">
              {{ t('bloom.budget', { claimed: money(data.claimed_micros), budget: money(data.budget_micros) }) }}
            </p>
            <p v-if="!section.tasks.length" class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ t('bloom.empty') }}</p>
            <ul v-else class="mt-2 divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
              <li
                v-for="task in section.tasks"
                :key="task.id"
                :data-template-id="task.template_id"
                tabindex="-1"
                class="px-3 py-3"
              >
                <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
                  <p class="text-sm font-medium text-gray-900 dark:text-white">
                    <span v-if="task.tier !== 'weekly'" class="mr-1 text-xs text-emerald-700 dark:text-emerald-300">{{ t(`bloom.tier.${task.tier}`) }}</span>
                    {{ t(`bloom.metric.${task.metric}`) }}
                  </p>
                  <span class="text-sm font-semibold tabular-nums text-gray-800 dark:text-dark-100">+{{ money(task.reward_micros) }}</span>
                </div>
                <div class="mt-2 flex items-center gap-2 text-xs text-gray-600 dark:text-dark-300">
                  <progress
                    class="h-2 min-w-0 flex-1 appearance-none overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"
                    :aria-label="t(`bloom.metric.${task.metric}`)"
                    :value="task.progress"
                    :max="Math.max(task.target, 1)"
                  />
                  <span class="tabular-nums">{{ localeNumber.format(task.progress) }} / {{ localeNumber.format(task.target) }}</span>
                </div>
                <div class="mt-2 flex flex-wrap items-center justify-between gap-2">
                  <span v-if="task.claimed || isComplete(task)" class="inline-flex items-center gap-1 text-xs font-semibold text-emerald-800 dark:text-emerald-300">
                    <Icon name="checkCircle" size="sm" aria-hidden="true" />
                    {{ t(task.claimed ? 'bloom.claimed' : 'bloom.complete') }}
                  </span>
                  <span v-else-if="!isCheckin(task)" class="text-xs text-gray-600 dark:text-dark-300">{{ t('bloom.inProgress') }}</span>
                  <button
                    v-if="!task.claimed && (isCheckin(task) || isComplete(task))"
                    type="button"
                    class="btn btn-primary btn-sm ml-auto min-h-9 aria-disabled:opacity-60"
                    :aria-disabled="claiming !== null || loading"
                    @click="claim(task, $event)"
                  >
                    {{ t(claiming === task.id ? 'bloom.claiming' : isCheckin(task) && !isComplete(task) ? 'bloom.checkin' : 'bloom.claim') }}
                  </button>
                </div>
                <p v-if="failedClaim === task.id" role="alert" class="mt-2 text-xs text-red-700 dark:text-red-300">{{ t('bloom.claimFailed') }}</p>
              </li>
            </ul>
          </section>
        </template>
        <p role="status" :class="feedback ? 'mt-4 text-sm font-medium text-emerald-800 dark:text-emerald-300' : 'sr-only'">
          {{ feedback }}
        </p>
      </div>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onMounted, ref, useId, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useEventListener } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import type { BloomTask } from '@/api/bloom'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useBloomStore } from '@/stores/bloom'
import { formatCurrency, formatDate } from '@/utils/format'
import BloomFlower from './BloomFlower.vue'

defineProps<{ collapsed: boolean }>()

const { t, locale } = useI18n()
const previewId = useId()
const show = ref(false)
const dialogBody = ref<HTMLElement | null>(null)
const bloom = useBloomStore()
const { data, loading, loadFailed, claiming } = storeToRefs(bloom)
const { refresh } = bloom
const failedClaim = ref<BloomTask['id'] | null>(null)
const feedback = ref('')

const tiers = ['easy', 'medium', 'hard'] as const
const stages = ['seed', 'sprout', 'flower'] as const
const REFRESH_INTERVAL_MS = 45_000
const badges = [
  { count: 1, name: 'peach', color: '#ee83a5' },
  { count: 3, name: 'amber', color: '#ed9a31' },
  { count: 6, name: 'jade', color: '#38af90' },
  { count: 12, name: 'iris', color: '#8c7ce8' }
] as const
const dailyTasks = computed(() => tiers.flatMap(tier => data.value?.tasks.filter(task => task.tier === tier) ?? []))
const completedDaily = computed(() => dailyTasks.value.filter(isComplete).length)
const nextTask = computed(() => dailyTasks.value.find(task => !isComplete(task)))
const currentBadge = computed(() => badgeFor(data.value?.collected_flowers ?? 0))
const nextBadge = computed(() => badges.find(badge => badge.count > (data.value?.collected_flowers ?? 0)))
const stage = computed(() => {
  if (!completedDaily.value) return 'seed'
  return completedDaily.value === dailyTasks.value.length ? 'flower' : 'sprout'
})
const sections = computed(() => [
  { name: 'daily', tasks: dailyTasks.value, reset: data.value?.resets_at },
  { name: 'weekly', tasks: data.value?.tasks.filter(task => task.tier === 'weekly') ?? [], reset: data.value?.week_resets_at }
] as const)
const localeNumber = computed(() => new Intl.NumberFormat(locale.value))

function badgeFor(count: number) {
  return [...badges].reverse().find(badge => badge.count <= count)
}

function isComplete(task: BloomTask) {
  return task.claimed || task.progress >= task.target
}

function isCheckin(task: BloomTask) {
  return task.tier === 'easy' && task.metric === 'checkin'
}

function money(micros: number) {
  return formatCurrency(micros / 1_000_000)
}

function resetTime(reset: string | undefined, timezone: string) {
  const options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }
  try {
    return formatDate(reset, { ...options, timeZone: timezone }, locale.value)
  } catch {
    return formatDate(reset, { ...options, timeZone: 'UTC' }, locale.value)
  }
}

function previewOnPointer(event: PointerEvent) {
  if (event.pointerType !== 'touch') void refresh()
}

function open(event: MouseEvent) {
  (event.currentTarget as HTMLButtonElement).focus({ preventScroll: true })
  show.value = true
  void refresh()
}

async function claim(task: BloomTask, event: MouseEvent) {
  if (claiming.value !== null || loading.value) return
  const button = event.currentTarget as HTMLButtonElement
  const row = button.closest('li')
  const restoreFocus = document.activeElement === button
  failedClaim.value = null
  feedback.value = ''
  try {
    const result = await bloom.claim(task)
    if (!result || !show.value) return
    const reward = t('bloom.claimSuccess', { reward: money(task.reward_micros) })
    const unlocked = result.unlocked && currentBadge.value && currentBadge.value !== badgeFor(result.previousFlowers)
    feedback.value = unlocked && currentBadge.value
      ? `${reward} ${t('bloom.badgeUnlocked', { badge: t(`bloom.badge.${currentBadge.value.name}`) })}`
      : reward
    await nextTick()
    if (restoreFocus && show.value && !button.isConnected && document.activeElement === document.body) {
      if (row?.isConnected) row.focus()
      else dialogBody.value?.closest('[role="dialog"]')?.querySelector<HTMLElement>('button')?.focus()
    }
  } catch {
    failedClaim.value = task.id
  }
}

useEventListener(computed(() => show.value ? document : undefined), 'keydown', (event: KeyboardEvent) => {
  if (event.key !== 'Tab' || !show.value) return
  const modal = dialogBody.value?.closest('[role="dialog"]')
  if (!modal?.contains(document.activeElement)) return
  const buttons = Array.from(modal.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'))
  const lastButton = buttons[buttons.length - 1]
  const boundary = event.shiftKey ? buttons[0] : lastButton
  if (document.activeElement === boundary) {
    event.preventDefault()
    const next = event.shiftKey ? lastButton : buttons[0]
    next?.focus()
  }
})

watch(show, (isOpen, _, onCleanup) => {
  if (!isOpen) {
    feedback.value = ''
    return
  }

  let interval: ReturnType<typeof setInterval> | undefined
  function syncRefresh(comingForward: boolean) {
    clearInterval(interval)
    if (document.visibilityState !== 'visible') return
    if (comingForward) void refresh()
    interval = setInterval(() => {
      if (document.visibilityState === 'visible') void refresh(false, { background: true })
    }, REFRESH_INTERVAL_MS)
  }

  const refreshOnReturn = () => syncRefresh(true)
  syncRefresh(false)
  document.addEventListener('visibilitychange', refreshOnReturn)
  window.addEventListener('focus', refreshOnReturn)
  onCleanup(() => {
    clearInterval(interval)
    document.removeEventListener('visibilitychange', refreshOnReturn)
    window.removeEventListener('focus', refreshOnReturn)
  })
})
onMounted(() => void refresh())
onActivated(() => void refresh())
onDeactivated(() => { show.value = false })
</script>

<style scoped>
.bloom-preview {
  @apply bg-white dark:bg-dark-800;
}

.bloom-garden-collapsed .bloom-preview {
  inset: 50% auto auto calc(100% + 1rem);
  margin: 0;
  transform: translateY(-50%);
}

@media (hover: hover) {
  .bloom-garden:hover [role='tooltip'] {
    visibility: visible;
  }
}

progress::-webkit-progress-bar {
  background: transparent;
}

progress::-webkit-progress-value {
  border-radius: 9999px;
  background: #2f9e76;
}

progress::-moz-progress-bar {
  border-radius: 9999px;
  background: #2f9e76;
}
</style>
