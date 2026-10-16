<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl space-y-5">
      <header class="flex flex-wrap items-end justify-between gap-4 border-b border-gray-200 pb-5 dark:border-dark-700">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ copy.title }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ copy.description }}
          </p>
        </div>
        <div class="flex gap-2">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loadingProfiles || loadingGroups"
            @click="loadData"
          >
            <Icon name="refresh" size="sm" class="mr-1.5" />
            {{ copy.refresh }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="loadingProfiles || loadingGroups"
            @click="openCreate"
          >
            <Icon name="plus" size="sm" class="mr-1.5" />
            {{ copy.newProfile }}
          </button>
        </div>
      </header>

      <div
        v-if="loadingProfiles || loadingGroups"
        class="flex min-h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
      >
        {{ copy.loading }}
      </div>

      <div v-else class="overflow-x-auto border-y border-gray-200 dark:border-dark-700">
        <table class="w-full min-w-[720px] text-left text-sm">
          <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3 font-medium">{{ copy.profileColumn }}</th>
              <th class="px-4 py-3 font-medium">{{ copy.statusColumn }}</th>
              <th class="px-4 py-3 font-medium">{{ copy.groupsColumn }}</th>
              <th class="px-4 py-3 font-medium">{{ copy.maxCandidatesColumn }}</th>
              <th class="px-4 py-3 font-medium">{{ copy.revisionColumn }}</th>
              <th class="px-4 py-3 text-right font-medium">{{ copy.actionsColumn }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-if="profiles.length === 0">
              <td colspan="6" class="px-4 py-12 text-center text-gray-500 dark:text-gray-400">
                {{ copy.emptyProfiles }}
              </td>
            </tr>
            <tr v-for="profile in profiles" :key="profile.id">
              <td class="px-4 py-3 font-medium text-gray-900 dark:text-white">
                <span>{{ profile.name }}</span>
                <span class="ml-2 font-mono text-xs text-gray-500">#{{ profile.id }}</span>
              </td>
              <td class="px-4 py-3">
                <span
                  class="inline-flex rounded px-2 py-1 text-xs font-medium"
                  :class="profile.status === 'active'
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                    : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
                >
                  {{ profile.status === 'active' ? copy.active : copy.disabled }}
                </span>
              </td>
              <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                {{ profile.group_ids.length }}
              </td>
              <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                {{ profile.max_candidates }}
              </td>
              <td class="px-4 py-3 font-mono text-xs text-gray-500">
                {{ profile.revision }}
              </td>
              <td class="px-4 py-3">
                <div class="flex justify-end gap-1">
                  <button
                    type="button"
                    class="p-2 text-gray-500 hover:text-primary-600 disabled:opacity-40 dark:hover:text-primary-400"
                    :title="copy.editProfile(profile.name)"
                    :aria-label="copy.editProfile(profile.name)"
                    :disabled="Boolean(openingProfileId)"
                    @click="openEdit(profile)"
                  >
                    <Icon name="edit" size="sm" />
                  </button>
                  <button
                    type="button"
                    class="p-2 text-gray-500 hover:text-red-600 disabled:opacity-40"
                    :title="copy.disableProfile(profile.name)"
                    :aria-label="copy.disableProfile(profile.name)"
                    :disabled="deletingProfileId === profile.id || profile.status === 'disabled'"
                    @click="disableProfile(profile)"
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <BaseDialog
      :show="editorOpen"
      :title="editorMode === 'create' ? copy.createTitle : copy.editTitle"
      width="wide"
      @close="closeEditor"
    >
      <form id="auto-group-profile-form" class="space-y-5" @submit.prevent="saveProfile">
        <div
          v-if="revisionConflict"
          class="flex flex-wrap items-center justify-between gap-3 border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200"
          role="alert"
        >
          <span>{{ copy.revisionConflict }}</span>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loadingLatest"
            @click="reloadLatest"
          >
            {{ loadingLatest ? copy.loading : copy.reloadLatest }}
          </button>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label for="auto-profile-name" class="input-label">{{ copy.profileName }}</label>
            <input
              id="auto-profile-name"
              v-model.trim="form.name"
              class="input"
              maxlength="100"
              required
              autocomplete="off"
            />
          </div>
          <div>
            <label for="auto-profile-max-candidates" class="input-label">{{ copy.maxUserCandidates }}</label>
            <input
              id="auto-profile-max-candidates"
              v-model.number="form.max_candidates"
              class="input"
              type="number"
              min="1"
              step="1"
              required
            />
          </div>
        </div>

        <fieldset class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <legend class="sr-only">{{ copy.profileSettings }}</legend>
          <label class="flex items-center justify-between gap-4">
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ copy.profileActive }}</span>
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ copy.profileActiveHint }}</span>
            </span>
            <input
              v-model="form.status"
              type="checkbox"
              true-value="active"
              false-value="disabled"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
            />
          </label>
          <label class="flex items-center justify-between gap-4">
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ copy.allowCustomOrder }}</span>
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ copy.allowCustomOrderHint }}</span>
            </span>
            <input
              v-model="form.allow_custom_order"
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
            />
          </label>
          <label class="flex items-center justify-between gap-4">
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ copy.allowCrossGroupRetry }}</span>
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ copy.crossGroupRetryHint }}</span>
            </span>
            <input
              v-model="form.cross_group_retry_allowed"
              disabled
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
            />
          </label>
        </fieldset>

        <section class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <div>
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ copy.allowedGroupsOrder }}</h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ copy.groupEligibilityHint }}</p>
            </div>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ copy.selectedGroups(form.group_ids.length) }}</span>
          </div>

          <div
            v-if="invalidGroupIds.length"
            class="space-y-2 border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200"
            role="alert"
          >
            <p>{{ copy.invalidGroups }}</p>
            <div v-for="id in invalidGroupIds" :key="id" class="flex items-center justify-between gap-3">
              <span>{{ copy.unavailableGroup(id) }}</span>
              <button type="button" class="text-xs underline" @click="removeGroup(id)">{{ copy.remove }}</button>
            </div>
          </div>

          <div v-if="orderedSelectedGroups.length" class="divide-y divide-gray-100 border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
            <div
              v-for="(group, index) in orderedSelectedGroups"
              :key="group.id"
              class="flex min-h-12 items-center gap-3 px-3 py-2"
            >
              <span class="w-7 shrink-0 font-mono text-xs text-gray-500">{{ index + 1 }}</span>
              <span class="min-w-0 flex-1 truncate text-sm text-gray-900 dark:text-white">{{ group.name }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ group.platform }}</span>
              <span
                v-if="group.status !== 'active'"
                class="text-xs text-amber-700 dark:text-amber-300"
                >{{ copy.inactive }}</span>
              <button
                type="button"
                class="p-1.5 text-gray-500 hover:text-gray-900 disabled:opacity-30 dark:hover:text-white"
                :disabled="index === 0"
                :title="copy.moveGroupUp(group.name)"
                :aria-label="copy.moveGroupUp(group.name)"
                @click="moveGroup(index, -1)"
              >
                <Icon name="chevronUp" size="sm" />
              </button>
              <button
                type="button"
                class="p-1.5 text-gray-500 hover:text-gray-900 disabled:opacity-30 dark:hover:text-white"
                :disabled="index === orderedSelectedGroups.length - 1"
                :title="copy.moveGroupDown(group.name)"
                :aria-label="copy.moveGroupDown(group.name)"
                @click="moveGroup(index, 1)"
              >
                <Icon name="chevronDown" size="sm" />
              </button>
              <button
                type="button"
                class="p-1.5 text-gray-500 hover:text-red-600"
                :title="copy.removeNamedGroup(group.name)"
                :aria-label="copy.removeNamedGroup(group.name)"
                @click="removeGroup(group.id)"
              >
                <Icon name="x" size="sm" />
              </button>
            </div>
          </div>
          <p v-else class="border border-dashed border-gray-300 px-3 py-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
            {{ copy.noGroupsSelected }}
          </p>

          <div class="space-y-2">
            <label for="auto-profile-group-search" class="input-label">{{ copy.addRemoveGroups }}</label>
            <input
              id="auto-profile-group-search"
              v-model="groupSearch"
              class="input"
              type="search"
              :placeholder="copy.filterGroups"
            />
            <div class="max-h-56 divide-y divide-gray-100 overflow-y-auto border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
              <label
                v-for="group in filteredGroups"
                :key="group.id"
                class="flex min-h-11 cursor-pointer items-center gap-3 px-3 py-2 hover:bg-gray-50 dark:hover:bg-dark-800"
              >
                <input
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
                  :checked="form.group_ids.includes(group.id)"
                  @change="toggleGroup(group.id, ($event.target as HTMLInputElement).checked)"
                />
                <span class="min-w-0 flex-1 truncate text-sm text-gray-900 dark:text-white">{{ group.name }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ group.platform }}</span>
                <span v-if="group.status !== 'active'" class="text-xs text-amber-700 dark:text-amber-300">{{ copy.inactive }}</span>
              </label>
              <p v-if="filteredGroups.length === 0" class="px-3 py-4 text-sm text-gray-500 dark:text-gray-400">
                {{ copy.noMatchingGroups }}
              </p>
            </div>
          </div>
        </section>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="closeEditor">{{ copy.cancel }}</button>
        <button
          type="submit"
          form="auto-group-profile-form"
          class="btn btn-primary"
          :disabled="saving || revisionConflict || !canSave"
        >
          <Icon v-if="!saving" name="check" size="sm" class="mr-1.5" />
          {{ saving ? copy.saving : editorMode === 'create' ? copy.createProfile : copy.saveChanges }}
        </button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { autoGroupsAPI, type AutoGroupProfile, type AutoGroupProfileStatus } from '@/api/admin/autoGroups'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores'
import type { AdminGroup } from '@/types'

interface ProfileForm {
  name: string
  status: AutoGroupProfileStatus
  max_candidates: number
  allow_custom_order: boolean
  cross_group_retry_allowed: boolean
  group_ids: number[]
}

const appStore = useAppStore()
const { locale } = useI18n()
const profiles = ref<AutoGroupProfile[]>([])
const groups = ref<AdminGroup[]>([])
const loadingProfiles = ref(true)
const loadingGroups = ref(true)
const editorOpen = ref(false)
const editorMode = ref<'create' | 'edit'>('create')
const editingId = ref<number | null>(null)
const baseRevision = ref<number | null>(null)
const openingProfileId = ref<number | null>(null)
const deletingProfileId = ref<number | null>(null)
const loadingLatest = ref(false)
const saving = ref(false)
const revisionConflict = ref(false)
const groupSearch = ref('')
const form = reactive<ProfileForm>(emptyForm())

const copy = computed(() => (locale.value ?? 'en').toLowerCase().startsWith('zh')
  ? {
      title: '自动分组',
      description: '配置自动路由可使用的现有分组及其顺序。',
      refresh: '刷新',
      newProfile: '新建配置',
      loading: '正在加载自动分组…',
      profileColumn: '配置',
      statusColumn: '状态',
      groupsColumn: '分组数',
      maxCandidatesColumn: '候选组上限',
      revisionColumn: '修订版本',
      actionsColumn: '操作',
      emptyProfiles: '尚未配置自动分组。',
      active: '启用',
      disabled: '停用',
      editProfile: (name: string) => `编辑 ${name}`,
      disableProfile: (name: string) => `停用 ${name}`,
      createTitle: '新建自动分组配置',
      editTitle: '编辑自动分组配置',
      revisionConflict: '此配置已在服务器上更新。草稿已保留；请加载最新修订版本后再继续。',
      reloadLatest: '放弃草稿并加载最新版本',
      profileName: '配置名称',
      maxUserCandidates: '用户候选组数量上限',
      profileSettings: '配置设置',
      profileActive: '启用配置',
      profileActiveHint: '仅启用时，新请求才可使用此配置。',
      allowCustomOrder: '允许用户自定义顺序',
      allowCustomOrderHint: '用户可以从此配置中选择分组并调整顺序。',
      allowCrossGroupRetry: '允许跨组重试',
      crossGroupRetryHint: '此选项暂不可用；系统仅会在派发上游请求前选择其他候选组。',
      allowedGroupsOrder: '允许的分组及默认顺序',
      groupEligibilityHint: '不包含组合分组。停用分组仍会显示，便于检查。',
      selectedGroups: (count: number) => `已选择 ${count} 个分组`,
      invalidGroups: '部分已保存的分组 ID 不存在或不符合条件，请移除后再保存。',
      unavailableGroup: (id: number) => `分组 #${id}（不可用或为组合分组）`,
      remove: '移除',
      inactive: '已停用',
      moveGroupUp: (name: string) => `将 ${name} 上移`,
      moveGroupDown: (name: string) => `将 ${name} 下移`,
      removeNamedGroup: (name: string) => `移除 ${name}`,
      noGroupsSelected: '尚未选择分组。',
      addRemoveGroups: '添加或移除分组',
      filterGroups: '按分组名称或平台筛选',
      noMatchingGroups: '没有符合条件的非组合分组。',
      cancel: '取消',
      saving: '正在保存…',
      createProfile: '创建配置',
      saveChanges: '保存更改',
      requestFailed: '请求未能完成。',
      createSuccess: '自动分组配置已创建。',
      updateSuccess: '自动分组配置已更新。',
      disableConfirm: (name: string) => `确定停用自动分组配置“${name}”吗？历史引用会保留。`,
      disableSuccess: '自动分组配置已停用。'
    }
  : {
      title: 'Auto Groups',
      description: 'Configure existing groups and their order for Auto routing.',
      refresh: 'Refresh',
      newProfile: 'New profile',
      loading: 'Loading Auto Groups…',
      profileColumn: 'Profile',
      statusColumn: 'Status',
      groupsColumn: 'Groups',
      maxCandidatesColumn: 'Max candidates',
      revisionColumn: 'Revision',
      actionsColumn: 'Actions',
      emptyProfiles: 'No Auto profiles configured.',
      active: 'Active',
      disabled: 'Disabled',
      editProfile: (name: string) => `Edit ${name}`,
      disableProfile: (name: string) => `Disable ${name}`,
      createTitle: 'Create Auto profile',
      editTitle: 'Edit Auto profile',
      revisionConflict: 'This profile changed on the server. Your draft is preserved; reload the latest revision to continue.',
      reloadLatest: 'Discard draft and reload',
      profileName: 'Profile name',
      maxUserCandidates: 'Maximum user candidates',
      profileSettings: 'Profile settings',
      profileActive: 'Profile active',
      profileActiveHint: 'New requests may use this profile only when active.',
      allowCustomOrder: 'Allow custom user ordering',
      allowCustomOrderHint: 'Users can choose and order groups from this profile.',
      allowCrossGroupRetry: 'Allow cross-group retry',
      crossGroupRetryHint: 'Unavailable in this version; automatic fallback is limited to pre-dispatch selection.',
      allowedGroupsOrder: 'Allowed groups and default order',
      groupEligibilityHint: 'Composite groups are excluded. Inactive groups remain visible for review.',
      selectedGroups: (count: number) => `${count} selected`,
      invalidGroups: 'Some saved group IDs are missing or ineligible. Remove them before saving.',
      unavailableGroup: (id: number) => `Group #${id} (unavailable or composite)`,
      remove: 'Remove',
      inactive: 'Inactive',
      moveGroupUp: (name: string) => `Move ${name} up`,
      moveGroupDown: (name: string) => `Move ${name} down`,
      removeNamedGroup: (name: string) => `Remove ${name}`,
      noGroupsSelected: 'No groups selected.',
      addRemoveGroups: 'Add or remove groups',
      filterGroups: 'Filter by group name or platform',
      noMatchingGroups: 'No matching non-composite groups.',
      cancel: 'Cancel',
      saving: 'Saving…',
      createProfile: 'Create profile',
      saveChanges: 'Save changes',
      requestFailed: 'The request could not be completed.',
      createSuccess: 'Auto profile created.',
      updateSuccess: 'Auto profile updated.',
      disableConfirm: (name: string) => `Disable Auto profile "${name}"? Historical references will be retained.`,
      disableSuccess: 'Auto profile disabled.'
    })

function emptyForm(): ProfileForm {
  return {
    name: '',
    status: 'disabled',
    max_candidates: 1,
    allow_custom_order: false,
    cross_group_retry_allowed: false,
    group_ids: []
  }
}

const orderedSelectedGroups = computed(() =>
  form.group_ids
    .map(id => groups.value.find(group => group.id === id))
    .filter((group): group is AdminGroup => Boolean(group))
)

const invalidGroupIds = computed(() =>
  form.group_ids.filter(id => !groups.value.some(group => group.id === id))
)

const filteredGroups = computed(() => {
  const query = groupSearch.value.trim().toLocaleLowerCase()
  return groups.value.filter(group => {
    if (!query) return true
    return `${group.name} ${group.platform}`.toLocaleLowerCase().includes(query)
  })
})

const canSave = computed(() =>
  form.name.trim().length > 0
  && Number.isSafeInteger(form.max_candidates)
  && form.max_candidates >= 1
  && invalidGroupIds.value.length === 0
  && (form.status === 'disabled' || form.group_ids.length > 0)
)

function profileToForm(profile: AutoGroupProfile): ProfileForm {
  return {
    name: profile.name,
    status: profile.status,
    max_candidates: profile.max_candidates,
    allow_custom_order: profile.allow_custom_order,
    cross_group_retry_allowed: profile.cross_group_retry_allowed,
    group_ids: [...profile.group_ids]
  }
}

function errorMessage(error: unknown): string {
  const value = error as {
    message?: string
    response?: { data?: { detail?: string; message?: string } }
  }
  return value?.response?.data?.detail
    || value?.response?.data?.message
    || value?.message
    || copy.value.requestFailed
}

function isRevisionConflict(error: unknown): boolean {
  const value = error as { status?: number; response?: { status?: number } }
  return value?.status === 409 || value?.response?.status === 409
}

async function loadData() {
  loadingProfiles.value = true
  loadingGroups.value = true
  const [profileResult, groupResult] = await Promise.allSettled([
    autoGroupsAPI.list(),
    adminAPI.groups.getAllIncludingInactive()
  ])

  if (profileResult.status === 'fulfilled') {
    profiles.value = profileResult.value
  } else {
    appStore.showError(errorMessage(profileResult.reason))
  }

  if (groupResult.status === 'fulfilled') {
    groups.value = groupResult.value
      .filter(group => group.platform !== 'composite')
      .sort((a, b) => a.name.localeCompare(b.name))
  } else {
    appStore.showError(errorMessage(groupResult.reason))
  }

  loadingProfiles.value = false
  loadingGroups.value = false
}

function resetEditor() {
  editingId.value = null
  baseRevision.value = null
  revisionConflict.value = false
  groupSearch.value = ''
  Object.assign(form, emptyForm())
}

function openCreate() {
  resetEditor()
  editorMode.value = 'create'
  editorOpen.value = true
}

async function openEdit(profile: AutoGroupProfile) {
  openingProfileId.value = profile.id
  try {
    const latest = await autoGroupsAPI.getById(profile.id)
    resetEditor()
    editorMode.value = 'edit'
    editingId.value = latest.id
    baseRevision.value = latest.revision
    Object.assign(form, profileToForm(latest))
    editorOpen.value = true
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    openingProfileId.value = null
  }
}

function closeEditor() {
  if (saving.value) return
  editorOpen.value = false
  resetEditor()
}

function toggleGroup(id: number, selected: boolean) {
  if (selected && !form.group_ids.includes(id)) {
    form.group_ids.push(id)
  } else if (!selected) {
    removeGroup(id)
  }
}

function removeGroup(id: number) {
  form.group_ids = form.group_ids.filter(groupId => groupId !== id)
}

function moveGroup(index: number, direction: -1 | 1) {
  const target = index + direction
  if (target < 0 || target >= form.group_ids.length) return
  const next = [...form.group_ids]
  ;[next[index], next[target]] = [next[target], next[index]]
  form.group_ids = next
}

async function saveProfile() {
  if (!canSave.value || revisionConflict.value || saving.value) return
  saving.value = true
  const payload = {
    name: form.name.trim(),
    status: form.status,
    max_candidates: form.max_candidates,
    allow_custom_order: form.allow_custom_order,
    cross_group_retry_allowed: form.cross_group_retry_allowed,
    group_ids: [...form.group_ids]
  }

  try {
    const saved = editorMode.value === 'create'
      ? await autoGroupsAPI.create(payload)
      : await autoGroupsAPI.update(editingId.value!, {
          ...payload,
          revision: baseRevision.value!
        })
    const existingIndex = profiles.value.findIndex(profile => profile.id === saved.id)
    if (existingIndex === -1) profiles.value.unshift(saved)
    else profiles.value.splice(existingIndex, 1, saved)
    appStore.showSuccess(editorMode.value === 'create' ? copy.value.createSuccess : copy.value.updateSuccess)
    closeEditor()
  } catch (error) {
    if (isRevisionConflict(error) && editorMode.value === 'edit') {
      revisionConflict.value = true
    } else {
      appStore.showError(errorMessage(error))
    }
  } finally {
    saving.value = false
  }
}

async function reloadLatest() {
  if (editingId.value === null) return
  loadingLatest.value = true
  try {
    const latest = await autoGroupsAPI.getById(editingId.value)
    baseRevision.value = latest.revision
    Object.assign(form, profileToForm(latest))
    revisionConflict.value = false
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    loadingLatest.value = false
  }
}

async function disableProfile(profile: AutoGroupProfile) {
  if (!window.confirm(copy.value.disableConfirm(profile.name))) return
  deletingProfileId.value = profile.id
  try {
    await autoGroupsAPI.delete(profile.id, profile.revision)
    await loadData()
    appStore.showSuccess(copy.value.disableSuccess)
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    deletingProfileId.value = null
  }
}

onMounted(loadData)
</script>
