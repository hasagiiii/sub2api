<template>
  <div v-if="siteKey" class="hcaptcha-wrapper">
    <div ref="containerRef" class="h-captcha"></div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'

interface HCaptchaRenderOptions {
  sitekey: string
  callback: (token: string) => void
  'expired-callback'?: () => void
  'error-callback'?: () => void
  theme?: 'light' | 'dark'
  size?: 'normal' | 'compact'
}

interface HCaptchaAPI {
  render: (container: HTMLElement, options: HCaptchaRenderOptions) => string
  reset: (widgetId?: string) => void
  remove: (widgetId?: string) => void
}

declare global {
  interface Window {
    hcaptcha?: HCaptchaAPI
    onHCaptchaLoad?: () => void
  }
}

const props = withDefaults(
  defineProps<{
    siteKey: string
    theme?: 'light' | 'dark' | 'auto'
    size?: 'normal' | 'compact' | 'flexible'
  }>(),
  {
    theme: 'auto',
    size: 'normal'
  }
)

const emit = defineEmits<{
  (e: 'verify', token: string): void
  (e: 'expire'): void
  (e: 'error'): void
}>()

const containerRef = ref<HTMLElement | null>(null)
const widgetId = ref<string | null>(null)
const scriptLoaded = ref(false)

const loadScript = (): Promise<void> => {
  return new Promise((resolve, reject) => {
    if (window.hcaptcha) {
      scriptLoaded.value = true
      resolve()
      return
    }

    const existingScript = document.querySelector('script[src*="hcaptcha.com/1/api.js"]')
    if (existingScript) {
      window.onHCaptchaLoad = () => {
        scriptLoaded.value = true
        resolve()
      }
      return
    }

    const script = document.createElement('script')
    script.src = 'https://js.hcaptcha.com/1/api.js?onload=onHCaptchaLoad&render=explicit'
    script.async = true
    script.defer = true

    window.onHCaptchaLoad = () => {
      scriptLoaded.value = true
      resolve()
    }
    script.onerror = () => reject(new Error('Failed to load hCaptcha script'))

    document.head.appendChild(script)
  })
}

const renderWidget = () => {
  if (!window.hcaptcha || !containerRef.value || !props.siteKey) {
    return
  }

  if (widgetId.value) {
    try {
      window.hcaptcha.remove(widgetId.value)
    } catch {
      // Ignore stale widget cleanup failures
    }
    widgetId.value = null
  }

  containerRef.value.innerHTML = ''
  widgetId.value = window.hcaptcha.render(containerRef.value, {
    sitekey: props.siteKey,
    callback: (token: string) => emit('verify', token),
    'expired-callback': () => emit('expire'),
    'error-callback': () => emit('error'),
    theme: props.theme === 'dark' ? 'dark' : 'light',
    size: props.size === 'compact' ? 'compact' : 'normal'
  })
}

const reset = () => {
  if (window.hcaptcha && widgetId.value) {
    window.hcaptcha.reset(widgetId.value)
  }
}

defineExpose({ reset })

onMounted(async () => {
  if (!props.siteKey) {
    return
  }

  try {
    await loadScript()
    renderWidget()
  } catch (error) {
    console.error('Failed to initialize hCaptcha:', error)
    emit('error')
  }
})

onUnmounted(() => {
  if (window.hcaptcha && widgetId.value) {
    try {
      window.hcaptcha.remove(widgetId.value)
    } catch {
      // Ignore stale widget cleanup failures
    }
  }
})

watch(
  () => props.siteKey,
  (newKey) => {
    if (newKey && scriptLoaded.value) {
      renderWidget()
    }
  }
)
</script>

<style scoped>
.hcaptcha-wrapper {
  width: 100%;
}
</style>
