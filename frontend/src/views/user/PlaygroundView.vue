<template>
  <AppLayout>
    <div class="playground-shell flex min-h-[calc(100vh-8rem)] flex-col overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
      <header class="flex flex-wrap items-end gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-700 sm:px-5">
        <label class="min-w-[12rem] flex-1 sm:max-w-xs">
          <span class="mb-1 block text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('playground.group') }}</span>
          <select v-model.number="selectedGroupId" class="input h-10 w-full" :disabled="loadingGroups || busy">
            <option :value="0" disabled>{{ t('playground.selectGroup') }}</option>
            <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option>
          </select>
        </label>
        <label class="min-w-[13rem] flex-[1.4] sm:max-w-md">
          <span class="mb-1 block text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('playground.model') }}</span>
          <select v-model="selectedModel" class="input h-10 w-full" :disabled="loadingModels || !selectedGroupId || busy">
            <option value="" disabled>{{ loadingModels ? t('common.loading') : t('playground.selectModel') }}</option>
            <option v-for="model in models" :key="model.id" :value="model.id">{{ model.id }}</option>
          </select>
        </label>
        <div class="ml-auto pb-2 text-right">
          <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('playground.balance') }}</span>
          <strong class="text-sm text-gray-900 dark:text-white">${{ balance.toFixed(4) }}</strong>
        </div>
      </header>

      <div class="flex items-center justify-between border-b border-gray-200 px-4 dark:border-dark-700 sm:px-5">
        <div class="flex" role="tablist">
          <button
            v-for="tab in tabs"
            :key="tab.value"
            type="button"
            class="border-b-2 px-4 py-3 text-sm font-medium transition-colors"
            :class="activeTab === tab.value ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
            :disabled="busy"
            @click="activeTab = tab.value"
          >{{ tab.label }}</button>
        </div>
        <button
          v-if="activeTab === 'chat' && messages.length"
          type="button"
          class="rounded p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white"
          :title="t('playground.clear')"
          :aria-label="t('playground.clear')"
          :disabled="streaming"
          @click="messages = []"
        >
          <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673A2.25 2.25 0 0115.916 21H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" /></svg>
        </button>
      </div>

      <section v-if="activeTab === 'chat'" class="grid min-h-0 flex-1 lg:grid-cols-[minmax(0,1fr)_17rem]">
        <div class="flex min-h-[34rem] min-w-0 flex-col">
          <div ref="messageViewport" class="min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-7">
            <div v-if="!messages.length" class="flex h-full min-h-72 items-center justify-center text-center text-sm text-gray-400">
              {{ selectedModel ? t('playground.emptyChat') : t('playground.selectModel') }}
            </div>
            <div v-else class="mx-auto max-w-3xl space-y-6">
              <article v-for="message in messages" :key="message.id" :class="message.role === 'user' ? 'flex justify-end' : ''">
                <div v-if="message.role === 'user'" class="max-w-[85%] whitespace-pre-wrap break-words rounded-lg bg-primary-600 px-4 py-3 text-sm leading-6 text-white">{{ message.content }}</div>
                <div v-else class="group min-w-0 max-w-none text-sm leading-7 text-gray-800 dark:text-gray-100">
                  <div class="playground-markdown" v-html="renderMarkdown(message.content || (streaming ? '…' : ''))"></div>
                  <button
                    v-if="message.content && !streaming"
                    type="button"
                    class="mt-2 rounded p-1.5 text-gray-400 opacity-0 transition-opacity hover:bg-gray-100 hover:text-gray-700 group-hover:opacity-100 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                    :title="t('playground.copy')"
                    :aria-label="t('playground.copy')"
                    @click="copyMessage(message.content)"
                  >
                    <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V10.875c0-.621.504-1.125 1.125-1.125H8.25m7.5 7.5h3.375c.621 0 1.125-.504 1.125-1.125V6.108c0-.298-.119-.585-.33-.796l-4.232-4.232a1.125 1.125 0 00-.796-.33H9.375c-.621 0-1.125.504-1.125 1.125V6.75m7.5 10.5H9.375A1.125 1.125 0 018.25 16.125V6.75m12 0h-3.375a1.125 1.125 0 01-1.125-1.125V2.25" /></svg>
                  </button>
                </div>
              </article>
              <p v-if="chatError" class="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">{{ chatError }}</p>
            </div>
          </div>

          <form class="border-t border-gray-200 p-4 dark:border-dark-700 sm:px-7" @submit.prevent="sendMessage">
            <div class="mx-auto flex max-w-3xl items-end gap-2 rounded-lg border border-gray-300 bg-white p-2 focus-within:border-primary-500 focus-within:ring-2 focus-within:ring-primary-500/15 dark:border-dark-600 dark:bg-dark-900">
              <textarea
                v-model="draft"
                rows="1"
                class="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-2 py-2 text-sm text-gray-900 outline-none placeholder:text-gray-400 dark:text-white"
                :placeholder="t('playground.messagePlaceholder')"
                :disabled="streaming"
                @keydown.enter.exact.prevent="sendMessage"
              ></textarea>
              <button
                v-if="streaming"
                type="button"
                class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-gray-900 text-white hover:bg-gray-700 dark:bg-white dark:text-gray-900"
                :title="t('playground.stop')"
                :aria-label="t('playground.stop')"
                @click="stopStream"
              ><span class="h-3 w-3 rounded-sm bg-current"></span></button>
              <button
                v-else
                type="submit"
                class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-primary-600 text-white hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-40"
                :disabled="!canSend"
                :title="t('playground.send')"
                :aria-label="t('playground.send')"
              ><svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M6 12 3.27 3.125A59.77 59.77 0 0121.485 12 59.768 59.768 0 013.27 20.875L6 12zm0 0h7.5" /></svg></button>
            </div>
          </form>
        </div>

        <aside class="border-t border-gray-200 p-4 dark:border-dark-700 lg:border-l lg:border-t-0">
          <label class="block">
            <span class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('playground.systemPrompt') }}</span>
            <textarea v-model="systemPrompt" rows="5" class="input w-full resize-y text-sm" :placeholder="t('playground.systemPromptPlaceholder')" :disabled="streaming"></textarea>
          </label>
          <label class="mt-5 block">
            <span class="mb-2 flex justify-between text-xs font-medium text-gray-600 dark:text-gray-300"><span>{{ t('playground.temperature') }}</span><span>{{ temperature.toFixed(1) }}</span></span>
            <input v-model.number="temperature" type="range" min="0" max="2" step="0.1" class="w-full accent-primary-600" :disabled="streaming" />
          </label>
        </aside>
      </section>

      <section v-else class="grid min-h-0 flex-1 lg:grid-cols-[20rem_minmax(0,1fr)]">
        <form class="border-b border-gray-200 p-5 dark:border-dark-700 lg:border-b-0 lg:border-r" @submit.prevent="generateImages">
          <label class="block">
            <span class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('playground.imagePrompt') }}</span>
            <textarea v-model="imagePrompt" rows="7" class="input w-full resize-y text-sm" :placeholder="t('playground.imagePromptPlaceholder')" :disabled="generating"></textarea>
          </label>
          <div class="mt-4 grid grid-cols-2 gap-3">
            <label>
              <span class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('playground.size') }}</span>
              <select v-model="imageSize" class="input h-10 w-full" :disabled="generating"><option>1024x1024</option><option>1536x1024</option><option>1024x1536</option></select>
            </label>
            <label>
              <span class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('playground.quality') }}</span>
              <select v-model="imageQuality" class="input h-10 w-full" :disabled="generating"><option value="auto">Auto</option><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select>
            </label>
            <label>
              <span class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('playground.count') }}</span>
              <input v-model.number="imageCount" type="number" min="1" max="4" class="input h-10 w-full" :disabled="generating" />
            </label>
          </div>
          <button type="submit" class="btn btn-primary mt-5 w-full" :disabled="!canGenerate">
            {{ generating ? t('playground.generating') : t('playground.generate') }}
          </button>
          <p v-if="imageError" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ imageError }}</p>
        </form>

        <div class="min-h-[30rem] overflow-y-auto p-5 sm:p-7">
          <div v-if="generating" class="flex min-h-72 items-center justify-center"><LoadingSpinner /></div>
          <div v-else-if="!images.length" class="flex min-h-72 items-center justify-center text-sm text-gray-400">{{ t('playground.noImages') }}</div>
          <div v-else class="grid gap-5 sm:grid-cols-2 xl:grid-cols-3">
            <figure v-for="(image, index) in images" :key="image.url.slice(0, 80) + index" class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900">
              <img :src="image.url" :alt="image.revisedPrompt || imagePrompt" class="aspect-square w-full object-contain" />
              <figcaption class="flex items-start gap-2 p-3">
                <p class="line-clamp-2 min-w-0 flex-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ image.revisedPrompt || imagePrompt }}</p>
                <a :href="image.url" :download="`playground-${index + 1}.png`" class="rounded p-2 text-gray-500 hover:bg-gray-200 hover:text-gray-800 dark:hover:bg-dark-700 dark:hover:text-white" :title="t('playground.download')" :aria-label="t('playground.download')">
                  <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5m-4.5-6L12 15m0 0-4.5-4.5M12 15V3" /></svg>
                </a>
              </figcaption>
            </figure>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { userGroupsAPI } from '@/api/groups'
import { playgroundAPI, type PlaygroundImage, type PlaygroundMessage, type PlaygroundModel } from '@/api/playground'
import { useAppStore, useAuthStore } from '@/stores'
import type { Group } from '@/types'

type Tab = 'chat' | 'images'
type UIMessage = { id: number; role: 'user' | 'assistant'; content: string }

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const groups = ref<Group[]>([])
const models = ref<PlaygroundModel[]>([])
const selectedGroupId = ref(0)
const selectedModel = ref('')
const loadingGroups = ref(false)
const loadingModels = ref(false)
const activeTab = ref<Tab>('chat')
const messages = ref<UIMessage[]>([])
const draft = ref('')
const systemPrompt = ref('')
const temperature = ref(0.7)
const streaming = ref(false)
const chatError = ref('')
const messageViewport = ref<HTMLElement | null>(null)
const imagePrompt = ref('')
const imageSize = ref('1024x1024')
const imageQuality = ref('auto')
const imageCount = ref(1)
const images = ref<PlaygroundImage[]>([])
const generating = ref(false)
const imageError = ref('')
let messageId = 0
let streamController: AbortController | null = null

const tabs = computed(() => [
  { value: 'chat' as const, label: t('playground.chat') },
  { value: 'images' as const, label: t('playground.images') }
])
const balance = computed(() => authStore.user?.balance ?? 0)
const busy = computed(() => streaming.value || generating.value)
const canSend = computed(() => Boolean(draft.value.trim() && selectedGroupId.value && selectedModel.value))
const canGenerate = computed(() => Boolean(imagePrompt.value.trim() && selectedGroupId.value && selectedModel.value && !generating.value))

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message
  if (error && typeof error === 'object' && 'message' in error) {
    const message = String((error as { message?: unknown }).message || '').trim()
    if (message) return message
  }
  return fallback
}

function renderMarkdown(content: string): string {
  return DOMPurify.sanitize(marked.parse(content, { async: false, gfm: true, breaks: true }) as string)
}

async function scrollToBottom() {
  await nextTick()
  if (messageViewport.value) messageViewport.value.scrollTop = messageViewport.value.scrollHeight
}

async function loadGroups() {
  loadingGroups.value = true
  try {
    groups.value = await userGroupsAPI.getAvailable()
    if (!selectedGroupId.value && groups.value.length) selectedGroupId.value = groups.value[0].id
  } catch (error) {
    appStore.showError(errorMessage(error, t('playground.loadModelsFailed')))
  } finally {
    loadingGroups.value = false
  }
}

async function loadModels(groupId: number) {
  models.value = []
  selectedModel.value = ''
  if (!groupId) return
  loadingModels.value = true
  try {
    models.value = await playgroundAPI.listModels(groupId)
    if (models.value.length) selectedModel.value = models.value[0].id
    else appStore.showError(t('playground.noModels'))
  } catch (error) {
    appStore.showError(errorMessage(error, t('playground.loadModelsFailed')))
  } finally {
    loadingModels.value = false
  }
}

async function sendMessage() {
  const content = draft.value.trim()
  if (!content || !canSend.value || streaming.value) return
  chatError.value = ''
  draft.value = ''
  messages.value.push({ id: ++messageId, role: 'user', content })
  const assistant = { id: ++messageId, role: 'assistant' as const, content: '' }
  messages.value.push(assistant)
  const assistantIndex = messages.value.length - 1
  streaming.value = true
  streamController = new AbortController()
  await scrollToBottom()

  const history: PlaygroundMessage[] = messages.value.slice(0, -1).map(message => ({ role: message.role, content: message.content }))
  if (systemPrompt.value.trim()) history.unshift({ role: 'system', content: systemPrompt.value.trim() })

  try {
    await playgroundAPI.streamChat({
      groupId: selectedGroupId.value,
      model: selectedModel.value,
      messages: history,
      temperature: temperature.value,
      signal: streamController.signal,
      onDelta(delta) {
        const current = messages.value[assistantIndex]
        if (current?.id === assistant.id) current.content += delta
        scrollToBottom()
      }
    })
    await authStore.refreshUser()
  } catch (error) {
    if ((error as Error).name !== 'AbortError') {
      chatError.value = errorMessage(error, t('playground.requestFailed'))
      if (!assistant.content) messages.value = messages.value.filter(message => message.id !== assistant.id)
    }
  } finally {
    streaming.value = false
    streamController = null
  }
}

function stopStream() {
  streamController?.abort()
}

async function copyMessage(content: string) {
  await navigator.clipboard.writeText(content)
  appStore.showSuccess(t('playground.copied'))
}

async function generateImages() {
  if (!canGenerate.value) return
  generating.value = true
  imageError.value = ''
  try {
    images.value = await playgroundAPI.generateImages({
      groupId: selectedGroupId.value,
      model: selectedModel.value,
      prompt: imagePrompt.value.trim(),
      size: imageSize.value,
      quality: imageQuality.value,
      count: Math.min(4, Math.max(1, imageCount.value))
    })
    await authStore.refreshUser()
  } catch (error) {
    imageError.value = errorMessage(error, t('playground.imageFailed'))
  } finally {
    generating.value = false
  }
}

watch(selectedGroupId, loadModels)
onMounted(loadGroups)
onBeforeUnmount(stopStream)
</script>

<style scoped>
.playground-markdown :deep(p) { margin: 0 0 0.75rem; }
.playground-markdown :deep(p:last-child) { margin-bottom: 0; }
.playground-markdown :deep(pre) { margin: 0.75rem 0; overflow-x: auto; border-radius: 0.375rem; background: #111827; padding: 0.875rem; color: #f3f4f6; }
.playground-markdown :deep(code:not(pre code)) { border-radius: 0.25rem; background: rgb(243 244 246); padding: 0.125rem 0.3rem; font-size: 0.85em; }
:global(.dark) .playground-markdown :deep(code:not(pre code)) { background: rgb(55 65 81); }
.playground-markdown :deep(ul), .playground-markdown :deep(ol) { margin: 0.5rem 0; padding-left: 1.5rem; }
.playground-markdown :deep(ul) { list-style: disc; }
.playground-markdown :deep(ol) { list-style: decimal; }
</style>
