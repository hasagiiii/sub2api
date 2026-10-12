<template>
  <BaseDialog :show="show" :title="t('admin.accounts.openaiData.title')" width="wide" @close="emit('close')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="break-all font-medium text-gray-900 dark:text-gray-100">{{ account?.name }}</p>
        <p v-if="current.fetchedAt" class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.accounts.openaiData.fetchedAt', { time: formatDateTime(current.fetchedAt) }) }}
        </p>
      </div>
      <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.accounts.openaiData.description') }}</p>
      <div class="flex flex-wrap gap-2" role="group" :aria-label="t('admin.accounts.openaiData.datasets')">
        <button v-for="kind in kinds" :key="kind" type="button" class="btn" :class="activeKind === kind ? 'btn-primary' : 'btn-secondary'" :aria-pressed="activeKind === kind" @click="activeKind = kind">
          {{ t(`admin.accounts.openaiData.${kind}`) }}
          <Icon v-if="states[kind].loading" name="refresh" size="sm" class="ml-2 animate-spin" />
        </button>
      </div>
      <form v-if="activeKind !== 'usage'" class="space-y-2" @submit.prevent="loadAnalytics">
        <div class="flex flex-wrap items-end gap-3">
          <label class="text-sm text-gray-600 dark:text-dark-300">
            {{ t('admin.accounts.openaiData.startDate') }}
            <input v-model="startDate" type="date" required :max="endDate || undefined" class="input mt-1" />
          </label>
          <label class="text-sm text-gray-600 dark:text-dark-300">
            {{ t('admin.accounts.openaiData.endDate') }}
            <input v-model="endDate" type="date" required :min="startDate || undefined" class="input mt-1" />
          </label>
          <button type="submit" class="btn btn-primary" :disabled="!datesValid">{{ t('admin.accounts.openaiData.query') }}</button>
        </div>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.openaiData.analyticsHint') }}</p>
        <p v-if="!datesValid" role="alert" class="text-sm text-red-600">{{ t('admin.accounts.openaiData.invalidDates') }}</p>
        <p v-if="current.range" class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.openaiData.resultRange', { range: current.range }) }}</p>
      </form>
      <div v-if="current.loading" role="status" class="flex items-center justify-center gap-2 py-12 text-sm text-gray-500">
        <Icon name="refresh" size="md" class="animate-spin" />
        {{ t('common.loading') }}
      </div>
      <div v-else-if="current.error" role="alert" class="break-words rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        {{ current.error }}
      </div>
      <pre v-else-if="current.payload !== null" class="max-h-[55vh] overflow-auto rounded-xl border border-gray-200 bg-gray-50 p-4 font-mono text-xs leading-6 text-gray-800 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-200" tabindex="0" :aria-label="t('admin.accounts.openaiData.response')"><code>{{ formattedJSON }}</code></pre>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="current.loading || current.payload === null" @click="copyToClipboard(formattedJSON)">
          <Icon name="copy" size="sm" class="mr-2" />{{ t('admin.accounts.openaiData.copy') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="current.loading || !account || (activeKind !== 'usage' && !datesValid)" @click="loadData(activeKind)">
          <Icon name="refresh" size="sm" class="mr-2" />{{ t('common.refresh') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { getOpenAIUsageData, getOpenAIUsageAnalytics, type OpenAIUsageData } from '@/api/admin/accounts'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean; account: { id: number; name: string } | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const kinds = ['usage', 'plugins', 'skills', 'tokens', 'workspace'] as const
type DataKind = typeof kinds[number]
interface DataState {
  payload: OpenAIUsageData | null
  loading: boolean
  error: string
  fetchedAt: Date | null
  range: string
}
const emptyState = (): DataState => ({ payload: null, loading: false, error: '', fetchedAt: null, range: '' })
const states = reactive<Record<DataKind, DataState>>({
  usage: emptyState(),
  plugins: emptyState(),
  skills: emptyState(),
  tokens: emptyState(),
  workspace: emptyState()
})
const requests: Partial<Record<DataKind, AbortController>> = {}
const activeKind = ref<DataKind>('usage')
const current = computed(() => states[activeKind.value])
const startDate = ref('')
const endDate = ref('')
const datesValid = computed(() => /^\d{4}-\d{2}-\d{2}$/.test(startDate.value) && /^\d{4}-\d{2}-\d{2}$/.test(endDate.value) && startDate.value <= endDate.value)
const formattedJSON = computed(() => current.value.payload === null ? '' : JSON.stringify(current.value.payload, null, 2))

function reset(kind: DataKind) {
  requests[kind]?.abort()
  delete requests[kind]
  states[kind] = emptyState()
}

function resetAll() {
  kinds.forEach(reset)
}

function setDefaultDates() {
  const end = new Date()
  const start = new Date(end)
  start.setDate(start.getDate() - 6)
  // Use local calendar dates so opening near midnight doesn't shift the range.
  const dateOnly = (date: Date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
  startDate.value = dateOnly(start)
  endDate.value = dateOnly(end)
}

async function loadData(kind: DataKind) {
  if (!props.show || !props.account || (kind !== 'usage' && !datesValid.value)) return
  reset(kind)
  const controller = new AbortController()
  requests[kind] = controller
  const state = states[kind]
  state.loading = true
  const dates = { start_date: startDate.value, end_date: endDate.value }
  if (kind !== 'usage') state.range = `${dates.start_date} ~ ${dates.end_date}`
  try {
    const result = kind === 'usage'
      ? await getOpenAIUsageData(props.account.id, controller.signal)
      : await getOpenAIUsageAnalytics(props.account.id, kind, dates, controller.signal)
    if (requests[kind] !== controller) return
    state.payload = result
    state.fetchedAt = new Date()
  } catch (err: unknown) {
    if (requests[kind] !== controller) return
    state.error = extractApiErrorMessage(err, t('admin.accounts.openaiData.failed'))
  } finally {
    if (requests[kind] === controller) {
      state.loading = false
      delete requests[kind]
    }
  }
}

function loadAnalytics() {
  kinds.forEach(kind => {
    if (kind !== 'usage') void loadData(kind)
  })
}

watch(() => [props.show, props.account?.id], () => {
  resetAll()
  if (props.show && props.account) {
    activeKind.value = 'usage'
    setDefaultDates()
    kinds.forEach(kind => { void loadData(kind) })
  }
}, { immediate: true })
onBeforeUnmount(resetAll)
</script>
