import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { useVideoPlayground } from '../useVideoPlayground'

const api = vi.hoisted(() => ({ submit: vi.fn(), status: vi.fn(), extractVideoUrls: vi.fn(() => []) }))
vi.mock('@/api/videoPlayground', () => ({ default: api }))
const instances: ReturnType<typeof useVideoPlayground>[] = []
function setup() { const instance = useVideoPlayground(); instances.push(instance); return instance }

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  sessionStorage.clear()
  vi.stubGlobal('crypto', {
    randomUUID: () => 'stable-task',
    subtle: { digest: async (_: string, data: Uint8Array) => data.buffer }
  })
  api.submit.mockResolvedValue({ request_id: 'accepted-task' })
  api.status.mockResolvedValue({ status: 'IN_PROGRESS' })
})
afterEach(() => {
  instances.splice(0).forEach(instance => instance.reset())
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('playground durable task recovery', () => {
  it('recovers a lost submit response by its original ID without another generation request', async () => {
    api.submit.mockRejectedValue({ code: 'ECONNABORTED', message: 'timeout of 60000ms exceeded' })
    api.status.mockResolvedValue({ status: 'COMPLETED', data: { images: [{ url: 'one' }, { url: 'two' }] } })
    const playground = setup()
    expect(await playground.start('openai/image', { num_images: 2 }, 'key')).toBe(true)
    await flushPromises()
    expect(api.submit).toHaveBeenCalledTimes(1)
    expect(api.status).toHaveBeenCalledWith('openai/image', 'stable-task', 'key', expect.any(Object))
    expect(playground.phase.value).toBe('completed')
    expect(playground.resultPayload.value?.images).toHaveLength(2)
    expect(playground.errorMessage.value).toBe('')
  })
  it('keeps polling after a transient status timeout and clears the notice after recovery', async () => {
    api.status.mockRejectedValueOnce(new Error('timeout')).mockResolvedValue({ status: 'COMPLETED', data: { images: [{ url: 'one' }] } })
    const playground = setup()
    await playground.start('image', {}, 'key')
    await flushPromises()
    expect(playground.notice.value).toBe('queryRetrying')
    await vi.advanceTimersByTimeAsync(3000)
    expect(playground.phase.value).toBe('completed')
    expect(playground.notice.value).toBe('')
  })
  it('resumes a persisted task after page teardown without storing the API key', async () => {
    const first = setup()
    await first.start('image', {}, 'secret-api-key')
    await flushPromises()
    const saved = Array.from({ length: sessionStorage.length }, (_, i) => sessionStorage.getItem(sessionStorage.key(i)!)).join('')
    expect(saved).not.toContain('secret-api-key')
    first.reset()
    const next = setup()
    expect(await next.resume('image', 'other-key')).toBe(false)
    expect(await next.resume('image', 'secret-api-key')).toBe(true)
    await flushPromises()
    expect(next.requestId.value).toBe('accepted-task')
    expect(api.submit).toHaveBeenCalledTimes(1)
  })
  it('does not let an obsolete response overwrite a reset playground', async () => {
    let resolve!: (value: unknown) => void
    api.status.mockReturnValue(new Promise(r => { resolve = r }))
    const playground = setup()
    await playground.start('image', {}, 'key')
    playground.reset()
    resolve({ status: 'COMPLETED', data: { images: [{ url: 'old' }] } })
    await flushPromises()
    expect(playground.phase.value).toBe('idle')
    expect(playground.resultPayload.value).toBeNull()
  })
  it('shows a structured backend failure and never treats it as a successful result', async () => {
    api.status.mockResolvedValue({ status: 'FAILED', error: { message: 'Generation outcome unknown; review required' } })
    const playground = setup()
    await playground.start('image', {}, 'key')
    await flushPromises()
    expect(playground.phase.value).toBe('failed')
    expect(playground.errorMessage.value).toContain('review required')
    expect(playground.resultPayload.value).toBeNull()
  })
  it('bounds confirmation retries for a task that was never accepted', async () => {
    api.submit.mockRejectedValue(new Error('connection lost'))
    api.status.mockRejectedValue({ response: { status: 404 } })
    const playground = setup()
    await playground.start('image', {}, 'key')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(63_000)
    expect(playground.phase.value).toBe('failed')
    expect(playground.errorMessage.value).toContain('Unable to confirm')
    expect(api.submit).toHaveBeenCalledTimes(1)
  })
})
