<template>
  <AppLayout>
    <div class="space-y-4">
      <section class="card p-4 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.iqTest.title') }}</h1>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.iqTest.description') }}</p>
            <p class="mt-3 break-words rounded-md bg-gray-50 px-3 py-2 font-mono text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300">
              {{ prompt }}
            </p>
          </div>
          <button type="button" class="btn btn-primary flex-shrink-0" :disabled="loading || loadingAccounts || selectedCount === 0" @click="run">
            <Icon name="play" size="sm" class="mr-1.5" />
            {{ loading ? t('admin.iqTest.running') : t('admin.iqTest.run') }}
          </button>
        </div>

        <div class="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-gray-200 pt-4 dark:border-dark-600">
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="allSelected"
              :disabled="loading || accounts.length === 0"
              @change="toggleAll"
            />
            <span>{{ t('admin.iqTest.selectAll') }}</span>
          </label>
          <div class="flex items-center gap-3 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.iqTest.selectedCount', { selected: selectedCount, total: accounts.length }) }}</span>
            <button type="button" class="btn btn-secondary btn-xs" :disabled="loading || loadingAccounts" @click="loadAccounts">
              <Icon name="refresh" size="xs" class="mr-1" />
              {{ t('admin.iqTest.refreshAccounts') }}
            </button>
          </div>
        </div>

        <div v-if="loadingAccounts" class="mt-4 text-sm text-gray-500">{{ t('common.loading') }}...</div>
        <div v-else-if="accounts.length === 0" class="mt-4 text-sm text-gray-500">{{ t('admin.iqTest.noEligible') }}</div>
        <div v-else class="mt-4 grid gap-2 md:grid-cols-2">
          <label
            v-for="account in accounts"
            :key="account.id"
            class="flex min-w-0 cursor-pointer items-center gap-3 rounded-md border px-3 py-2 transition-colors"
            :class="selectedIds.has(account.id) ? 'border-primary-300 bg-primary-50 dark:border-primary-700 dark:bg-primary-950/20' : 'border-gray-200 dark:border-dark-600'"
          >
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="selectedIds.has(account.id)"
              :disabled="loading"
              @change="toggleAccount(account.id)"
            />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">{{ account.name }}</span>
              <span class="block text-xs text-gray-500 dark:text-gray-400">#{{ account.id }} · {{ account.type }} · {{ account.status }}</span>
            </span>
          </label>
        </div>
      </section>

      <div v-if="error" class="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
        {{ error }}
      </div>

      <section v-if="testStates.length > 0" class="space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-2 px-1">
          <h2 class="font-medium text-gray-900 dark:text-white">{{ t('admin.iqTest.results') }}</h2>
          <div class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.iqTest.progress', { completed: completedCount, total: testStates.length }) }}
            <span class="ml-3">{{ t('admin.iqTest.totalCost') }}: ${{ totalCost.toFixed(6) }}</span>
          </div>
        </div>

        <article v-for="state in testStates" :key="state.account.id" class="card overflow-hidden">
          <header class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-600">
            <div class="flex min-w-0 items-center gap-3">
              <h3 class="truncate font-medium text-gray-900 dark:text-white">{{ state.account.name }}</h3>
              <span class="font-mono text-xs text-gray-400">#{{ state.account.id }}</span>
              <span class="rounded-full px-2 py-0.5 text-xs" :class="statusClass(state.status)">
                {{ statusLabel(state.status) }}
              </span>
            </div>
            <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500">
              <span>{{ t('admin.iqTest.input') }}: {{ state.inputTokens.toLocaleString() }}</span>
              <span>{{ t('admin.iqTest.output') }}: {{ state.outputTokens.toLocaleString() }}</span>
              <span>{{ t('admin.iqTest.total') }}: {{ state.totalTokens.toLocaleString() }}</span>
              <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.iqTest.cost') }}: ${{ state.costUSD.toFixed(6) }}</span>
            </div>
          </header>

          <div v-if="state.retryable503" class="flex flex-wrap items-center justify-between gap-3 border-b border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-200">
            <span>{{ state.errorMessage }}</span>
            <button type="button" class="btn btn-primary btn-sm" :disabled="loading" @click="continueAfter503(state)">
              {{ t('admin.iqTest.continue') }}
            </button>
          </div>
          <div v-else-if="state.errorMessage" class="border-b border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/40 dark:bg-red-950/20 dark:text-red-300">
            {{ state.errorMessage }}
          </div>

          <div v-if="state.output" class="grid gap-4 p-4 xl:grid-cols-2">
            <div class="min-w-0">
              <h4 class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.iqTest.response') }}</h4>
              <pre class="max-h-[560px] overflow-auto rounded-md bg-gray-950 p-4 text-xs leading-relaxed text-gray-100"><code>{{ state.output }}</code></pre>
            </div>
            <div v-if="state.status === 'success'" class="min-w-0">
              <h4 class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.iqTest.preview') }}</h4>
              <iframe class="h-[560px] w-full rounded-md border border-gray-200 bg-white dark:border-dark-600" sandbox="allow-scripts" :srcdoc="previewHTML(state.output)" :title="state.account.name" />
            </div>
          </div>
        </article>
      </section>

      <div v-else-if="!loadingAccounts" class="card p-8 text-center text-sm text-gray-500">{{ t('admin.iqTest.empty') }}</div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { buildApiUrl } from '@/api/client'
import { ADMIN_UI_REQUEST_HEADER } from '@/api/adminUIRequest'
import type { IQTestAccount } from '@/api/admin/iqTest'

const { t } = useI18n()
const prompt = '创建一个HTML，内容是SVG绘制一个鹈鹏骑自行车的2D动画，你不需要任何测试'
const overloadMessage = 'Our servers are currently overloaded. Please try again later.'

type TestStatus = 'pending' | 'running' | 'success' | 'failed' | 'skipped'

interface IQTestState {
  account: IQTestAccount
  status: TestStatus
  output: string
  errorMessage: string
  retryable503: boolean
  inputTokens: number
  outputTokens: number
  totalTokens: number
  costUSD: number
}

const accounts = ref<IQTestAccount[]>([])
const selectedIds = ref<Set<number>>(new Set())
const testStates = ref<IQTestState[]>([])
const loadingAccounts = ref(false)
const loading = ref(false)
const error = ref('')
const queueIndex = ref(-1)
let abortController: AbortController | null = null

const selectedCount = computed(() => selectedIds.value.size)
const allSelected = computed(() => accounts.value.length > 0 && selectedCount.value === accounts.value.length)
const completedCount = computed(() => testStates.value.filter((state) => ['success', 'failed', 'skipped'].includes(state.status)).length)
const totalCost = computed(() => testStates.value.reduce((sum, state) => sum + state.costUSD, 0))

onMounted(loadAccounts)

async function loadAccounts(): Promise<void> {
  loadingAccounts.value = true
  error.value = ''
  try {
    const nextAccounts = await adminAPI.iqTest.listAccounts()
    accounts.value = nextAccounts
    selectedIds.value = new Set(nextAccounts.map((account) => account.id))
    testStates.value = []
  } catch (err: any) {
    error.value = err?.response?.data?.message || err?.message || t('admin.iqTest.loadAccountsFailed')
  } finally {
    loadingAccounts.value = false
  }
}

function toggleAccount(id: number): void {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}

function toggleAll(event: Event): void {
  const checked = (event.target as HTMLInputElement).checked
  selectedIds.value = checked ? new Set(accounts.value.map((account) => account.id)) : new Set()
}

async function run(): Promise<void> {
  if (selectedCount.value === 0) return
  abortController?.abort()
  abortController = new AbortController()
  loading.value = true
  error.value = ''
  testStates.value = accounts.value
    .filter((account) => selectedIds.value.has(account.id))
    .map((account) => ({
      account,
      status: 'pending',
      output: '',
      errorMessage: '',
      retryable503: false,
      inputTokens: 0,
      outputTokens: 0,
      totalTokens: 0,
      costUSD: 0
    }))
  queueIndex.value = -1
  await processQueue()
}

async function processQueue(): Promise<void> {
  for (let index = queueIndex.value + 1; index < testStates.value.length; index += 1) {
    queueIndex.value = index
    const state = testStates.value[index]
    state.status = 'running'
    state.errorMessage = ''
    state.retryable503 = false
    const result = await streamAccountTest(state)
    if (result.retryable503) {
      state.status = 'failed'
      state.retryable503 = true
      loading.value = false
      return
    }
    state.status = result.success ? 'success' : 'failed'
  }
  loading.value = false
  queueIndex.value = -1
}

async function continueAfter503(state: IQTestState): Promise<void> {
  if (loading.value || state.status !== 'failed' || !state.retryable503) return
  state.retryable503 = false
  state.status = 'skipped'
  await processQueue()
}

async function streamAccountTest(state: IQTestState): Promise<{ success: boolean; retryable503: boolean }> {
  const controller = new AbortController()
  abortController = controller
  try {
    const response = await fetch(buildApiUrl(`/admin/accounts/${state.account.id}/test`), {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token') || ''}`,
        'Content-Type': 'application/json',
        [ADMIN_UI_REQUEST_HEADER]: '1'
      },
      body: JSON.stringify({ model_id: 'gpt-6-astra', prompt }),
      signal: controller.signal
    })

    if (!response.ok) {
      const body = await response.text()
      const message = extractHTTPError(body) || `HTTP ${response.status}`
      state.errorMessage = message
      return { success: false, retryable503: response.status === 503 || isOverloadMessage(message) }
    }

    const reader = response.body?.getReader()
    if (!reader) {
      state.errorMessage = t('admin.iqTest.noResponseBody')
      return { success: false, retryable503: false }
    }

    const decoder = new TextDecoder()
    let buffer = ''
    let success = false

    const handleSSELine = (line: string): boolean => {
      if (!line.startsWith('data:')) return false
      const raw = line.slice(5).trim()
      if (!raw) return false
      try {
        const event = JSON.parse(raw) as {
          type?: string
          text?: string
          error?: string
          success?: boolean
          input_tokens?: number
          output_tokens?: number
          total_tokens?: number
          cost_usd?: number
        }
        if (event.type === 'content' || event.type === 'status') state.output += event.text || ''
        if (event.type === 'error') {
          state.errorMessage = event.error || t('admin.iqTest.runFailed')
          if (isOverloadMessage(state.errorMessage)) return true
        }
        if (event.type === 'usage') {
          state.inputTokens = event.input_tokens || 0
          state.outputTokens = event.output_tokens || 0
          state.totalTokens = event.total_tokens || state.inputTokens + state.outputTokens
          state.costUSD = event.cost_usd || 0
        }
        if (event.type === 'test_complete') success = event.success === true
      } catch {
        state.output += raw
      }
      return false
    }

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        if (handleSSELine(line)) return { success: false, retryable503: true }
      }
    }

    buffer += decoder.decode()
    if (buffer && handleSSELine(buffer)) return { success: false, retryable503: true }
    return { success: success && !state.errorMessage, retryable503: false }
  } catch (err: unknown) {
    if (err instanceof DOMException && err.name === 'AbortError') return { success: false, retryable503: false }
    state.errorMessage = err instanceof Error ? err.message : t('admin.iqTest.runFailed')
    return { success: false, retryable503: isOverloadMessage(state.errorMessage) }
  }
}

function isOverloadMessage(message: string): boolean {
  return message.includes(overloadMessage)
}

function extractHTTPError(body: string): string {
  try {
    const parsed = JSON.parse(body) as { message?: string; error?: string; data?: { message?: string } }
    return parsed.message || parsed.error || parsed.data?.message || ''
  } catch {
    return body.trim()
  }
}

function statusLabel(status: TestStatus): string {
  return t(`admin.iqTest.statuses.${status}`)
}

function statusClass(status: TestStatus): string {
  if (status === 'success') return 'bg-green-100 text-green-700 dark:bg-green-950/50 dark:text-green-300'
  if (status === 'running') return 'bg-blue-100 text-blue-700 dark:bg-blue-950/50 dark:text-blue-300'
  if (status === 'pending') return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'
  if (status === 'skipped') return 'bg-amber-100 text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
  return 'bg-red-100 text-red-700 dark:bg-red-950/50 dark:text-red-300'
}

function previewHTML(source: string): string {
  const trimmed = source.trim()
  const fenced = trimmed.match(/```(?:html)?\s*([\s\S]*?)\s*```/i)
  return fenced ? fenced[1] : trimmed
}
</script>
