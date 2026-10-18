import { computed, ref, watch, type Ref } from 'vue'

/** Keep all preview actions on the selected result, resetting for a new result set. */
export function useResultPreviewPagination<T>(items: Readonly<Ref<T[]>>) {
  const page = ref(0)
  watch(items, () => { page.value = 0 }, { flush: 'sync' })
  const current = computed(() => items.value[page.value] ?? null)
  function previous() {
    page.value = Math.max(0, page.value - 1)
  }
  function next() {
    page.value = Math.max(0, Math.min(items.value.length - 1, page.value + 1))
  }
  return { page, current, previous, next }
}
