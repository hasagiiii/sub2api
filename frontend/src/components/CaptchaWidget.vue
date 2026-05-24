<template>
  <TurnstileWidget
    v-if="provider === 'turnstile'"
    ref="turnstileRef"
    :site-key="siteKey"
    :theme="theme"
    :size="size"
    @verify="(token) => emit('verify', token)"
    @expire="emit('expire')"
    @error="emit('error')"
  />
  <HCaptchaWidget
    v-else-if="provider === 'hcaptcha'"
    ref="hcaptchaRef"
    :site-key="siteKey"
    :theme="theme"
    :size="size"
    @verify="(token) => emit('verify', token)"
    @expire="emit('expire')"
    @error="emit('error')"
  />
</template>

<script setup lang="ts">
import { ref } from 'vue'
import HCaptchaWidget from '@/components/HCaptchaWidget.vue'
import TurnstileWidget from '@/components/TurnstileWidget.vue'

type CaptchaProvider = 'turnstile' | 'hcaptcha'

const props = withDefaults(
  defineProps<{
    provider: CaptchaProvider
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

const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const hcaptchaRef = ref<InstanceType<typeof HCaptchaWidget> | null>(null)

const reset = () => {
  if (props.provider === 'turnstile') {
    turnstileRef.value?.reset()
    return
  }
  hcaptchaRef.value?.reset()
}

defineExpose({ reset })
</script>
