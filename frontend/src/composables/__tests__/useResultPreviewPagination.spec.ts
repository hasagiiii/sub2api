import { effectScope, ref } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import { useResultPreviewPagination } from '../useResultPreviewPagination'

const scopes: ReturnType<typeof effectScope>[] = []
function setup(initial = [{ url: 'first' }, { url: 'second' }]) {
  const items = ref(initial)
  const scope = effectScope()
  scopes.push(scope)
  return { items, ...scope.run(() => useResultPreviewPagination(items))! }
}
afterEach(() => scopes.splice(0).forEach(scope => scope.stop()))

describe('result preview pagination', () => {
  it('selects the current image for preview, download and material actions within page bounds', () => {
    const preview = setup()
    expect(preview.current.value?.url).toBe('first')
    preview.previous()
    expect(preview.page.value).toBe(0)
    preview.next()
    expect(preview.current.value?.url).toBe('second')
    preview.next()
    expect(preview.page.value).toBe(1)
    preview.previous()
    expect(preview.current.value?.url).toBe('first')
  })
  it('resets to the first image when a new task or history result arrives', () => {
    const preview = setup()
    preview.next()
    preview.items.value = [{ url: 'new-first' }, { url: 'new-second' }]
    expect(preview.page.value).toBe(0)
    expect(preview.current.value?.url).toBe('new-first')
    preview.next()
    preview.items.value = [{ url: 'single' }]
    expect(preview.current.value?.url).toBe('single')
  })
  it('does not retain a previous image when the new result is empty', () => {
    const preview = setup()
    preview.next()
    preview.items.value = []
    preview.next()
    preview.previous()
    expect(preview.page.value).toBe(0)
    expect(preview.current.value).toBeNull()
  })
})
