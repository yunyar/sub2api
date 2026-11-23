<template>
  <section class="card" aria-labelledby="bloom-planner-title">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="bloom-planner-title" class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('bloomPlanner.title') }}
      </h2>
    </div>
    <div class="space-y-5 p-6">
      <p v-if="loading" class="text-sm text-gray-500 dark:text-gray-400" role="status">{{ t('bloomPlanner.loading') }}</p>
      <div v-else-if="!saved" class="flex flex-wrap items-center gap-3">
        <p class="text-sm text-red-600 dark:text-red-400" role="alert">{{ t('bloomPlanner.loadFailed') }} {{ loadError }}</p>
        <button type="button" class="btn btn-secondary" @click="loadSettings">{{ t('bloomPlanner.retry') }}</button>
      </div>
      <template v-else>
        <div class="flex items-center justify-between gap-3">
          <span id="bloom-planner-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('bloomPlanner.enabled') }}</span>
          <Toggle v-model="enabled" aria-labelledby="bloom-planner-enabled" :disabled="saving" />
        </div>
        <div v-if="enabled" class="space-y-3">
          <div class="grid gap-4 md:grid-cols-2">
            <div class="min-w-0">
              <label for="bloom-planner-key" class="input-label">{{ t('bloomPlanner.key') }}</label>
              <select id="bloom-planner-key" v-model.number="keyId" class="input" :disabled="saving || keysLoading || !!keysError">
                <option :value="0" disabled>{{ keysLoading ? t('bloomPlanner.loading') : t('bloomPlanner.selectKey') }}</option>
                <option v-if="keyId && !selectedKey" :value="keyId" disabled>{{ t('bloomPlanner.unavailableKey', { id: keyId }) }}</option>
                <option v-for="key in keys" :key="key.id" :value="key.id">{{ keyLabel(key) }}</option>
              </select>
              <div v-if="keysError" class="mt-1 flex flex-wrap items-center gap-2">
                <p class="text-sm text-red-600 dark:text-red-400" role="alert">{{ t('bloomPlanner.keysFailed') }} {{ keysError }}</p>
                <button type="button" class="btn btn-secondary" @click="loadKeys">{{ t('bloomPlanner.retry') }}</button>
              </div>
              <p v-else-if="!keysLoading && !keys.length" class="input-hint">{{ t('bloomPlanner.noKeys') }}</p>
            </div>
            <div>
              <label for="bloom-planner-model" class="input-label">{{ t('bloomPlanner.model') }}</label>
              <input
                id="bloom-planner-model"
                v-model.trim="model"
                type="text"
                class="input"
                :disabled="saving"
                autocomplete="off"
                @keydown.enter.prevent
              />
            </div>
          </div>
          <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('bloomPlanner.chargeNotice') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" class="btn btn-primary" :disabled="!canSave" @click="save">
            {{ saving ? t('bloomPlanner.saving') : enabled ? t('bloomPlanner.generate') : t('bloomPlanner.restore') }}
          </button>
          <p v-if="saveError" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ t('bloomPlanner.saveFailed') }} {{ saveError }}</p>
          <p v-else-if="saveSucceeded" class="text-sm text-green-700 dark:text-green-400" role="status">{{ t('bloomPlanner.saved') }}</p>
        </div>
        <div class="overflow-x-auto">
          <table v-if="profiles" class="w-full text-sm text-gray-700 dark:text-gray-300">
            <caption class="pb-2 text-left font-medium">{{ saved.enabled ? t('bloomPlanner.savedProfiles') : t('bloomPlanner.defaultProfiles') }}</caption>
            <thead>
              <tr class="border-b border-gray-100 dark:border-dark-700">
                <th scope="col" class="py-2 pr-3 text-left font-medium">{{ t('bloomPlanner.group') }}</th>
                <th v-for="tier in bloomPlannerTiers" :key="tier" scope="col" class="px-2 py-2 text-right font-medium">{{ t(`bloomPlanner.${tier}`) }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="group in bloomPlannerGroups" :key="group" class="border-b border-gray-100 last:border-0 dark:border-dark-700">
                <th scope="row" class="py-2 pr-3 text-left font-normal">{{ t(`bloomPlanner.${group}`) }}</th>
                <td v-for="tier in bloomPlannerTiers" :key="tier" class="px-2 py-2 text-right tabular-nums">
                  {{ formatPercent(profiles[group]?.[tier]) }}
                </td>
              </tr>
            </tbody>
          </table>
          <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('bloomPlanner.noProfiles') }}</p>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  bloomPlannerGroups,
  bloomPlannerTiers,
  getBloomPlannerSettings,
  updateBloomPlannerSettings,
  type BloomPlannerSettings
} from '@/api/admin/bloomPlanner'
import { list as listKeys } from '@/api/keys'
import Toggle from '@/components/common/Toggle.vue'
import { isStepUpCancelled, type StepUpController } from '@/composables/useStepUp'
import type { ApiKey } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ stepUp: StepUpController }>()

const { t, locale } = useI18n({ useScope: 'global' })

type KeyOption = Pick<ApiKey, 'id' | 'name' | 'quota' | 'quota_used'>

const defaultProfile = { easy: 1, medium: 3, hard: 6 }
const defaultProfiles = { cold_start: defaultProfile, text: defaultProfile, image: defaultProfile, mixed: defaultProfile }
const saved = ref<BloomPlannerSettings>()
const enabled = ref(false)
const keyId = ref(0)
const model = ref('')
const keys = ref<KeyOption[]>([])
const loading = ref(true)
const keysLoading = ref(false)
const saving = ref(false)
const loadError = ref('')
const keysError = ref('')
const saveError = ref('')
const saveSucceeded = ref(false)
const selectedKey = computed(() => keys.value.find((key) => key.id === keyId.value))
const profiles = computed(() => saved.value?.enabled ? saved.value.profiles : defaultProfiles)
const canSave = computed(() => !!saved.value && !saving.value && (
  enabled.value
    ? !keysLoading.value && !keysError.value && !!selectedKey.value && !!model.value.trim()
    : saved.value.enabled
))
const quotaFormat = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 6 }))
const percentFormat = computed(() => new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 2 }))

watch([enabled, keyId, model], () => {
  saveSucceeded.value = false
  saveError.value = ''
}, { flush: 'sync' })

function keyLabel(key: KeyOption): string {
  const quota = key.quota > 0
    ? t('bloomPlanner.quota', { amount: quotaFormat.value.format(Math.max(0, key.quota - key.quota_used)) })
    : t('bloomPlanner.unlimited')
  return `${key.name} (#${key.id}) · ${quota}`
}

function formatPercent(amount: number | undefined): string {
  return typeof amount === 'number' && Number.isFinite(amount) ? percentFormat.value.format(amount / 100) : '—'
}

function applySettings(settings: BloomPlannerSettings) {
  saved.value = settings
  enabled.value = settings.enabled
  keyId.value = settings.key_id
  model.value = settings.model
}

async function loadSettings() {
  loading.value = true
  loadError.value = ''
  try {
    applySettings(await getBloomPlannerSettings())
  } catch (error) {
    loadError.value = extractApiErrorMessage(error, '')
  } finally {
    loading.value = false
  }
}

async function loadKeys() {
  keysLoading.value = true
  keysError.value = ''
  const options: KeyOption[] = []
  try {
    let pages = 1
    for (let page = 1; page <= pages; page++) {
      const result = await listKeys(page, 100, { status: 'active' })
      pages = result.pages
      for (const key of result.items ?? []) {
        if (key.status === 'active'
          && (!key.expires_at || Date.parse(key.expires_at) > Date.now())
          && (key.quota <= 0 || key.quota_used < key.quota)) {
          options.push({ id: key.id, name: key.name, quota: key.quota, quota_used: key.quota_used })
        }
      }
    }
    keys.value = options
  } catch (error) {
    keysError.value = extractApiErrorMessage(error, t('bloomPlanner.keysFailed'))
  } finally {
    keysLoading.value = false
  }
}

async function save() {
  if (!canSave.value) return
  saving.value = true
  saveError.value = ''
  saveSucceeded.value = false
  try {
    const payload = enabled.value
      ? { enabled: true, key_id: keyId.value, model: model.value.trim() }
      : { enabled: false, key_id: 0, model: '' }
    applySettings(await props.stepUp.run(() => updateBloomPlannerSettings(payload)))
    saveSucceeded.value = true
  } catch (error) {
    if (!isStepUpCancelled(error)) {
      saveError.value = extractApiErrorMessage(error, t('bloomPlanner.saveFailed'))
    }
  } finally {
    saving.value = false
    if (enabled.value) void loadKeys()
  }
}

onMounted(() => {
  void loadSettings()
  void loadKeys()
})
</script>
