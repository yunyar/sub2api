<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl space-y-6">
      <div class="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('adminCommunityQRCodes.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            {{ t('adminCommunityQRCodes.description') }}
          </p>
        </div>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary" @click="openCreate">
            <Icon name="plus" size="sm" class="mr-1.5" />
            {{ t('adminCommunityQRCodes.add') }}
          </button>
          <button type="button" class="btn btn-primary" :disabled="saving || loading" @click="saveAll">
            <Icon name="check" size="sm" class="mr-1.5" />
            {{ t('adminCommunityQRCodes.saveAll') }}
          </button>
        </div>
      </div>

      <div v-if="loading" class="flex min-h-64 items-center justify-center">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <div v-else-if="items.length === 0" class="flex min-h-64 items-center justify-center border-y border-gray-200 py-12 text-center dark:border-dark-700">
        <div class="max-w-md">
          <Icon name="users" size="xl" class="mx-auto text-gray-400" />
          <h2 class="mt-4 text-lg font-semibold text-gray-900 dark:text-white">{{ t('adminCommunityQRCodes.emptyTitle') }}</h2>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('adminCommunityQRCodes.emptyDescription') }}</p>
          <button type="button" class="btn btn-primary mt-5" @click="openCreate">
            <Icon name="plus" size="sm" class="mr-1.5" />
            {{ t('adminCommunityQRCodes.add') }}
          </button>
        </div>
      </div>

      <div v-else class="space-y-3">
        <div
          v-for="(item, index) in items"
          :key="item.id || index"
          class="grid items-center gap-4 rounded border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800 md:grid-cols-[72px_minmax(0,1fr)_140px_auto]"
        >
          <div class="h-20 w-20 overflow-hidden rounded border border-gray-200 bg-white p-1 dark:border-dark-600">
            <img :src="item.image_data" :alt="item.name" class="h-full w-full object-contain" />
          </div>
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span class="truncate font-medium text-gray-900 dark:text-white">{{ item.name }}</span>
              <span class="badge" :class="statusClass(item)">{{ statusLabel(item) }}</span>
            </div>
            <p v-if="item.description" class="mt-1 line-clamp-2 text-sm text-gray-500 dark:text-dark-400">{{ item.description }}</p>
          </div>
          <div class="text-xs text-gray-500 dark:text-dark-400">
            {{ item.expires_at ? formatDateTime(item.expires_at) : t('adminCommunityQRCodes.never') }}
          </div>
          <div class="flex justify-end gap-1">
            <button type="button" class="p-2 text-gray-500 hover:text-gray-900 disabled:opacity-30 dark:hover:text-white" :disabled="index === 0" :title="t('admin.settings.customMenu.moveUp')" @click="move(index, -1)">
              <Icon name="chevronUp" size="sm" />
            </button>
            <button type="button" class="p-2 text-gray-500 hover:text-gray-900 disabled:opacity-30 dark:hover:text-white" :disabled="index === items.length - 1" :title="t('admin.settings.customMenu.moveDown')" @click="move(index, 1)">
              <Icon name="chevronDown" size="sm" />
            </button>
            <button type="button" class="p-2 text-gray-500 hover:text-primary-600" :title="t('common.edit')" @click="openEdit(index)">
              <Icon name="edit" size="sm" />
            </button>
            <button type="button" class="p-2 text-gray-500 hover:text-red-600" :title="t('common.delete')" @click="remove(index)">
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <BaseDialog :show="dialogOpen" :title="editingIndex === null ? t('adminCommunityQRCodes.create') : t('adminCommunityQRCodes.edit')" width="wide" @close="dialogOpen = false">
      <form id="community-qr-form" class="space-y-4" @submit.prevent="applyForm">
        <div>
          <label class="input-label">{{ t('adminCommunityQRCodes.name') }}</label>
          <input v-model="form.name" class="input" maxlength="50" required :placeholder="t('adminCommunityQRCodes.namePlaceholder')" />
        </div>
        <div>
          <label class="input-label">{{ t('adminCommunityQRCodes.descriptionLabel') }}</label>
          <textarea v-model="form.description" class="input" rows="3" maxlength="300" :placeholder="t('adminCommunityQRCodes.descriptionPlaceholder')"></textarea>
        </div>
        <div>
          <label class="input-label">{{ t('adminCommunityQRCodes.image') }}</label>
          <ImageUpload v-model="form.image_data" mode="image" :max-size="300 * 1024" :hint="t('adminCommunityQRCodes.imageHint')" />
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('adminCommunityQRCodes.startsAt') }}</label>
            <input v-model="form.starts_at" type="datetime-local" class="input" />
          </div>
          <div>
            <label class="input-label">{{ t('adminCommunityQRCodes.expiresAt') }}</label>
            <input v-model="form.expires_at" type="datetime-local" class="input" />
          </div>
        </div>
        <label class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700">
          <span class="font-medium text-gray-900 dark:text-white">{{ t('adminCommunityQRCodes.enabled') }}</span>
          <Toggle v-model="form.enabled" />
        </label>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</button>
        <button type="submit" form="community-qr-form" class="btn btn-primary">{{ t('common.confirm') }}</button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImageUpload from '@/components/common/ImageUpload.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores'
import type { CommunityQRCode } from '@/types'

type QRCodeForm = Omit<CommunityQRCode, 'starts_at' | 'expires_at'> & { starts_at: string; expires_at: string }

const { t } = useI18n()
const appStore = useAppStore()
const items = ref<CommunityQRCode[]>([])
const loading = ref(true)
const saving = ref(false)
const dialogOpen = ref(false)
const editingIndex = ref<number | null>(null)
const form = reactive<QRCodeForm>(emptyForm())

function emptyForm(): QRCodeForm {
  return { id: '', name: '', description: '', image_data: '', enabled: true, sort_order: 0, starts_at: '', expires_at: '' }
}

function toLocalInput(value?: string | null): string {
  if (!value) return ''
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function toPayload(value: QRCodeForm): CommunityQRCode {
  return {
    ...value,
    starts_at: value.starts_at ? new Date(value.starts_at).toISOString() : null,
    expires_at: value.expires_at ? new Date(value.expires_at).toISOString() : null
  }
}

function assignForm(item: QRCodeForm) {
  Object.assign(form, item)
}

function openCreate() {
  editingIndex.value = null
  assignForm(emptyForm())
  dialogOpen.value = true
}

function openEdit(index: number) {
  const item = items.value[index]
  editingIndex.value = index
  assignForm({ ...item, starts_at: toLocalInput(item.starts_at), expires_at: toLocalInput(item.expires_at) })
  dialogOpen.value = true
}

function applyForm() {
  if (!form.name.trim() || !form.image_data) {
    appStore.showError(t('adminCommunityQRCodes.required'))
    return
  }
  if (form.starts_at && form.expires_at && new Date(form.expires_at) <= new Date(form.starts_at)) {
    appStore.showError(t('adminCommunityQRCodes.invalidTime'))
    return
  }
  const item = toPayload({ ...form, name: form.name.trim(), description: form.description.trim() })
  if (editingIndex.value === null) items.value.push(item)
  else items.value[editingIndex.value] = item
  items.value.forEach((entry, index) => { entry.sort_order = index })
  dialogOpen.value = false
}

function remove(index: number) {
  if (!window.confirm(t('adminCommunityQRCodes.confirmDelete', { name: items.value[index].name }))) return
  items.value.splice(index, 1)
  items.value.forEach((entry, order) => { entry.sort_order = order })
}

function move(index: number, direction: -1 | 1) {
  const target = index + direction
  if (target < 0 || target >= items.value.length) return
  const copy = [...items.value]
  ;[copy[index], copy[target]] = [copy[target], copy[index]]
  copy.forEach((entry, order) => { entry.sort_order = order })
  items.value = copy
}

function statusLabel(item: CommunityQRCode): string {
  const now = Date.now()
  if (!item.enabled) return t('adminCommunityQRCodes.disabled')
  if (item.starts_at && new Date(item.starts_at).getTime() > now) return t('adminCommunityQRCodes.scheduled')
  if (item.expires_at && new Date(item.expires_at).getTime() <= now) return t('adminCommunityQRCodes.expired')
  return t('adminCommunityQRCodes.active')
}

function statusClass(item: CommunityQRCode): string {
  const label = statusLabel(item)
  return label === t('adminCommunityQRCodes.active') ? 'badge-success' : label === t('adminCommunityQRCodes.disabled') ? 'badge-gray' : 'badge-warning'
}

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

async function load() {
  loading.value = true
  try {
    items.value = await adminAPI.communityQRCodes.list()
  } catch {
    appStore.showError(t('adminCommunityQRCodes.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function saveAll() {
  saving.value = true
  try {
    items.value = await adminAPI.communityQRCodes.update(items.value)
    appStore.showSuccess(t('adminCommunityQRCodes.saved'))
  } catch (error: any) {
    appStore.showError(error.response?.data?.message || t('adminCommunityQRCodes.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
