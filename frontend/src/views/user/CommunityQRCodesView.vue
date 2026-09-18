<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-5xl space-y-6">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
          {{ t('communityQRCodes.title') }}
        </h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('communityQRCodes.subtitle') }}
        </p>
      </div>

      <div v-if="loading" class="flex min-h-64 items-center justify-center">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <div v-else-if="items.length === 0" class="flex min-h-64 items-center justify-center border-y border-gray-200 py-12 text-center dark:border-dark-700">
        <div class="max-w-md">
          <Icon name="users" size="xl" class="mx-auto text-gray-400" />
          <h2 class="mt-4 text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('communityQRCodes.emptyTitle') }}
          </h2>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
            {{ t('communityQRCodes.emptyDescription') }}
          </p>
        </div>
      </div>

      <div v-else class="grid gap-5 sm:grid-cols-2 xl:grid-cols-3">
        <article
          v-for="item in items"
          :key="item.id"
          class="card flex flex-col p-5"
        >
          <button type="button" :aria-label="t('communityQRCodes.enlarge')" class="mx-auto aspect-square w-full max-w-64 cursor-zoom-in overflow-hidden rounded border border-gray-200 bg-white p-3 transition hover:shadow-lg focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600" @click="preview = item">
            <img :src="item.image_data" :alt="item.name" class="h-full w-full object-contain" />
          </button>
          <p class="mt-2 text-center text-xs text-gray-400">{{ t('communityQRCodes.enlarge') }}</p>
          <h2 class="mt-4 text-center text-base font-semibold text-gray-900 dark:text-white">
            {{ item.name }}
          </h2>
          <p v-if="item.description" class="mt-2 whitespace-pre-line text-center text-sm text-gray-600 dark:text-dark-300">
            {{ item.description }}
          </p>
          <p class="mt-3 text-center text-xs text-gray-400 dark:text-dark-500">
            {{ item.expires_at ? t('communityQRCodes.validUntil', { time: formatDateTime(item.expires_at) }) : t('communityQRCodes.noExpiry') }}
          </p>
          <button type="button" class="btn btn-secondary mt-4 w-full" @click="downloadQRCode(item)">
            <Icon name="download" size="sm" class="mr-1.5" />
            {{ t('communityQRCodes.download') }}
          </button>
        </article>
      </div>
    </div>
    <BaseDialog :show="Boolean(preview)" :title="preview?.name || ''" :close-on-click-outside="true" @close="preview = null">
      <template v-if="preview">
        <img :src="preview.image_data" :alt="preview.name" class="mx-auto max-h-[65vh] w-full rounded-lg bg-white p-4 object-contain" />
        <p class="mt-4 text-center text-sm text-gray-500">{{ preview.description }}</p>
        <button type="button" class="btn btn-primary mt-4 w-full" @click="downloadQRCode(preview)">{{ t('communityQRCodes.download') }}</button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { communityQRCodesAPI } from '@/api'
import { useAppStore } from '@/stores'
import type { CommunityQRCode } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const items = ref<CommunityQRCode[]>([])
const loading = ref(true)
const preview = ref<CommunityQRCode | null>(null)

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function downloadQRCode(item: CommunityQRCode) {
  const link = document.createElement('a')
  link.href = item.image_data
  link.download = `${item.name}.png`
  link.click()
}

onMounted(async () => {
  try {
    items.value = await communityQRCodesAPI.list()
  } catch {
    appStore.showError(t('communityQRCodes.loadFailed'))
  } finally {
    loading.value = false
  }
})
</script>
