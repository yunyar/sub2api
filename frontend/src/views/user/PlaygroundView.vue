<template>
  <AppLayout>
    <div class="studio">
      <aside class="history-panel" :class="{ 'mobile-open': historyOpen }">
        <div class="flex items-center justify-between">
          <h1 class="text-lg font-semibold">{{ t('playground.title') }}</h1>
          <button class="text-sm lg:hidden" @click="historyOpen = false">{{ t('common.close') }}</button>
        </div>
        <button class="btn btn-primary mt-5 w-full" :disabled="busy" @click="newConversation">＋ {{ t('playground.newChat') }}</button>
        <p class="mt-6 text-xs font-medium text-gray-400 dark:text-dark-400">{{ t('playground.history') }}</p>
        <div class="mt-3 min-h-0 flex-1 space-y-1 overflow-y-auto">
          <p v-if="!conversations.length" class="py-5 text-sm text-gray-400 dark:text-dark-400">{{ t('playground.noHistory') }}</p>
          <div v-for="item in conversations" :key="item.id" class="history-item" :class="{ selected: current.id === item.id }">
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
          <button class="btn btn-secondary lg:hidden" @click="historyOpen = !historyOpen">☰</button>
          <label class="min-w-0 flex-1 sm:flex-none">
            <span class="toolbar-label">{{ t('playground.group') }}</span>
            <select v-model.number="current.groupId" class="toolbar-select" :disabled="busy || loadingGroups">
              <option :value="0" disabled>{{ t('playground.selectGroup') }}</option>
              <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option>
            </select>
          </label>
          <label class="min-w-0 flex-1">
            <span class="toolbar-label">{{ t('playground.model') }}</span>
            <select v-model="current.model" class="toolbar-select w-full" :disabled="busy || loadingModels">
              <option value="" disabled>{{ loadingModels ? t('common.loading') : t('playground.selectModel') }}</option>
              <option v-for="model in models" :key="model.id" :value="model.id">{{ model.id }}</option>
            </select>
          </label>
          <div class="ml-auto hidden text-right sm:block"><span class="toolbar-label">{{ t('playground.balance') }}</span><span class="text-sm font-medium tabular-nums">${{ (authStore.user?.balance ?? 0).toFixed(4) }}</span></div>
        </header>

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
                  <span class="mb-2 block text-xs font-semibold text-gray-400 dark:text-dark-400">{{ current.model }}</span>
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
            <p v-if="saveError" role="alert" class="mb-3 text-sm text-amber-600">{{ saveError }} <button class="underline" :disabled="busy" @click="saveConversation">{{ t('playground.retrySave') }}</button></p>
            <div v-if="settingsOpen" class="settings-panel">
              <template v-if="!imageModelSelected">
                <label class="col-span-2 text-xs">{{ t('playground.systemPrompt') }}<textarea v-model="current.systemPrompt" class="input mt-2 w-full" rows="2" :disabled="busy" /></label>
                <label class="col-span-2 text-xs">{{ t('playground.temperature') }} · {{ current.temperature.toFixed(1) }}<input v-model.number="current.temperature" class="mt-3 w-full accent-primary-500" type="range" min="0" max="2" step="0.1" :disabled="busy" /></label>
              </template>
              <template v-else>
                <label class="text-xs">{{ t('playground.size') }}<select v-model="imageSize" class="input mt-2 w-full" :disabled="busy"><option>1024x1024</option><option>1536x1024</option><option>1024x1536</option></select></label>
                <label class="text-xs">{{ t('playground.quality') }}<select v-model="imageQuality" class="input mt-2 w-full" :disabled="busy"><option>auto</option><option>low</option><option>medium</option><option>high</option></select></label>
              </template>
            </div>
            <form class="composer" @submit.prevent="send">
              <textarea v-model="draft" rows="3" class="composer-input" :placeholder="t(imageModelSelected ? 'playground.imagePromptPlaceholder' : 'playground.messagePlaceholder')" :disabled="streaming" @keydown.enter.exact="onEnter" />
              <div class="flex items-center justify-between gap-3 px-3 pb-3">
                <div class="flex items-center gap-1">
                  <span class="mode-indicator">{{ t(imageModelSelected ? 'playground.autoImage' : 'playground.autoChat') }}</span>
                  <button type="button" class="mode-button" :aria-expanded="settingsOpen" :disabled="streaming" @click="settingsOpen = !settingsOpen">{{ t('playground.settings') }}</button>
                </div>
                <button v-if="streaming" type="button" class="btn btn-secondary" @click="controller?.abort()">{{ t('playground.stop') }}</button>
                <button v-else class="btn btn-primary" type="submit" :disabled="!canSend">{{ t(imageModelSelected ? 'playground.generate' : 'playground.send') }} ↑</button>
              </div>
            </form>
            <p class="mt-3 text-center text-xs leading-5 text-gray-400 dark:text-dark-400">{{ t(imageModelSelected ? 'playground.imageRetention' : 'playground.retention') }}</p>
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
import { playgroundHistory, type Conversation } from '@/api/playgroundHistory'
import { useAppStore, useAuthStore } from '@/stores'
import type { Group } from '@/types'
import { isPlaygroundImageModel } from '@/utils/playgroundModel'

type ImageEntry = { id: string; conversationId: string; messageId: number; url: string; prompt: string; expiresAt: number }
const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const groups = ref<Group[]>([])
const models = ref<PlaygroundModel[]>([])
const conversations = ref<Conversation[]>([])
const current = ref<Conversation>(emptyConversation())
const draft = ref('')
const error = ref('')
const saveError = ref('')
const loadingGroups = ref(false)
const loadingModels = ref(false)
const streaming = ref(false)
const generatingCount = ref(0)
const saving = ref(false)
const settingsOpen = ref(false)
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
const generating = computed(() => generatingCount.value > 0)
const imageModelSelected = computed(() => isPlaygroundImageModel(current.value.model))
const busy = computed(() => streaming.value || generating.value || saving.value)
const hasBalance = computed(() => Number(authStore.user?.balance ?? 0) > 0)
const canSend = computed(() => hasBalance.value && !streaming.value && !loadingModels.value && Boolean(draft.value.trim() && current.value.groupId && current.value.model))
const currentImages = computed(() => images.value.filter(image => image.conversationId === current.value.id && image.expiresAt > now.value))

function emptyConversation(): Conversation {
  return { id: crypto.randomUUID(), title: '', groupId: 0, model: '', systemPrompt: '', temperature: 0.7, messages: [], revision: 0, expiresAt: 0 }
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
  const { groupId, model } = current.value
  current.value = { ...emptyConversation(), groupId, model }
  draft.value = ''; error.value = ''; saveError.value = ''; historyOpen.value = false
}
function openConversation(item: Conversation) {
  if (busy.value) return
  current.value = JSON.parse(JSON.stringify(item))
  draft.value = ''; error.value = ''; saveError.value = ''; historyOpen.value = false
  scrollBottom()
}
async function refreshHistory() {
  try { conversations.value = (await playgroundHistory.list()).sort((first, second) => second.expiresAt - first.expiresAt) }
  catch { appStore.showError(t('playground.historyFailed')) }
}
async function saveConversation() {
  if (!current.value.messages.length) return true
  const task = saveQueue.then(async () => {
    saving.value = true
    try {
      const saved = await playgroundHistory.save(JSON.parse(JSON.stringify(current.value)))
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
async function deleteConversation() {
  if (busy.value) return
  const id = deleteTarget.value
  try {
    await playgroundHistory.delete(id)
    conversations.value = conversations.value.filter(item => item.id !== id)
    images.value = images.value.filter(image => image.conversationId !== id)
    if (current.value.id === id) newConversation()
    deleteTarget.value = ''
  } catch { appStore.showError(t('playground.historyFailed')) }
}
async function loadModels(groupId: number) {
  const request = ++modelRequest
  const preferred = current.value.model
  models.value = []; loadingModels.value = true
  try {
    const result = groupId ? await playgroundAPI.listModels(groupId) : []
    if (request !== modelRequest) return
    models.value = result
    current.value.model = result.some(item => item.id === preferred) ? preferred : (result[0]?.id ?? '')
  } catch { if (request === modelRequest) { current.value.model = ''; error.value = t('playground.loadModelsFailed') } }
  finally { if (request === modelRequest) loadingModels.value = false }
}
function onEnter(event: KeyboardEvent) { if (!event.isComposing) { event.preventDefault(); send() } }
async function send() {
  if (!hasBalance.value) { error.value = t('playground.insufficientBalance'); return }
  if (!canSend.value) return
  if (current.value.expiresAt && current.value.expiresAt <= Date.now()) { newConversation(); error.value = t('playground.expired'); return }
  const prompt = draft.value.trim()
  const userId = Date.now()
  const conversationId = current.value.id
  current.value.messages.push({ id: userId, role: 'user', content: prompt })
  if (!current.value.title) current.value.title = prompt.slice(0, 60)
  if (!(await saveConversation())) { current.value.messages.pop(); return }
  draft.value = ''; error.value = ''
  if (imageModelSelected.value) {
    generatingCount.value += 1
    try {
      const generated = await playgroundAPI.generateImages({ groupId: current.value.groupId, model: current.value.model, prompt, size: imageSize.value, quality: imageQuality.value, count: 1 })
      if (!disposed) images.value.push(...generated.map(image => ({ id: crypto.randomUUID(), conversationId, messageId: userId, url: image.url, prompt, expiresAt: Date.now() + 600000 })))
      current.value.messages.push({ id: userId + 1, role: 'assistant', content: t('playground.imageRetention') })
    } catch (caught) { error.value = caught instanceof Error ? caught.message : t('playground.imageFailed') }
    finally { generatingCount.value -= 1 }
  } else {
    const history: PlaygroundMessage[] = current.value.messages.map(message => ({ role: message.role, content: message.content }))
    if (current.value.systemPrompt) history.unshift({ role: 'system', content: current.value.systemPrompt })
    const assistantId = userId + 1
    current.value.messages.push({ id: assistantId, role: 'assistant', content: '' })
    streaming.value = true; controller = new AbortController()
    try {
      await playgroundAPI.streamChat({ groupId: current.value.groupId, model: current.value.model, messages: history, temperature: current.value.temperature, signal: controller.signal, onDelta(delta) {
        const assistant = current.value.messages.find(message => message.id === assistantId)
        if (assistant) assistant.content += delta
        scrollBottom()
      } })
    } catch (caught) { if ((caught as Error).name !== 'AbortError') error.value = caught instanceof Error ? caught.message : t('playground.requestFailed') }
    finally { streaming.value = false; controller = null; current.value.messages = current.value.messages.filter(message => message.content) }
  }
  await saveConversation()
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
watch(() => current.value.groupId, loadModels)
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
.studio-toolbar { display:flex; align-items:center; gap:20px; padding:18px 24px; border-bottom:1px solid #f0f1f3; }
.toolbar-label { display:block; margin-bottom:4px; color:#9ca3af; font-size:10px; }
.toolbar-select { max-width:280px; background:transparent; font-size:13px; font-weight:500; outline-offset:3px; }
.conversation-scroll { flex:1; min-height:0; overflow:auto; padding:32px; }
.welcome { display:flex; height:100%; min-height:280px; flex-direction:column; align-items:center; justify-content:center; text-align:center; }
.welcome-icon { display:grid; place-items:center; width:56px; height:56px; margin-bottom:24px; border-radius:18px; background:#eef2ff; color:#6366f1; font-size:30px; }
.suggestion { border:1px solid #e5e7eb; border-radius:12px; padding:18px; text-align:left; font-size:13px; color:#6b7280; transition:background .2s; }
.suggestion:hover { background:#f8f9fb; }
.user-bubble { max-width:85%; border-radius:18px 18px 4px 18px; padding:12px 18px; background:#f0f2f6; white-space:pre-wrap; overflow-wrap:anywhere; font-size:14px; line-height:1.8; }
.assistant-message { font-size:14px; line-height:1.9; overflow-wrap:anywhere; }
.composer-area { padding:12px 32px 20px; }
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
:global(html.dark) .studio,:global(html.dark) .conversation-panel,:global(html.dark) .studio-toolbar,:global(html.dark) .conversation-scroll,:global(html.dark) .composer-area { background:#151b26; color:#e5e7eb; border-color:#303747; color-scheme:dark; }
:global(html.dark) .history-panel { background:#111721; border-color:#303747; }
:global(html.dark) .history-item.selected,:global(html.dark) .history-item:hover,:global(html.dark) .user-bubble { background:#252e3f; }
:global(html.dark) .retention-note { color:#64748b; }
:global(html.dark) .composer,:global(html.dark) .settings-panel,:global(html.dark) .image-card,:global(html.dark) .suggestion { border-color:#303747; background:#1b2432; }
:global(html.dark) .composer-input,:global(html.dark) .toolbar-select { color:#e5e7eb; }
:global(html.dark) .mode-button { color:#94a3b8; }
:global(html.dark) .mode-indicator { background:#252e3f; color:#a5b4fc; }
:global(html.dark) .suggestion { color:#cbd5e1; }
:global(html.dark) .suggestion:hover { background:#252e3f; }
:global(html.dark) .image-preview { background:#111721; }
:global(html.dark) .playground-markdown :deep(td),:global(html.dark) .playground-markdown :deep(th) { border-color:#475569; }
:global(html.dark) .toolbar-select option { background:#151b26; color:#e5e7eb; }
:global(html.dark) .composer-input::placeholder { color:#64748b; }
@media(max-width:1023px) { .history-panel { display:none; } .history-panel.mobile-open { display:flex; position:absolute; inset:0; width:min(300px,85%); z-index:20; box-shadow:12px 0 30px #0002; } .studio { position:relative; } }
@media(max-width:640px) { .studio { height:calc(100dvh - 7rem); min-height:440px; border-radius:12px; } .studio-toolbar { padding:12px; gap:10px; } .toolbar-select { max-width:100%; } .conversation-scroll { padding:20px 14px; } .composer-area { padding:8px 12px 14px; } .mode-button { padding:6px; } }
</style>
