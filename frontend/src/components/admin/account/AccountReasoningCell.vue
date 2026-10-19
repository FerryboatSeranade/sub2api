<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import type { AccountListItem } from '@/types'
import type { ReasoningOverviewItem } from '@/api/admin/scheduledTests'
const props = defineProps<{ account: AccountListItem; result: ReasoningOverviewItem['result']; loadError: boolean; busy: boolean }>()
const emit = defineEmits<{ policy: [enabled: boolean] }>()
const { t } = useI18n()
const enabled = computed(() => props.account.extra?.reasoning_auto_exclude === true)
const verdict = computed(() => props.loadError ? 'loadError' : props.result?.quality?.verdict ?? 'untested')
const label = computed(() => t(`admin.accounts.reasoning.${verdict.value}`))
const color = computed(() => verdict.value === 'degraded' ? 'text-red-600' : verdict.value === 'not_degraded' ? 'text-emerald-600' : 'text-gray-500')
const checkedAt = computed(() => props.result ? new Date(props.result.finished_at).toLocaleString() : '')
</script>
<template>
 <div v-if="account.platform === 'openai'" class="flex min-w-[145px] flex-col gap-1 text-xs">
  <HelpTooltip :content="[t('admin.accounts.reasoning.rule'), result?.model_id, checkedAt, result?.error_message].filter(Boolean).join(' · ')">
   <template #trigger><span tabindex="0" :class="['font-medium', color]">{{ label }}</span></template>
  </HelpTooltip>
  <span v-if="checkedAt" class="text-[10px] text-gray-500">{{ checkedAt }}</span>
  <HelpTooltip :content="t('admin.accounts.reasoning.policyHint')">
   <template #trigger>
    <label class="inline-flex cursor-pointer items-center gap-1.5">
     <input type="checkbox" :checked="enabled" :disabled="busy" :aria-label="t('admin.accounts.reasoning.autoExclude')" @change="emit('policy', !enabled)" />
     {{ t('admin.accounts.reasoning.autoExclude') }}
    </label>
   </template>
  </HelpTooltip>
  <span v-if="enabled && verdict === 'degraded'" class="text-red-600">{{ t('admin.accounts.reasoning.excluded') }}</span>
 </div>
 <span v-else class="text-xs text-gray-400">{{ t('admin.accounts.reasoning.notApplicable') }}</span>
</template>
