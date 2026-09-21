<template>
  <AppLayout>
    <div class="studio">
      <aside v-show="activeTab === 'standard'" class="history-panel" :class="{ 'mobile-open': historyOpen }">
        <div class="flex items-center justify-between">
          <h1 class="text-lg font-semibold">{{ t('playground.title') }}</h1>
          <button class="text-sm lg:hidden" @click="historyOpen = false">{{ t('common.close') }}</button>
        </div>
        <button class="btn btn-primary mt-5 w-full" :disabled="busy" @click="newConversation">＋ {{ t('playground.newChat') }}</button>
        <p class="mt-6 text-xs font-medium text-gray-400 dark:text-dark-400">{{ t('playground.history') }}</p>
        <div class="mt-3 min-h-0 flex-1 space-y-1 overflow-y-auto">
          <p v-if="!normalConversations.length" class="py-5 text-sm text-gray-400 dark:text-dark-400">{{ t('playground.noHistory') }}</p>
          <div v-for="item in normalConversations" :key="item.id" class="history-item" :class="{ selected: current.id === item.id }">
            <button class="min-w-0 flex-1 text-left" :disabled="busy" @click="openConversation(item)">
              <span class="block truncate text-sm font-medium">{{ item.title }}</span>
              <span class="mt-1 block truncate text-xs text-gray-400 dark:text-dark-400">{{ formatExpiry(item.expiresAt) }}</span>
            </button>
            <button class="rounded p-2 text-gray-400 hover:text-red-500 dark:text-dark-400" :disabled="busy" :aria-label="t('common.delete')" @click="deleteTarget = item.id">×</button>
          </div>
        </div>
        <p class="retention-note">{{ t('playground.retention') }}</p>
        <button class="mt-3 text-left text-xs text-gray-500 hover:text-primary-500 dark:text-dark-400" :disabled="busy" @click="refreshHistory">{{ t('playground.refreshHistory') }}</button>
      </aside>

      <main class="conversation-panel">
        <header class="studio-toolbar">
          <button v-if="activeTab === 'standard'" class="btn btn-secondary lg:hidden" @click="historyOpen = !historyOpen">☰</button>
          <div class="playground-tabs" role="tablist">
            <button class="playground-tab" :class="{ active: activeTab === 'standard' }" role="tab" :aria-selected="activeTab === 'standard'" @click="activeTab = 'standard'">{{ t('playground.standard') }}</button>
            <button class="playground-tab" :class="{ active: activeTab === 'workflow' }" role="tab" :aria-selected="activeTab === 'workflow'" @click="openWorkflowTab">{{ t('playground.workflow') }}</button>
          </div>
          <label class="min-w-0 flex-1 sm:flex-none">
            <span class="toolbar-label">{{ t('playground.group') }}</span>
            <select v-model.number="selectedGroupId" class="toolbar-select" :disabled="streaming || submitting || loadingGroups">
              <option :value="0" disabled>{{ t('playground.selectGroup') }}</option>
              <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option>
            </select>
          </label>
          <label class="min-w-0 flex-1">
            <span class="toolbar-label">{{ t('playground.chatModel') }}</span>
            <select v-model="selectedChatModel" class="toolbar-select w-full" :disabled="streaming || submitting || loadingModels">
              <option value="" disabled>{{ loadingModels ? t('common.loading') : t('playground.selectChatModel') }}</option>
              <option v-for="model in chatModels" :key="model.id" :value="model.id">{{ model.id }}</option>
            </select>
          </label>
          <p v-if="!loadingModels && (!chatModels.length || !imageModels.length)" class="toolbar-warning">
            {{ !chatModels.length ? t('playground.noChatModel') : t('playground.noImageModel') }}
          </p>
          <label class="min-w-0 flex-1">
            <span class="toolbar-label">{{ t('playground.imageModel') }}</span>
            <select v-model="selectedImageModel" class="toolbar-select w-full" :disabled="streaming || submitting || loadingModels">
              <option value="">{{ loadingModels ? t('common.loading') : t('playground.selectImageModel') }}</option>
              <option v-for="model in imageModels" :key="model.id" :value="model.id">{{ model.id }}</option>
            </select>
          </label>
          <div class="ml-auto hidden text-right sm:block"><span class="toolbar-label">{{ t('playground.balance') }}</span><span class="text-sm font-medium tabular-nums">${{ (authStore.user?.balance ?? 0).toFixed(4) }}</span></div>
        </header>

        <WorkflowPlayground v-show="activeTab === 'workflow'" :groups="groups" :group-id="workflowPresets.groupId" :chat-model="workflowPresets.chatModel" :image-model="workflowPresets.imageModel" @presets="applyWorkflowPresets" />
        <div v-show="activeTab === 'standard'" class="standard-playground">
        <div ref="viewport" class="conversation-scroll">
          <div v-if="!current.messages.length && !currentImages.length" class="welcome">
            <div class="welcome-icon">✦</div>
            <h2 class="text-2xl font-semibold tracking-tight sm:text-3xl">{{ t('playground.welcome') }}</h2>
            <p class="mt-3 max-w-md text-sm leading-6 text-gray-500 dark:text-dark-300">{{ t('playground.welcomeDescription') }}</p>
            <div class="mt-8 grid w-full max-w-lg gap-3 sm:grid-cols-2">
              <button class="suggestion" @click="draft = t('playground.chatSuggestion')">{{ t('playground.chatSuggestion') }} ↗</button>
              <button class="suggestion" @click="draft = t('playground.imageSuggestion')">{{ t('playground.imageSuggestion') }} ↗</button>
            </div>
          </div>
          <div v-else class="mx-auto w-full max-w-3xl space-y-7">
            <template v-for="message in current.messages" :key="message.id">
              <article :class="message.role === 'user' ? 'flex justify-end' : 'assistant-message'">
                <div v-if="message.role === 'user'" class="user-bubble">{{ message.content }}</div>
                <div v-else>
                  <span class="mb-2 block text-xs font-semibold text-gray-400 dark:text-dark-400">{{ message.model || current.model }}</span>
                  <div class="playground-markdown" v-html="renderMarkdown(message.content || (streaming ? '…' : ''))"></div>
                  <button v-if="message.content" class="mt-2 text-xs text-gray-400 hover:text-primary-500 dark:text-dark-400" @click="copyMessage(message.content)">{{ t('playground.copy') }}</button>
                </div>
              </article>
              <div v-if="currentImages.some(image => image.messageId === message.id)" class="grid gap-4 sm:grid-cols-2">
                <figure v-for="image in currentImages.filter(entry => entry.messageId === message.id)" :key="image.id" class="image-card">
                  <button class="image-preview block w-full cursor-zoom-in" :aria-label="t('playground.preview')" @click="preview = image"><img :src="image.url" :alt="image.prompt" class="max-h-96 w-full object-contain" /></button>
                  <figcaption class="flex items-center justify-between gap-2 p-3">
                    <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ remaining(image.expiresAt) }}</span>
                    <button class="text-sm font-medium text-primary-600" @click="downloadImage(image)">{{ t('playground.download') }}</button>
                  </figcaption>
                </figure>
              </div>
            </template>
            <p v-if="generating" class="animate-pulse text-sm text-gray-500 dark:text-dark-400">{{ t('playground.generating') }}…</p>
          </div>
        </div>

        <div class="composer-area">
          <div class="mx-auto max-w-3xl">
            <p v-if="error" role="alert" class="mb-3 text-sm text-red-500">{{ error }}</p>
            <p v-else-if="!hasBalance" role="alert" class="mb-3 text-sm text-amber-600 dark:text-amber-400">{{ t('playground.insufficientBalance') }}</p>
            <p v-if="saveError" role="alert" class="mb-3 text-sm text-amber-600">{{ saveError }} <button class="underline" :disabled="busy" @click="retrySave">{{ t('playground.retrySave') }}</button></p>
            <div v-if="settingsOpen" class="settings-panel">
              <label class="col-span-2 text-xs">{{ t('playground.systemPrompt') }}<textarea v-model="current.systemPrompt" class="input mt-2 w-full" rows="2" :disabled="streaming || submitting" /></label>
              <label class="col-span-2 text-xs">{{ t('playground.temperature') }} · {{ current.temperature.toFixed(1) }}<input v-model.number="current.temperature" class="mt-3 w-full accent-primary-500" type="range" min="0" max="2" step="0.1" :disabled="streaming || submitting" /></label>
              <label class="text-xs">{{ t('playground.size') }}<select v-model="imageSize" class="input mt-2 w-full" :disabled="streaming || submitting"><option>1024x1024</option><option>1536x1024</option><option>1024x1536</option></select></label>
              <label class="text-xs">{{ t('playground.quality') }}<select v-model="imageQuality" class="input mt-2 w-full" :disabled="streaming || submitting"><option>auto</option><option>low</option><option>medium</option><option>high</option></select></label>
            </div>
            <form class="composer" @submit.prevent="send">
              <textarea v-model="draft" rows="3" class="composer-input" :placeholder="t('playground.messagePlaceholder')" :disabled="streaming || submitting" @keydown.enter.exact="onEnter" />
              <div class="flex items-center justify-between gap-3 px-3 pb-3">
                <div class="flex items-center gap-1">
                  <span class="mode-indicator">{{ t('playground.autoIntent') }}</span>
                  <button type="button" class="mode-button" :aria-expanded="settingsOpen" :disabled="streaming || submitting" @click="settingsOpen = !settingsOpen">{{ t('playground.settings') }}</button>
                </div>
                <button v-if="streaming" type="button" class="btn btn-secondary" @click="controller?.abort()">{{ t('playground.stop') }}</button>
                <button v-else class="btn btn-primary" type="submit" :disabled="!canSend">{{ t('playground.send') }} ↑</button>
              </div>
            </form>
            <p class="mt-3 text-center text-xs leading-5 text-gray-400 dark:text-dark-400">{{ t('playground.retention') }} {{ t('playground.imageRetention') }}</p>
          </div>
        </div>
        </div>
      </main>
    </div>
    <BaseDialog :show="Boolean(preview)" :title="t('playground.preview')" width="wide" :close-on-click-outside="true" @close="preview = null">
      <template v-if="preview"><img :src="preview.url" :alt="preview.prompt" class="max-h-[65vh] w-full object-contain" /><button class="btn btn-primary mt-4" @click="downloadImage(preview)">{{ t('playground.download') }}</button></template>
    </BaseDialog>
    <BaseDialog :show="Boolean(deleteTarget)" :title="t('common.delete')" @close="deleteTarget = ''"><p>{{ t('playground.deleteConfirm') }}</p><button class="btn btn-primary mt-4" @click="deleteConversation">{{ t('common.delete') }}</button></BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { userGroupsAPI } from '@/api/groups'
import { playgroundAPI, type PlaygroundModel, type PlaygroundMessage } from '@/api/playground'
import { playgroundHistory, type Conversation, type ConversationMessage } from '@/api/playgroundHistory'
import { useAppStore, useAuthStore } from '@/stores'
import type { Group } from '@/types'
import { isPlaygroundImageModel, isPlaygroundVideoModel } from '@/utils/playgroundModel'
import { resolvePlaygroundIntent } from '@/utils/playgroundIntent'
import { createPlaygroundId } from '@/utils/playgroundId'
import WorkflowPlayground from '@/components/playground/WorkflowPlayground.vue'

type PlaygroundKind = 'chat' | 'image'
type StoredMessage = ConversationMessage & { model?: string; kind?: PlaygroundKind }
type PlaygroundConversation = Omit<Conversation, 'messages'> & { messages: StoredMessage[] }
type ImageEntry = { id: string; conversationId: string; messageId: number; url: string; prompt: string; expiresAt: number }
const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const groups = ref<Group[]>([])
const models = ref<PlaygroundModel[]>([])
const conversations = ref<PlaygroundConversation[]>([])
const current = ref<PlaygroundConversation>(emptyConversation())
const draft = ref('')
const error = ref('')
const saveError = ref('')
const loadingGroups = ref(false)
const loadingModels = ref(false)
const streaming = ref(false)
const generatingCount = ref(0)
const saving = ref(false)
const submitting = ref(false)
const settingsOpen = ref(false)
const activeTab = ref<'standard' | 'workflow'>('standard')
const workflowPresets = ref({ groupId: 0, chatModel: '', imageModel: '' })
const historyOpen = ref(false)
const deleteTarget = ref('')
const imageSize = ref('1024x1024')
const imageQuality = ref('auto')
const images = ref<ImageEntry[]>([])
const preview = ref<ImageEntry | null>(null)
const viewport = ref<HTMLElement | null>(null)
const now = ref(Date.now())
let controller: AbortController | null = null
let timer: ReturnType<typeof setInterval> | undefined
let modelRequest = 0
let disposed = false
let saveQueue: Promise<boolean> = Promise.resolve(true)
let lastMessageId = Date.now()
const acceptedRevisions = new Map<string, number>()
const generating = computed(() => generatingCount.value > 0)
const busy = computed(() => streaming.value || generating.value || saving.value)
const hasBalance = computed(() => Number(authStore.user?.balance ?? 0) > 0)
const canSend = computed(() => hasBalance.value && !streaming.value && !submitting.value && !loadingModels.value && Boolean(draft.value.trim() && current.value.groupId))
const chatModels = computed(() => models.value.filter((model) => !isPlaygroundImageModel(model.id) && !isPlaygroundVideoModel(model.id)))
const imageModels = computed(() => models.value.filter((model) => isPlaygroundImageModel(model.id)))
const normalConversations = computed(() => conversations.value.filter((conversation) => conversation.kind !== 'workflow'))
const currentImages = computed(() => images.value.filter(image => image.conversationId === current.value.id && image.expiresAt > now.value))
const selectedGroupId = computed({
  get: () => activeTab.value === 'workflow' ? workflowPresets.value.groupId : current.value.groupId,
  set: (value: number) => {
    if (activeTab.value === 'workflow') workflowPresets.value.groupId = value
    else current.value.groupId = value
  }
})
const selectedChatModel = computed({
  get: () => activeTab.value === 'workflow' ? workflowPresets.value.chatModel : current.value.model,
  set: (value: string) => {
    if (activeTab.value === 'workflow') workflowPresets.value.chatModel = value
    else current.value.model = value
  }
})
const selectedImageModel = computed({
  get: () => activeTab.value === 'workflow' ? workflowPresets.value.imageModel : current.value.imageModel || '',
  set: (value: string) => {
    if (activeTab.value === 'workflow') workflowPresets.value.imageModel = value
    else current.value.imageModel = value
  }
})

function emptyConversation(): PlaygroundConversation {
  return { id: createPlaygroundId(), title: '', groupId: 0, model: '', imageModel: '', systemPrompt: '', temperature: 0.7, messages: [], revision: 0, expiresAt: 0 }
}
function createMessageId(): number {
  lastMessageId = Math.max(lastMessageId + 1, Date.now())
  return lastMessageId
}
function reseedMessageId(messages: StoredMessage[]) {
  const maxExistingId = messages.reduce((maximum, message) => Math.max(maximum, message.id), 0)
  lastMessageId = Math.max(lastMessageId + 1, Date.now(), maxExistingId + 1)
}
function renderMarkdown(content: string) {
  return DOMPurify.sanitize(marked.parse(content, { async: false, gfm: true, breaks: true }) as string)
}
function formatExpiry(expiry: number) { return t('playground.expires', { time: new Date(expiry).toLocaleString() }) }
function remaining(expiry: number) {
  const seconds = Math.max(0, Math.ceil((expiry - now.value) / 1000))
  return t('playground.remaining', { time: `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` })
}
async function scrollBottom() { await nextTick(); if (viewport.value) viewport.value.scrollTop = viewport.value.scrollHeight }
function newConversation() {
  if (busy.value) return
  const { groupId, model, imageModel } = current.value
  current.value = { ...emptyConversation(), groupId, model, imageModel }
  acceptedRevisions.clear()
  acceptedRevisions.set(current.value.id, current.value.revision)
  draft.value = ''; error.value = ''; saveError.value = ''; historyOpen.value = false
}
function openWorkflowTab() {
  if (!workflowPresets.value.groupId) {
    workflowPresets.value = {
      groupId: current.value.groupId,
      chatModel: current.value.model,
      imageModel: current.value.imageModel || ''
    }
  }
  activeTab.value = 'workflow'
}
function applyWorkflowPresets(value: { groupId: number; chatModel: string; imageModel: string }) {
  workflowPresets.value = { ...value }
  void loadModels(value.groupId, 'workflow')
}
function openConversation(item: PlaygroundConversation) {
  if (busy.value) return
  current.value = JSON.parse(JSON.stringify(item)) as PlaygroundConversation
  acceptedRevisions.clear()
  acceptedRevisions.set(current.value.id, current.value.revision)
  reseedMessageId(current.value.messages)
  draft.value = ''; error.value = ''; saveError.value = ''; historyOpen.value = false
  scrollBottom()
}
async function refreshHistory() {
  try { conversations.value = (await playgroundHistory.list()).sort((first, second) => second.expiresAt - first.expiresAt) }
  catch { appStore.showError(t('playground.historyFailed')) }
}
async function saveConversation(snapshot = JSON.parse(JSON.stringify(current.value)) as PlaygroundConversation) {
  if (!snapshot.messages.length) return true
  const task = saveQueue.then(async () => {
    saving.value = true
    try {
      snapshot.revision = acceptedRevisions.get(snapshot.id) ?? snapshot.revision
      const saved = await playgroundHistory.save(snapshot)
      acceptedRevisions.set(saved.id, saved.revision)
      if (current.value.id === saved.id) {
        current.value.revision = saved.revision; current.value.expiresAt = saved.expiresAt
      }
      conversations.value = [saved, ...conversations.value.filter(item => item.id !== saved.id)]
      saveError.value = ''
      return true
    } catch { saveError.value = t('playground.saveFailed'); return false }
    finally { saving.value = false }
  })
  saveQueue = task.catch(() => false)
  return task
}
async function retrySave() { await saveConversation() }
async function deleteConversation() {
  if (busy.value) return
  const id = deleteTarget.value
  try {
    await playgroundHistory.delete(id)
    conversations.value = conversations.value.filter(item => item.id !== id)
    images.value = images.value.filter(image => image.conversationId !== id)
    acceptedRevisions.delete(id)
    if (current.value.id === id) newConversation()
    deleteTarget.value = ''
  } catch { appStore.showError(t('playground.historyFailed')) }
}
async function loadModels(groupId: number, target: 'standard' | 'workflow') {
  const request = ++modelRequest
  const preferred = target === 'workflow' ? workflowPresets.value : {
    groupId: current.value.groupId,
    chatModel: current.value.model,
    imageModel: current.value.imageModel || ''
  }
  models.value = []; loadingModels.value = true
  try {
    const result = groupId ? await playgroundAPI.listModels(groupId) : []
    if (request !== modelRequest) return
    models.value = result
    const chatModel = chatModels.value.some(item => item.id === preferred.chatModel) ? preferred.chatModel : (chatModels.value[0]?.id ?? '')
    const imageModel = imageModels.value.some(item => item.id === preferred.imageModel) ? preferred.imageModel : (imageModels.value[0]?.id ?? '')
    if (target === 'workflow') workflowPresets.value = { groupId, chatModel, imageModel }
    else { current.value.model = chatModel; current.value.imageModel = imageModel }
  } catch { if (request === modelRequest) {
    if (target === 'workflow') workflowPresets.value = { groupId, chatModel: '', imageModel: '' }
    else { current.value.model = ''; current.value.imageModel = '' }
    error.value = t('playground.loadModelsFailed')
  } }
  finally { if (request === modelRequest) loadingModels.value = false }
}
function onEnter(event: KeyboardEvent) { if (!event.isComposing) { event.preventDefault(); send() } }
function messageText(content: unknown): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .filter((part): part is { type: 'text'; text: string } => typeof part === 'object' && part !== null && part.type === 'text' && typeof part.text === 'string')
    .map((part) => part.text)
    .join('\n')
}
function isImageRevisionPrompt(prompt: string): boolean {
  return /(?:再来|再生成|重画|重新画|换成|改成|换个|换一|改一|更写实|更真实|更亮|更暗|加上|去掉|去除|背景|颜色|构图|画风|风格|分辨率)|\b(?:another|redraw|regenerate|brighter|darker|background|colou?r|style|replace|remove|add)\b/i.test(prompt)
}
async function send() {
  if (!hasBalance.value) { error.value = t('playground.insufficientBalance'); return }
  if (!canSend.value || submitting.value) return
  if (current.value.expiresAt && current.value.expiresAt <= Date.now()) { newConversation(); error.value = t('playground.expired'); return }
  const prompt = draft.value.trim()
  const conversation = current.value
  const previousIntent = [...conversation.messages].reverse().find(message => message.role === 'assistant')?.kind
  const kind = resolvePlaygroundIntent(prompt, previousIntent)
  const model = kind === 'image' ? conversation.imageModel : conversation.model
  const groupId = conversation.groupId
  const systemPrompt = conversation.systemPrompt
  const temperature = conversation.temperature
  const priorImagePrompt = previousIntent === 'image' && isImageRevisionPrompt(prompt)
    ? messageText([...conversation.messages].reverse().find(message => message.role === 'assistant' && message.kind === 'image')?.content)
    : ''
  if (!model) { error.value = t(kind === 'image' ? 'playground.noImageModel' : 'playground.noChatModel'); return }
  const userId = createMessageId()
  const conversationId = conversation.id
  conversation.messages.push({ id: userId, role: 'user', content: prompt, model, kind })
  if (!conversation.title) conversation.title = prompt.slice(0, 60)
  submitting.value = true
  let initialSaveSucceeded = false
  try {
    initialSaveSucceeded = await saveConversation(JSON.parse(JSON.stringify(conversation)) as PlaygroundConversation)
  } finally {
    submitting.value = false
  }
  if (!initialSaveSucceeded) {
    conversation.messages = conversation.messages.filter(message => message.id !== userId)
    return
  }
  draft.value = ''; error.value = ''
  if (kind === 'image') {
    generatingCount.value += 1
    try {
      const generationPrompt = priorImagePrompt ? `${priorImagePrompt}\n\n${prompt}` : prompt
      const generated = await playgroundAPI.generateImages({ groupId, model, prompt: generationPrompt, size: imageSize.value, quality: imageQuality.value, count: 1 })
      if (!generated.length) throw new Error(t('playground.imageFailed'))
      if (!disposed) images.value.push(...generated.map(image => ({ id: createPlaygroundId(), conversationId, messageId: userId, url: image.url, prompt: generationPrompt, expiresAt: Date.now() + 600000 })))
      conversation.messages.push({ id: createMessageId(), role: 'assistant', content: generated[0].revisedPrompt || generationPrompt, model, kind })
    } catch (caught) { error.value = caught instanceof Error ? caught.message : t('playground.imageFailed') }
    finally { generatingCount.value -= 1 }
  } else {
    const history: PlaygroundMessage[] = conversation.messages.map(message => ({ role: message.role, content: message.content }))
    if (systemPrompt) history.unshift({ role: 'system', content: systemPrompt })
    const assistantId = createMessageId()
    conversation.messages.push({ id: assistantId, role: 'assistant', content: '', model, kind })
    streaming.value = true; controller = new AbortController()
    try {
      await playgroundAPI.streamChat({ groupId, model, messages: history, temperature, signal: controller.signal, onDelta(delta) {
        const assistant = conversation.messages.find(message => message.id === assistantId)
        if (assistant) assistant.content += delta
        if (current.value.id === conversationId) scrollBottom()
      } })
    } catch (caught) { if ((caught as Error).name !== 'AbortError') error.value = caught instanceof Error ? caught.message : t('playground.requestFailed') }
    finally { streaming.value = false; controller = null; conversation.messages = conversation.messages.filter(message => message.content) }
  }
  await saveConversation(JSON.parse(JSON.stringify(conversation)) as PlaygroundConversation)
  await authStore.refreshUser().catch(() => {})
  scrollBottom()
}
async function copyMessage(content: string) {
  try { await navigator.clipboard.writeText(content); appStore.showSuccess(t('playground.copied')) }
  catch { appStore.showError(t('playground.copyFailed')) }
}
async function downloadImage(image: ImageEntry) {
  if (image.expiresAt <= Date.now()) return
  try {
    const result = await fetch(image.url)
    if (!result.ok) throw new Error('download')
    const blob = await result.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = `playground-${image.id}.png`; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch { appStore.showError(t('playground.downloadFailed')) }
}
watch(
  () => [activeTab.value, selectedGroupId.value] as const,
  ([target, groupId]) => { void loadModels(groupId, target) }
)
onMounted(async () => {
  timer = setInterval(() => {
    now.value = Date.now()
    images.value = images.value.filter(image => image.expiresAt > now.value)
    if (preview.value && preview.value.expiresAt <= now.value) preview.value = null
    conversations.value = conversations.value.filter(item => item.expiresAt > now.value)
    if (!busy.value && current.value.expiresAt && current.value.expiresAt <= now.value) { newConversation(); error.value = t('playground.expired') }
  }, 1000)
  loadingGroups.value = true
  try { groups.value = await userGroupsAPI.getAvailable(); current.value.groupId = groups.value[0]?.id ?? 0 }
  catch { error.value = t('playground.loadModelsFailed') }
  finally { loadingGroups.value = false }
  await refreshHistory()
})
onBeforeUnmount(() => { disposed = true; controller?.abort(); clearInterval(timer); images.value = []; preview.value = null })
</script>

<style scoped>
.studio { display:flex; height:calc(100dvh - 8rem); min-height:540px; overflow:hidden; border:1px solid #e5e7eb; border-radius:20px; background:#fff; color:#1f2937; }
.history-panel { display:flex; flex-direction:column; width:244px; flex-shrink:0; padding:24px 16px; border-right:1px solid #e5e7eb; background:#f8f9fb; }
.history-item { display:flex; align-items:center; gap:4px; padding:12px 8px; border-radius:10px; }
.history-item:hover,.history-item.selected { background:#eaeef5; }
.retention-note { margin-top:20px; font-size:11px; line-height:1.8; color:#9ca3af; }
.conversation-panel { display:flex; flex:1; min-width:0; flex-direction:column; }
.standard-playground { display:flex; flex:1; min-height:0; flex-direction:column; overflow:hidden; }
.studio-toolbar { display:flex; align-items:center; gap:20px; padding:18px 24px; border-bottom:1px solid #f0f1f3; }
.toolbar-label { display:block; margin-bottom:4px; color:#9ca3af; font-size:10px; }
.toolbar-select { max-width:280px; background:transparent; font-size:13px; font-weight:500; outline-offset:3px; }
.toolbar-warning { max-width:180px; color:#b45309; font-size:11px; line-height:1.4; }
.playground-tabs { display:flex; gap:2px; padding:3px; border:1px solid #e5e7eb; border-radius:8px; background:#f8f9fb; }
.playground-tab { border-radius:5px; padding:5px 8px; color:#6b7280; font-size:12px; }
.playground-tab.active { background:#fff; color:#4f46e5; box-shadow:0 1px 2px #0000000d; }
.conversation-scroll { flex:1; min-height:0; overflow:auto; padding:32px; }
.welcome { display:flex; height:100%; min-height:280px; flex-direction:column; align-items:center; justify-content:center; text-align:center; }
.welcome-icon { display:grid; place-items:center; width:56px; height:56px; margin-bottom:24px; border-radius:18px; background:#eef2ff; color:#6366f1; font-size:30px; }
.suggestion { border:1px solid #e5e7eb; border-radius:12px; padding:18px; text-align:left; font-size:13px; color:#6b7280; transition:background .2s; }
.suggestion:hover { background:#f8f9fb; }
.user-bubble { max-width:85%; border-radius:18px 18px 4px 18px; padding:12px 18px; background:#f0f2f6; white-space:pre-wrap; overflow-wrap:anywhere; font-size:14px; line-height:1.8; }
.assistant-message { font-size:14px; line-height:1.9; overflow-wrap:anywhere; }
.composer-area { flex-shrink:0; padding:12px 32px 20px; }
.composer { border:1px solid #dfe3ea; border-radius:18px; box-shadow:0 4px 20px #00000005; }
.composer:focus-within { border-color:#a5b4fc; }
.composer-input { display:block; width:100%; resize:none; padding:16px 18px 8px; background:transparent; outline:none; font-size:14px; }
.mode-button { border-radius:8px; padding:7px 10px; color:#9ca3af; font-size:12px; }
.mode-indicator { border-radius:8px; padding:7px 10px; background:#eef2ff; color:#6366f1; font-size:12px; }
.settings-panel { display:grid; grid-template-columns:1fr 1fr; gap:16px; margin-bottom:12px; padding:16px; border:1px solid #e5e7eb; border-radius:12px; }
.image-card { overflow:hidden; border:1px solid #e5e7eb; border-radius:14px; background:#fff; }
.image-preview { background:#f9fafb; }
.playground-markdown :deep(p) { margin-bottom:12px; }
.playground-markdown :deep(pre) { overflow:auto; padding:16px; margin:16px 0; border-radius:12px; background:#111827; color:#f3f4f6; }
.playground-markdown :deep(ul),.playground-markdown :deep(ol) { padding-left:24px; list-style:revert; }
.playground-markdown :deep(table) { display:block; max-width:100%; overflow:auto; border-collapse:collapse; }
.playground-markdown :deep(td),.playground-markdown :deep(th) { border:1px solid #d1d5db; padding:8px; }
:global(html.dark .studio),:global(html.dark .conversation-panel),:global(html.dark .studio-toolbar),:global(html.dark .conversation-scroll),:global(html.dark .composer-area) { background:#151b26; color:#e5e7eb; border-color:#303747; color-scheme:dark; }
:global(html.dark .history-panel),:global(html.dark .image-preview) { background:#111721; border-color:#303747; }
:global(html.dark .history-item.selected),:global(html.dark .history-item:hover),:global(html.dark .user-bubble),:global(html.dark .suggestion:hover) { background:#252e3f; }
:global(html.dark .retention-note),:global(html.dark .toolbar-label) { color:#94a3b8; }
:global(html.dark .composer),:global(html.dark .settings-panel),:global(html.dark .image-card),:global(html.dark .suggestion),:global(html.dark .playground-tabs) { border-color:#303747; background:#1b2432; }
:global(html.dark .composer-input),:global(html.dark .toolbar-select),:global(html.dark .toolbar-warning) { color:#e5e7eb; }
:global(html.dark .mode-button) { color:#94a3b8; }
:global(html.dark .mode-indicator),:global(html.dark .playground-tab.active) { background:#252e3f; color:#a5b4fc; }
:global(html.dark .playground-tab),:global(html.dark .suggestion) { color:#cbd5e1; }
:global(html.dark .playground-markdown td),:global(html.dark .playground-markdown th) { border-color:#475569; }
:global(html.dark .toolbar-select option) { background:#151b26; color:#e5e7eb; }
:global(html.dark .composer-input::placeholder) { color:#64748b; }
@media(max-width:1023px) { .history-panel { display:none; } .history-panel.mobile-open { display:flex; position:absolute; inset:0; width:min(300px,85%); z-index:20; box-shadow:12px 0 30px #0002; } .studio { position:relative; } }
@media(max-width:640px) { .studio { height:calc(100dvh - 7rem); min-height:440px; border-radius:12px; } .studio-toolbar { flex-wrap:wrap; padding:12px; gap:10px; } .studio-toolbar > .btn { flex:0 0 auto; } .playground-tabs { flex:0 0 auto; flex-wrap:nowrap; white-space:nowrap; } .playground-tab { flex-shrink:0; white-space:nowrap; } .studio-toolbar > label { flex:1 1 30%; min-width:100px; } .toolbar-select { width:100%; max-width:100%; } .toolbar-warning { flex-basis:100%; max-width:none; } .conversation-scroll { padding:20px 14px; } .composer-area { padding:8px 12px 14px; } .mode-button { padding:6px; } }
</style>
