<template>
  <section class="workflow-shell">
    <aside class="workflow-history">
      <button class="btn btn-primary w-full" :disabled="locked" @click="newWorkflow">{{ t('workflow.new') }}</button>
      <button class="btn btn-secondary mt-2 w-full" :disabled="locked" @click="load">{{ t('workflow.refresh') }}</button>
      <p class="mt-4 text-xs text-gray-500 dark:text-dark-400">{{ t('workflow.history') }}</p>
      <div class="mt-2 space-y-1 overflow-y-auto">
        <div v-for="item in history" :key="item.id" class="flex gap-1 rounded border border-gray-200 p-2 dark:border-dark-600">
          <button class="min-w-0 flex-1 text-left" :disabled="locked" @click="openWorkflow(item)"><span class="block truncate text-sm">{{ item.title || t('workflow.untitled') }}</span><span class="text-xs text-gray-500 dark:text-dark-400">{{ expiry(item.expiresAt) }}</span></button>
          <button class="btn btn-secondary btn-xs" :disabled="locked" @click="removeWorkflow(item.id)">{{ t('workflow.delete') }}</button>
        </div>
      </div>
    </aside>
    <main class="workflow-main">
      <header class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-200 pb-3 dark:border-dark-600"><h2>{{ t('workflow.title') }}</h2><div class="flex gap-2"><button class="btn btn-secondary" :disabled="locked" @click="save">{{ t('workflow.save') }}</button><button class="btn btn-secondary" :disabled="steps.length >= 50 || locked" @click="addStep">{{ t('workflow.addStep') }}</button></div></header>
      <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('workflow.retention') }}</p>
      <p v-if="steps.length >= 50" class="mt-2 text-sm text-amber-600">{{ t('workflow.limit') }}</p>
      <p v-if="!hasBalance" class="mt-2 text-sm text-amber-600">{{ t('workflow.insufficientBalance') }}</p>
      <p v-if="saveError || error" class="mt-2 text-sm text-red-500">{{ saveError || error }}</p>
      <div class="workflow-scroll">
        <article v-for="(step, index) in steps" :key="step.id" class="workflow-step" :class="{ current: index === currentStep }">
          <div class="flex items-center justify-between gap-2"><strong>{{ t('workflow.step', { number: index + 1 }) }}</strong><div class="flex gap-1"><button v-if="index !== currentStep" class="btn btn-secondary btn-xs" :disabled="locked" @click="currentStep = index">{{ t('workflow.goTo') }}</button><button class="btn btn-secondary btn-xs" :disabled="steps.length === 1 || locked" @click="removeStep(index)">{{ t('workflow.remove') }}</button></div></div>
          <textarea v-model="step.prompt" class="input mt-2 w-full min-h-24" :disabled="locked" :maxlength="16000" @input="editStep(index)" />
          <p v-if="promptTooLong(step.prompt)" class="mt-1 text-sm text-red-500">{{ t('workflow.promptTooLong') }}</p>
          <div v-if="index === currentStep" class="mt-2 flex flex-wrap gap-2"><button class="btn btn-primary" :disabled="locked || !canRun(step)" @click="runCurrent()">{{ result(step.id) ? t('workflow.regenerate') : t('workflow.run') }}</button><button v-if="result(step.id) && index < steps.length - 1" class="btn btn-secondary" :disabled="locked" @click="next">{{ t('workflow.next') }}</button><button v-if="busy" class="btn btn-secondary" @click="controller?.abort()">{{ t('workflow.stop') }}</button></div>
          <div v-if="index === currentStep && result(step.id)" class="mt-3"><textarea v-model="guidance" class="input w-full min-h-20" :disabled="locked" :placeholder="t('workflow.revisionPlaceholder')" :maxlength="8000" /><button class="btn btn-secondary mt-2" :disabled="locked || !guidance.trim()" @click="regenerate">{{ t('workflow.revise') }}</button></div>
          <article v-if="result(step.id)" class="mt-3 whitespace-pre-wrap text-sm">{{ result(step.id)?.content }}</article>
          <figure v-if="images[step.id]" class="mt-3"><img v-if="!expired(step.id)" :src="images[step.id].url" class="max-h-96 max-w-full object-contain" /><p v-else class="text-sm text-amber-600">{{ t('workflow.imageExpired') }}</p><button v-if="!expired(step.id)" class="btn btn-secondary mt-2" @click="download(step.id)">{{ t('workflow.download') }}</button></figure><p v-else-if="result(step.id)?.kind === 'image'" class="mt-2 text-sm text-amber-600">{{ t('workflow.imageExpired') }}</p>
        </article>
      </div>
    </main>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Group } from '@/types'
import { useAuthStore } from '@/stores'
import { playgroundAPI, type PlaygroundMessage, type PlaygroundMultimodalMessage } from '@/api/playground'
import { playgroundHistory, type Conversation, type ConversationMessage, type WorkflowStep } from '@/api/playgroundHistory'
import { createPlaygroundId } from '@/utils/playgroundId'
import { resolvePlaygroundIntent } from '@/utils/playgroundIntent'

const props = defineProps<{ groups: Group[]; groupId: number; chatModel: string; imageModel: string }>()
const emit = defineEmits<{ presets: [value: { groupId: number; chatModel: string; imageModel: string }] }>()
const { t } = useI18n()
const auth = useAuthStore()
type Result = { content: string; model: string; kind: 'chat' | 'image' }
type Image = { url: string; expiresAt: number }
const history = ref<Conversation[]>([])
const id = ref(createPlaygroundId())
const title = ref('')
const steps = ref<WorkflowStep[]>([{ id: createPlaygroundId(), prompt: '' }])
const currentStep = ref(0)
const results = ref<Record<string, Result>>({})
const images = ref<Record<string, Image>>({})
const revision = ref(0)
const expiresAt = ref(0)
const presets = ref({ groupId: props.groupId, chatModel: props.chatModel, imageModel: props.imageModel })
const busy = ref(false)
const pendingSaves = ref(0)
const error = ref('')
const saveError = ref('')
const guidance = ref('')
let controller: AbortController | null = null
let saveQueue: Promise<boolean> = Promise.resolve(true)
let messageSequence = 0
let ticker: ReturnType<typeof setInterval> | undefined

const current = computed(() => steps.value[currentStep.value])
const locked = computed(() => busy.value || pendingSaves.value > 0)
const hasBalance = computed(() => Number(auth.user?.balance ?? 0) > 0)
function messageID() {
  messageSequence += 1
  return Date.now() * 100 + (messageSequence % 100)
}

function result(stepID: string) { return results.value[stepID] }
function expired(stepID: string) { return (images.value[stepID]?.expiresAt ?? 0) <= Date.now() }
function promptTooLong(prompt: string) { return new TextEncoder().encode(prompt).length > 16000 }
function expiry(value: number) { return value ? t('workflow.expires', { time: new Date(value).toLocaleString() }) : '' }
function selectedGroupIsValid() {
  return props.groups.some(group => group.id === presets.value.groupId)
}

function canRun(step: WorkflowStep) {
  return Boolean(step.prompt.trim() && !promptTooLong(step.prompt) && selectedGroupIsValid() && hasBalance.value)
}
function cloneSteps(value: WorkflowStep[]) {
  return value.map(step => ({ id: step.id, prompt: step.prompt }))
}

function draftMessages(): ConversationMessage[] {
  return steps.value.flatMap(step => {
    const item = result(step.id)
    return item ? [{ id: messageID(), role: 'assistant' as const, content: item.content, model: item.model, kind: item.kind, stepId: step.id }] : []
  })
}

function snapshot(): Conversation {
  return {
    id: id.value,
    title: title.value || steps.value[0]?.prompt.slice(0, 60) || '',
    groupId: presets.value.groupId,
    model: presets.value.chatModel,
    imageModel: presets.value.imageModel,
    kind: 'workflow',
    workflow: { steps: cloneSteps(steps.value), currentStep: currentStep.value },
    systemPrompt: '',
    temperature: 0.7,
    messages: draftMessages(),
    revision: revision.value,
    expiresAt: expiresAt.value
  }
}
async function save() {
  pendingSaves.value += 1
  const task = saveQueue.then(async () => {
    const value = snapshot()
    try {
      const stored = await playgroundHistory.save(value)
      if (id.value === stored.id) {
        revision.value = stored.revision
        expiresAt.value = stored.expiresAt
      }
      history.value = [stored, ...history.value.filter(item => item.id !== stored.id)]
      saveError.value = ''
      return true
    } catch (caught) {
      saveError.value = caught instanceof Error ? caught.message : t('workflow.saveFailed')
      return false
    } finally {
      pendingSaves.value -= 1
    }
  })
  saveQueue = task.catch(() => false)
  return task
}
async function load() {
  try {
    history.value = (await playgroundHistory.list()).filter(item => item.kind === 'workflow').sort((a, b) => b.expiresAt - a.expiresAt)
  } catch {
    saveError.value = t('workflow.loadFailed')
  }
}

function newWorkflow() {
  if (locked.value) return
  id.value = createPlaygroundId()
  title.value = ''
  steps.value = [{ id: createPlaygroundId(), prompt: '' }]
  currentStep.value = 0
  results.value = {}
  images.value = {}
  revision.value = 0
  expiresAt.value = 0
  guidance.value = ''
  error.value = ''
  saveError.value = ''
  presets.value = { groupId: props.groupId, chatModel: props.chatModel, imageModel: props.imageModel }
}
function openWorkflow(item: Conversation) { if (locked.value || item.kind !== 'workflow' || !item.workflow) return; id.value = item.id; title.value = item.title; steps.value = cloneSteps(item.workflow.steps); currentStep.value = item.workflow.currentStep; results.value = Object.fromEntries(item.messages.filter(message => message.stepId).map(message => [message.stepId!, { content: message.content, model: message.model || item.model, kind: message.kind || 'chat' }])); images.value = {}; revision.value = item.revision; expiresAt.value = item.expiresAt; presets.value = { groupId: item.groupId, chatModel: item.model, imageModel: item.imageModel || '' }; emit('presets', presets.value); guidance.value = ''; error.value = '' }
async function removeWorkflow(value: string) { if (locked.value) return; try { await playgroundHistory.delete(value); history.value = history.value.filter(item => item.id !== value); if (id.value === value) newWorkflow() } catch { saveError.value = t('workflow.deleteFailed') } }
function addStep() { if (steps.value.length < 50) steps.value.push({ id: createPlaygroundId(), prompt: '' }) }
function removeStep(index: number) {
  if (steps.value.length <= 1 || locked.value) return
  const [step] = steps.value.splice(index, 1)
  delete results.value[step.id]
  delete images.value[step.id]
  currentStep.value = Math.min(currentStep.value, steps.value.length - 1)
  invalidateFrom(index - 1)
}

function invalidateFrom(index: number) {
  for (const step of steps.value.slice(index + 1)) {
    delete results.value[step.id]
    delete images.value[step.id]
  }
  currentStep.value = Math.min(currentStep.value, Math.max(0, index))
}
function editStep(index: number) { if (result(steps.value[index].id)) { delete results.value[steps.value[index].id]; delete images.value[steps.value[index].id]; invalidateFrom(index) } }
function priorContext(): Array<PlaygroundMessage | PlaygroundMultimodalMessage> | null { const output: Array<PlaygroundMessage | PlaygroundMultimodalMessage> = []; for (const step of steps.value.slice(0, currentStep.value)) { const prior = result(step.id); if (!prior) return null; const image = images.value[step.id]; if (prior.kind === 'image') { if (!image || expired(step.id)) return null; output.push({ role: 'user', content: [{ type: 'text', text: prior.content }, { type: 'image_url', image_url: { url: image.url } }] }) } else output.push({ role: 'assistant', content: prior.content }) }; return output }
async function runCurrent(instruction = ''): Promise<boolean> {
  if (locked.value) return false
  const step = current.value
  if (expiresAt.value && expiresAt.value <= Date.now()) {
    error.value = t('workflow.expired')
    return false
  }
  if (!step || !canRun(step) || busy.value) {
    error.value = !hasBalance.value ? t('workflow.insufficientBalance') : t('workflow.invalidGroup')
    return false
  }
  const context = priorContext()
  if (!context) {
    error.value = t('workflow.regenerateDependency')
    return false
  }
  const previous = currentStep.value ? result(steps.value[currentStep.value - 1].id)?.kind : undefined
  const intent = resolvePlaygroundIntent(step.prompt, previous)
  const model = intent === 'image' ? presets.value.imageModel : presets.value.chatModel
  const groupId = presets.value.groupId
  if (!model) {
    error.value = t('workflow.noModel')
    return false
  }
  const old = result(step.id)?.content || ''
  const prompt = instruction ? `${step.prompt}\n\nPrevious result:\n${old}\n\nRevision guidance:\n${instruction}` : step.prompt
  busy.value = true
  error.value = ''
  controller = new AbortController()
  try {
    if (intent === 'image') {
      const text = [...context.map(message => typeof message.content === 'string'
        ? message.content
        : message.content.filter(part => part.type === 'text').map(part => part.text).join('\n')), prompt].join('\n\n')
      const generated = await playgroundAPI.generateImages({ groupId, model, prompt: text, size: '1024x1024', quality: 'auto', count: 1, signal: controller.signal })
      if (controller.signal.aborted) return false
      if (!generated[0]) throw new Error(t('workflow.imageFailed'))
      results.value[step.id] = { content: generated[0].revisedPrompt || text, model, kind: 'image' }
      images.value[step.id] = { url: generated[0].url, expiresAt: Date.now() + 600000 }
    } else {
      let content = ''
      await playgroundAPI.streamChat({ groupId, model, messages: [...context, { role: 'user', content: prompt }], signal: controller.signal, onDelta: delta => { content += delta } })
      if (controller.signal.aborted) return false
      if (!content.trim()) throw new Error(t('workflow.emptyResult'))
      results.value[step.id] = { content, model, kind: 'chat' }
      delete images.value[step.id]
    }
    invalidateFrom(currentStep.value)
    await save()
    await auth.refreshUser().catch(() => {})
    return true
  } catch (caught) {
    if ((caught as Error).name !== 'AbortError') error.value = caught instanceof Error ? caught.message : t('workflow.failed')
    return false
  } finally {
    busy.value = false
    controller = null
  }
}
async function next() {
  if (locked.value || !current.value || !result(current.value.id) || currentStep.value >= steps.value.length - 1) return
  const previous = currentStep.value
  currentStep.value += 1
  if (!await runCurrent()) currentStep.value = previous
}

async function regenerate() {
  const value = guidance.value.trim()
  if (!value || locked.value) return
  if (await runCurrent(value)) guidance.value = ''
}
async function download(stepID: string) { const image = images.value[stepID]; if (!image || expired(stepID)) return; try { const response = await fetch(image.url); if (!response.ok) throw new Error(); const blob = await response.blob(); const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = `workflow-${stepID}.png`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) } catch { error.value = t('workflow.downloadFailed') } }
onMounted(() => { load(); ticker = setInterval(() => { for (const key of Object.keys(images.value)) if (expired(key)) delete images.value[key] }, 1000) })
watch(
  () => [props.groupId, props.chatModel, props.imageModel, locked.value] as const,
  ([groupId, chatModel, imageModel, isLocked]) => {
    if (!isLocked) presets.value = { groupId, chatModel, imageModel }
  }
)
onBeforeUnmount(() => { controller?.abort(); if (ticker) clearInterval(ticker) })
</script>

<style scoped>
.workflow-shell {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  flex: 1;
  min-height: 0;
  min-width: 0;
  border: 1px solid #d1d5db;
  background: #fff;
}
.workflow-history, .workflow-main, .workflow-scroll { min-height: 0; }
.workflow-history { display: flex; flex-direction: column; padding: 12px; border-right: 1px solid #d1d5db; overflow: hidden; }
.workflow-history > div { min-height: 0; }
.workflow-main { display: flex; min-width: 0; flex-direction: column; padding: 16px; }
.workflow-scroll { flex: 1; overflow: auto; padding-top: 12px; }
.workflow-step { margin-bottom: 12px; padding: 12px; border: 1px solid #d1d5db; border-radius: 6px; }
.workflow-step.current { border-color: #3b82f6; }
:global(html.dark .workflow-shell) { border-color: #475569; background: #172033; color: #e5e7eb; }
:global(html.dark .workflow-history) { border-color: #475569; }
:global(html.dark .workflow-step) { border-color: #475569; background: #1e293b; }
@media (max-width: 768px) {
  .workflow-shell { grid-template-columns: 1fr; grid-template-rows: auto minmax(0, 1fr); }
  .workflow-history { max-height: 180px; border-right: 0; border-bottom: 1px solid #d1d5db; }
  .workflow-main { min-height: 0; padding: 12px; }
}
</style>
