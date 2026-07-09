<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="wide" @close="handleClose">
    <div v-if="user" class="space-y-4">
      <div class="flex flex-col gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700 sm:flex-row sm:items-center sm:justify-between">
        <div class="flex min-w-0 items-center gap-3">
          <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
            <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
          </div>
          <div class="min-w-0">
            <p class="truncate font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
            <p class="truncate text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p>
          </div>
        </div>
        <button
          type="button"
          class="btn btn-primary inline-flex items-center justify-center gap-1.5"
          :title="t('admin.users.createApiKeyHint')"
          :aria-label="t('admin.users.createApiKeyHint')"
          @click="openCreateDialog"
        >
          <Icon name="plus" size="sm" />
          {{ t('admin.users.createApiKey') }}
        </button>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <Icon name="refresh" size="xl" class="animate-spin text-primary-500" />
      </div>
      <div v-else-if="apiKeys.length === 0" class="py-8 text-center">
        <p class="text-sm text-gray-500">{{ t('admin.users.noApiKeys') }}</p>
      </div>
      <div v-else ref="scrollContainerRef" class="max-h-96 space-y-3 overflow-y-auto pr-1" @scroll="closeGroupSelector">
        <div v-for="key in apiKeys" :key="key.id" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <div class="mb-1 flex flex-wrap items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-white">{{ key.name }}</span>
                <span :class="['badge text-xs', key.status === 'active' ? 'badge-success' : 'badge-danger']">
                  {{ formatKeyStatus(key.status) }}
                </span>
              </div>
              <p class="truncate font-mono text-sm text-gray-500">{{ maskApiKey(key.key) }}</p>
            </div>
            <button
              type="button"
              class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              :title="t('admin.users.editApiKeyHint')"
              :aria-label="t('admin.users.editApiKeyHint')"
              :disabled="updatingKeyIds.has(key.id)"
              @click="openEditDialog(key)"
            >
              <Icon name="edit" size="sm" />
            </button>
          </div>

          <div class="mt-3 grid gap-3 text-xs text-gray-500 sm:grid-cols-2 lg:grid-cols-4">
            <div class="min-w-0">
              <span>{{ t('admin.users.group') }}:</span>
              <button
                :ref="(el) => setGroupButtonRef(key.id, el)"
                class="-mx-1 -my-0.5 inline-flex max-w-full cursor-pointer items-center gap-1 rounded-md px-1 py-0.5 align-middle transition-colors hover:bg-gray-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 disabled:cursor-not-allowed disabled:opacity-60 dark:hover:bg-dark-700"
                :title="t('admin.users.changeApiKeyGroupHint')"
                :aria-label="t('admin.users.changeApiKeyGroupHint')"
                :disabled="updatingKeyIds.has(key.id)"
                @click="openGroupSelector(key)"
              >
                <GroupBadge
                  v-if="key.group_id && key.group"
                  :name="key.group.name"
                  :platform="key.group.platform"
                  :subscription-type="key.group.subscription_type"
                  :rate-multiplier="key.group.rate_multiplier"
                  :peak-rate-enabled="key.group.peak_rate_enabled"
                  :peak-start="key.group.peak_start"
                  :peak-end="key.group.peak_end"
                  :peak-rate-multiplier="key.group.peak_rate_multiplier"
                />
                <span v-else class="italic text-gray-400">{{ t('admin.users.none') }}</span>
                <Icon v-if="updatingKeyIds.has(key.id)" name="refresh" size="xs" class="animate-spin text-primary-500" />
                <Icon v-else name="sort" size="xs" class="text-gray-400" :stroke-width="2" />
              </button>
            </div>
            <div>
              <span>{{ t('admin.users.apiKeyQuota') }}:</span>
              <span class="ml-1 font-medium text-gray-700 dark:text-gray-200">
                {{ key.quota > 0 ? `$${formatAmount(key.quota_used)} / $${formatAmount(key.quota)}` : t('admin.users.unlimited') }}
              </span>
            </div>
            <div>
              <span>{{ t('admin.users.extraQuota') }}:</span>
              <span class="ml-1 font-medium text-gray-700 dark:text-gray-200">
                {{ key.extra_quota > 0 ? `$${formatAmount(key.extra_quota_used)} / $${formatAmount(key.extra_quota)}` : t('admin.users.extraQuotaDisabled') }}
              </span>
            </div>
            <div>
              <span>{{ t('admin.users.apiKeyExpiresAt') }}:</span>
              <span class="ml-1 font-medium text-gray-700 dark:text-gray-200">
                {{ key.expires_at ? formatDateTime(key.expires_at) : t('admin.users.neverExpires') }}
              </span>
            </div>
          </div>

          <div class="mt-3 grid gap-2 text-xs text-gray-500 sm:grid-cols-3">
            <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700">
              <div class="flex items-center justify-between gap-2">
                <span>5h</span>
                <span class="font-medium text-gray-700 dark:text-gray-200">{{ formatRateWindow(key.usage_5h, key.rate_limit_5h) }}</span>
              </div>
            </div>
            <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700">
              <div class="flex items-center justify-between gap-2">
                <span>1d</span>
                <span class="font-medium text-gray-700 dark:text-gray-200">{{ formatRateWindow(key.usage_1d, key.rate_limit_1d) }}</span>
              </div>
            </div>
            <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700">
              <div class="flex items-center justify-between gap-2">
                <span>7d</span>
                <span class="font-medium text-gray-700 dark:text-gray-200">{{ formatRateWindow(key.usage_7d, key.rate_limit_7d) }}</span>
              </div>
            </div>
          </div>

          <div class="mt-3 flex flex-wrap gap-4 text-xs text-gray-500">
            <div>{{ t('admin.users.columns.created') }}: {{ formatDateTime(key.created_at) }}</div>
            <div v-if="key.last_used_at">{{ t('admin.users.columns.lastUsed') }}: {{ formatDateTime(key.last_used_at) }}</div>
          </div>
        </div>
      </div>
    </div>
  </BaseDialog>

  <BaseDialog
    :show="showKeyFormDialog"
    :title="editingKey ? t('admin.users.editApiKey') : t('admin.users.createApiKey')"
    width="wide"
    :z-index="100000030"
    @close="closeKeyFormDialog"
  >
    <form class="space-y-5" @submit.prevent="submitKeyForm">
      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <label class="input-label" for="admin-api-key-name">{{ t('admin.users.apiKeyName') }}</label>
          <input id="admin-api-key-name" v-model.trim="keyForm.name" type="text" class="input" maxlength="100" required />
        </div>
        <div>
          <label class="input-label" for="admin-api-key-status">{{ t('admin.users.apiKeyStatus') }}</label>
          <select id="admin-api-key-status" v-model="keyForm.status" class="input">
            <option value="active">{{ t('admin.users.apiKeyStatusActive') }}</option>
            <option value="disabled">{{ t('admin.users.apiKeyStatusDisabled') }}</option>
          </select>
        </div>
      </div>

      <div v-if="!editingKey">
        <label class="input-label" for="admin-api-key-custom">{{ t('admin.users.customApiKey') }}</label>
        <input
          id="admin-api-key-custom"
          v-model.trim="keyForm.custom_key"
          type="text"
          class="input font-mono"
          autocomplete="off"
          :placeholder="t('admin.users.customApiKeyPlaceholder')"
        />
        <p class="input-hint">{{ t('admin.users.customApiKeyHint') }}</p>
      </div>

      <div>
        <label class="input-label" for="admin-api-key-group">{{ t('admin.users.group') }}</label>
        <select id="admin-api-key-group" v-model.number="keyForm.group_id" class="input">
          <option :value="0">{{ t('admin.users.none') }}</option>
          <option v-for="group in allGroups" :key="group.id" :value="group.id">{{ group.name }}</option>
        </select>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <label class="input-label" for="admin-api-key-quota">{{ t('admin.users.apiKeyQuota') }}</label>
          <div class="relative">
            <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
            <input id="admin-api-key-quota" v-model.number="keyForm.quota" type="number" min="0" step="0.01" class="input pl-7" />
          </div>
          <p class="input-hint">{{ t('admin.users.zeroUnlimitedHint') }}</p>
          <label v-if="editingKey" class="mt-2 flex items-center gap-2 text-xs text-gray-600 dark:text-dark-300">
            <input v-model="keyForm.reset_quota" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('admin.users.resetQuotaUsed') }}
          </label>
        </div>
        <div>
          <label class="input-label" for="admin-api-key-extra-quota">{{ t('admin.users.extraQuota') }}</label>
          <div class="relative">
            <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
            <input id="admin-api-key-extra-quota" v-model.number="keyForm.extra_quota" type="number" min="0" step="0.01" class="input pl-7" />
          </div>
          <p class="input-hint">{{ t('admin.users.extraQuotaHint') }}</p>
          <label v-if="editingKey" class="mt-2 flex items-center gap-2 text-xs text-gray-600 dark:text-dark-300">
            <input v-model="keyForm.reset_extra_quota" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('admin.users.resetExtraQuotaUsed') }}
          </label>
        </div>
      </div>

      <div>
        <label class="input-label" for="admin-api-key-expires-at">{{ t('admin.users.apiKeyExpiresAt') }}</label>
        <input id="admin-api-key-expires-at" v-model="keyForm.expires_at" type="datetime-local" class="input" />
        <p class="input-hint">{{ t('admin.users.apiKeyExpiresAtHint') }}</p>
      </div>

      <div class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.users.rateLimitSection') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.users.rateLimitHint') }}</p>
        </div>
        <div class="grid gap-4 md:grid-cols-3">
          <div>
            <label class="input-label" for="admin-api-key-rate-5h">{{ t('admin.users.rateLimit5h') }}</label>
            <div class="relative">
              <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
              <input id="admin-api-key-rate-5h" v-model.number="keyForm.rate_limit_5h" type="number" min="0" step="0.01" class="input pl-7" />
            </div>
          </div>
          <div>
            <label class="input-label" for="admin-api-key-rate-1d">{{ t('admin.users.rateLimit1d') }}</label>
            <div class="relative">
              <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
              <input id="admin-api-key-rate-1d" v-model.number="keyForm.rate_limit_1d" type="number" min="0" step="0.01" class="input pl-7" />
            </div>
          </div>
          <div>
            <label class="input-label" for="admin-api-key-rate-7d">{{ t('admin.users.rateLimit7d') }}</label>
            <div class="relative">
              <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
              <input id="admin-api-key-rate-7d" v-model.number="keyForm.rate_limit_7d" type="number" min="0" step="0.01" class="input pl-7" />
            </div>
          </div>
        </div>
        <label v-if="editingKey" class="flex items-center gap-2 text-xs text-gray-600 dark:text-dark-300">
          <input v-model="keyForm.reset_rate_limit_usage" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
          {{ t('admin.users.resetRateLimitUsage') }}
        </label>
      </div>

      <div v-if="editingKey" class="rounded-xl bg-gray-50 p-4 text-xs text-gray-600 dark:bg-dark-700 dark:text-dark-300">
        <div class="grid gap-2 md:grid-cols-2">
          <div>{{ t('admin.users.apiKeyQuotaUsed') }}: ${{ formatAmount(editingKey.quota_used) }}</div>
          <div>{{ t('admin.users.extraQuotaUsed') }}: ${{ formatAmount(editingKey.extra_quota_used) }}</div>
          <div>5h: {{ formatRateWindow(editingKey.usage_5h, editingKey.rate_limit_5h) }}</div>
          <div>1d: {{ formatRateWindow(editingKey.usage_1d, editingKey.rate_limit_1d) }}</div>
          <div>7d: {{ formatRateWindow(editingKey.usage_7d, editingKey.rate_limit_7d) }}</div>
        </div>
      </div>
    </form>

    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="submittingKeyForm" @click="closeKeyFormDialog">
        {{ t('common.cancel') }}
      </button>
      <button
        type="button"
        class="btn btn-primary inline-flex items-center gap-1.5"
        :disabled="submittingKeyForm"
        :title="editingKey ? t('admin.users.saveApiKeyHint') : t('admin.users.createApiKeyHint')"
        @click="submitKeyForm"
      >
        <Icon v-if="submittingKeyForm" name="refresh" size="sm" class="animate-spin" />
        {{ editingKey ? t('common.save') : t('admin.users.createApiKey') }}
      </button>
    </template>
  </BaseDialog>

  <!-- Group Selector Dropdown -->
  <Teleport to="body">
    <div
      v-if="groupSelectorKeyId !== null && dropdownPosition"
      ref="dropdownRef"
      class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-64 overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 duration-200 dark:bg-dark-800 dark:ring-white/10"
      :style="{ top: dropdownPosition.top + 'px', left: dropdownPosition.left + 'px' }"
    >
      <div class="max-h-64 overflow-y-auto p-1.5">
        <button
          :class="[
            'flex w-full items-center rounded-lg px-3 py-2 text-sm transition-colors',
            !selectedKeyForGroup?.group_id
              ? 'bg-primary-50 dark:bg-primary-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-dark-700'
          ]"
          @click="changeGroup(selectedKeyForGroup!, null)"
        >
          <span class="italic text-gray-500">{{ t('admin.users.none') }}</span>
          <Icon v-if="!selectedKeyForGroup?.group_id" name="check" size="sm" class="ml-auto shrink-0 text-primary-600 dark:text-primary-400" :stroke-width="2" />
        </button>
        <button
          v-for="group in allGroups"
          :key="group.id"
          :class="[
            'flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors',
            selectedKeyForGroup?.group_id === group.id
              ? 'bg-primary-50 dark:bg-primary-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-dark-700'
          ]"
          @click="changeGroup(selectedKeyForGroup!, group.id)"
        >
          <GroupOptionItem
            :name="group.name"
            :platform="group.platform"
            :subscription-type="group.subscription_type"
            :rate-multiplier="group.rate_multiplier"
            :peak-rate-enabled="group.peak_rate_enabled"
            :peak-start="group.peak_start"
            :peak-end="group.peak_end"
            :peak-rate-multiplier="group.peak_rate_multiplier"
            :description="group.description"
            :selected="selectedKeyForGroup?.group_id === group.id"
          />
        </button>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { AdminGroup, AdminUser, ApiKey, CreateApiKeyRequest, UpdateApiKeyRequest } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Icon from '@/components/icons/Icon.vue'

type EditableAPIKeyStatus = 'active' | 'disabled'

interface KeyFormState {
  name: string
  status: EditableAPIKeyStatus
  custom_key: string
  group_id: number
  quota: number
  extra_quota: number
  expires_at: string
  rate_limit_5h: number
  rate_limit_1d: number
  rate_limit_7d: number
  reset_quota: boolean
  reset_extra_quota: boolean
  reset_rate_limit_usage: boolean
}

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close'])
const { t } = useI18n()
const appStore = useAppStore()

const apiKeys = ref<ApiKey[]>([])
const allGroups = ref<AdminGroup[]>([])
const loading = ref(false)
const updatingKeyIds = ref(new Set<number>())
const groupSelectorKeyId = ref<number | null>(null)
const dropdownPosition = ref<{ top: number; left: number } | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const scrollContainerRef = ref<HTMLElement | null>(null)
const groupButtonRefs = ref<Map<number, HTMLElement>>(new Map())
const showKeyFormDialog = ref(false)
const submittingKeyForm = ref(false)
const editingKey = ref<ApiKey | null>(null)

const keyForm = reactive<KeyFormState>({
  name: '',
  status: 'active',
  custom_key: '',
  group_id: 0,
  quota: 0,
  extra_quota: 0,
  expires_at: '',
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  reset_quota: false,
  reset_extra_quota: false,
  reset_rate_limit_usage: false
})

const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

watch(() => props.show, (v) => {
  if (v && props.user) {
    load()
    loadGroups()
  } else {
    closeGroupSelector()
    closeKeyFormDialog()
  }
})

const setGroupButtonRef = (keyId: number, el: Element | ComponentPublicInstance | null) => {
  if (el instanceof HTMLElement) {
    groupButtonRefs.value.set(keyId, el)
  } else {
    groupButtonRefs.value.delete(keyId)
  }
}

const load = async () => {
  if (!props.user) return
  loading.value = true
  groupButtonRefs.value.clear()
  try {
    const res = await adminAPI.users.getUserApiKeys(props.user.id)
    apiKeys.value = res.items || []
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.failedToLoadApiKeys'))
  } finally {
    loading.value = false
  }
}

const loadGroups = async () => {
  try {
    const groups = await adminAPI.groups.getAll()
    allGroups.value = groups
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const resetKeyForm = () => {
  keyForm.name = ''
  keyForm.status = 'active'
  keyForm.custom_key = ''
  keyForm.group_id = 0
  keyForm.quota = 0
  keyForm.extra_quota = 0
  keyForm.expires_at = ''
  keyForm.rate_limit_5h = 0
  keyForm.rate_limit_1d = 0
  keyForm.rate_limit_7d = 0
  keyForm.reset_quota = false
  keyForm.reset_extra_quota = false
  keyForm.reset_rate_limit_usage = false
}

const openCreateDialog = () => {
  closeGroupSelector()
  editingKey.value = null
  resetKeyForm()
  showKeyFormDialog.value = true
}

const openEditDialog = (key: ApiKey) => {
  closeGroupSelector()
  editingKey.value = key
  keyForm.name = key.name
  keyForm.status = key.status === 'active' ? 'active' : 'disabled'
  keyForm.custom_key = ''
  keyForm.group_id = key.group_id ?? 0
  keyForm.quota = normalizeNumber(key.quota)
  keyForm.extra_quota = normalizeNumber(key.extra_quota)
  keyForm.expires_at = toDateTimeLocal(key.expires_at)
  keyForm.rate_limit_5h = normalizeNumber(key.rate_limit_5h)
  keyForm.rate_limit_1d = normalizeNumber(key.rate_limit_1d)
  keyForm.rate_limit_7d = normalizeNumber(key.rate_limit_7d)
  keyForm.reset_quota = false
  keyForm.reset_extra_quota = false
  keyForm.reset_rate_limit_usage = false
  showKeyFormDialog.value = true
}

const closeKeyFormDialog = () => {
  if (submittingKeyForm.value) return
  showKeyFormDialog.value = false
  editingKey.value = null
  resetKeyForm()
}

const submitKeyForm = async () => {
  if (!props.user) return
  if (!keyForm.name.trim()) {
    appStore.showError(t('admin.users.apiKeyNameRequired'))
    return
  }
  if (!validateNonNegativeFields()) return

  submittingKeyForm.value = true
  try {
    if (editingKey.value) {
      const payload: UpdateApiKeyRequest = {
        name: keyForm.name.trim(),
        status: keyForm.status,
        group_id: keyForm.group_id === 0 ? 0 : keyForm.group_id,
        quota: normalizeNumber(keyForm.quota),
        extra_quota: normalizeNumber(keyForm.extra_quota),
        expires_at: keyForm.expires_at ? new Date(keyForm.expires_at).toISOString() : '',
        rate_limit_5h: normalizeNumber(keyForm.rate_limit_5h),
        rate_limit_1d: normalizeNumber(keyForm.rate_limit_1d),
        rate_limit_7d: normalizeNumber(keyForm.rate_limit_7d)
      }
      if (keyForm.reset_quota) payload.reset_quota = true
      if (keyForm.reset_extra_quota) payload.reset_extra_quota = true
      if (keyForm.reset_rate_limit_usage) payload.reset_rate_limit_usage = true

      const result = await adminAPI.apiKeys.update(editingKey.value.id, payload)
      replaceKey(result.api_key)
      if (result.auto_granted_group_access && result.granted_group_name) {
        appStore.showSuccess(t('admin.users.apiKeySavedWithGrant', { group: result.granted_group_name }))
      } else {
        appStore.showSuccess(t('admin.users.apiKeySaved'))
      }
    } else {
      const payload: CreateApiKeyRequest = {
        name: keyForm.name.trim(),
        group_id: keyForm.group_id === 0 ? null : keyForm.group_id,
        quota: normalizeNumber(keyForm.quota),
        extra_quota: normalizeNumber(keyForm.extra_quota),
        rate_limit_5h: normalizeNumber(keyForm.rate_limit_5h),
        rate_limit_1d: normalizeNumber(keyForm.rate_limit_1d),
        rate_limit_7d: normalizeNumber(keyForm.rate_limit_7d)
      }
      if (keyForm.custom_key.trim()) {
        payload.custom_key = keyForm.custom_key.trim()
      }
      if (keyForm.expires_at) {
        payload.expires_at = new Date(keyForm.expires_at).toISOString()
      }
      const created = await adminAPI.apiKeys.createForUser(props.user.id, payload)
      apiKeys.value = [created, ...apiKeys.value]
      appStore.showSuccess(t('admin.users.apiKeyCreated'))
    }
    showKeyFormDialog.value = false
    editingKey.value = null
    resetKeyForm()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.apiKeySaveFailed'))
  } finally {
    submittingKeyForm.value = false
  }
}

const validateNonNegativeFields = () => {
  const values = [
    keyForm.quota,
    keyForm.extra_quota,
    keyForm.rate_limit_5h,
    keyForm.rate_limit_1d,
    keyForm.rate_limit_7d
  ]
  if (values.some((v) => Number.isFinite(Number(v)) && Number(v) < 0)) {
    appStore.showError(t('admin.users.nonNegativeAmountRequired'))
    return false
  }
  if (values.some((v) => Number.isNaN(Number(v)))) {
    appStore.showError(t('admin.users.nonNegativeAmountRequired'))
    return false
  }
  if (keyForm.expires_at && Number.isNaN(new Date(keyForm.expires_at).getTime())) {
    appStore.showError(t('admin.users.invalidExpiration'))
    return false
  }
  return true
}

const replaceKey = (key: ApiKey) => {
  const idx = apiKeys.value.findIndex((k) => k.id === key.id)
  if (idx !== -1) {
    apiKeys.value[idx] = key
  }
}

const DROPDOWN_HEIGHT = 272
const DROPDOWN_GAP = 4

const openGroupSelector = (key: ApiKey) => {
  if (groupSelectorKeyId.value === key.id) {
    closeGroupSelector()
    return
  }

  const buttonEl = groupButtonRefs.value.get(key.id)
  if (buttonEl) {
    const rect = buttonEl.getBoundingClientRect()
    const spaceBelow = window.innerHeight - rect.bottom
    const openUpward = spaceBelow < DROPDOWN_HEIGHT && rect.top > spaceBelow
    dropdownPosition.value = {
      top: openUpward ? rect.top - DROPDOWN_HEIGHT - DROPDOWN_GAP : rect.bottom + DROPDOWN_GAP,
      left: rect.left
    }
  }
  groupSelectorKeyId.value = key.id
}

const closeGroupSelector = () => {
  groupSelectorKeyId.value = null
  dropdownPosition.value = null
}

const changeGroup = async (key: ApiKey, newGroupId: number | null) => {
  closeGroupSelector()
  if (key.group_id === newGroupId || (!key.group_id && newGroupId === null)) return

  updatingKeyIds.value.add(key.id)
  try {
    const result = await adminAPI.apiKeys.updateApiKeyGroup(key.id, newGroupId)
    replaceKey(result.api_key)
    if (result.auto_granted_group_access && result.granted_group_name) {
      appStore.showSuccess(t('admin.users.groupChangedWithGrant', { group: result.granted_group_name }))
    } else {
      appStore.showSuccess(t('admin.users.groupChangedSuccess'))
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.groupChangeFailed'))
  } finally {
    updatingKeyIds.value.delete(key.id)
  }
}

const handleKeyDown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && groupSelectorKeyId.value !== null) {
    event.stopPropagation()
    closeGroupSelector()
  }
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (dropdownRef.value && !dropdownRef.value.contains(target)) {
    for (const el of groupButtonRefs.value.values()) {
      if (el.contains(target)) return
    }
    closeGroupSelector()
  }
}

const handleClose = () => {
  closeGroupSelector()
  closeKeyFormDialog()
  emit('close')
}

const normalizeNumber = (value: number | null | undefined) => {
  const n = Number(value)
  return Number.isFinite(n) && n > 0 ? n : 0
}

const formatAmount = (value: number | null | undefined) => normalizeNumber(value).toFixed(2)

const formatRateWindow = (used: number, limit: number) => {
  if (!limit || limit <= 0) return t('admin.users.unlimited')
  return `$${formatAmount(used)} / $${formatAmount(limit)}`
}

const formatKeyStatus = (status: string) => {
  switch (status) {
    case 'active':
      return t('admin.users.apiKeyStatusActive')
    case 'disabled':
    case 'inactive':
      return t('admin.users.apiKeyStatusDisabled')
    case 'quota_exhausted':
      return t('admin.users.apiKeyStatusQuotaExhausted')
    case 'expired':
      return t('admin.users.apiKeyStatusExpired')
    default:
      return status
  }
}

const maskApiKey = (key: string) => {
  if (!key) return ''
  if (key.length <= 28) return key
  return `${key.substring(0, 20)}...${key.substring(key.length - 8)}`
}

const toDateTimeLocal = (value: string | null | undefined) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}`
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeyDown, true)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeyDown, true)
})
</script>
