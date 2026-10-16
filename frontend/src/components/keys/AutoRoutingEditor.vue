<template>
  <BaseDialog
    :show="show"
    :title="copy.title"
    width="wide"
    :close-on-escape="!saving"
    :show-close-button="!saving"
    @close="close"
  >
    <div v-if="loading" class="flex min-h-36 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      <Icon name="refresh" size="md" class="mr-2 animate-spin" />
      {{ copy.loading }}
    </div>

    <div v-else class="space-y-5">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="min-w-0 text-sm font-medium text-gray-800 dark:text-gray-100">
          {{ apiKey?.name }}
        </p>
        <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">
          {{ copy.revision }} {{ config.routing_revision }}
        </span>
      </div>

      <div class="inline-flex rounded-md border border-gray-200 p-1 dark:border-dark-600" role="group">
        <button
          type="button"
          class="min-h-9 px-4 text-sm font-medium transition-colors"
          :class="mode === 'fixed'
            ? 'rounded-sm bg-gray-100 text-gray-900 dark:bg-dark-700 dark:text-white'
            : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
          :aria-pressed="mode === 'fixed'"
          :disabled="saving"
          @click="mode = 'fixed'"
        >
          {{ copy.fixed }}
        </button>
        <button
          type="button"
          class="min-h-9 px-4 text-sm font-medium transition-colors"
          :class="mode === 'auto'
            ? 'rounded-sm bg-primary-50 text-primary-700 dark:bg-primary-500/15 dark:text-primary-300'
            : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
          :aria-pressed="mode === 'auto'"
          :disabled="saving || availableProfiles.length === 0"
          @click="mode = 'auto'"
        >
          {{ copy.auto }}
        </button>
      </div>

      <div v-if="mode === 'fixed'" class="space-y-2">
        <label for="key-auto-routing-fixed-group" class="input-label">{{ copy.fixedGroup }}</label>
        <select
          id="key-auto-routing-fixed-group"
          v-model.number="fixedGroupId"
          class="input"
          :disabled="saving"
        >
          <option :value="0" disabled>{{ copy.selectGroup }}</option>
          <option v-for="group in groups" :key="group.id" :value="group.id">
            {{ group.name }}
          </option>
        </select>
      </div>

      <div v-else class="space-y-5">
        <div class="space-y-2">
          <label for="key-auto-routing-profile" class="input-label">{{ copy.profile }}</label>
          <select
            id="key-auto-routing-profile"
            v-model.number="profileId"
            class="input"
            :disabled="saving || availableProfiles.length === 0"
            @change="onProfileChange"
          >
            <option :value="0" disabled>{{ copy.selectProfile }}</option>
            <option
              v-if="profileId > 0 && !selectedProfile"
              :value="profileId"
              disabled
            >
              {{ copy.unavailableProfile }} #{{ profileId }}
            </option>
            <option
              v-for="profile in availableProfiles"
              :key="profile.id"
              :value="profile.id"
            >
              {{ profile.name }}
            </option>
          </select>
        </div>

        <template v-if="selectedProfile">
          <div class="space-y-2">
            <span class="input-label">{{ copy.selection }}</span>
            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                class="min-h-9 rounded-sm border px-3 text-sm transition-colors"
                :class="selectionMode === 'inherit'
                  ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
                  : 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-300'"
                :aria-pressed="selectionMode === 'inherit'"
                :disabled="saving"
                @click="setSelectionMode('inherit')"
              >
                {{ copy.inherit }}
              </button>
              <button
                v-if="selectedProfile.allow_custom_order"
                type="button"
                class="min-h-9 rounded-sm border px-3 text-sm transition-colors"
                :class="selectionMode === 'custom'
                  ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
                  : 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-300'"
                :aria-pressed="selectionMode === 'custom'"
                :disabled="saving"
                @click="setSelectionMode('custom')"
              >
                {{ copy.customOrder }}
              </button>
            </div>
          </div>

          <div v-if="selectionMode === 'inherit'" class="space-y-2">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ copy.profileOrder }}</p>
            <ol class="max-h-56 space-y-1 overflow-y-auto rounded-md border border-gray-200 dark:border-dark-600">
              <li
                v-for="(group, index) in eligibleGroups.slice(0, selectedProfile.max_candidates)"
                :key="group.id"
                class="flex min-h-10 items-center gap-3 border-b border-gray-100 px-3 py-2 last:border-0 dark:border-dark-700"
              >
                <span class="w-6 shrink-0 text-right text-xs tabular-nums text-gray-400">{{ index + 1 }}</span>
                <span class="min-w-0 break-words text-sm text-gray-800 dark:text-gray-100">{{ group.name }}</span>
              </li>
              <li v-if="eligibleGroups.length === 0" class="px-3 py-4 text-sm text-gray-500 dark:text-gray-400">
                {{ copy.noAuthorizedGroups }}
              </li>
            </ol>
          </div>

          <div v-else class="space-y-3">
            <div class="space-y-1">
              <label
                v-for="group in eligibleGroups"
                :key="group.id"
                class="flex min-h-10 items-center gap-3 border-b border-gray-100 py-2 text-sm last:border-0 dark:border-dark-700"
              >
                <input
                  type="checkbox"
                  class="checkbox shrink-0"
                  :checked="customGroupIds.includes(group.id)"
                  :disabled="saving || (!customGroupIds.includes(group.id) && customGroupIds.length >= selectedProfile.max_candidates)"
                  @change="toggleCustomGroup(group.id, ($event.target as HTMLInputElement).checked)"
                />
                <span class="min-w-0 flex-1 break-words text-gray-800 dark:text-gray-100">{{ group.name }}</span>
                <span
                  v-if="customGroupIds.includes(group.id)"
                  class="flex shrink-0 items-center gap-1"
                >
                  <span class="w-5 text-center text-xs tabular-nums text-gray-500">
                    {{ customGroupIds.indexOf(group.id) + 1 }}
                  </span>
                  <button
                    type="button"
                    class="inline-flex h-8 w-8 items-center justify-center rounded-sm text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700"
                    :aria-label="copy.moveUp"
                    :title="copy.moveUp"
                    :disabled="saving || customGroupIds.indexOf(group.id) === 0"
                    @click="moveGroup(group.id, -1)"
                  >
                    <Icon name="arrowUp" size="sm" />
                  </button>
                  <button
                    type="button"
                    class="inline-flex h-8 w-8 items-center justify-center rounded-sm text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700"
                    :aria-label="copy.moveDown"
                    :title="copy.moveDown"
                    :disabled="saving || customGroupIds.indexOf(group.id) === customGroupIds.length - 1"
                    @click="moveGroup(group.id, 1)"
                  >
                    <Icon name="arrowDown" size="sm" />
                  </button>
                </span>
              </label>
              <p v-if="eligibleGroups.length === 0" class="py-3 text-sm text-gray-500 dark:text-gray-400">
                {{ copy.noAuthorizedGroups }}
              </p>
            </div>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ copy.customCount }} {{ customGroupIds.length }}/{{ selectedProfile.max_candidates }}
            </p>
          </div>

          <label class="flex min-h-10 items-center gap-3 border-t border-gray-200 pt-3 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
            <input type="checkbox" class="checkbox" disabled />
            <span>{{ copy.crossGroupRetryOff }}</span>
          </label>
        </template>
      </div>

      <div v-if="loadError" class="flex flex-wrap items-center justify-between gap-2" role="alert">
        <p class="min-w-0 break-words text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
        <button
          v-if="!loading && !saving"
          type="button"
          class="btn btn-secondary btn-sm shrink-0"
          @click="load"
        >
          {{ copy.reload }}
        </button>
      </div>
    </div>

    <template #footer>
      <div class="flex w-full justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">
          {{ copy.cancel }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!canSave"
          @click="save"
        >
          <Icon v-if="saving" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ saving ? copy.saving : copy.save }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { keysAPI } from '@/api'
import { autoRoutingAPI, type AvailableAutoGroupProfile, type KeyRoutingMode, type AutoSelectionMode } from '@/api/autoRouting'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Group } from '@/types'

interface KeySummary {
  id: number
  name: string
  group_id: number | null
}

const props = defineProps<{
  show: boolean
  apiKey: KeySummary | null
  groups: Group[]
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { locale } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const availableProfiles = ref<AvailableAutoGroupProfile[]>([])
const config = reactive({
  routing_mode: 'fixed' as KeyRoutingMode,
  auto_profile_id: null as number | null,
  auto_selection_mode: 'inherit' as AutoSelectionMode,
  group_ids: [] as number[],
  cross_group_retry: false,
  routing_revision: 0,
  group_id: null as number | null
})
const mode = ref<KeyRoutingMode>('fixed')
const profileId = ref(0)
const selectionMode = ref<AutoSelectionMode>('inherit')
const customGroupIds = ref<number[]>([])
const fixedGroupId = ref(0)

const copy = computed(() => (locale?.value ?? 'en').toLowerCase().startsWith('zh')
  ? {
      title: '密钥路由配置',
      loading: '加载中...',
      revision: '修订版本',
      fixed: '固定分组',
      auto: '自动分组',
      fixedGroup: '固定分组',
      selectGroup: '选择分组',
      profile: '自动分组配置',
      selectProfile: '选择配置',
      unavailableProfile: '当前配置不可用',
      selection: '候选分组',
      inherit: '继承默认顺序',
      customOrder: '自定义分组和顺序',
      profileOrder: '按管理员配置顺序使用有权限的分组。',
      noAuthorizedGroups: '当前没有可用的授权分组。',
      moveUp: '上移',
      moveDown: '下移',
      customCount: '已选数量',
      crossGroupRetryOff: '跨分组重试：关闭',
      loadFailed: '无法加载路由配置',
      saveFailed: '无法保存路由配置',
      saved: '路由配置已保存',
      reload: '重新加载',
      cancel: '取消',
      save: '保存',
      saving: '保存中...'
    }
  : {
      title: 'Key routing',
      loading: 'Loading...',
      revision: 'Revision',
      fixed: 'Fixed',
      auto: 'Auto',
      fixedGroup: 'Fixed group',
      selectGroup: 'Select a group',
      profile: 'Auto profile',
      selectProfile: 'Select a profile',
      unavailableProfile: 'Current profile unavailable',
      selection: 'Candidate groups',
      inherit: 'Inherit default order',
      customOrder: 'Custom groups and order',
      profileOrder: 'Authorized groups follow the administrator profile order.',
      noAuthorizedGroups: 'No authorized groups are currently available.',
      moveUp: 'Move up',
      moveDown: 'Move down',
      customCount: 'Selected',
      crossGroupRetryOff: 'Cross-group retry: off',
      loadFailed: 'Could not load routing configuration',
      saveFailed: 'Could not save routing configuration',
      saved: 'Routing configuration saved',
      reload: 'Reload',
      cancel: 'Cancel',
      save: 'Save',
      saving: 'Saving...'
    })

const selectedProfile = computed(() =>
  availableProfiles.value.find((profile) => profile.id === profileId.value) ?? null
)

const eligibleGroups = computed(() => {
  const authorizedGroups = new Map(props.groups.map((group) => [group.id, group]))
  return (selectedProfile.value?.group_ids ?? [])
    .filter((groupId) => authorizedGroups.has(groupId))
    .map((groupId) => authorizedGroups.get(groupId)!)
    .filter((group) => group.platform.toLowerCase() !== 'composite')
})

const canSave = computed(() => {
  if (!props.apiKey || loading.value || saving.value || !!loadError.value) return false
  if (mode.value === 'fixed') {
    return fixedGroupId.value > 0 && props.groups.some((group) => group.id === fixedGroupId.value)
  }
  if (!selectedProfile.value || eligibleGroups.value.length === 0) return false
  if (selectionMode.value === 'custom') {
    return selectedProfile.value.allow_custom_order &&
      customGroupIds.value.length > 0 &&
      customGroupIds.value.length <= selectedProfile.value.max_candidates &&
      customGroupIds.value.every((id) => eligibleGroups.value.some((group) => group.id === id))
  }
  return true
})

const clearFormError = () => {
  loadError.value = ''
}

const load = async () => {
  if (!props.apiKey) return
  loading.value = true
  loadError.value = ''
  try {
    const [routingConfig, profiles] = await Promise.all([
      keysAPI.getAutoRouting(props.apiKey.id),
      autoRoutingAPI.getAvailableProfiles()
    ])
    availableProfiles.value = profiles.filter((profile) => profile.status === 'active')
    Object.assign(config, routingConfig)
    mode.value = routingConfig.routing_mode
    profileId.value = routingConfig.auto_profile_id ?? 0
    selectionMode.value = routingConfig.auto_selection_mode ?? 'inherit'
    const profile = availableProfiles.value.find((item) => item.id === profileId.value)
    const availableIds = new Set(
      (profile?.group_ids ?? []).filter((id) =>
        props.groups.some((group) => group.id === id && group.platform.toLowerCase() !== 'composite')
      )
    )
    customGroupIds.value = [...new Set(routingConfig.group_ids ?? [])]
      .filter((id) => availableIds.has(id))
    fixedGroupId.value = routingConfig.group_id ?? props.apiKey.group_id ?? 0
  } catch (error) {
    loadError.value = apiErrorMessage(error) || copy.value.loadFailed
  } finally {
    loading.value = false
  }
}

const onProfileChange = () => {
  selectionMode.value = 'inherit'
  customGroupIds.value = []
  clearFormError()
}

const setSelectionMode = (nextMode: AutoSelectionMode) => {
  if (nextMode === 'custom' && !selectedProfile.value?.allow_custom_order) return
  selectionMode.value = nextMode
  if (nextMode === 'custom' && customGroupIds.value.length === 0 && selectedProfile.value) {
    customGroupIds.value = eligibleGroups.value
      .slice(0, selectedProfile.value.max_candidates)
      .map((group) => group.id)
  }
  clearFormError()
}

const toggleCustomGroup = (groupId: number, checked: boolean) => {
  if (checked) {
    if (selectedProfile.value && customGroupIds.value.length >= selectedProfile.value.max_candidates) return
    if (!customGroupIds.value.includes(groupId)) customGroupIds.value.push(groupId)
  } else {
    customGroupIds.value = customGroupIds.value.filter((id) => id !== groupId)
  }
  clearFormError()
}

const moveGroup = (groupId: number, direction: -1 | 1) => {
  const index = customGroupIds.value.indexOf(groupId)
  const target = index + direction
  if (index < 0 || target < 0 || target >= customGroupIds.value.length) return
  const next = [...customGroupIds.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  customGroupIds.value = next
  clearFormError()
}

const save = async () => {
  if (!props.apiKey || !canSave.value) return
  saving.value = true
  loadError.value = ''
  try {
    const payload = {
      routing_mode: mode.value,
      auto_profile_id: mode.value === 'auto' ? profileId.value : null,
      auto_selection_mode: mode.value === 'auto' ? selectionMode.value : 'inherit',
      group_ids: mode.value === 'auto' && selectionMode.value === 'custom'
        ? [...customGroupIds.value]
        : [],
      cross_group_retry: false,
      routing_revision: config.routing_revision,
      group_id: mode.value === 'fixed' ? fixedGroupId.value : null
    } satisfies Parameters<typeof keysAPI.updateAutoRouting>[1]
    const updated = await keysAPI.updateAutoRouting(props.apiKey.id, payload)
    Object.assign(config, updated)
    appStore.showSuccess(copy.value.saved)
    emit('saved')
    emit('close')
  } catch (error) {
    loadError.value = apiErrorMessage(error) || copy.value.saveFailed
  } finally {
    saving.value = false
  }
}

const close = () => {
  if (!saving.value) emit('close')
}

function apiErrorMessage(error: unknown): string {
  if (!error || typeof error !== 'object') return ''
  const value = error as {
    message?: string
    response?: { data?: { detail?: string; message?: string } }
  }
  if (value.response?.data?.detail) return value.response.data.detail
  if (value.response?.data?.message) return value.response.data.message
  if (value.message && value.message !== 'Unknown error') return value.message
  return ''
}

watch(
  () => [props.show, props.apiKey?.id] as const,
  ([isOpen]) => {
    if (isOpen) void load()
  },
  { immediate: true }
)
</script>
