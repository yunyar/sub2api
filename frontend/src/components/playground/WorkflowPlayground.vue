<template>
  <section class="workflow-shell">
    <aside class="workflow-history">
      <button class="btn btn-primary w-full" @click="newWorkflow">{{ t('workflow.new') }}</button>
      <button class="btn btn-secondary mt-2 w-full" @click="load">{{ t('workflow.refresh') }}</button>
      <p class="mt-4 text-xs text-gray-500 dark:text-dark-400">{{ t('workflow.history') }}</p>
      <div class="mt-2 space-y-1 overflow-y-auto">
        <div v-for="item in history" :key="item.id" class="flex gap-1 rounded border border-gray-200 p-2 dark:border-dark-600">
          <button class="min-w-0 flex-1 text-left" @click="openWorkflow(item)"><span class="block truncate text-sm">{{ item.title || t('workflow.untitled') }}</span><span class="text-xs text-gray-500 dark:text-dark-400">{{ expiry(item.expiresAt) }}</span></button>
          <button class="btn btn-secondary btn-xs" @click="removeWorkflow(item.id)">{{ t('workflow.delete') }}</button>
        </div>
      </div>
    </aside>
    <main class="workflow-main">
      <header class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-200 pb-3 dark:border-dark-600"><h2>{{ t('workflow.title') }}</h2><div class="flex flex-wrap items-center gap-2"><label class="text-xs text-gray-500 dark:text-dark-400">{{ t('playground.modeLabel') }}<select v-model="imageMode" class="input ml-2"><option value="auto">{{ t('playground.modeAuto') }}</option><option value="chat">{{ t('playground.modeChat') }}</option><option value="image">{{ t('playground.modeImage') }}</option></select></label><button class="btn btn-secondary" @click="save()">{{ t('workflow.save') }}</button><button class="btn btn-secondary" :disabled="steps.length >= 50" @click="addStep">{{ t('workflow.addStep') }}</button></div></header>
      <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('workflow.retention') }}</p>
      <p v-if="steps.length >= 50" class="mt-2 text-sm text-amber-600">{{ t('workflow.limit') }}</p>
      <p v-if="!hasBalance" class="mt-2 text-sm text-amber-600">{{ t('workflow.insufficientBalance') }}</p>
      <p v-if="saveError || cacheError || error" class="mt-2 text-sm text-red-500">{{ saveError || cacheError || error }}</p>
      <div class="workflow-scroll">
        <article v-for="(step, index) in steps" :key="step.id" class="workflow-step" :class="{ current: index === currentStep }">
          <div class="flex items-center justify-between gap-2"><strong>{{ t('workflow.step', { number: index + 1 }) }}</strong><div class="flex gap-1"><button v-if="index !== currentStep" class="btn btn-secondary btn-xs" @click="currentStep = index">{{ t('workflow.goTo') }}</button><button class="btn btn-secondary btn-xs" :disabled="steps.length === 1" @click="removeStep(index)">{{ t('workflow.remove') }}</button></div></div>
          <textarea v-model="step.prompt" class="input mt-2 w-full min-h-24" :disabled="isRunning(step.id)" :maxlength="16000" @input="editStep(index)" />
          <div v-if="index === currentStep" class="workflow-attachments">
            <div v-for="attachment in attachments" :key="attachment.id" class="workflow-attachment">
              <img v-if="attachment.kind === 'image'" :src="attachment.dataUrl" :alt="attachment.name" class="workflow-attachment-thumb" />
              <span v-else class="workflow-attachment-icon">TXT</span>
              <span class="min-w-0 flex-1 truncate">{{ attachment.name }}</span>
              <button type="button" class="workflow-attachment-remove" :aria-label="t('playground.removeAttachment', { name: attachment.name })" @click="emit('removeAttachment', attachment.id)">×</button>
            </div>
            <button type="button" class="btn btn-secondary btn-xs" :disabled="attachmentsLoading" @click="emit('requestAttachments')">{{ t('playground.attachFiles') }}</button>
          </div>
          <p v-if="promptTooLong(step.prompt)" class="mt-1 text-sm text-red-500">{{ t('workflow.promptTooLong') }}</p>
          <div v-if="index === currentStep" class="mt-2 flex flex-wrap items-center gap-2"><input v-if="isImageStep(step)" v-model.number="imageCount" class="input w-20" type="number" min="1" max="10" :disabled="isRunning(step.id)" :aria-label="t('workflow.imageCount')" /><button class="btn btn-primary" :disabled="!canRun(step) || isRunning(step.id)" @click="runCurrent()">{{ result(step.id) ? t('workflow.regenerate') : t('workflow.run') }}</button><button v-if="result(step.id) && index < steps.length - 1" class="btn btn-secondary" @click="next">{{ t('workflow.next') }}</button><button v-if="isRunning(step.id)" class="btn btn-secondary" @click="stopStep(step.id)">{{ t('workflow.stop') }}</button></div>
          <div v-if="index === currentStep && result(step.id)" class="mt-3"><textarea v-model="guidance" class="input w-full min-h-20" :disabled="isRunning(step.id)" :placeholder="t('workflow.revisionPlaceholder')" :maxlength="8000" /><button class="btn btn-secondary mt-2" :disabled="isRunning(step.id) || !guidance.trim()" @click="regenerate">{{ t('workflow.revise') }}</button></div>
          <article v-if="result(step.id)" class="mt-3 whitespace-pre-wrap text-sm">{{ result(step.id)?.kind === 'image' ? t('playground.imagesGenerated', { count: images[step.id]?.length || 0 }) : result(step.id)?.content }}</article>
          <div v-if="images[step.id]?.length" class="mt-3 space-y-3"><figure v-for="(image, imageIndex) in images[step.id]" :key="image.id"><img v-if="!expired(image)" :src="image.url" class="max-h-96 max-w-full object-contain" /><p v-else class="text-sm text-amber-600">{{ t('workflow.imageExpired') }}</p><button v-if="!expired(image)" class="btn btn-secondary mt-2" @click="download(image, imageIndex)">{{ t('workflow.download') }}</button></figure></div><p v-else-if="result(step.id)?.kind === 'image'" class="mt-2 text-sm text-amber-600">{{ t('workflow.imageExpired') }}</p>
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
import { PlaygroundImageGenerationError, playgroundAPI, type PlaygroundImage, type PlaygroundMessage, type PlaygroundMultimodalMessage } from '@/api/playground'
import { playgroundHistory, type Conversation, type ConversationMessage, type WorkflowStep } from '@/api/playgroundHistory'
import { createPlaygroundId } from '@/utils/playgroundId'
import { resolvePlaygroundIntent, type PlaygroundMode } from '@/utils/playgroundIntent'
import { resolvePlaygroundImageCount } from '@/utils/playgroundImageCount'
import { playgroundImageBlob, playgroundImageCache, type CachedPlaygroundImage } from '@/utils/playgroundImageCache'

type WorkflowAttachment = { id: string; name: string; kind: 'text' | 'image'; text?: string; dataUrl?: string; size: number }
const props = defineProps<{ groups: Group[]; groupId: number; chatModel: string; imageModel: string; imageMode?: PlaygroundMode; attachments?: WorkflowAttachment[]; attachmentsLoading?: boolean }>()
const emit = defineEmits<{
  presets: [value: { groupId: number; chatModel: string; imageModel: string }]
  mode: [value: PlaygroundMode]
  requestAttachments: []
  removeAttachment: [id: string]
  consumeAttachments: [ids: string[]]
}>()
const { t } = useI18n()
const auth = useAuthStore()
type Result = { content: string; model: string; kind: 'chat' | 'image' }
type Image = { id: string; url: string; expiresAt: number; prompt: string; persistent?: boolean }
type WorkflowContext = { accountId: string; conversationId: string; epoch: number }
type StepRequest = WorkflowContext & { stepId: string; stepIndex: number; controller: AbortController }
type PriorStep = { prompt: string; result: Result; images: Image[] }
const history = ref<Conversation[]>([])
const id = ref(createPlaygroundId())
const title = ref('')
const steps = ref<WorkflowStep[]>([{ id: createPlaygroundId(), prompt: '' }])
const currentStep = ref(0)
const results = ref<Record<string, Result>>({})
const images = ref<Record<string, Image[]>>({})
const revision = ref(0)
const expiresAt = ref(0)
const presets = ref({ groupId: props.groupId, chatModel: props.chatModel, imageModel: props.imageModel })
const running = ref<Record<string, boolean>>({})
const pendingSaves = ref(0)
const error = ref('')
const saveError = ref('')
const cacheError = ref('')
const guidance = ref('')
const imageCount = ref(1)
const imageMode = ref<PlaygroundMode>(props.imageMode ?? 'chat')
const requests = new Map<string, StepRequest>()
let saveQueue: Promise<boolean> = Promise.resolve(true)
const savedRevisions = new Map<string, number>()
let epoch = 0
let loadSequence = 0
let messageSequence = 0
let ticker: ReturnType<typeof setInterval> | undefined
let disposed = false

const current = computed(() => steps.value[currentStep.value])
const hasBalance = computed(() => Number(auth.user?.balance ?? 0) > 0)
function messageID() {
  messageSequence += 1
  return Date.now() * 100 + (messageSequence % 100)
}

function result(stepID: string) { return results.value[stepID] }
function isRunning(stepID: string) { return Boolean(running.value[stepID]) }
function stopStep(stepID: string) { requests.get(stepID)?.controller.abort() }
function workflowContext(): WorkflowContext { return { accountId: accountID(), conversationId: id.value, epoch } }
function isCurrentContext(context: WorkflowContext) {
  return !disposed && context.epoch === epoch && context.accountId === accountID() && context.conversationId === id.value
}
function isCurrentRequest(request: StepRequest) {
  return isCurrentContext(request) && requests.get(request.stepId) === request && !request.controller.signal.aborted && steps.value.some(step => step.id === request.stepId)
}
function resetRequests() {
  epoch += 1
  for (const request of requests.values()) request.controller.abort()
  requests.clear()
  running.value = {}
  pendingSaves.value = 0
}
function expired(image: Image) { return !image.persistent && image.expiresAt <= Date.now() }
function promptTooLong(prompt: string) { return new TextEncoder().encode(prompt).length > 16000 }
function expiry(value: number) { return value ? t('workflow.expires', { time: new Date(value).toLocaleString() }) : '' }
function selectedGroupIsValid() {
  return props.groups.some(group => group.id === presets.value.groupId)
}

function canRun(step: WorkflowStep) {
  return Boolean((step.prompt.trim() || props.attachments?.length) && !promptTooLong(step.prompt) && selectedGroupIsValid() && hasBalance.value && !props.attachmentsLoading)
}
function isImageStep(step: WorkflowStep) {
  const previous = currentStep.value ? result(steps.value[currentStep.value - 1].id)?.kind : undefined
  return resolvePlaygroundIntent(step.prompt, previous, imageMode.value) === 'image'
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
function clearImages() {
  for (const entries of Object.values(images.value)) for (const image of entries) if (image.url.startsWith('blob:')) URL.revokeObjectURL(image.url)
  images.value = {}
}
function accountID() { return String(auth.user?.id ?? '') }
function revisionKey(context: WorkflowContext) { return `${context.accountId}:${context.conversationId}` }
async function cacheImage(request: StepRequest, prompt: string, image: PlaygroundImage): Promise<Image | null> {
  const expiresAt = Date.now() + 600000
  if (!isCurrentRequest(request)) return null
  if (!request.accountId) return { id: createPlaygroundId(), url: image.url, expiresAt, prompt }
  try {
    const blob = await playgroundImageBlob(image.url)
    if (!isCurrentRequest(request)) return null
    const entry: CachedPlaygroundImage = { id: createPlaygroundId(), accountId: request.accountId, conversationId: request.conversationId, messageId: messageID(), stepId: request.stepId, prompt, expiresAt, blob }
    await playgroundImageCache.put(entry)
    if (!isCurrentRequest(request)) return null
    cacheError.value = ''
    return { id: entry.id, url: URL.createObjectURL(blob), expiresAt, prompt, persistent: true }
  } catch {
    if (!isCurrentRequest(request)) return null
    cacheError.value = t('workflow.imageCacheFailed')
    return { id: createPlaygroundId(), url: image.url, expiresAt, prompt }
  }
}
async function deleteCachedStep(stepID: string, context = workflowContext()) {
  if (!isCurrentContext(context)) return false
  if (!context.accountId) return true
  try {
    await playgroundImageCache.deleteStep(context.accountId, context.conversationId, stepID)
    return isCurrentContext(context)
  } catch {
    if (isCurrentContext(context)) cacheError.value = t('workflow.imageCacheFailed')
    return false
  }
}
function clearStepImages(stepID: string, clearCache = true) {
  for (const image of images.value[stepID] || []) if (image.url.startsWith('blob:')) URL.revokeObjectURL(image.url)
  delete images.value[stepID]
  if (clearCache) void deleteCachedStep(stepID)
}
async function restoreImages() {
  clearImages()
  const context = workflowContext()
  if (!context.accountId) return
  try {
    const cached = await playgroundImageCache.listConversation(context.accountId, context.conversationId)
    if (!isCurrentContext(context)) return
    images.value = cached.reduce<Record<string, Image[]>>((output, image) => {
      if (!image.stepId || isRunning(image.stepId)) return output
      const entry = { id: image.id, url: URL.createObjectURL(image.blob), expiresAt: image.expiresAt ?? 0, prompt: image.prompt, persistent: true }
      output[image.stepId] = [...(output[image.stepId] || []), entry]
      return output
    }, {})
    cacheError.value = ''
  } catch { if (isCurrentContext(context)) cacheError.value = t('workflow.imageCacheFailed') }
}
async function save(value = snapshot(), context = workflowContext(), request?: StepRequest) {
  pendingSaves.value += 1
  const task = saveQueue.then(async () => {
    try {
      if (!isCurrentContext(context) || (request && !isCurrentRequest(request))) return false
      const key = revisionKey(context)
      value.revision = savedRevisions.get(key) ?? value.revision
      const stored = await playgroundHistory.save(value)
      savedRevisions.set(key, Math.max(savedRevisions.get(key) ?? 0, stored.revision))
      if (context.accountId === accountID() && !disposed) {
        history.value = [stored, ...history.value.filter(item => item.id !== stored.id)]
      }
      if (isCurrentContext(context) && id.value === stored.id) {
        revision.value = stored.revision
        expiresAt.value = stored.expiresAt
      }
      if (isCurrentContext(context)) saveError.value = ''
      return true
    } catch (caught) {
      if (isCurrentContext(context)) saveError.value = caught instanceof Error ? caught.message : t('workflow.saveFailed')
      return false
    } finally {
      if (isCurrentContext(context)) pendingSaves.value -= 1
    }
  })
  saveQueue = task.catch(() => false)
  return task
}
async function load() {
  const context = workflowContext()
  const sequence = ++loadSequence
  try {
    const items = await playgroundHistory.list()
    if (isCurrentContext(context) && sequence === loadSequence) history.value = items.filter(item => item.kind === 'workflow').sort((a, b) => b.expiresAt - a.expiresAt)
  } catch {
    if (isCurrentContext(context) && sequence === loadSequence) saveError.value = t('workflow.loadFailed')
  }
}

function newWorkflow() {
  resetWorkflow()
}
function resetWorkflow() {
  resetRequests()
  id.value = createPlaygroundId()
  savedRevisions.set(revisionKey(workflowContext()), 0)
  title.value = ''
  steps.value = [{ id: createPlaygroundId(), prompt: '' }]
  currentStep.value = 0
  results.value = {}
  clearImages()
  revision.value = 0
  expiresAt.value = 0
  guidance.value = ''
  imageCount.value = 1
  error.value = ''
  saveError.value = ''
  cacheError.value = ''
  presets.value = { groupId: props.groupId, chatModel: props.chatModel, imageModel: props.imageModel }
}
async function openWorkflow(item: Conversation) { if (item.kind !== 'workflow' || !item.workflow) return; resetRequests(); id.value = item.id; title.value = item.title; steps.value = cloneSteps(item.workflow.steps); currentStep.value = item.workflow.currentStep; results.value = Object.fromEntries(item.messages.filter(message => message.role === 'assistant' && message.stepId).map(message => [message.stepId!, { content: message.content, model: message.model || item.model, kind: message.kind || 'chat' }])); const context = workflowContext(); revision.value = Math.max(item.revision, savedRevisions.get(revisionKey(context)) ?? item.revision); savedRevisions.set(revisionKey(context), revision.value); expiresAt.value = item.expiresAt; presets.value = { groupId: item.groupId, chatModel: item.model, imageModel: item.imageModel || '' }; emit('presets', presets.value); guidance.value = ''; error.value = ''; saveError.value = ''; cacheError.value = ''; imageCount.value = 1; await restoreImages() }
async function removeWorkflow(value: string) { const context = workflowContext(); try { await playgroundHistory.delete(value); if (!isCurrentContext(context)) return; history.value = history.value.filter(item => item.id !== value); if (id.value === value) newWorkflow() } catch { if (isCurrentContext(context)) saveError.value = t('workflow.deleteFailed') } }
function addStep() { if (steps.value.length < 50) steps.value.push({ id: createPlaygroundId(), prompt: '' }) }
function removeStep(index: number) {
  if (steps.value.length <= 1) return
  const [step] = steps.value.splice(index, 1)
  stopStep(step.id)
  requests.delete(step.id)
  delete running.value[step.id]
  delete results.value[step.id]
  clearStepImages(step.id)
  currentStep.value = index < currentStep.value ? currentStep.value - 1 : Math.min(currentStep.value, steps.value.length - 1)
}

function editStep(index: number) { const step = steps.value[index]; if (step && !isRunning(step.id) && result(step.id)) { delete results.value[step.id]; clearStepImages(step.id) } }
async function upstreamImageURL(image: Image): Promise<string | null> {
  if (!image.url.startsWith('blob:')) return image.url
  try {
    const blob = await (await fetch(image.url)).blob()
    return await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => typeof reader.result === 'string' ? resolve(reader.result) : reject(new Error('image conversion failed'))
      reader.onerror = () => reject(reader.error || new Error('image conversion failed'))
      reader.readAsDataURL(blob)
    })
  } catch { return null }
}
async function priorContextFor(priors: PriorStep[]): Promise<Array<PlaygroundMessage | PlaygroundMultimodalMessage> | null> {
  const output: Array<PlaygroundMessage | PlaygroundMultimodalMessage> = []
  for (const step of priors) {
    const prior = step.result
    const stepImages = step.images
    if (prior.kind === 'image') {
      const urls = await Promise.all(stepImages.map(upstreamImageURL))
      const availableURLs = urls.filter((url): url is string => Boolean(url))
      output.push(availableURLs.length
        ? {
            role: 'user',
            content: [{ type: 'text', text: `${step.prompt}\n\n${prior.content}` }, ...availableURLs.map(url => ({ type: 'image_url' as const, image_url: { url } }))]
          }
        : { role: 'user', content: `${step.prompt}\n\n${prior.content}` })
    } else {
      output.push({ role: 'user', content: step.prompt }, { role: 'assistant', content: prior.content })
    }
  }
  return output
}
async function runCurrent(instruction = ''): Promise<boolean> {
  const step = current.value
  if (!step || isRunning(step.id) || disposed) return false
  if (expiresAt.value && expiresAt.value <= Date.now()) {
    error.value = t('workflow.expired')
    return false
  }
  if (!canRun(step)) {
    error.value = !hasBalance.value ? t('workflow.insufficientBalance') : t('workflow.invalidGroup')
    return false
  }
  const stepIndex = currentStep.value
  const stepID = step.id
  const attachmentSnapshot = (props.attachments || []).map(attachment => ({ ...attachment }))
  const previous = stepIndex ? result(steps.value[stepIndex - 1].id)?.kind : undefined
  const intent = resolvePlaygroundIntent(step.prompt, previous, imageMode.value)
  const model = intent === 'image' ? presets.value.imageModel : presets.value.chatModel
  const groupId = presets.value.groupId
  if (!model) {
    error.value = t('workflow.noModel')
    return false
  }
  const count = intent === 'image' ? resolvePlaygroundImageCount('', imageCount.value) : null
  if (count?.error) {
    error.value = t(count.error === 'too_many' ? 'workflow.imageCountTooMany' : 'workflow.imageCountInvalid')
    return false
  }
  const old = result(stepID)?.content || ''
  const attachmentNotes = attachmentSnapshot
    .filter((attachment): attachment is WorkflowAttachment & { text: string } => attachment.kind === 'text' && typeof attachment.text === 'string')
    .map(attachment => `\n\n--- ${attachment.name} ---\n${attachment.text}`)
  const imageNotes = attachmentSnapshot.filter(attachment => attachment.kind === 'image').map(attachment => `\n\n[Image: ${attachment.name}]`)
  const stepPrompt = `${step.prompt || (attachmentSnapshot.length ? t('playground.attachmentOnlyPrompt') : '')}${attachmentNotes.join('')}${imageNotes.join('')}`
  const prompt = instruction ? `${stepPrompt}\n\nPrevious result:\n${old}\n\nRevision guidance:\n${instruction}` : stepPrompt
  if (promptTooLong(prompt)) {
    error.value = t('playground.attachmentTooMuchText')
    return false
  }
  const priors = steps.value.slice(0, stepIndex).flatMap(priorStep => {
    const prior = result(priorStep.id)
    return prior ? [{ prompt: priorStep.prompt, result: { ...prior }, images: (images.value[priorStep.id] || []).filter(image => !expired(image)).map(image => ({ ...image })) }] : []
  })
  const request: StepRequest = { ...workflowContext(), stepId: stepID, stepIndex, controller: new AbortController() }
  const draft = snapshot()
  draft.workflow!.steps[request.stepIndex].prompt = stepPrompt
  requests.set(stepID, request)
  running.value[stepID] = true
  try {
    const context = await priorContextFor(priors)
    if (!isCurrentRequest(request)) return false
    if (!context) {
      error.value = t('workflow.regenerateDependency')
      return false
    }
    if (!await save(draft, request, request) || !isCurrentRequest(request)) return false
    const activeStep = steps.value.find(item => item.id === stepID)!
    activeStep.prompt = stepPrompt
    error.value = ''
    if (attachmentSnapshot.length) emit('consumeAttachments', attachmentSnapshot.map(attachment => attachment.id))
    const imageAttachments = attachmentSnapshot.filter((attachment): attachment is WorkflowAttachment & { dataUrl: string } => attachment.kind === 'image' && Boolean(attachment.dataUrl))
    if (intent === 'image') {
      const text = [...context.map(message => typeof message.content === 'string'
        ? message.content
        : message.content.filter(part => part.type === 'text').map(part => part.text).join('\n')), prompt].join('\n\n')
      let replacedImages = false
      let cacheCleared = false
      const deliveredImageURLs = new Set<string>()
      let imageQueue = Promise.resolve()
      const saveImage = (image: PlaygroundImage) => {
        if (!isCurrentRequest(request) || deliveredImageURLs.has(image.url)) return imageQueue
        deliveredImageURLs.add(image.url)
        imageQueue = imageQueue.then(async () => {
          if (!isCurrentRequest(request)) return
          if (!replacedImages) {
            cacheCleared = await deleteCachedStep(stepID, request)
            if (!isCurrentRequest(request)) return
            clearStepImages(stepID, false)
            replacedImages = true
          }
          const nextImage = cacheCleared
            ? await cacheImage(request, text, image)
            : { id: createPlaygroundId(), url: image.url, expiresAt: Date.now() + 600000, prompt: text }
          if (!isCurrentRequest(request) || !nextImage) return
          images.value[stepID] = [...(images.value[stepID] || []), nextImage]
          results.value[stepID] = { content: image.revisedPrompt || prompt, model, kind: 'image' }
        })
        return imageQueue
      }
      let generated: PlaygroundImage[]
      let generationError: unknown
      try {
        const inputImages = [...context.flatMap(message => typeof message.content === 'string' ? [] : message.content.flatMap(part => part.type === 'image_url' ? [part.image_url.url] : [])), ...imageAttachments.map(attachment => attachment.dataUrl)]
        generated = await playgroundAPI.generateImages({
          groupId,
          model,
          prompt: text,
          size: '1024x1024',
          quality: 'auto',
          count: count!.count,
          ...(inputImages.length ? { inputImages } : {}),
          signal: request.controller.signal,
          onImage: saveImage
        })
      } catch (caught) {
        if (caught instanceof PlaygroundImageGenerationError && caught.images.length) {
          generated = caught.images
          generationError = caught
        } else throw caught
      }
      await imageQueue
      if (!isCurrentRequest(request)) return false
      if (!generated[0]) throw new Error(t('workflow.imageFailed'))
      for (const image of generated) await saveImage(image)
      if (!isCurrentRequest(request)) return false
      results.value[stepID] = { content: generated[0].revisedPrompt || prompt, model, kind: 'image' }
      if (generationError) error.value = (generationError as PlaygroundImageGenerationError).status === 402
        ? t('workflow.insufficientBalance')
        : (generationError as Error).message
    } else {
      let content = ''
      const userMessage: PlaygroundMessage | PlaygroundMultimodalMessage = imageAttachments.length
        ? {
            role: 'user',
            content: [
              { type: 'text', text: prompt },
              ...imageAttachments.map(attachment => ({ type: 'image_url' as const, image_url: { url: attachment.dataUrl } }))
            ]
          }
        : { role: 'user', content: prompt }
      let replacedImages = false
      await playgroundAPI.streamChat({ groupId, model, messages: [...context, userMessage], signal: request.controller.signal, onDelta: delta => {
        if (!isCurrentRequest(request)) return
        content += delta
        if (!content.trim()) return
        results.value[stepID] = { content, model, kind: 'chat' }
        if (!replacedImages) { clearStepImages(stepID); replacedImages = true }
      } })
      if (!isCurrentRequest(request)) return false
      if (!content.trim()) throw new Error(t('workflow.emptyResult'))
      results.value[stepID] = { content, model, kind: 'chat' }
    }
    if (!await save(snapshot(), request, request) || !isCurrentRequest(request)) return false
    await auth.refreshUser().catch(() => {})
    return isCurrentRequest(request)
  } catch (caught) {
    if (isCurrentRequest(request) && (caught as Error | null)?.name !== 'AbortError') {
      error.value = (caught as { status?: number } | null)?.status === 402
        ? t('workflow.insufficientBalance')
        : caught instanceof Error ? caught.message : t('workflow.failed')
    }
    return false
  } finally {
    if (requests.get(stepID) === request) {
      requests.delete(stepID)
      delete running.value[stepID]
    }
  }
}
async function next() {
  if (!current.value || !result(current.value.id) || currentStep.value >= steps.value.length - 1) return
  const context = workflowContext()
  const previous = currentStep.value
  currentStep.value += 1
  const destination = currentStep.value
  const destinationID = current.value.id
  if (result(steps.value[previous].id)?.kind === 'image') imageMode.value = 'chat'
  if (isRunning(destinationID)) return
  if (!await runCurrent() && isCurrentContext(context) && currentStep.value === destination && current.value?.id === destinationID) currentStep.value = previous
}

async function regenerate() {
  const value = guidance.value.trim()
  const stepID = current.value?.id
  if (!value) return
  if (await runCurrent(value) && current.value?.id === stepID && guidance.value.trim() === value) guidance.value = ''
}
async function download(image: Image, index: number) { if (expired(image)) return; try { const response = await fetch(image.url); if (!response.ok) throw new Error(); const blob = await response.blob(); const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = `workflow-${current.value?.id || 'image'}-${index + 1}.png`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) } catch { error.value = t('workflow.downloadFailed') } }
onMounted(() => { load(); ticker = setInterval(() => { for (const [stepID, entries] of Object.entries(images.value)) { const active = entries.filter(image => !expired(image)); for (const image of entries) if (expired(image) && image.url.startsWith('blob:')) URL.revokeObjectURL(image.url); if (active.length) images.value[stepID] = active; else delete images.value[stepID] } }, 1000) })
watch(
  () => [props.groupId, props.chatModel, props.imageModel] as const,
  ([groupId, chatModel, imageModel]) => {
    presets.value = { groupId, chatModel, imageModel }
  }
)
watch(() => props.imageMode, value => { if (value) imageMode.value = value })
watch(imageMode, value => emit('mode', value))
watch(accountID, () => { resetWorkflow(); history.value = []; void load() }, { flush: 'sync' })
watch(currentStep, () => { guidance.value = '' })
onBeforeUnmount(() => { disposed = true; resetRequests(); if (ticker) clearInterval(ticker); clearImages() })
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
.workflow-attachments { display:flex; flex-wrap:wrap; align-items:center; gap:6px; margin-top:8px; }
.workflow-attachment { display:flex; align-items:center; gap:6px; max-width:220px; padding:4px 6px; border:1px solid #d1d5db; border-radius:6px; font-size:12px; }
.workflow-attachment-thumb, .workflow-attachment-icon { width:28px; height:28px; object-fit:cover; border-radius:4px; }
.workflow-attachment-icon { display:grid; place-items:center; background:#eef2ff; color:#4f46e5; font-size:8px; font-weight:700; }
.workflow-attachment-remove { color:#94a3b8; font-size:16px; }
:global(html.dark .workflow-attachment) { border-color:#475569; background:#172033; }
:global(html.dark .workflow-shell) { border-color: #475569; background: #172033; color: #e5e7eb; }
:global(html.dark .workflow-history) { border-color: #475569; }
:global(html.dark .workflow-step) { border-color: #475569; background: #1e293b; }
@media (max-width: 768px) {
  .workflow-shell { grid-template-columns: 1fr; grid-template-rows: auto minmax(0, 1fr); }
  .workflow-history { max-height: 180px; border-right: 0; border-bottom: 1px solid #d1d5db; }
  .workflow-main { min-height: 0; padding: 12px; }
}
</style>
