<template>
  <BaseDialog :show="Boolean(user)" :title="t('playground.storageSettings')" @close="emit('close')">
    <form id="playground-storage-form" class="space-y-3" @submit.prevent="save">
      <p class="text-sm">{{ user?.email }}</p>
      <label class="block text-sm">
        {{ t('playground.storageQuotaLabel') }}
        <input v-model.number="limitMB" class="input mt-2 w-full" type="number" min="0" max="120" step="1" required :disabled="loading || saving" />
      </label>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    </form>
    <template #footer>
      <button class="btn btn-secondary" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="submit" form="playground-storage-form" :disabled="loading || saving || !loaded">{{ t('common.save') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { playgroundHistory } from '@/api/playgroundHistory'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { AdminUser } from '@/types'

const props = defineProps<{ user: AdminUser | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const limitMB = ref(50)
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const error = ref('')

watch(() => props.user, async (user, _oldUser, onCleanup) => {
  let current = true
  onCleanup(() => { current = false })
  error.value = ''
  loaded.value = false
  saving.value = false
  if (!user) return
  loading.value = true
  try {
    const quota = await playgroundHistory.adminQuota(user.id)
    if (current) {
      limitMB.value = quota.limitMB
      loaded.value = true
    }
  } catch {
    if (current) error.value = t('playground.historyFailed')
  } finally {
    if (current) loading.value = false
  }
})

async function save() {
  const user = props.user
  if (!user || !loaded.value || saving.value || !Number.isInteger(limitMB.value)) return
  saving.value = true
  error.value = ''
  try {
    await playgroundHistory.setAdminQuota(user.id, limitMB.value)
    if (props.user === user) emit('close')
  } catch (caught) {
    if (props.user === user) error.value = (caught as Error)?.message || t('playground.saveFailed')
  } finally {
    if (props.user === user) saving.value = false
  }
}
</script>
