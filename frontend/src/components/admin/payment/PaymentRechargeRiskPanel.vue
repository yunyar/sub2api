<template>
  <div class="space-y-5">
    <section class="card space-y-3 p-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.risk.title') }}</h2>
          <p class="mt-1 max-w-4xl text-sm text-gray-600 dark:text-gray-300">{{ t('payment.admin.risk.frequencyRuleDescription') }}</p>
          <p class="mt-1 max-w-4xl text-sm text-gray-600 dark:text-gray-300">{{ t('payment.admin.risk.permanentBlockRuleDescription') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || scanning" @click="showScanDialog = true">
            {{ scanning ? t('payment.admin.risk.scanning') : t('payment.admin.risk.scanNow') }}
          </button>
          <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="loadRiskIPs">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('common.refresh') }}
          </button>
        </div>
      </div>

      <div class="grid gap-2 text-sm text-gray-600 dark:text-gray-300 sm:grid-cols-2">
        <p>{{ t('payment.admin.risk.registrationCheck') }}</p>
        <p>{{ t('payment.admin.risk.periodicScan') }}</p>
        <p>{{ t('payment.admin.risk.privateIPsExcluded') }}</p>
        <p>{{ t('payment.admin.risk.adminAccountsExcluded') }}</p>
      </div>
      <p class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
        {{ t('payment.admin.risk.sharedIPWarning') }}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.risk.evidenceSourceHint') }}</p>
    </section>

    <section class="card space-y-3 p-4">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.risk.blockOrderTitle') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.risk.blockOrderHint') }}</p>
      </div>
      <div class="flex flex-wrap items-end gap-3">
        <label class="block">
          <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('payment.admin.risk.orderID') }}</span>
          <input v-model.number="orderID" type="number" min="1" step="1" class="form-input w-44">
        </label>
        <label class="min-w-[240px] flex-1">
          <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('payment.admin.risk.blockReason') }}</span>
          <input v-model="blockReason" class="form-input w-full" :placeholder="t('payment.admin.risk.blockReasonPlaceholder')">
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.risk.reasonByteCount', { count: blockReasonBytes }) }}</span>
        </label>
        <button type="button" class="btn btn-danger" :disabled="!canBlock" @click="openBlockDialog">
          {{ t('payment.admin.risk.blockOrderAction') }}
        </button>
      </div>
    </section>

    <section class="card overflow-hidden">
      <div v-if="loading" class="flex justify-center py-12">
        <LoadingSpinner />
      </div>
      <div v-else-if="items.length === 0" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('payment.admin.risk.empty') }}
      </div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-800">
            <tr>
              <th v-for="heading in headings" :key="heading" class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t(`payment.admin.risk.columns.${heading}`) }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('payment.admin.risk.actions') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="item in items" :key="item.ip" class="align-top hover:bg-gray-50 dark:hover:bg-dark-800/60">
              <td class="whitespace-nowrap px-4 py-3 font-mono text-sm text-gray-900 dark:text-white">{{ item.ip }}</td>
              <td class="whitespace-nowrap px-4 py-3">
                <span
                  class="rounded-full px-2 py-1 text-xs font-medium"
                  :class="item.active ? 'bg-rose-100 text-rose-700 dark:bg-rose-500/20 dark:text-rose-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
                >
                  {{ item.active ? t('payment.admin.risk.active') : t('payment.admin.risk.released') }}
                </span>
                <p v-if="!item.active" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.risk.historyRetained') }}</p>
              </td>
              <td class="whitespace-nowrap px-4 py-3 text-sm text-gray-700 dark:text-gray-300">
                {{ item.linked_users }}
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.risk.linkedUsersCountOnly') }}</span>
              </td>
              <td class="whitespace-nowrap px-4 py-3 text-sm text-gray-600 dark:text-gray-400">{{ formatDateTime(item.created_at) }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-sm text-gray-600 dark:text-gray-400">{{ item.actor || '—' }}</td>
              <td class="max-w-xs px-4 py-3 text-sm text-gray-600 dark:text-gray-400">{{ item.reason || '—' }}</td>
              <td class="min-w-64 px-4 py-3 text-sm text-gray-600 dark:text-gray-400">
                <details v-if="item.evidence">
                  <summary class="cursor-pointer text-primary-600 dark:text-primary-400">{{ t('payment.admin.risk.showEvidence') }}</summary>
                  <pre class="mt-2 max-w-xl overflow-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ prettyEvidence(item.evidence) }}</pre>
                </details>
                <span v-else>—</span>
              </td>
              <td class="whitespace-nowrap px-4 py-3 text-right">
                <button v-if="item.active" type="button" class="btn btn-danger btn-sm" @click="openUnblockDialog(item)">
                  {{ t('payment.admin.risk.unblockAction') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="flex items-center justify-between border-t border-gray-100 px-4 py-3 dark:border-dark-700">
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.risk.total', { total }) }}</span>
        <div class="flex items-center gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="changePage(page - 1)">
            {{ t('payment.admin.risk.previousPage') }}
          </button>
          <span class="text-xs text-gray-500 dark:text-gray-400">{{ page }} / {{ pages }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="page >= pages || loading" @click="changePage(page + 1)">
            {{ t('payment.admin.risk.nextPage') }}
          </button>
        </div>
      </div>
    </section>

    <div v-if="showBlockDialog" class="fixed inset-0 z-[100000100] flex items-center justify-center bg-black/50 p-4" @click.self="closeBlockDialog">
      <section class="w-full max-w-lg rounded-xl bg-white p-5 shadow-xl dark:bg-dark-800" role="dialog" aria-modal="true" :aria-label="t('payment.admin.risk.blockDialogTitle')">
        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.risk.blockDialogTitle') }}</h3>
        <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t('payment.admin.risk.blockDialogBody', { id: orderID }) }}</p>
        <p class="mt-2 text-sm text-amber-700 dark:text-amber-300">{{ t('payment.admin.risk.sharedIPWarning') }}</p>
        <label class="mt-4 flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200">
          <input v-model="confirmBlock" type="checkbox" class="mt-1">
          <span>{{ t('payment.admin.risk.confirmBlockCheckbox') }}</span>
        </label>
        <div class="mt-5 flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="blocking" @click="closeBlockDialog">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-danger" :disabled="blocking || !confirmBlock" @click="blockOrderIP">
            {{ blocking ? t('payment.admin.risk.blocking') : t('payment.admin.risk.confirmBlock') }}
          </button>
        </div>
      </section>
    </div>

    <div v-if="unblockTarget" class="fixed inset-0 z-[100000100] flex items-center justify-center bg-black/50 p-4" @click.self="closeUnblockDialog">
      <section class="w-full max-w-lg rounded-xl bg-white p-5 shadow-xl dark:bg-dark-800" role="dialog" aria-modal="true" :aria-label="t('payment.admin.risk.unblockDialogTitle')">
        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.risk.unblockDialogTitle') }}</h3>
        <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t('payment.admin.risk.unblockDialogBody', { ip: unblockTarget.ip }) }}</p>
        <p class="mt-2 text-sm text-amber-700 dark:text-amber-300">{{ t('payment.admin.risk.unblockAccountWarning') }}</p>
        <label class="mt-4 block">
          <span class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('payment.admin.risk.unblockReason') }}</span>
          <textarea v-model="unblockReason" rows="3" class="form-input w-full" :placeholder="t('payment.admin.risk.unblockReasonPlaceholder')"></textarea>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.risk.reasonByteCount', { count: unblockReasonBytes }) }}</span>
        </label>
        <label class="mt-4 flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200">
          <input v-model="confirmUnblock" type="checkbox" class="mt-1">
          <span>{{ t('payment.admin.risk.confirmUnblockCheckbox') }}</span>
        </label>
        <div class="mt-5 flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="unblocking" @click="closeUnblockDialog">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-danger" :disabled="unblocking || !unblockReason.trim() || unblockReasonBytes > 512 || !confirmUnblock" @click="unblockIP">
            {{ unblocking ? t('payment.admin.risk.unblocking') : t('payment.admin.risk.confirmUnblock') }}
          </button>
        </div>
      </section>
    </div>

    <div v-if="showScanDialog" class="fixed inset-0 z-[100000100] flex items-center justify-center bg-black/50 p-4" @click.self="showScanDialog = false">
      <section class="w-full max-w-lg rounded-xl bg-white p-5 shadow-xl dark:bg-dark-800" role="dialog" aria-modal="true" :aria-label="t('payment.admin.risk.scanDialogTitle')">
        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.risk.scanDialogTitle') }}</h3>
        <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ t('payment.admin.risk.scanDialogBody') }}</p>
        <label class="mt-4 flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200">
          <input v-model="confirmScan" type="checkbox" class="mt-1">
          <span>{{ t('payment.admin.risk.confirmScanCheckbox') }}</span>
        </label>
        <div class="mt-5 flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="scanning" @click="showScanDialog = false">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="scanning || !confirmScan" @click="scanAccounts">
            {{ scanning ? t('payment.admin.risk.scanning') : t('payment.admin.risk.confirmScan') }}
          </button>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI, type PaymentRechargeRiskIPRecord } from '@/api/admin/payment'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const { t, locale } = useI18n()
const appStore = useAppStore()
const PAGE_SIZE = 20
const headings = ['ip', 'status', 'linkedUsers', 'createdAt', 'actor', 'reason', 'evidence'] as const
const items = ref<PaymentRechargeRiskIPRecord[]>([])
const page = ref(1)
const total = ref(0)
const loading = ref(false)
const blocking = ref(false)
const unblocking = ref(false)
const scanning = ref(false)
const orderID = ref<number | null>(null)
const blockReason = ref('')
const showBlockDialog = ref(false)
const confirmBlock = ref(false)
const unblockTarget = ref<PaymentRechargeRiskIPRecord | null>(null)
const unblockReason = ref('')
const confirmUnblock = ref(false)
const showScanDialog = ref(false)
const confirmScan = ref(false)
const pages = computed(() => Math.max(1, Math.ceil(total.value / PAGE_SIZE)))
const blockReasonBytes = computed(() => new TextEncoder().encode(blockReason.value.trim()).length)
const unblockReasonBytes = computed(() => new TextEncoder().encode(unblockReason.value.trim()).length)
const canBlock = computed(() => Number.isInteger(orderID.value) && (orderID.value ?? 0) > 0 &&
  blockReasonBytes.value > 0 && blockReasonBytes.value <= 512 && !blocking.value)

function showError(error: unknown) {
  appStore.showError(extractI18nErrorMessage(error, t, 'payment.errors', t('common.error')))
}

function formatDateTime(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '—'
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value * 1000))
}

function prettyEvidence(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

async function loadRiskIPs() {
  loading.value = true
  try {
    const response = await adminPaymentAPI.listRiskIPs({ page: page.value, page_size: PAGE_SIZE })
    items.value = response.data.items
    total.value = response.data.total
    if (page.value > pages.value) {
      page.value = pages.value
      await loadRiskIPs()
    }
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

function changePage(nextPage: number) {
  page.value = nextPage
  void loadRiskIPs()
}

function openBlockDialog() {
  if (canBlock.value) {
    confirmBlock.value = false
    showBlockDialog.value = true
  }
}

function closeBlockDialog() {
  showBlockDialog.value = false
  confirmBlock.value = false
}

async function blockOrderIP() {
  const orderId = orderID.value
  if (!canBlock.value || !confirmBlock.value || orderId === null) return
  blocking.value = true
  try {
    await adminPaymentAPI.blockRiskOrderIP({
      order_id: orderId,
      reason: blockReason.value.trim(),
      confirm: true
    })
    appStore.showSuccess(t('payment.admin.risk.blockSuccess'))
    orderID.value = null
    blockReason.value = ''
    closeBlockDialog()
    page.value = 1
    await loadRiskIPs()
  } catch (error) {
    showError(error)
  } finally {
    blocking.value = false
  }
}

function openUnblockDialog(record: PaymentRechargeRiskIPRecord) {
  unblockTarget.value = record
  unblockReason.value = ''
  confirmUnblock.value = false
}

function closeUnblockDialog() {
  unblockTarget.value = null
  unblockReason.value = ''
  confirmUnblock.value = false
}

async function unblockIP() {
  const target = unblockTarget.value
  if (!target || !unblockReason.value.trim() || unblockReasonBytes.value > 512 || !confirmUnblock.value) return
  unblocking.value = true
  try {
    await adminPaymentAPI.unblockRiskIP({
      ip: target.ip,
      reason: unblockReason.value.trim(),
      confirm: true
    })
    appStore.showSuccess(t('payment.admin.risk.unblockSuccess'))
    closeUnblockDialog()
    await loadRiskIPs()
  } catch (error) {
    showError(error)
  } finally {
    unblocking.value = false
  }
}

async function scanAccounts() {
  if (!confirmScan.value) return
  scanning.value = true
  try {
    const response = await adminPaymentAPI.scanBlockedRiskAccounts({ confirm: true })
    appStore.showSuccess(t('payment.admin.risk.scanSuccess', { count: response.data.restricted_accounts }))
    confirmScan.value = false
    showScanDialog.value = false
    await loadRiskIPs()
  } catch (error) {
    showError(error)
  } finally {
    scanning.value = false
  }
}

onMounted(() => {
  void loadRiskIPs()
})
</script>
