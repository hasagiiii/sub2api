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
          <button type="button" class="btn btn-primary flex-shrink-0" :disabled="loading" @click="run">
            <Icon name="play" size="sm" class="mr-1.5" />
            {{ loading ? t('admin.iqTest.running') : t('admin.iqTest.run') }}
          </button>
        </div>
        <div v-if="response" class="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-3">
          <div class="rounded-md border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500">{{ t('admin.iqTest.eligible') }}</p>
            <p class="mt-1 text-lg font-semibold">{{ response.results.length }}</p>
          </div>
          <div class="rounded-md border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500">{{ t('admin.iqTest.totalTokens') }}</p>
            <p class="mt-1 text-lg font-semibold">{{ totalTokens.toLocaleString() }}</p>
          </div>
          <div class="col-span-2 rounded-md border border-gray-200 p-3 dark:col-span-1 dark:border-dark-600">
            <p class="text-xs text-gray-500">{{ t('admin.iqTest.totalCost') }}</p>
            <p class="mt-1 text-lg font-semibold">${{ totalCost.toFixed(6) }}</p>
          </div>
        </div>
      </section>

      <div v-if="error" class="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
        {{ error }}
      </div>
      <div v-if="!response && !loading" class="card p-8 text-center text-sm text-gray-500">{{ t('admin.iqTest.empty') }}</div>
      <div v-else-if="response && response.results.length === 0" class="card p-8 text-center text-sm text-gray-500">{{ t('admin.iqTest.noEligible') }}</div>

      <article v-for="result in response?.results" :key="result.account_id" class="card overflow-hidden">
        <header class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-600">
          <div class="flex min-w-0 items-center gap-3">
            <h2 class="truncate font-medium text-gray-900 dark:text-white">{{ result.account_name }}</h2>
            <span class="font-mono text-xs text-gray-400">#{{ result.account_id }}</span>
            <span class="rounded-full px-2 py-0.5 text-xs" :class="result.status === 'success' ? 'bg-green-100 text-green-700 dark:bg-green-950/50 dark:text-green-300' : 'bg-red-100 text-red-700 dark:bg-red-950/50 dark:text-red-300'">
              {{ result.status === 'success' ? t('admin.iqTest.success') : t('admin.iqTest.failed') }}
            </span>
          </div>
          <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500">
            <span>{{ t('admin.iqTest.input') }}: {{ result.input_tokens.toLocaleString() }}</span>
            <span>{{ t('admin.iqTest.output') }}: {{ result.output_tokens.toLocaleString() }}</span>
            <span>{{ t('admin.iqTest.total') }}: {{ result.total_tokens.toLocaleString() }}</span>
            <span class="font-medium text-gray-700 dark:text-gray-300">${{ result.cost_usd.toFixed(6) }}</span>
          </div>
        </header>
        <div v-if="result.error_message" class="border-b border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/40 dark:bg-red-950/20 dark:text-red-300">
          <span class="font-medium">{{ t('admin.iqTest.error') }}:</span> {{ result.error_message }}
        </div>
        <div v-if="result.html" class="grid gap-4 p-4 xl:grid-cols-2">
          <div class="min-w-0">
            <h3 class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.iqTest.source') }}</h3>
            <pre class="max-h-[560px] overflow-auto rounded-md bg-gray-950 p-4 text-xs leading-relaxed text-gray-100"><code>{{ result.html }}</code></pre>
          </div>
          <div class="min-w-0">
            <h3 class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.iqTest.preview') }}</h3>
            <iframe class="h-[560px] w-full rounded-md border border-gray-200 bg-white dark:border-dark-600" sandbox="allow-scripts" :srcdoc="previewHTML(result.html)" :title="result.account_name" />
          </div>
        </div>
      </article>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { IQTestResponse } from '@/api/admin/iqTest'

const { t } = useI18n()
const prompt = '创建一个HTML，内容是SVG绘制一个鹈鹏骑自行车的2D动画，你不需要任何测试'
const loading = ref(false)
const error = ref('')
const response = ref<IQTestResponse | null>(null)

const totalTokens = computed(() => response.value?.results.reduce((sum, item) => sum + item.total_tokens, 0) ?? 0)
const totalCost = computed(() => response.value?.results.reduce((sum, item) => sum + item.cost_usd, 0) ?? 0)

async function run() {
  loading.value = true
  error.value = ''
  try {
    response.value = await adminAPI.iqTest.run()
  } catch (err: any) {
    error.value = err?.response?.data?.message || err?.message || t('admin.iqTest.runFailed')
  } finally {
    loading.value = false
  }
}

function previewHTML(source: string): string {
  const trimmed = source.trim()
  const fenced = trimmed.match(/^```(?:html)?\s*([\s\S]*?)\s*```$/i)
  return fenced ? fenced[1] : trimmed
}
</script>
