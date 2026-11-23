<template>
  <component :is="mobile ? 'div' : AppLayout">
    <div
      class="studio"
      :class="{ 'mobile-studio': mobile, 'settings-expanded': mobileControlsOpen }"
      :style="mobile && mobileViewportHeight ? { height: `${mobileViewportHeight}px` } : undefined"
      @keydown.esc="closeMobilePanels"
      @paste="onPasteAttachments"
      @dragenter.prevent="onDragEnter" @dragover.prevent @dragleave.prevent="onDragLeave" @drop.prevent="onDropFiles"
    >
      <button v-if="historyOpen" class="history-scrim" type="button" :aria-label="t('common.close')" @click="historyOpen = false" />
      <aside v-show="activeTab === 'standard'" class="history-panel" :class="{ 'mobile-open': historyOpen }">
        <div class="flex items-center justify-between">
          <h1 class="text-lg font-semibold">{{ t('playground.title') }}</h1>
          <button class="text-sm" :class="{ 'lg:hidden': !mobile }" @click="historyOpen = false">{{ t('common.close') }}</button>
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
        <p class="retention-note">{{ t('playground.retention') }} {{ t('playground.storageLimit', { limit: storageLimitMB }) }}</p>
        <button class="mt-3 text-left text-xs text-gray-500 hover:text-primary-500 dark:text-dark-400" :disabled="busy" @click="refreshHistory()">{{ t('playground.refreshHistory') }}</button>
      </aside>

      <main class="conversation-panel" :class="{ 'is-dragging-files': draggingFiles }">
        <div v-if="mobile" class="mobile-header">
          <button class="studio-icon" type="button" :aria-label="t('playground.history')" :aria-expanded="historyOpen" @click="openMobileHistory">
            <Icon name="menu" size="md" />
          </button>
          <button class="mobile-conversation-picker" type="button" :aria-label="t('playground.settings')" :aria-expanded="mobileControlsOpen" @click="mobileControlsOpen = !mobileControlsOpen">
            <span class="mobile-conversation-title">{{ activeTab === 'workflow' ? t('playground.workflow') : current.title || t('playground.title') }}</span>
            <span class="mobile-model-label">
              <span class="truncate">{{ mobileModelLabel }}</span>
              <Icon name="chevronDown" size="xs" />
            </span>
          </button>
          <button class="studio-icon" type="button" :aria-label="t('playground.newChat')" :disabled="busy" @click="newMobileChat">
            <Icon name="plus" size="md" />
          </button>
        </div>
        <header class="studio-toolbar">
          <button v-if="!mobile && activeTab === 'standard'" class="studio-icon lg:hidden" type="button" :aria-label="t('playground.history')" @click="historyOpen = !historyOpen"><Icon name="menu" size="md" /></button>
          <div class="playground-tabs" role="tablist">
            <button class="playground-tab" :class="{ active: activeTab === 'standard' }" role="tab" :aria-selected="activeTab === 'standard'" @click="activeTab = 'standard'">{{ t('playground.standard') }}</button>
            <button class="playground-tab" :class="{ active: activeTab === 'workflow' }" role="tab" :aria-selected="activeTab === 'workflow'" @click="openWorkflowTab">{{ t('playground.workflow') }}</button>
          </div>
          <label class="min-w-0 flex-1 sm:flex-none">
            <span class="toolbar-label">{{ t('playground.group') }}</span>
            <select v-model="selectedRouteValue" class="toolbar-select" :disabled="streaming || submitting || loadingGroups || loadingProfiles">
              <option value="" disabled>{{ t('playground.selectGroup') }}</option>
              <option
                v-if="selectedRouteValue.startsWith('auto:') && !selectedAutoProfile"
                :value="selectedRouteValue"
                disabled
              >
                {{ t('playground.autoProfileUnavailable') }}
              </option>
              <optgroup :label="t('playground.fixedGroups')">
                <option v-for="group in groups" :key="`group-${group.id}`" :value="`group:${group.id}`">{{ group.name }}</option>
              </optgroup>
              <optgroup v-if="autoProfiles.length" :label="t('playground.autoProfiles')">
                <option v-for="profile in autoProfiles" :key="`auto-${profile.id}`" :value="`auto:${profile.id}`">
                  {{ profile.name }}
                </option>
              </optgroup>
            </select>
          </label>
          <button
            v-if="selectedRouteValue.startsWith('auto:')"
            type="button"
            class="grid h-9 w-9 shrink-0 place-items-center rounded border border-gray-200 text-gray-600 hover:bg-gray-50 disabled:opacity-40 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700"
            :aria-label="t('playground.configureAuto')"
            :title="t('playground.configureAuto')"
            :disabled="!selectedAutoProfile || streaming || submitting"
            @click="openAutoRoutingEditor"
          >
            <Icon name="cog" size="sm" />
          </button>
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
          <div class="toolbar-account">
            <div class="text-right"><span class="toolbar-label">{{ t('playground.balance') }}</span><span class="text-sm font-medium tabular-nums">${{ (authStore.user?.balance ?? 0).toFixed(4) }}</span></div>
            <RouterLink v-if="!busy" class="studio-view-link" :to="mobile ? '/playground' : '/playground/mobile'">{{ t(mobile ? 'playground.desktopView' : 'playground.mobileView') }}</RouterLink>
          </div>
        </header>

        <WorkflowPlayground v-show="activeTab === 'workflow'" :groups="groups" :group-id="workflowPresets.groupId" :routing="workflowPresets.routing" :route="workflowRequestRoute" :route-valid="workflowRouteValid" :models="models" :models-loading="loadingModels" :chat-model="workflowPresets.chatModel" :image-model="workflowPresets.imageModel" :image-mode="imageMode" :attachments="attachments" :attachments-loading="loadingAttachments" :attachment-error="attachmentError" @request-attachments="attachmentInput?.click()" @remove-attachment="removeAttachment" @consume-attachments="removeAttachments" @presets="applyWorkflowPresets" @mode="imageMode = $event" />
        <div v-show="activeTab === 'standard'" class="standard-playground">
        <div ref="viewport" class="conversation-scroll" @scroll.passive="onViewportScroll">
          <div v-if="!current.messages.length && !currentImages.length" class="welcome">
            <h2 class="welcome-title text-xl font-semibold sm:text-2xl">{{ t('playground.welcome') }}</h2>
            <div v-if="!mobile" class="mt-5 grid w-full max-w-lg gap-2 sm:grid-cols-2">
              <button class="suggestion" @click="draft = t('playground.chatSuggestion')">{{ t('playground.chatSuggestion') }}</button>
              <button class="suggestion" @click="draft = t('playground.imageSuggestion')">{{ t('playground.imageSuggestion') }}</button>
            </div>
          </div>
          <div v-else class="mx-auto w-full max-w-3xl space-y-7">
            <template v-for="message in current.messages" :key="message.id">
              <article class="message-entry" :class="message.role === 'user' ? 'flex justify-end' : 'assistant-message'">
                <div v-if="message.role === 'user'" class="user-message">
                  <div v-if="editingMessageId === message.id" class="user-edit">
                    <textarea v-model="editingText" rows="3" class="composer-input user-edit-input" :aria-label="t('common.edit')" :disabled="submitting" @keydown.enter.exact="onEditEnter" @keydown.esc="cancelEdit" />
                    <div class="user-edit-actions">
                      <button type="button" class="studio-view-link" :disabled="submitting" @click="cancelEdit">{{ t('common.cancel') }}</button>
                      <button type="button" class="btn btn-primary btn-sm" :disabled="!editingText.trim() || busy || submitting" @click="submitEdit">{{ t('playground.send') }}</button>
                    </div>
                  </div>
                  <template v-else>
                    <div class="user-bubble">{{ message.content }}</div>
                    <div v-if="!busy && !submitting" class="user-message-actions">
                      <button type="button" class="text-xs text-gray-400 hover:text-primary-500 dark:text-dark-400" @click="startEdit(message)">{{ t('common.edit') }}</button>
                    </div>
                  </template>
                </div>
                <div v-else>
                  <span class="mb-2 block text-xs font-semibold text-gray-400 dark:text-dark-400">{{ message.model || current.model }}</span>
                  <p v-if="message.kind === 'image'">{{ pendingImageIds.includes(message.id) ? t('playground.generating') : (message.content || t('playground.imageUnavailable')) }}</p>
                  <template v-else>
                    <div class="playground-markdown" v-html="renderMarkdown(message.content || (streaming ? '…' : ''))"></div>
                    <div v-if="message.content" class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2">
                      <button class="text-xs text-gray-400 hover:text-primary-500 dark:text-dark-400" @click="copyMessage(message.content)">{{ t('playground.copy') }}</button>
                      <button v-for="artifact in artifactsForMessage(message)" :key="artifact.id" class="text-xs font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="downloadPlaygroundArtifact(artifact)">{{ t('playground.downloadArtifact', { name: artifact.name }) }}</button>
                    </div>
                  </template>
                </div>
              </article>
              <div v-if="currentImages.some(image => image.messageId === message.id)" class="grid gap-4 sm:grid-cols-2">
                <figure v-for="image in currentImages.filter(entry => entry.messageId === message.id)" :key="image.id" class="image-card">
                  <button class="image-preview block w-full cursor-zoom-in" :aria-label="t('playground.preview')" @click="preview = image"><img :src="image.url" :alt="image.prompt" class="max-h-96 w-full object-contain" /></button>
                  <figcaption class="flex items-center justify-between gap-2 p-3">
                    <span v-if="image.expiresAt" class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ remaining(image.expiresAt) }}</span>
                    <div class="ml-auto flex flex-wrap gap-3">
                      <button type="button" class="studio-view-link" @click="selectImageReference(image)">{{ t('playground.useImageReference') }}</button>
                      <button type="button" class="studio-view-link" @click="downloadImage(image)">{{ t('playground.download') }}</button>
                    </div>
                  </figcaption>
                </figure>
              </div>
            </template>
            <p v-if="generating" class="animate-pulse text-sm text-gray-500 dark:text-dark-400">{{ t('playground.generating') }}…</p>
          </div>
        </div>

        <div class="composer-area">
          <div class="mx-auto max-w-3xl">
            <p v-if="error || attachmentError" role="alert" class="mb-3 text-sm text-red-500">{{ attachmentError || error }}</p>
            <p v-else-if="!hasBalance" role="alert" class="mb-3 text-sm text-amber-600 dark:text-amber-400">{{ t('playground.insufficientBalance') }}</p>
            <p v-if="saveError" role="alert" class="mb-3 text-sm text-amber-600">{{ saveError }} <button class="underline" :disabled="busy" @click="retrySave">{{ t('playground.retrySave') }}</button></p>
            <p v-if="cacheError" role="alert" class="mb-3 text-sm text-amber-600">{{ cacheError }}</p>
            <div v-if="settingsOpen" class="settings-panel">
              <label class="col-span-2 text-xs">{{ t('playground.systemPrompt') }}<textarea v-model="current.systemPrompt" class="input mt-2 w-full" rows="2" :disabled="streaming || submitting" /></label>
              <label class="col-span-2 text-xs">{{ t('playground.temperature') }} · {{ current.temperature.toFixed(1) }}<input v-model.number="current.temperature" class="mt-3 w-full accent-primary-500" type="range" min="0" max="2" step="0.1" :disabled="streaming || submitting" /></label>
              <label class="text-xs">{{ t('playground.size') }}<select v-model="imageSize" class="input mt-2 w-full" :disabled="streaming || submitting"><option>1024x1024</option><option>1536x1024</option><option>1024x1536</option></select></label>
              <label class="text-xs">{{ t('playground.quality') }}<select v-model="imageQuality" class="input mt-2 w-full" :disabled="streaming || submitting"><option>auto</option><option>low</option><option>medium</option><option>high</option></select></label>
              <label class="text-xs">{{ t('playground.count') }}<select v-model.number="imageCount" class="input mt-2 w-full" :disabled="streaming || submitting"><option v-for="count in 10" :key="count" :value="count">{{ count }}</option></select></label>
            </div>
            <form class="composer" @submit.prevent="send()">
              <div v-if="selectedImageReference" class="attachment-list">
                <div class="attachment-item">
                  <img :src="selectedImageReference.url" alt="" class="attachment-thumb" />
                  <span class="min-w-0 flex-1 truncate text-xs">{{ t('playground.imageReference') }}</span>
                  <button type="button" class="attachment-remove" :aria-label="t('common.delete')" @click="imageReferenceId = ''">×</button>
                </div>
              </div>
              <div v-if="attachments.length" class="attachment-list" aria-live="polite">
                <div v-for="attachment in attachments" :key="attachment.id" class="attachment-item">
                  <img v-if="attachment.kind === 'image'" :src="attachment.dataUrl" :alt="attachment.name" class="attachment-thumb" />
                  <span v-else class="attachment-file-icon" aria-hidden="true">TXT</span>
                  <span class="min-w-0 flex-1 truncate text-xs">{{ attachment.name }}</span>
                  <button type="button" class="attachment-remove" :aria-label="t('playground.removeAttachment', { name: attachment.name })" :disabled="submitting" @click="removeAttachment(attachment.id)">×</button>
                </div>
              </div>
              <textarea ref="composerInput" v-model="draft" rows="2" class="composer-input" :aria-label="t('playground.messagePlaceholder')" :placeholder="t('playground.messagePlaceholder')" :disabled="submitting" @keydown.enter.exact="onEnter" />
              <div class="composer-actions">
                <div class="composer-tools">
                  <input ref="attachmentInput" class="sr-only" type="file" multiple :accept="attachmentAccept" :disabled="submitting || loadingAttachments" @change="handleAttachmentSelection" />
                  <button type="button" :class="mobile ? 'studio-icon' : 'mode-button'" :aria-label="t('playground.attachFiles')" :title="t('playground.attachmentNote')" :disabled="submitting || loadingAttachments" @click="attachmentInput?.click()">
                    <Icon v-if="mobile" name="plus" size="sm" />
                    <span v-else>{{ t('playground.attachFiles') }}</span>
                  </button>
                  <label class="mode-control">
                    <span class="sr-only">{{ t('playground.modeLabel') }}</span>
                    <select v-model="imageMode" class="mode-select" :disabled="submitting">
                      <option value="auto">{{ t(mobile ? 'playground.mobileModeAuto' : 'playground.modeAuto') }}</option>
                      <option value="chat">{{ t(mobile ? 'playground.mobileModeChat' : 'playground.modeChat') }}</option>
                      <option value="image">{{ t(mobile ? 'playground.mobileModeImage' : 'playground.modeImage') }}</option>
                    </select>
                  </label>
                  <span v-if="imageMode === 'image'" class="mode-indicator">{{ t('playground.imagesToGenerate', { count: imageCountResolution.count }) }}</span>
                  <button type="button" class="studio-icon" :aria-label="t('playground.settings')" :title="t('playground.settings')" :aria-expanded="settingsOpen" :disabled="submitting" @click="settingsOpen = !settingsOpen"><Icon name="cog" size="sm" /></button>
                </div>
                <span v-if="activeRequestCount" class="request-status" aria-live="polite">{{ t('playground.activeRequests', { count: activeRequestCount }) }}</span>
                <button v-if="activeRequestCount" type="button" class="btn btn-secondary" @click="abortActiveRequests">{{ t('playground.stop') }}</button>
                <button class="btn btn-primary studio-send" type="submit" :aria-label="t('playground.send')" :title="t('playground.send')" :disabled="!canSend"><Icon name="arrowUp" size="sm" /></button>
              </div>
            </form>
            <p class="composer-retention">{{ t('playground.imageRetention') }}</p>
          </div>
        </div>
        </div>
        <div v-if="draggingFiles" class="drag-overlay" aria-live="polite">{{ t('playground.dropFiles') }}</div>
      </main>
    </div>
    <BaseDialog :show="Boolean(preview)" :title="t('playground.preview')" width="wide" :close-on-click-outside="true" @close="preview = null">
      <template v-if="preview"><img :src="preview.url" :alt="preview.prompt" class="max-h-[65vh] w-full object-contain" /><button class="btn btn-primary mt-4" @click="downloadImage(preview)">{{ t('playground.download') }}</button></template>
    </BaseDialog>
    <BaseDialog :show="autoRoutingEditorOpen" :title="t('playground.autoRoutingTitle')" width="wide" @close="closeAutoRoutingEditor">
      <div v-if="loadingAutoRouting" class="flex min-h-32 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="md" class="mr-2 animate-spin" />
        {{ t('common.loading') }}
      </div>
      <div v-else-if="selectedAutoProfile" class="space-y-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ selectedAutoProfile.name }}</p>
          <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">
            {{ t('playground.routingRevision') }} {{ autoRoutingConfig?.routing_revision ?? 0 }}
          </span>
        </div>
        <div class="inline-flex rounded-md border border-gray-200 p-1 dark:border-dark-600" role="group">
          <button
            type="button"
            class="min-h-9 px-4 text-sm font-medium"
            :class="autoSelectionMode === 'inherit' ? 'rounded-sm bg-gray-100 text-gray-900 dark:bg-dark-700 dark:text-white' : 'text-gray-600 dark:text-gray-300'"
            :aria-pressed="autoSelectionMode === 'inherit'"
            :disabled="savingAutoRouting"
            @click="setAutoSelectionMode('inherit')"
          >
            {{ t('playground.inheritAutoOrder') }}
          </button>
          <button
            v-if="selectedAutoProfile.allow_custom_order"
            type="button"
            class="min-h-9 px-4 text-sm font-medium"
            :class="autoSelectionMode === 'custom' ? 'rounded-sm bg-primary-50 text-primary-700 dark:bg-primary-500/15 dark:text-primary-300' : 'text-gray-600 dark:text-gray-300'"
            :aria-pressed="autoSelectionMode === 'custom'"
            :disabled="savingAutoRouting"
            @click="setAutoSelectionMode('custom')"
          >
            {{ t('playground.customAutoOrder') }}
          </button>
        </div>
        <ol v-if="autoSelectionMode === 'inherit'" class="max-h-64 overflow-y-auto rounded border border-gray-200 dark:border-dark-600">
          <li
            v-for="(group, index) in autoInheritedGroups"
            :key="group.id"
            class="flex min-h-10 items-center gap-3 border-b border-gray-100 px-3 py-2 last:border-0 dark:border-dark-700"
          >
            <span class="w-6 shrink-0 text-right text-xs tabular-nums text-gray-400">{{ index + 1 }}</span>
            <span class="min-w-0 break-words text-sm text-gray-800 dark:text-gray-100">{{ group.name }}</span>
          </li>
          <li v-if="autoInheritedGroups.length === 0" class="px-3 py-4 text-sm text-gray-500 dark:text-gray-400">
            {{ t('playground.noAutoGroups') }}
          </li>
        </ol>
        <div v-else class="space-y-1">
          <label
            v-for="group in autoProfileGroups"
            :key="group.id"
            class="flex min-h-10 items-center gap-3 border-b border-gray-100 py-2 text-sm last:border-0 dark:border-dark-700"
          >
            <input
              type="checkbox"
              class="checkbox shrink-0"
              :checked="autoGroupIds.includes(group.id)"
              :disabled="savingAutoRouting || (!autoGroupIds.includes(group.id) && autoGroupIds.length >= selectedAutoProfile.max_candidates)"
              @change="toggleAutoGroup(group.id, ($event.target as HTMLInputElement).checked)"
            />
            <span class="min-w-0 flex-1 break-words text-gray-800 dark:text-gray-100">{{ group.name }}</span>
            <span v-if="autoGroupIds.includes(group.id)" class="flex shrink-0 items-center gap-1">
              <span class="w-5 text-center text-xs tabular-nums text-gray-500">{{ autoGroupIds.indexOf(group.id) + 1 }}</span>
              <button type="button" class="grid h-8 w-8 place-items-center rounded-sm text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700" :aria-label="t('playground.moveAutoGroupUp')" :title="t('playground.moveAutoGroupUp')" :disabled="savingAutoRouting || autoGroupIds.indexOf(group.id) === 0" @click="moveAutoGroup(group.id, -1)">
                <Icon name="arrowUp" size="sm" />
              </button>
              <button type="button" class="grid h-8 w-8 place-items-center rounded-sm text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-dark-700" :aria-label="t('playground.moveAutoGroupDown')" :title="t('playground.moveAutoGroupDown')" :disabled="savingAutoRouting || autoGroupIds.indexOf(group.id) === autoGroupIds.length - 1" @click="moveAutoGroup(group.id, 1)">
                <Icon name="arrowDown" size="sm" />
              </button>
            </span>
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('playground.autoGroupCount', { selected: autoGroupIds.length, max: selectedAutoProfile.max_candidates }) }}
          </p>
        </div>
        <label
          v-if="selectedAutoProfile.cross_group_retry_allowed"
          class="flex items-start gap-3 border-t border-gray-200 pt-3 dark:border-dark-600"
        >
          <input v-model="crossGroupRetry" type="checkbox" class="checkbox mt-0.5 shrink-0" :disabled="savingAutoRouting" />
          <span class="text-sm text-gray-700 dark:text-gray-200">
            <span class="block font-medium">{{ t('playground.crossGroupRetry') }}</span>
            <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('playground.crossGroupRetryHint') }}</span>
          </span>
        </label>
        <p v-else class="border-t border-gray-200 pt-3 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">
          {{ t('playground.crossGroupRetryUnavailable') }}
        </p>
        <p v-if="autoRoutingError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ autoRoutingError }}</p>
      </div>
      <p v-else class="text-sm text-red-600 dark:text-red-400">{{ autoRoutingError || t('playground.autoProfileUnavailable') }}</p>
      <template #footer>
        <div class="flex w-full justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="savingAutoRouting" @click="closeAutoRoutingEditor">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="!canSaveAutoRouting" @click="saveAutoRouting">
            <Icon v-if="savingAutoRouting" name="refresh" size="sm" class="mr-2 animate-spin" />
            {{ savingAutoRouting ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>
    <BaseDialog :show="Boolean(deleteTarget)" :title="t('common.delete')" @close="deleteTarget = ''"><p>{{ t('playground.deleteConfirm') }}</p><button class="btn btn-primary mt-4" @click="deleteConversation">{{ t('common.delete') }}</button></BaseDialog>
  </component>
</template>

<script setup lang="ts">
import { computed, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { userGroupsAPI } from '@/api/groups'
import Icon from '@/components/icons/Icon.vue'
import { autoRoutingAPI, type AvailableAutoGroupProfile } from '@/api/autoRouting'
import { PlaygroundImageGenerationError, playgroundAPI, playgroundAttachmentLimitError, playgroundModelSupportsEndpoint, type PlaygroundAutoRoutingConfig, type PlaygroundImage, type PlaygroundModel, type PlaygroundMessage, type PlaygroundMultimodalMessage, type PlaygroundRequestRoute } from '@/api/playground'
import { playgroundHistory, type Conversation, type ConversationMessage, type ConversationRouting } from '@/api/playgroundHistory'
import { useAppStore, useAuthStore } from '@/stores'
import type { Group } from '@/types'
import { isPlaygroundImageModel, isPlaygroundVideoModel } from '@/utils/playgroundModel'
import { resolvePlaygroundIntent, type PlaygroundMode } from '@/utils/playgroundIntent'
import { createPlaygroundId } from '@/utils/playgroundId'
import { resolvePlaygroundImageCount } from '@/utils/playgroundImageCount'
import { selectPlaygroundContext } from '@/utils/playgroundContext'
import { createStreamTyper } from '@/utils/playgroundStreamTyper'
import { playgroundImageBlob, playgroundImageCache } from '@/utils/playgroundImageCache'
import { playgroundImageExtension } from '@/utils/playgroundImageFormat'
import { loadPlaygroundPreferences, savePlaygroundPreferences } from '@/utils/playgroundPreferences'
import { artifactGenerationInstruction, downloadPlaygroundArtifact, extractPlaygroundArtifacts, type PlaygroundArtifact } from '@/utils/playgroundArtifacts'
import WorkflowPlayground from '@/components/playground/WorkflowPlayground.vue'

type PlaygroundKind = 'chat' | 'image'
type StoredMessage = ConversationMessage & { model?: string; kind?: PlaygroundKind }
type PlaygroundConversation = Omit<Conversation, 'messages'> & { messages: StoredMessage[] }
type ImageEntry = { id: string; conversationId: string; messageId: number; url: string; prompt: string; expiresAt: number; objectUrl?: boolean }
type TextAttachment = { id: string; name: string; kind: 'text'; text: string; size: number }
type ImageAttachment = { id: string; name: string; kind: 'image'; dataUrl: string; size: number }
type PlaygroundAttachment = TextAttachment | ImageAttachment
type RequestContext = { accountId: number | null; epoch: number; conversationId: string }
defineOptions({ name: 'PlaygroundView' })

const maxMessageBytes = 1024 * 1024
const maxConversationBytes = 4 * 1024 * 1024
const textAttachmentExtensions = new Set([
  'txt', 'md', 'markdown', 'csv', 'json', 'js', 'jsx', 'mjs', 'cjs', 'ts', 'tsx', 'vue',
  'html', 'htm', 'css', 'scss', 'sass', 'less', 'py', 'go', 'rs', 'java', 'kt', 'swift',
  'c', 'h', 'cc', 'cpp', 'cxx', 'hpp', 'cs', 'php', 'rb', 'sh', 'bash', 'zsh', 'yml',
  'yaml', 'toml', 'xml', 'sql', 'svelte', 'r', 'R', 'lua', 'pl', 'ex', 'exs', 'erl',
  'hrl', 'clj', 'cljs', 'edn', 'scala', 'sc', 'dart', 'proto', 'graphql', 'gql', 'ini',
  'conf', 'env', 'makefile', 'dockerfile'
])
const imageAttachmentExtensions = new Set(['png', 'jpg', 'jpeg', 'webp'])
const attachmentAccept = [
  ...[...textAttachmentExtensions].map(extension => `.${extension}`),
  'image/png', 'image/jpeg', 'image/webp'
].join(',')
const { t } = useI18n()
const props = defineProps<{ mobile?: boolean }>()
const mobileControlsOpen = ref(false)
const storageLimitMB = ref(50)
const composerInput = ref<HTMLTextAreaElement | null>(null)
const mobileViewportHeight = ref(0)
const mobileModelLabel = computed(() => {
  const model = imageMode.value === 'image' ? selectedImageModel.value : selectedChatModel.value
  return (activeTab.value === 'standard' && model) || t('playground.settings')
})

function updateMobileViewport() {
  const viewport = window.visualViewport
  mobileViewportHeight.value = viewport?.scale === 1 ? Math.round(viewport.height) : 0
}

function syncMobileViewport() {
  window.visualViewport?.removeEventListener('resize', updateMobileViewport)
  if (pageActive && props.mobile) {
    updateMobileViewport()
    window.visualViewport?.addEventListener('resize', updateMobileViewport)
  }
}

function closeMobilePanels() {
  if (!props.mobile) return
  historyOpen.value = false
  mobileControlsOpen.value = false
  settingsOpen.value = false
}
const imageReferenceId = ref('')
const authStore = useAuthStore()
const appStore = useAppStore()
const groups = ref<Group[]>([])
const autoProfiles = ref<AvailableAutoGroupProfile[]>([])
const models = ref<PlaygroundModel[]>([])
const conversations = ref<PlaygroundConversation[]>([])
const current = ref<PlaygroundConversation>(emptyConversation())
const draft = ref('')
const attachments = ref<PlaygroundAttachment[]>([])
const attachmentInput = ref<HTMLInputElement | null>(null)
const loadingAttachments = ref(false)
const attachmentError = ref('')
const error = ref('')
const saveError = ref('')
const cacheError = ref('')
const loadingGroups = ref(false)
const loadingProfiles = ref(false)
const loadingModels = ref(false)
const streamingCount = ref(0)
const generatingCount = ref(0)
const pendingImageIds = ref<number[]>([])
const saving = ref(false)
const submitting = ref(false)
const editingMessageId = ref<StoredMessage['id'] | null>(null)
const editingText = ref('')
const settingsOpen = ref(false)
const activeTab = ref<'standard' | 'workflow'>('standard')
const workflowPresets = ref<{ groupId: number; routing?: ConversationRouting; chatModel: string; imageModel: string }>({ groupId: 0, routing: { mode: 'fixed' }, chatModel: '', imageModel: '' })
const autoRoutingEditorOpen = ref(false)
const loadingAutoRouting = ref(false)
const savingAutoRouting = ref(false)
const autoRoutingError = ref('')
const autoRoutingConfig = ref<PlaygroundAutoRoutingConfig | null>(null)
const autoSelectionMode = ref<'inherit' | 'custom'>('inherit')
const autoGroupIds = ref<number[]>([])
const crossGroupRetry = ref(false)
const historyOpen = ref(false)
const deleteTarget = ref('')
const imageSize = ref('1024x1024')
const imageQuality = ref('auto')
const imageCount = ref(1)
const imageMode = ref<PlaygroundMode>('chat')
const images = ref<ImageEntry[]>([])
const preview = ref<ImageEntry | null>(null)
const viewport = ref<HTMLElement | null>(null)
const draggingFiles = ref(false)
const now = ref(Date.now())
const chatControllers = new Set<AbortController>()
const imageControllers = new Set<AbortController>()
let dragDepth = 0
let timer: ReturnType<typeof setInterval> | undefined
let modelRequest = 0
let routeOptionsRequest = 0
let accountEpoch = 0
let attachmentQueue: Promise<void> = Promise.resolve()
let attachmentBatchCount = 0
let disposed = false
let pageActive = true
let saveQueue: Promise<boolean> = Promise.resolve(true)
let lastMessageId = Date.now()
const acceptedRevisions = new Map<string, number>()
const artifactCache = new Map<number, { content: string; artifacts: PlaygroundArtifact[] }>()
const generating = computed(() => generatingCount.value > 0)
const streaming = computed(() => streamingCount.value > 0)
const activeRequestCount = computed(() => streamingCount.value + generatingCount.value)
const busy = computed(() => streaming.value || generating.value || saving.value)
const hasBalance = computed(() => Number(authStore.user?.balance ?? 0) > 0)
const canSend = computed(() => hasBalance.value && !submitting.value && !loadingAttachments.value && !loadingModels.value && Boolean((draft.value.trim() || attachments.value.length) && currentRequestRoute.value))
const accountId = computed(() => authStore.user?.id ?? null)
const imageCountResolution = computed(() => resolvePlaygroundImageCount('', imageCount.value))
const chatModels = computed(() => models.value.filter((model) =>
  playgroundModelSupportsEndpoint(model, 'chat', !isPlaygroundImageModel(model.id) && !isPlaygroundVideoModel(model.id))
))
const imageModels = computed(() => models.value.filter((model) =>
  playgroundModelSupportsEndpoint(model, 'images_generation', isPlaygroundImageModel(model.id)) ||
  playgroundModelSupportsEndpoint(model, 'images_edit', isPlaygroundImageModel(model.id))
))
const normalConversations = computed(() => conversations.value.filter((conversation) => conversation.kind !== 'workflow'))
const currentImages = computed(() => images.value.filter(image => image.conversationId === current.value.id && (!image.expiresAt || image.expiresAt > now.value)))
const selectedImageReference = computed(() => currentImages.value.find(image => image.id === imageReferenceId.value))
const activeRouteSource = computed(() => activeTab.value === 'workflow' ? workflowPresets.value : current.value)
const selectedAutoProfile = computed(() => {
  const routing = activeRouteSource.value.routing
  return routing?.mode === 'auto'
    ? autoProfiles.value.find(profile => profile.id === routing.autoProfileId) ?? null
    : null
})
const selectedRouteValue = computed({
  get: () => {
    const source = activeRouteSource.value
    if (source.routing?.mode === 'auto') return `auto:${source.routing.autoProfileId ?? ''}`
    return source.groupId > 0 ? `group:${source.groupId}` : ''
  },
  set: (value: string) => setRouteValue(value)
})
const currentRequestRoute = computed(() => requestRouteFor(current.value))
const workflowRequestRoute = computed(() => requestRouteFor(workflowPresets.value))
const workflowRouteValid = computed(() => Boolean(workflowRequestRoute.value))
const autoProfileGroups = computed(() => {
  const authorizedGroups = new Map(groups.value.map(group => [group.id, group]))
  return (selectedAutoProfile.value?.group_ids ?? [])
    .filter(groupID => authorizedGroups.has(groupID))
    .map(groupID => authorizedGroups.get(groupID)!)
    .filter(group => group.platform.toLowerCase() !== 'composite' && group.status === 'active')
})
const autoInheritedGroups = computed(() =>
  autoProfileGroups.value.slice(0, selectedAutoProfile.value?.max_candidates ?? 0)
)
const canSaveAutoRouting = computed(() => {
  const profile = selectedAutoProfile.value
  if (!profile || loadingAutoRouting.value || savingAutoRouting.value || !!autoRoutingError.value) return false
  if (autoSelectionMode.value === 'custom') {
    return profile.allow_custom_order &&
      autoGroupIds.value.length > 0 &&
      autoGroupIds.value.length <= profile.max_candidates &&
      autoGroupIds.value.every(groupID => autoProfileGroups.value.some(group => group.id === groupID))
  }
  return autoInheritedGroups.value.length > 0
})
const selectedChatModel = computed({
  get: () => activeTab.value === 'workflow' ? workflowPresets.value.chatModel : current.value.model,
  set: (value: string) => {
    if (activeTab.value === 'workflow') {
      workflowPresets.value.chatModel = value
      savePlaygroundPreferences(accountId.value, { ...workflowPresets.value, imageMode: imageMode.value })
    }
    else current.value.model = value
  }
})
const selectedImageModel = computed({
  get: () => activeTab.value === 'workflow' ? workflowPresets.value.imageModel : current.value.imageModel || '',
  set: (value: string) => {
    if (activeTab.value === 'workflow') {
      workflowPresets.value.imageModel = value
      savePlaygroundPreferences(accountId.value, { ...workflowPresets.value, imageMode: imageMode.value })
    }
    else current.value.imageModel = value
  }
})

function emptyConversation(): PlaygroundConversation {
  return { id: createPlaygroundId(), title: '', groupId: 0, model: '', imageModel: '', systemPrompt: '', temperature: 0.7, messages: [], revision: 0, expiresAt: 0 }
}
function requestRouteFor(source: { groupId: number; routing?: ConversationRouting }): PlaygroundRequestRoute | null {
  if (source.routing?.mode === 'auto') {
    const profileID = source.routing.autoProfileId
    if (!profileID || !autoProfiles.value.some(profile => profile.id === profileID && profile.status === 'active')) return null
    return { mode: 'auto', autoProfileId: profileID }
  }
  const group = groups.value.find(item => item.id === source.groupId && item.status === 'active')
  return group ? { mode: 'fixed', groupId: group.id } : null
}
function saveRoutePreference(source: {
  groupId: number
  routing?: ConversationRouting
  chatModel?: string
  model?: string
  imageModel?: string
}) {
  savePlaygroundPreferences(accountId.value, {
    groupId: source.groupId,
    routing: source.routing,
    chatModel: source.chatModel ?? source.model ?? '',
    imageModel: source.imageModel ?? '',
    imageMode: imageMode.value
  })
}
function setRouteValue(value: unknown) {
  if (typeof value !== 'string') return
  const separator = value.indexOf(':')
  if (separator < 0) return
  const mode = value.slice(0, separator)
  const id = Number(value.slice(separator + 1))
  if (mode === 'group' && Number.isInteger(id) && groups.value.some(group => group.id === id && group.status === 'active')) {
    const source = activeRouteSource.value
    source.groupId = id
    source.routing = { mode: 'fixed' }
    saveRoutePreference(source)
    return
  }
  if (mode === 'auto' && Number.isInteger(id) && autoProfiles.value.some(profile => profile.id === id && profile.status === 'active')) {
    activeRouteSource.value.routing = { mode: 'auto', autoProfileId: id }
    saveRoutePreference(activeRouteSource.value)
  }
}
async function initializeRouteOptions(account: number | null) {
  const request = ++routeOptionsRequest
  if (account === null) {
    groups.value = []
    autoProfiles.value = []
    loadingGroups.value = false
    loadingProfiles.value = false
    return
  }
  loadingGroups.value = true
  loadingProfiles.value = true
  const [groupsResult, profilesResult] = await Promise.allSettled([
    userGroupsAPI.getAvailable(),
    autoRoutingAPI.getAvailableProfiles()
  ])
  if (request !== routeOptionsRequest || disposed || account !== accountId.value) return
  groups.value = groupsResult.status === 'fulfilled' ? groupsResult.value : []
  autoProfiles.value = profilesResult.status === 'fulfilled' ? profilesResult.value : []
  loadingGroups.value = false
  loadingProfiles.value = false
  if (groupsResult.status === 'rejected' || profilesResult.status === 'rejected') {
    error.value = t('playground.loadModelsFailed')
  }
  const preferences = loadPlaygroundPreferences(account)
  const selectedGroup = groups.value.find(group => group.id === preferences.groupId && group.status === 'active')
    ?? groups.value.find(group => group.status === 'active')
  const savedProfileID = preferences.routing?.mode === 'auto' ? preferences.routing.autoProfileId : undefined
  const savedProfile = autoProfiles.value.find(profile => profile.id === savedProfileID && profile.status === 'active')
  current.value = {
    ...emptyConversation(),
    groupId: selectedGroup?.id ?? 0,
    routing: savedProfile ? { mode: 'auto', autoProfileId: savedProfile.id } : { mode: 'fixed' },
    model: preferences.chatModel,
    imageModel: preferences.imageModel
  }
  workflowPresets.value = {
    groupId: current.value.groupId,
    routing: current.value.routing ? { ...current.value.routing } : { mode: 'fixed' },
    chatModel: current.value.model,
    imageModel: current.value.imageModel || ''
  }
  imageMode.value = preferences.imageMode
  const context = requestContext()
  await refreshHistory(context)
  if (!isCurrentRequest(context) || request !== routeOptionsRequest) return
  const lastConversation = conversations.value.find(item => item.id === loadLastConversation(account))
  if (lastConversation) openConversation(lastConversation)
}
async function openAutoRoutingEditor() {
  const profile = selectedAutoProfile.value
  if (!profile || loadingAutoRouting.value || savingAutoRouting.value) return
  autoRoutingEditorOpen.value = true
  loadingAutoRouting.value = true
  autoRoutingError.value = ''
  try {
    const config = await playgroundAPI.getAutoRouting(profile.id)
    if (!autoRoutingEditorOpen.value || selectedAutoProfile.value?.id !== profile.id) return
    autoRoutingConfig.value = config
    crossGroupRetry.value = Boolean(config.cross_group_retry && profile.cross_group_retry_allowed)
    autoSelectionMode.value = config.auto_selection_mode === 'custom' && profile.allow_custom_order
      ? 'custom'
      : 'inherit'
    const allowedGroupIDs = new Set(autoProfileGroups.value.map(group => group.id))
    autoGroupIds.value = config.auto_selection_mode === 'custom'
      ? (config.group_ids ?? []).filter(groupID => allowedGroupIDs.has(groupID))
      : autoInheritedGroups.value.map(group => group.id)
  } catch {
    autoRoutingConfig.value = null
    autoRoutingError.value = t('playground.autoRoutingLoadFailed')
  } finally {
    loadingAutoRouting.value = false
  }
}
function closeAutoRoutingEditor() {
  if (savingAutoRouting.value) return
  autoRoutingEditorOpen.value = false
  autoRoutingError.value = ''
}
function setAutoSelectionMode(mode: 'inherit' | 'custom') {
  if (mode === 'custom' && !selectedAutoProfile.value?.allow_custom_order) return
  autoSelectionMode.value = mode
  if (mode === 'custom' && autoGroupIds.value.length === 0 && selectedAutoProfile.value) {
    autoGroupIds.value = autoProfileGroups.value
      .slice(0, selectedAutoProfile.value.max_candidates)
      .map(group => group.id)
  }
  autoRoutingError.value = ''
}
function toggleAutoGroup(groupID: number, checked: boolean) {
  if (checked) {
    const profile = selectedAutoProfile.value
    if (!profile || autoGroupIds.value.length >= profile.max_candidates) return
    if (!autoGroupIds.value.includes(groupID)) autoGroupIds.value.push(groupID)
  } else {
    autoGroupIds.value = autoGroupIds.value.filter(id => id !== groupID)
  }
  autoRoutingError.value = ''
}
function moveAutoGroup(groupID: number, direction: -1 | 1) {
  const index = autoGroupIds.value.indexOf(groupID)
  const target = index + direction
  if (index < 0 || target < 0 || target >= autoGroupIds.value.length) return
  const next = [...autoGroupIds.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  autoGroupIds.value = next
  autoRoutingError.value = ''
}
async function saveAutoRouting() {
  const profile = selectedAutoProfile.value
  const config = autoRoutingConfig.value
  if (!profile || !config || !canSaveAutoRouting.value) return
  savingAutoRouting.value = true
  autoRoutingError.value = ''
  try {
    const updated = await playgroundAPI.updateAutoRouting(profile.id, {
      auto_selection_mode: autoSelectionMode.value,
      group_ids: autoSelectionMode.value === 'custom' ? [...autoGroupIds.value] : [],
      cross_group_retry: selectedAutoProfile.value.cross_group_retry_allowed && crossGroupRetry.value,
      routing_revision: config.routing_revision
    })
    autoRoutingConfig.value = updated
    crossGroupRetry.value = Boolean(updated.cross_group_retry && profile.cross_group_retry_allowed)
    autoSelectionMode.value = updated.auto_selection_mode
    autoGroupIds.value = updated.auto_selection_mode === 'custom'
      ? [...updated.group_ids]
      : autoInheritedGroups.value.map(group => group.id)
    appStore.showSuccess(t('playground.autoRoutingSaved'))
    autoRoutingEditorOpen.value = false
    void loadModels(requestRouteFor(activeRouteSource.value), activeTab.value)
  } catch {
    autoRoutingError.value = t('playground.autoRoutingSaveFailed')
  } finally {
    savingAutoRouting.value = false
  }
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
function artifactsForMessage(message: StoredMessage) {
  const cached = artifactCache.get(message.id)
  if (cached?.content === message.content) return cached.artifacts
  const artifacts = extractPlaygroundArtifacts(message.content)
  artifactCache.set(message.id, { content: message.content, artifacts })
  return artifacts
}
function formatExpiry(expiry: number) { return t('playground.expires', { time: new Date(expiry).toLocaleString() }) }
function remaining(expiry: number) {
  const seconds = Math.max(0, Math.ceil((expiry - now.value) / 1000))
  return t('playground.remaining', { time: `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` })
}
function requestContext(): RequestContext {
  return { accountId: accountId.value, epoch: accountEpoch, conversationId: current.value.id }
}
function isCurrentAccount(context: RequestContext) {
  return !disposed && context.epoch === accountEpoch && context.accountId === accountId.value
}
function isCurrentRequest(context: RequestContext) {
  return isCurrentAccount(context) && context.conversationId === current.value.id
}
function fileExtension(name: string) {
  const filename = name.toLowerCase()
  const dot = filename.lastIndexOf('.')
  return dot < 0 ? filename : filename.slice(dot + 1)
}
function displayAttachmentName(name: string) {
  const characters = Array.from(name)
  if (characters.length <= 120) return name
  const extensionStart = name.lastIndexOf('.')
  const extension = extensionStart > 0 ? Array.from(name.slice(extensionStart)).slice(-24).join('') : ''
  const prefix = characters.slice(0, 117 - Array.from(extension).length).join('')
  return `${prefix}...${extension}`
}
function imageMime(bytes: Uint8Array): string | null {
  if (bytes.length >= 8 && [137, 80, 78, 71, 13, 10, 26, 10].every((value, index) => bytes[index] === value)) return 'image/png'
  if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return 'image/jpeg'
  if (bytes.length >= 12 && String.fromCharCode(...bytes.subarray(0, 4)) === 'RIFF' && String.fromCharCode(...bytes.subarray(8, 12)) === 'WEBP') return 'image/webp'
  return null
}
function bytesToDataUrl(bytes: Uint8Array, mime: string) {
  let binary = ''
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000))
  }
  return `data:${mime};base64,${btoa(binary)}`
}
async function readAttachment(file: File): Promise<PlaygroundAttachment> {
  const extension = fileExtension(file.name)
  if (imageAttachmentExtensions.has(extension)) {
    const expectedMime = extension === 'png' ? 'image/png' : extension === 'webp' ? 'image/webp' : 'image/jpeg'
    const bytes = new Uint8Array(await file.arrayBuffer())
    const detectedImageMime = imageMime(bytes)
    if (detectedImageMime !== expectedMime || (file.type && file.type !== expectedMime)) {
      throw new Error(t('playground.attachmentImageInvalid', { name: file.name }))
    }
    return { id: createPlaygroundId(), name: displayAttachmentName(file.name), kind: 'image', dataUrl: bytesToDataUrl(bytes, expectedMime), size: bytes.byteLength }
  }
  if (!textAttachmentExtensions.has(extension)) {
    throw new Error(t('playground.attachmentUnsupported', { name: file.name }))
  }
  const bytes = new Uint8Array(await file.arrayBuffer())
  let text: string
  try { text = new TextDecoder('utf-8', { fatal: true }).decode(bytes) }
  catch { throw new Error(t('playground.attachmentInvalidText', { name: file.name })) }
  if (Array.prototype.some.call(text, (character: string) => {
    const code = character.charCodeAt(0)
    return (code < 32 && ![9, 10, 13].includes(code)) || (code >= 127 && code <= 159)
  })) {
    throw new Error(t('playground.attachmentInvalidText', { name: file.name }))
  }
  return { id: createPlaygroundId(), name: displayAttachmentName(file.name), kind: 'text', text, size: bytes.byteLength }
}
function handleAttachmentSelection(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  addAttachments(files)
}
function onPasteAttachments(event: ClipboardEvent) {
  const clipboard = event.clipboardData
  if (!clipboard) return

  const files = Array.from(clipboard.files)
  if (!files.length) {
    for (const item of Array.from(clipboard.items ?? [])) {
      if (item.kind !== 'file') continue
      const file = item.getAsFile()
      if (file) files.push(file)
    }
  }
  if (!files.length) return

  event.preventDefault()
  addAttachments(files)
}
function addAttachments(files: File[]) {
  if (!files.length) return
  const context = requestContext()
  attachmentBatchCount += 1
  loadingAttachments.value = true
  attachmentQueue = attachmentQueue.then(async () => {
    if (!isCurrentRequest(context)) return
    let rejection = ''
    for (const file of files) {
      if (!isCurrentRequest(context)) return
      const totalBytes = attachments.value.reduce((total, item) => total + item.size, 0)
      const limitError = playgroundAttachmentLimitError(attachments.value.length + 1, totalBytes + file.size)
      if (limitError) {
        rejection ||= t(limitError)
        continue
      }
      try {
        const attachment = await readAttachment(file)
        if (!isCurrentRequest(context)) return
        attachments.value.push(attachment)
      } catch (caught) {
        rejection ||= caught instanceof Error ? caught.message : t('playground.requestFailed')
      }
    }
    if (isCurrentRequest(context)) attachmentError.value = rejection
  }).finally(() => {
    attachmentBatchCount = Math.max(0, attachmentBatchCount - 1)
    if (isCurrentRequest(context)) loadingAttachments.value = attachmentBatchCount > 0
  })
}
function onDragEnter(event: DragEvent) {
  if (!event.dataTransfer?.types.includes('Files')) return
  dragDepth += 1
  draggingFiles.value = true
}
function onDragLeave() {
  dragDepth = Math.max(0, dragDepth - 1)
  if (dragDepth === 0) draggingFiles.value = false
}
function onDropFiles(event: DragEvent) {
  dragDepth = 0
  draggingFiles.value = false
  addAttachments(Array.from(event.dataTransfer?.files ?? []))
}
function removeAttachment(id: string) {
  attachments.value = attachments.value.filter(attachment => attachment.id !== id)
}
function removeAttachments(ids: string[]) {
  if (!ids.length) return
  const consumed = new Set(ids)
  attachments.value = attachments.value.filter(attachment => !consumed.has(attachment.id))
  attachmentError.value = ''
}
function expandedUserContent(prompt: string, snapshot: PlaygroundAttachment[], kind: PlaygroundKind) {
  const text = prompt || t(kind === 'image' ? 'playground.editAttachedImage' : 'playground.attachmentOnlyPrompt')
  const textSections = snapshot
    .filter((attachment): attachment is TextAttachment => attachment.kind === 'text')
    .map(attachment => `\n\n--- ${attachment.name} ---\n${attachment.text}`)
  const imageReferences = snapshot
    .filter((attachment): attachment is ImageAttachment => attachment.kind === 'image')
    .map(attachment => `\n\n[Image: ${attachment.name}]`)
  return `${text}${textSections.join('')}${imageReferences.join('')}`
}
function historyLimitError(conversation: PlaygroundConversation) {
  if (conversation.messages.some(message => new TextEncoder().encode(message.content).byteLength > maxMessageBytes)) {
    return t('playground.historyMessageTooLarge')
  }
  if (conversation.messages.length > 200 || new TextEncoder().encode(JSON.stringify(conversation)).byteLength > maxConversationBytes) {
    return t('playground.historyConversationTooLarge')
  }
  return ''
}
// 流式输出时只在用户停留在底部附近才跟随滚动，便于边看边往上翻
let followFrame = 0
let stickToBottom = true
async function scrollBottom() { stickToBottom = true; await nextTick(); if (viewport.value) viewport.value.scrollTop = viewport.value.scrollHeight }
function onViewportScroll() {
  const el = viewport.value
  if (el) stickToBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80
}
function followBottom() {
  if (!pageActive || !viewport.value || followFrame || !stickToBottom) return
  followFrame = requestAnimationFrame(() => {
    followFrame = 0
    if (viewport.value) viewport.value.scrollTop = viewport.value.scrollHeight
  })
}
function newConversation() {
  if (busy.value) return
  const { groupId, routing, model, imageModel } = current.value
  current.value = { ...emptyConversation(), groupId, routing: routing ? { ...routing } : { mode: 'fixed' }, model, imageModel }
  acceptedRevisions.clear()
  acceptedRevisions.set(current.value.id, current.value.revision)
  imageReferenceId.value = ''
  attachmentError.value = ''
  draft.value = ''; attachments.value = []; loadingAttachments.value = false; error.value = ''; saveError.value = ''; historyOpen.value = false
}
function releaseImages(entries: ImageEntry[]) {
  entries.filter(image => image.objectUrl).forEach(image => URL.revokeObjectURL(image.url))
}
function abortActiveRequests() {
  for (const chatController of chatControllers) chatController.abort()
  for (const imageController of imageControllers) imageController.abort()
}
async function restoreImages(conversationId: string) {
  const context = requestContext()
  const restoredAccountId = context.accountId
  if (restoredAccountId === null) return
  let cached
  try { cached = await playgroundImageCache.listConversation(restoredAccountId, conversationId) }
  catch {
    if (isCurrentRequest(context)) cacheError.value = t('playground.imageCacheRestoreFailed')
    return
  }
  if (!isCurrentRequest(context)) return
  const existing = images.value.filter(image => image.conversationId === conversationId)
  releaseImages(existing)
  images.value = images.value.filter(image => image.conversationId !== conversationId)
  images.value.push(...cached.filter(image => !image.expiresAt || image.expiresAt > Date.now()).map(image => ({
    id: image.id,
    conversationId: image.conversationId,
    messageId: image.messageId,
    url: URL.createObjectURL(image.blob),
    objectUrl: true,
    prompt: image.prompt,
    expiresAt: image.expiresAt ?? 0
  })))
}
async function cacheGeneratedImage(image: Omit<ImageEntry, 'objectUrl'>, context = requestContext()): Promise<ImageEntry> {
  const imageAccountId = context.accountId
  if (imageAccountId === null) return image
  const blob = await playgroundImageBlob(image.url)
  if (!isCurrentRequest(context)) return image
  await playgroundImageCache.put({ ...image, accountId: String(imageAccountId), blob })
  if (!isCurrentRequest(context)) return image
  return { ...image, url: URL.createObjectURL(blob), objectUrl: true }
}
function openWorkflowTab() {
  if (!workflowPresets.value.groupId && workflowPresets.value.routing?.mode !== 'auto') {
    workflowPresets.value = {
      groupId: current.value.groupId,
      routing: current.value.routing ? { ...current.value.routing } : { mode: 'fixed' },
      chatModel: current.value.model,
      imageModel: current.value.imageModel || ''
    }
  }
  activeTab.value = 'workflow'
}
function applyWorkflowPresets(value: { groupId: number; routing?: ConversationRouting; chatModel: string; imageModel: string }) {
  workflowPresets.value = { ...value, routing: value.routing ?? { mode: 'fixed' } }
  saveRoutePreference(workflowPresets.value)
}
function saveLastConversation(account: number, conversationId: string) {
  try {
    localStorage.setItem(`playground-last-conversation:${account}`, conversationId)
    return true
  } catch {
    return false
  }
}
function removeLastConversation(account: number, conversationId: string) {
  try {
    const key = `playground-last-conversation:${account}`
    if (localStorage.getItem(key) === conversationId) localStorage.removeItem(key)
    return true
  } catch {
    return false
  }
}
function loadLastConversation(account: number) {
  try {
    return localStorage.getItem(`playground-last-conversation:${account}`)
  } catch {
    return null
  }
}
function openConversation(item: PlaygroundConversation) {
  if (busy.value) return
  current.value = JSON.parse(JSON.stringify(item)) as PlaygroundConversation
  current.value.routing ??= { mode: 'fixed' }
  acceptedRevisions.clear()
  acceptedRevisions.set(current.value.id, current.value.revision)
  reseedMessageId(current.value.messages)
  imageReferenceId.value = ''
  attachmentError.value = ''
  draft.value = ''; attachments.value = []; loadingAttachments.value = false; error.value = ''; saveError.value = ''; historyOpen.value = false
  if (accountId.value !== null) saveLastConversation(accountId.value, item.id)
  void restoreImages(item.id)
  scrollBottom()
}
async function refreshHistory(context = requestContext()) {
  void playgroundHistory.quota().then(quota => {
    if (isCurrentAccount(context)) storageLimitMB.value = quota.limitMB
  }).catch(() => {})
  try {
    const history = await playgroundHistory.list()
    if (isCurrentAccount(context)) conversations.value = history.sort((first, second) => second.expiresAt - first.expiresAt)
  } catch {
    if (isCurrentAccount(context)) appStore.showError(t('playground.historyFailed'))
  }
}
async function saveConversation(snapshot = JSON.parse(JSON.stringify(current.value)) as PlaygroundConversation, context?: RequestContext) {
  if (!snapshot.messages.length) return true
  const sizeError = historyLimitError(snapshot)
  if (sizeError) { error.value = sizeError; return false }
  const task = saveQueue.then(async () => {
    if (context && !isCurrentRequest(context)) return false
    saving.value = true
    try {
      snapshot.revision = acceptedRevisions.get(snapshot.id) ?? snapshot.revision
      const saved = await playgroundHistory.save(snapshot)
      if (context && !isCurrentRequest(context)) return false
      if (disposed) return false
      acceptedRevisions.set(saved.id, saved.revision)
      if (current.value.id === saved.id) {
        current.value.revision = saved.revision; current.value.expiresAt = saved.expiresAt
        if (accountId.value !== null) saveLastConversation(accountId.value, saved.id)
      }
      conversations.value = [saved, ...conversations.value.filter(item => item.id !== saved.id)]
      saveError.value = ''
      return true
    } catch (caught) {
      if (!context || isCurrentRequest(context)) {
        saveError.value = t((caught as { status?: number } | null)?.status === 413
          ? 'playground.storageFull'
          : 'playground.saveFailed')
      }
      return false
    }
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
    const deletedImages = images.value.filter(image => image.conversationId === id)
    releaseImages(deletedImages)
    images.value = images.value.filter(image => image.conversationId !== id)
    if (accountId.value !== null) {
      await playgroundImageCache.deleteConversation(accountId.value, id).catch(() => { cacheError.value = t('playground.imageCacheDeleteFailed') })
      removeLastConversation(accountId.value, id)
    }
    acceptedRevisions.delete(id)
    if (current.value.id === id) newConversation()
    deleteTarget.value = ''
  } catch { appStore.showError(t('playground.historyFailed')) }
}
async function loadModels(route: PlaygroundRequestRoute | null, target: 'standard' | 'workflow') {
  const request = ++modelRequest
  const preferred = target === 'workflow' ? workflowPresets.value : {
    groupId: current.value.groupId,
    routing: current.value.routing,
    chatModel: current.value.model,
    imageModel: current.value.imageModel || ''
  }
  models.value = []; loadingModels.value = true
  try {
    const result = route ? await playgroundAPI.listModels(route) : []
    if (request !== modelRequest) return
    models.value = result
    const chatModel = chatModels.value.some(item => item.id === preferred.chatModel) ? preferred.chatModel : (chatModels.value[0]?.id ?? '')
    const imageModel = imageModels.value.some(item => item.id === preferred.imageModel) ? preferred.imageModel : (imageModels.value[0]?.id ?? '')
    if (target === 'workflow') {
      workflowPresets.value = { ...workflowPresets.value, chatModel, imageModel }
      saveRoutePreference(workflowPresets.value)
    }
    else {
      current.value.model = chatModel; current.value.imageModel = imageModel
      saveRoutePreference({
        groupId: current.value.groupId,
        routing: current.value.routing,
        chatModel,
        imageModel
      })
    }
  } catch { if (request === modelRequest) {
    if (target === 'workflow') workflowPresets.value = { ...workflowPresets.value, chatModel: '', imageModel: '' }
    else { current.value.model = ''; current.value.imageModel = '' }
    error.value = t('playground.loadModelsFailed')
  } }
  finally { if (request === modelRequest) loadingModels.value = false }
}
function startEdit(message: StoredMessage) {
  if (busy.value || submitting.value) return
  editingMessageId.value = message.id
  // 附件展开的内容（文本附件正文、图片标记）不放进编辑框
  editingText.value = message.content.replace(/\n\n--- [^\n]+ ---\n[\s\S]*$/, '').replace(/(\n\n\[Image: [^\]]+\])+$/, '')
  void nextTick(() => {
    const input = document.querySelector<HTMLTextAreaElement>('.user-edit-input')
    input?.focus()
    input?.setSelectionRange(input.value.length, input.value.length)
  })
}
function cancelEdit() {
  editingMessageId.value = null
  editingText.value = ''
}
function submitEdit() {
  if (editingMessageId.value === null) return
  void send({ fromId: editingMessageId.value, content: editingText.value })
}
function onEditEnter(event: KeyboardEvent) {
  if (!event.isComposing && !window.matchMedia?.('(pointer: coarse)').matches) {
    event.preventDefault()
    submitEdit()
  }
}
function onEnter(event: KeyboardEvent) {
  if (!event.isComposing && !window.matchMedia?.('(pointer: coarse)').matches) {
    event.preventDefault()
    void send()
  }
}
function openMobileHistory() {
  activeTab.value = 'standard'
  historyOpen.value = !historyOpen.value
}
function newMobileChat() {
  activeTab.value = 'standard'
  newConversation()
}
function selectImageReference(image: ImageEntry) {
  imageReferenceId.value = image.id
  imageMode.value = 'image'
  composerInput.value?.focus()
}
function isImageRevisionPrompt(prompt: string): boolean {
  return /(?:再来|再生成|重画|重新画|换成|改成|换个|换一|改一|更写实|更真实|更亮|更暗|加上|去掉|去除|背景|颜色|构图|画风|风格|分辨率)|\b(?:another|redraw|regenerate|brighter|darker|background|colou?r|style|replace|remove|add)\b/i.test(prompt)
}
// edit：重新编辑某条已发送的用户消息——替换该消息并丢弃其后的回复，再重新生成
async function send(edit?: { fromId: StoredMessage['id']; content: string }) {
  if (!hasBalance.value) { error.value = t('playground.insufficientBalance'); return }
  if (edit
    ? (!edit.content.trim() || busy.value || submitting.value || loadingModels.value || !currentRequestRoute.value)
    : (!canSend.value || submitting.value)) return
  if (current.value.expiresAt && current.value.expiresAt <= Date.now()) { newConversation(); error.value = t('playground.expired'); return }
  const prompt = edit ? edit.content.trim() : draft.value.trim()
  const attachmentSnapshot = edit ? [] : attachments.value.map(attachment => ({ ...attachment }))
  const imageSnapshot = attachmentSnapshot.filter((attachment): attachment is ImageAttachment => attachment.kind === 'image')
  const textSnapshot = attachmentSnapshot.filter((attachment): attachment is TextAttachment => attachment.kind === 'text')
  const context = requestContext()
  const requestedImageCount = resolvePlaygroundImageCount('', imageCount.value)
  const conversation = current.value
  const route = currentRequestRoute.value
  if (!route) { error.value = t('playground.selectGroup'); return }
  const editIndex = edit ? conversation.messages.findIndex(message => message.id === edit.fromId && message.role === 'user') : -1
  if (edit && editIndex < 0) return
  const messagesBefore = conversation.messages
  const baseMessages = edit ? conversation.messages.slice(0, editIndex) : conversation.messages
  const previousIntent = [...baseMessages].reverse().find(message => message.role === 'assistant')?.kind
  const kind = resolvePlaygroundIntent(prompt, previousIntent, imageMode.value)
  if ((imageMode.value === 'image' || kind === 'image') && textSnapshot.length) {
    error.value = t('playground.attachmentImageOnly')
    return
  }
  const model = kind === 'image' ? conversation.imageModel : conversation.model
  if (!model) { error.value = t(kind === 'image' ? 'playground.noImageModel' : 'playground.noChatModel'); return }
  const modelCapability = models.value.find(item => item.id === model)
  const implicitReference = kind === 'image' && !imageSnapshot.length && previousIntent === 'image' && isImageRevisionPrompt(prompt)
    ? [...baseMessages].reverse().find(message => message.role === 'assistant' && currentImages.value.some(image => image.messageId === message.id))
    : undefined
  const referenceImage = kind === 'image'
    ? (edit ? undefined : selectedImageReference.value) ?? currentImages.value.find(image => image.messageId === implicitReference?.id)
    : undefined
  let referenceBlob: Blob | undefined
  const endpoint = kind === 'image'
    ? imageSnapshot.length || referenceImage ? 'images_edit' : 'images_generation'
    : 'chat'
  const fallbackCapability = kind === 'image' ? isPlaygroundImageModel(model) : !isPlaygroundImageModel(model) && !isPlaygroundVideoModel(model)
  if (!playgroundModelSupportsEndpoint(modelCapability ?? { id: model }, endpoint, fallbackCapability)) {
    error.value = t('playground.modelEndpointUnsupported')
    return
  }
  const systemPrompt = conversation.systemPrompt
  const temperature = conversation.temperature
  const priorImagePrompt = !referenceImage && previousIntent === 'image' && isImageRevisionPrompt(prompt)
    ? baseMessages
      .filter(message => message.role === 'user' && message.kind === 'image')
      .map(message => message.content.replace(/\n\n\[Image: [^\]]+\]/g, ''))
      .slice(-4)
      .join('\n\n')
    : ''
  if (kind === 'image' && requestedImageCount.error) {
    error.value = t(requestedImageCount.error === 'too_many' ? 'playground.imageCountTooMany' : 'playground.imageCountInvalid')
    return
  }
  const userContent = expandedUserContent(prompt, attachmentSnapshot, kind)
  const userBytes = new TextEncoder().encode(userContent).byteLength
  if (userBytes > maxMessageBytes) { error.value = t('playground.historyMessageTooLarge'); return }
  const title = conversation.title || userContent.slice(0, 60)
  conversation.routing = route.mode === 'auto'
    ? { mode: 'auto', autoProfileId: route.autoProfileId }
    : { mode: 'fixed' }
  const userId = createMessageId()
  const conversationId = conversation.id
  const userMessage: StoredMessage = { id: userId, role: 'user', content: userContent, model, kind }
  const initialSnapshot = JSON.parse(JSON.stringify({
    ...conversation,
    title,
    messages: [...baseMessages, userMessage]
  })) as PlaygroundConversation
  const sizeError = historyLimitError(initialSnapshot)
  if (sizeError) { error.value = sizeError; return }
  const history: Array<PlaygroundMessage | PlaygroundMultimodalMessage> = initialSnapshot.messages
    .filter(message => message.role !== 'assistant' || message.content.trim())
    .map(message => ({ role: message.role, content: message.content }))
  if (imageSnapshot.length && kind === 'chat') {
    history[history.length - 1] = {
      role: 'user',
      content: [
        { type: 'text', text: userContent },
        ...imageSnapshot.map(image => ({ type: 'image_url' as const, image_url: { url: image.dataUrl } }))
      ]
    }
  }
  const outgoingSystemPrompt = [systemPrompt, artifactGenerationInstruction].filter(Boolean).join('\n\n')
  const chatContext = kind === 'chat' ? selectPlaygroundContext(history, outgoingSystemPrompt, modelCapability) : null
  if (kind === 'chat' && !chatContext) {
    error.value = t('playground.contextTooLarge')
    return
  }
  if (referenceImage) {
    submitting.value = true
    try {
      referenceBlob = await playgroundImageBlob(referenceImage.url)
      if (!isCurrentRequest(context)) return
      const limitError = playgroundAttachmentLimitError(
        attachmentSnapshot.length + 1,
        attachmentSnapshot.reduce((total, attachment) => total + attachment.size, referenceBlob.size)
      )
      if (limitError) { error.value = t(limitError); return }
    } catch {
      if (isCurrentRequest(context)) error.value = t('playground.imageUnavailable')
      return
    } finally {
      if (isCurrentRequest(context)) submitting.value = false
    }
  }
  if (edit) conversation.messages = [...baseMessages, userMessage]
  else conversation.messages.push(userMessage)
  conversation.title = title
  submitting.value = true
  let initialSaveSucceeded = false
  try {
    initialSaveSucceeded = await saveConversation(initialSnapshot, context)
  } finally {
    if (isCurrentRequest(context)) submitting.value = false
  }
  if (!initialSaveSucceeded || !isCurrentRequest(context)) {
    if (isCurrentRequest(context)) {
      conversation.messages = edit ? messagesBefore : conversation.messages.filter(message => message.id !== userId)
    }
    return
  }
  if (edit) {
    editingMessageId.value = null
  } else {
    draft.value = ''
    imageReferenceId.value = ''
    removeAttachments(attachmentSnapshot.map(attachment => attachment.id))
  }
  error.value = ''
  if (kind === 'image') {
    generatingCount.value += 1
    const generationPromptText = prompt || t('playground.editAttachedImage')
    const generationPrompt = priorImagePrompt ? `${priorImagePrompt}\n\n${generationPromptText}` : generationPromptText
    conversation.messages.push({ id: createMessageId(), role: 'assistant', content: '', model, kind })
    const assistantId = conversation.messages[conversation.messages.length - 1].id
    pendingImageIds.value.push(assistantId)
    const imageController = new AbortController()
    imageControllers.add(imageController)
    try {
      const deliveredImageURLs = new Set<string>()
      const saveImage = async (image: PlaygroundImage) => {
        if (!isCurrentRequest(context)) return
        const generatedImage: ImageEntry = { id: createPlaygroundId(), conversationId, messageId: assistantId, url: image.url, prompt: generationPrompt, expiresAt: Date.now() + 600000 }
        if (deliveredImageURLs.has(generatedImage.url)) return
        deliveredImageURLs.add(generatedImage.url)
        images.value.push(generatedImage)
        const cached = await cacheGeneratedImage(generatedImage, context).catch(() => null)
        if (!isCurrentRequest(context)) return
        if (!cached) { cacheError.value = t('playground.imageCacheFailed'); return }
        images.value = [...images.value.filter(image => image.id !== generatedImage.id), cached]
      }
      let generated: PlaygroundImage[]
      let generationError: unknown
      try {
        const inputImages = imageSnapshot.map(image => image.dataUrl)
        if (referenceBlob) {
          const bytes = new Uint8Array(await referenceBlob.arrayBuffer())
          inputImages.push(bytesToDataUrl(bytes, referenceBlob.type))
        }
        if (!isCurrentRequest(context)) return
        generated = await playgroundAPI.generateImages({
          route,
          model,
          prompt: generationPrompt,
          size: imageSize.value,
          quality: imageQuality.value,
          count: requestedImageCount.count,
          ...(inputImages.length ? { inputImages } : {}),
          signal: imageController.signal,
          onImage: saveImage
        })
      } catch (caught) {
        if (caught instanceof PlaygroundImageGenerationError && caught.images.length) {
          generated = caught.images
          generationError = caught
        } else throw caught
      }
      if (!isCurrentRequest(context)) return
      if (!generated.length) throw new Error(t('playground.imageFailed'))
      for (const image of generated) await saveImage(image)
      const assistant = conversation.messages.find(message => message.id === assistantId)
      if (assistant) assistant.content = t('playground.imagesGenerated', { count: deliveredImageURLs.size })
      if (generationError) error.value = (generationError as PlaygroundImageGenerationError).status === 402
        ? t('playground.insufficientBalance')
        : (generationError as Error).message
    } catch (caught) {
      if (!isCurrentRequest(context)) return
      if (!images.value.some(image => image.messageId === assistantId)) {
        conversation.messages = conversation.messages.filter(message => message.id !== assistantId)
      }
      error.value = (caught as { status?: number } | null)?.status === 402
        ? t('playground.insufficientBalance')
        : caught instanceof Error ? caught.message : t('playground.imageFailed')
    }
    finally {
      imageControllers.delete(imageController)
      if (isCurrentRequest(context)) {
        pendingImageIds.value = pendingImageIds.value.filter(id => id !== assistantId)
        generatingCount.value -= 1
      }
    }
  } else if (chatContext) {
    const assistantId = createMessageId()
    conversation.messages.push({ id: assistantId, role: 'assistant', content: '', model, kind })
    const chatController = new AbortController()
    chatControllers.add(chatController)
    streamingCount.value += 1
    // 增量先进缓冲，再按动画帧匀速写入，避免"一顿一顿"地蹦字
    const typer = createStreamTyper((chunk) => {
      if (!isCurrentRequest(context)) return
      const assistant = conversation.messages.find(message => message.id === assistantId)
      if (assistant) assistant.content += chunk
      if (current.value.id === conversationId) followBottom()
    })
    try {
      await playgroundAPI.streamChat({
        route,
        model,
        messages: chatContext,
        temperature,
        signal: chatController.signal,
        onDelta(delta) {
          if (!isCurrentRequest(context)) return
          typer.push(delta)
          if (!pageActive) typer.flush()
        }
      })
      await typer.finish()
    } catch (caught) {
      typer.flush()
      if (isCurrentRequest(context) && (caught as Error).name !== 'AbortError') {
        error.value = caught instanceof Error ? caught.message : t('playground.requestFailed')
      }
    } finally {
      if (!conversation.messages.find(message => message.id === assistantId)?.content) {
        conversation.messages = conversation.messages.filter(message => message.id !== assistantId)
      }
      chatControllers.delete(chatController)
      if (isCurrentAccount(context)) {
        streamingCount.value = Math.max(0, streamingCount.value - 1)
      }
    }
  }
  if (!isCurrentRequest(context)) return
  await saveConversation(JSON.parse(JSON.stringify(conversation)) as PlaygroundConversation, context)
  if (!isCurrentRequest(context)) return
  await authStore.refreshUser().catch(() => {})
  if (isCurrentRequest(context)) scrollBottom()
}
async function copyMessage(content: string) {
  try { await navigator.clipboard.writeText(content); appStore.showSuccess(t('playground.copied')) }
  catch { appStore.showError(t('playground.copyFailed')) }
}
async function downloadImage(image: ImageEntry) {
  if (!image.objectUrl && image.expiresAt <= Date.now()) return
  try {
    const result = await fetch(image.url)
    if (!result.ok) throw new Error('download')
    const blob = await result.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = `playground-${image.id}.${playgroundImageExtension(blob.type)}`; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch { appStore.showError(t('playground.downloadFailed')) }
}
watch(
  () => [activeTab.value, selectedRouteValue.value] as const,
  ([target]) => { void loadModels(requestRouteFor(activeRouteSource.value), target) }
)
watch(
  () => [current.value.groupId, current.value.routing?.mode, current.value.routing?.autoProfileId, current.value.model, current.value.imageModel || ''] as const,
  ([groupId, mode, autoProfileId, chatModel, imageModel]) => savePlaygroundPreferences(accountId.value, {
    groupId,
    routing: mode === 'auto' ? { mode, autoProfileId } : { mode: 'fixed' },
    chatModel,
    imageModel,
    imageMode: imageMode.value
  })
)
watch(() => current.value.id, () => cancelEdit())
watch(imageMode, (value) => {
  const preferences = loadPlaygroundPreferences(accountId.value)
  savePlaygroundPreferences(accountId.value, { ...preferences, imageMode: value })
})
watch(accountId, (account, previousAccount) => {
  if (account === previousAccount) return
  routeOptionsRequest += 1
  accountEpoch += 1
  abortActiveRequests()
  modelRequest += 1
  releaseImages(images.value)
  images.value = []
  preview.value = null
  artifactCache.clear()
  current.value = emptyConversation()
  conversations.value = []
  acceptedRevisions.clear()
  draft.value = ''
  attachments.value = []
  attachmentError.value = ''
  imageReferenceId.value = ''
  storageLimitMB.value = 50
  loadingAttachments.value = false
  streamingCount.value = 0
  generatingCount.value = 0
  pendingImageIds.value = []
  submitting.value = false
  saving.value = false
  models.value = []
  loadingModels.value = false
  error.value = ''
  saveError.value = ''
  cacheError.value = ''
  workflowPresets.value = { groupId: 0, routing: { mode: 'fixed' }, chatModel: '', imageModel: '' }
  if (account === null) {
    groups.value = []
    autoProfiles.value = []
    loadingGroups.value = false
    loadingProfiles.value = false
    return
  }
  void initializeRouteOptions(account)
})
watch(() => props.mobile, syncMobileViewport)
onActivated(() => {
  pageActive = true
  syncMobileViewport()
  followBottom()
})
onDeactivated(() => {
  pageActive = false
  syncMobileViewport()
  preview.value = null
  autoRoutingEditorOpen.value = false
  deleteTarget.value = ''
})
onMounted(async () => {
  syncMobileViewport()
  timer = setInterval(() => {
    now.value = Date.now()
    const expiredImages = images.value.filter(image => image.expiresAt && image.expiresAt <= now.value)
    releaseImages(expiredImages)
    images.value = images.value.filter(image => !image.expiresAt || image.expiresAt > now.value)
    if (expiredImages.length) void playgroundImageCache.deleteExpired(now.value).catch(() => {})
    if (preview.value?.expiresAt && preview.value.expiresAt <= now.value) preview.value = null
    conversations.value = conversations.value.filter(item => item.expiresAt > now.value)
    if (!busy.value && current.value.expiresAt && current.value.expiresAt <= now.value) { newConversation(); error.value = t('playground.expired') }
  }, 1000)
  void Promise.resolve(playgroundImageCache.deleteExpired(Date.now())).catch(() => {})
  await initializeRouteOptions(accountId.value)
})
onBeforeUnmount(() => { window.visualViewport?.removeEventListener('resize', updateMobileViewport); disposed = true; abortActiveRequests(); clearInterval(timer); releaseImages(images.value); images.value = []; preview.value = null })
</script>

<style scoped>
.studio { position:relative; display:flex; height:calc(100dvh - 8rem); min-height:480px; overflow:hidden; border:1px solid #e5e7eb; border-radius:8px; background:#fff; color:#1f2937; }
.history-panel { display:flex; flex-direction:column; width:224px; flex-shrink:0; padding:20px 12px; border-right:1px solid #e5e7eb; background:#f8f9fb; }
.history-scrim { display:none; }
.mobile-header { display:none; }
.toolbar-account { display:flex; flex-shrink:0; align-items:center; gap:12px; margin-left:auto; }
.studio-view-link { color:#0f766e; font-size:12px; font-weight:600; }
.studio-view-link:hover { text-decoration:underline; }
.studio-icon,.studio-send { display:grid; width:36px; height:36px; flex:0 0 36px; place-items:center; padding:0; border-radius:8px; }
.studio-icon:hover,.studio-icon[aria-expanded="true"] { background:#64748b20; }
.studio-icon:disabled { opacity:.45; }
.composer-retention { margin-top:8px; text-align:center; font-size:11px; line-height:1.4; color:#64748b; }
.history-item { display:flex; align-items:center; gap:4px; padding:12px 8px; border-radius:10px; }
.history-item:hover,.history-item.selected { background:#eaeef5; }
.retention-note { margin-top:20px; font-size:11px; line-height:1.8; color:#9ca3af; }
.conversation-panel { position:relative; display:flex; flex:1; min-width:0; flex-direction:column; }
.conversation-panel.is-dragging-files { background:#eef2ff; }
.drag-overlay { position:absolute; z-index:30; inset:12px; display:grid; place-items:center; pointer-events:none; border:2px dashed #6366f1; border-radius:18px; background:#6366f11a; color:#4f46e5; font-size:18px; font-weight:700; animation:playground-drop-in .16s ease-out; }
.request-status { color:#6366f1; font-size:12px; white-space:nowrap; }
.standard-playground { display:flex; flex:1; min-height:0; flex-direction:column; overflow:hidden; }
.studio-toolbar { display:flex; align-items:center; flex-wrap:wrap; gap:12px; padding:12px 18px; border-bottom:1px solid #e5e7eb; }
.toolbar-label { display:block; margin-bottom:4px; color:#9ca3af; font-size:10px; }
.toolbar-select { max-width:280px; background:transparent; font-size:13px; font-weight:500; outline-offset:3px; }
.toolbar-warning { max-width:180px; color:#b45309; font-size:11px; line-height:1.4; }
.playground-tabs { display:flex; gap:2px; padding:3px; border:1px solid #e5e7eb; border-radius:8px; background:#f8f9fb; }
.playground-tab { border-radius:5px; padding:5px 8px; color:#6b7280; font-size:12px; }
.playground-tab.active { background:#fff; color:#4f46e5; box-shadow:0 1px 2px #0000000d; }
.conversation-scroll { flex:1; min-height:0; overflow:auto; padding:32px; }
.welcome { display:flex; height:100%; min-height:280px; flex-direction:column; align-items:center; justify-content:center; text-align:center; }
.suggestion { border:1px solid #e5e7eb; border-radius:8px; padding:12px; text-align:left; font-size:13px; color:#6b7280; }
.suggestion:hover { background:#f8f9fb; }
.user-message { display:flex; flex-direction:column; align-items:flex-end; max-width:85%; min-width:0; }
.user-message .user-bubble { max-width:100%; }
.user-message-actions { margin-top:6px; padding-right:4px; }
.user-edit { width:min(640px,80vw); max-width:100%; }
.user-edit-input { width:100%; min-height:84px; border:1px solid #d1d5db; border-radius:16px; padding:12px 16px; resize:vertical; }
:global(html.dark .user-edit-input) { border-color:#475569; }
.user-edit-actions { display:flex; justify-content:flex-end; align-items:center; gap:16px; margin-top:8px; }
.user-bubble { max-width:85%; border-radius:18px 18px 4px 18px; padding:12px 18px; background:#f0f2f6; white-space:pre-wrap; overflow-wrap:anywhere; font-size:14px; line-height:1.8; }
.assistant-message { font-size:14px; line-height:1.9; overflow-wrap:anywhere; }
.composer-area { flex-shrink:0; padding:12px 32px 20px; }
.composer { border:1px solid #dfe3ea; border-radius:8px; transition:border-color .15s ease; }
.composer:focus-within { border-color:#0f766e; }
.composer-input { display:block; width:100%; resize:none; padding:16px 18px 8px; background:transparent; outline:none; font-size:14px; }
.composer-actions { display:flex; align-items:center; justify-content:flex-end; flex-wrap:wrap; gap:8px; padding:4px 12px 12px; }
.composer-tools { display:flex; flex:1 1 auto; align-items:center; flex-wrap:wrap; gap:4px; }
.message-entry { animation:playground-message-in .2s ease-out; }
.attachment-list { display:flex; flex-wrap:wrap; gap:8px; padding:12px 12px 0; }
.attachment-item { display:flex; align-items:center; gap:8px; width:min(100%,240px); min-width:0; border:1px solid #e5e7eb; border-radius:6px; padding:6px; }
.attachment-thumb { flex:0 0 40px; width:40px; height:40px; border-radius:4px; object-fit:cover; }
.attachment-file-icon { display:grid; flex:0 0 40px; width:40px; height:40px; place-items:center; border-radius:4px; background:#eef2ff; color:#4f46e5; font-size:10px; font-weight:700; }
.attachment-remove { display:grid; flex:0 0 28px; width:28px; height:28px; place-items:center; border-radius:4px; color:#9ca3af; font-size:18px; }
.attachment-remove:hover { background:#f3f4f6; color:#dc2626; }
.mode-button { border-radius:8px; padding:7px 10px; color:#9ca3af; font-size:12px; }
.mode-select { border-radius:8px; padding:7px 10px; background:#eef2ff; color:#4f46e5; font-size:12px; }
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
:global(html.dark .attachment-item) { border-color:#303747; }
:global(html.dark .attachment-file-icon) { background:#252e3f; color:#a5b4fc; }
:global(html.dark .attachment-remove:hover) { background:#303747; }
:global(html.dark .composer-input),:global(html.dark .toolbar-select),:global(html.dark .toolbar-warning) { color:#e5e7eb; }
:global(html.dark .mode-button) { color:#94a3b8; }
:global(html.dark .mode-select) { background:#252e3f; color:#a5b4fc; }
:global(html.dark .mode-indicator),:global(html.dark .playground-tab.active) { background:#252e3f; color:#a5b4fc; }
:global(html.dark .playground-tab),:global(html.dark .suggestion) { color:#cbd5e1; }
:global(html.dark .playground-markdown td),:global(html.dark .playground-markdown th) { border-color:#475569; }
:global(html.dark .toolbar-select option) { background:#151b26; color:#e5e7eb; }
:global(html.dark .composer-input::placeholder) { color:#64748b; }
:global(html.dark .drag-overlay) { color:#c7d2fe; background:#1e293bd9; border-color:#818cf8; }
:global(html.dark .request-status) { color:#a5b4fc; }
:global(html.dark .studio-view-link) { color:#5eead4; }
:global(html.dark .composer-retention) { color:#94a3b8; }
.mobile-studio {
  --mobile-background:#f7f6f2;
  --mobile-surface:#fffefa;
  --mobile-ink:#302d28;
  --mobile-muted:#716a5f;
  --mobile-line:#e1ddd4;
  --mobile-selection:#ede9e0;
  --mobile-accent:#a4472d;
  --mobile-accent-hover:#883a24;
  height:100dvh;
  min-height:0;
  border:0;
  border-radius:0;
}
:global(html.dark .mobile-studio) {
  --mobile-background:#242321;
  --mobile-surface:#302e2b;
  --mobile-ink:#f0ede5;
  --mobile-muted:#bcb4a6;
  --mobile-line:#4a463f;
  --mobile-selection:#3b3832;
  --mobile-accent:#af5034;
  --mobile-accent-hover:#9d432b;
}
.mobile-studio .mobile-header {
  display:grid;
  grid-template-columns:44px minmax(0,1fr) 44px;
  align-items:center;
  gap:8px;
  padding:max(8px,env(safe-area-inset-top)) max(14px,env(safe-area-inset-right)) 8px max(14px,env(safe-area-inset-left));
}
.mobile-conversation-picker { justify-self:center; max-width:100%; min-width:0; padding:4px 10px; border-radius:12px; }
.mobile-conversation-picker[aria-expanded="true"] { background:var(--mobile-selection); }
.mobile-conversation-title { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:15px; font-weight:600; }
.mobile-model-label { display:flex; align-items:center; justify-content:center; gap:4px; color:var(--mobile-muted); font-size:12px; }
.mobile-model-label svg { flex:none; }
.mobile-studio .studio-toolbar { display:none; flex-shrink:0; padding:8px 16px 12px; border-color:var(--mobile-line); max-height:38dvh; overflow:auto; }
.mobile-studio.settings-expanded .studio-toolbar { display:flex; }
.mobile-studio .studio-toolbar > label { flex:1 1 30%; min-width:92px; }
.mobile-studio .toolbar-select { width:100%; max-width:100%; font-size:16px; }
.mobile-studio .history-panel { display:none; }
.mobile-studio .history-panel.mobile-open { display:flex; position:absolute; inset:0 auto 0 0; width:min(320px,85%); z-index:20; padding:20px 16px max(20px,env(safe-area-inset-bottom)); border-color:var(--mobile-line); }
.mobile-studio .history-scrim { display:block; position:absolute; inset:0; z-index:15; background:#14110e66; }
.mobile-studio .conversation-scroll { padding:20px 20px 12px; overscroll-behavior:contain; }
.mobile-studio .welcome { min-height:0; }
.mobile-studio .welcome-title { font-family:Georgia,"Noto Serif SC",serif; font-size:clamp(26px,7vw,38px); font-weight:400; line-height:1.25; }
.mobile-studio .conversation-panel,.mobile-studio .studio-toolbar,.mobile-studio .conversation-scroll,.mobile-studio .composer-area,.mobile-studio .history-panel {
  background:var(--mobile-background);
  color:var(--mobile-ink);
}
.mobile-studio .user-bubble,.mobile-studio .history-item:hover,.mobile-studio .history-item.selected { background:var(--mobile-selection); }
.mobile-studio .user-bubble { border-radius:16px; padding:10px 14px; }
.mobile-studio .user-bubble,.mobile-studio .assistant-message { font-size:16px; line-height:1.65; }
.mobile-studio .composer-area { padding:8px max(12px,env(safe-area-inset-right)) max(10px,env(safe-area-inset-bottom)) max(12px,env(safe-area-inset-left)); }
.mobile-studio .composer,.mobile-studio .settings-panel { background:var(--mobile-surface); border:1px solid var(--mobile-line); border-radius:22px; }
.mobile-studio .composer:focus-within { border-color:var(--mobile-muted); }
.mobile-studio .settings-panel { max-height:30dvh; overflow:auto; }
.mobile-studio .composer-input { color:var(--mobile-ink); font-size:16px; padding:13px 15px 3px; }
.mobile-studio .composer-input::placeholder,.mobile-studio .composer-retention,.mobile-studio .retention-note,.mobile-studio .toolbar-label,.mobile-studio .studio-view-link { color:var(--mobile-muted); }
.mobile-studio .studio-icon,.mobile-studio .studio-send { width:44px; height:44px; flex-basis:44px; border-radius:50%; }
.mobile-studio .btn-primary { background:var(--mobile-accent); color:#fff; }
.mobile-studio .btn-primary:hover:not(:disabled) { background:var(--mobile-accent-hover); }
.mobile-studio .composer-actions { padding:2px 8px 7px; gap:4px; }
.mobile-studio .mode-select,.mobile-studio .mode-indicator { background:var(--mobile-selection); color:var(--mobile-ink); }
.mobile-studio .mode-select { min-height:40px; border-radius:20px; font-size:16px; }
.mobile-studio .attachment-list { flex-wrap:nowrap; overflow-x:auto; padding:8px 10px 0; }
.mobile-studio .attachment-item { flex:0 0 152px; border-color:var(--mobile-line); border-radius:10px; }
@keyframes playground-drop-in { from { opacity:0; transform:scale(.98); } to { opacity:1; transform:scale(1); } }
@keyframes playground-message-in { from { opacity:0; transform:translateY(6px); } to { opacity:1; transform:translateY(0); } }
@media(prefers-reduced-motion:reduce) { .drag-overlay,.request-status,.message-entry { animation:none; } .composer { transition:none; } .composer:focus-within { transform:none; } }
@media(max-width:1023px) { .history-panel { display:none; } .history-panel.mobile-open { display:flex; position:absolute; inset:0 auto 0 0; width:min(300px,85%); z-index:20; } .history-scrim { display:block; position:absolute; inset:0; z-index:15; background:#11182765; } }
@media(max-width:640px) { .studio:not(.mobile-studio) { height:calc(100dvh - 7rem); min-height:440px; } .studio-toolbar { padding:10px; gap:8px; } .playground-tabs { flex:0 0 auto; white-space:nowrap; } .studio-toolbar > label { flex:1 1 30%; min-width:92px; } .toolbar-select { width:100%; max-width:100%; } .toolbar-warning { flex-basis:100%; max-width:none; } .conversation-scroll { padding:18px 14px; } .composer-area { padding:8px 12px 14px; } .mode-button { padding:6px; } }
</style>
