<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import type { AccountListItem } from '@/types'
import type { ReasoningOverviewItem } from '@/api/admin/scheduledTests'
const props = defineProps<{ account: AccountListItem; result: ReasoningOverviewItem['result']; loadError: boolean; busy: boolean; globalEnabled?: boolean; routingMode?: string }>()
const emit = defineEmits<{ policy: [mode: string] }>()
const { t } = useI18n()
const mode = computed(() => String(props.account.extra?.reasoning_exclusion_policy ?? (props.account.extra?.reasoning_auto_exclude === true ? 'enabled' : 'inherit')))
const enabled = computed(() => mode.value === 'enabled' || (mode.value === 'inherit' && props.globalEnabled === true))
const verdict = computed(() => props.loadError ? 'loadError' : props.result?.quality?.verdict ?? 'untested')
const label = computed(() => t(`admin.accounts.reasoning.${verdict.value}`))
const color = computed(() => verdict.value === 'degraded' ? 'text-red-600' : verdict.value === 'not_degraded' ? 'text-emerald-600' : 'text-gray-500')
const routingLabel = computed(() => {
 let latest = props.account.extra?.reasoning_latest as { verdict?: string; checked_at?: string } | undefined
 if (latest?.verdict === 'inconclusive') latest = (props.account.extra?.reasoning_last_valid as typeof latest) ?? latest
 const age = latest?.checked_at ? Date.now() - Date.parse(latest.checked_at) : Infinity
 if (!Number.isFinite(age) || age > 48 * 3600 * 1000) return '未知 / 过期质量候补层'
 return latest?.verdict === 'not_degraded' ? '质量优先调度' : latest?.verdict === 'degraded' ? '降智兜底层' : '未知质量候补层'
})
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
    <select :value="mode" :disabled="busy" :aria-label="t('admin.accounts.reasoning.autoExclude')" class="rounded border border-gray-300 bg-transparent px-1 py-1 dark:border-gray-600" @change="emit('policy', ($event.target as HTMLSelectElement).value)">
     <option value="inherit">{{ t('admin.accounts.reasoning.inherit') }}</option>
     <option value="enabled">{{ t('admin.accounts.reasoning.alwaysEnabled') }}</option>
     <option value="disabled">{{ t('admin.accounts.reasoning.alwaysDisabled') }}</option>
    </select>
   </template>
  </HelpTooltip>
  <span v-if="!enabled && mode === 'inherit' && routingMode === 'prefer'" class="text-blue-600" title="先使用可用的不降智账号；未知状态其次，降智最后兜底。48 小时前的结果视为过期，调用异常保留上次有效结论。">{{ routingLabel }}</span>
  <span v-if="enabled && verdict === 'inconclusive'" class="text-amber-600">{{ t('admin.accounts.reasoning.excludedInconclusive') }}</span>
  <span v-if="enabled && verdict === 'degraded'" class="text-red-600">{{ t('admin.accounts.reasoning.excluded') }}</span>
 </div>
 <span v-else class="text-xs text-gray-400">{{ t('admin.accounts.reasoning.notApplicable') }}</span>
</template>
