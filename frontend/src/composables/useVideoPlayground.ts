/**
 * useVideoPlayground
 *
 * 视频演练台的任务状态机 composable，封装：
 *   1. submit → 拿 request_id
 *   2. 每 3 秒查询统一的 /requests/{id} 接口
 *   3. status=COMPLETED 时直接读取响应 data
 *   4. reset 本地状态（已提交任务仍在服务端继续执行）
 *
 * 不做超时兜底（视频渲染可以跑几分钟）。
 */

import { ref, computed } from 'vue'
import videoPlaygroundAPI, {
  type StatusResponse,
  type ResultResponse,
} from '@/api/videoPlayground'

const POLL_INTERVAL_MS = 3000

export type PlaygroundPhase = 'idle' | 'submitting' | 'queued' | 'running' | 'completed' | 'failed' | 'canceled'

export function useVideoPlayground() {
  const phase = ref<PlaygroundPhase>('idle')
  const requestId = ref<string>('')
  // internalRequestId：提交前由前端预生成的稳定 ID（UUID v4），作为 x-client-request-id header
  // 传入后端，后端会把它落到任务表，便于幂等追踪。
  const internalRequestId = ref<string>('')
  const errorMessage = ref<string>('')
  const notice = ref<'submitUnconfirmed' | 'queryRetrying' | ''>('')
  let generation = 0
  let recoveryStartedAt = 0
  let activeStorageKey = ''
  const statusPayload = ref<StatusResponse | null>(null)
  const resultPayload = ref<ResultResponse | null>(null)
  const submittedAt = ref<number>(0)

  let pollTimer: number | null = null
  let pollAbort: AbortController | null = null

  const videoUrls = computed(() => videoPlaygroundAPI.extractVideoUrls(resultPayload.value))
  const elapsedSeconds = computed(() => {
    if (!submittedAt.value) return 0
    return Math.floor((Date.now() - submittedAt.value) / 1000)
  })
  // 让 elapsedSeconds 每秒自动重算
  const tick = ref(0)
  let tickTimer: number | null = null

  function startTickTimer() {
    stopTickTimer()
    tickTimer = window.setInterval(() => {
      tick.value += 1
    }, 1000)
  }
  function stopTickTimer() {
    if (tickTimer !== null) {
      window.clearInterval(tickTimer)
      tickTimer = null
    }
  }

  const displayElapsed = computed(() => {
    // 依赖 tick 触发响应式重算
    void tick.value
    if (!submittedAt.value) return 0
    return Math.floor((Date.now() - submittedAt.value) / 1000)
  })

  function extractErrorMessage(err: unknown, fallback: string): string {
    if (!err) return fallback
    if (typeof err === 'string') return err
    if (typeof err === 'object') {
      const anyErr = err as { response?: { data?: { detail?: unknown; error?: unknown; message?: unknown } }; message?: string }
      const data = anyErr.response?.data
      if (data && typeof data === 'object') {
        for (const k of ['detail', 'error', 'message'] as const) {
          const v = (data as Record<string, unknown>)[k]
          if (typeof v === 'string' && v) return v
          if (v && typeof v === 'object') {
            const nested = (v as Record<string, unknown>).message
            if (typeof nested === 'string' && nested) return nested
          }
        }
      }
      if (typeof anyErr.message === 'string' && anyErr.message) return anyErr.message
    }
    return fallback
  }

  function stopPolling() {
    if (pollTimer !== null) {
      window.clearTimeout(pollTimer)
      pollTimer = null
    }
    if (pollAbort) {
      try {
        pollAbort.abort()
      } catch {
        // ignore
      }
      pollAbort = null
    }
  }

  async function pollOnce(slug: string, apiKey: string): Promise<void> {
    const run = generation
    const controller = new AbortController()
    pollAbort = controller
    let s: StatusResponse
    try {
      s = await videoPlaygroundAPI.status(slug, requestId.value, apiKey, {
        signal: controller.signal,
      })
    } catch (err) {
      if (run !== generation || controller.signal.aborted) return
      // 静默失败：网络抖动/单次 5xx 都允许下一次轮询继续尝试
      // 但 401/403（例如 Key 被停用）视为终态失败
      const anyErr = err as { response?: { status?: number } }
      const st = anyErr.response?.status
      if (st === 404 && recoveryStartedAt && Date.now() - recoveryStartedAt >= 60_000) {
        phase.value = 'failed'
        notice.value = 'submitUnconfirmed'
        errorMessage.value = 'Unable to confirm the task. Check task history before submitting again.'
        stopPolling()
        stopTickTimer()
        return
      }
      if (st === 401 || st === 403 || (st === 404 && !recoveryStartedAt)) {
        phase.value = 'failed'
        errorMessage.value = extractErrorMessage(err, `Auth error (HTTP ${st}).`)
        stopPolling()
        stopTickTimer()
        return
      }
      notice.value = recoveryStartedAt ? 'submitUnconfirmed' : 'queryRetrying'
      // 其他错误：安排下一次轮询
      pollTimer = window.setTimeout(() => {
        void pollOnce(slug, apiKey)
      }, POLL_INTERVAL_MS)
      return
    }
    if (run !== generation || controller.signal.aborted) return
    notice.value = ''
    recoveryStartedAt = 0
    persist(slug)
    statusPayload.value = s
    const st = String(s.status || '').toUpperCase()

    if (st === 'COMPLETED') {
      phase.value = 'completed'
      stopPolling()
      resultPayload.value = s.data ?? null
      if (!resultPayload.value) {
        phase.value = 'failed'
        errorMessage.value = 'Completed task did not include a result.'
      }
      stopTickTimer()
      return
    }

    if (st === 'FAILED' || st === 'ERROR') {
      phase.value = 'failed'
      errorMessage.value = extractErrorMessage({ response: { data: s } }, 'Task failed on upstream.')
      stopPolling()
      stopTickTimer()
      return
    }

    if (st === 'CANCELED' || st === 'CANCELLED') {
      phase.value = 'canceled'
      stopPolling()
      stopTickTimer()
      return
    }

    if (st === 'IN_PROGRESS') {
      phase.value = 'running'
    } else {
      // IN_QUEUE 或其他中间态
      phase.value = 'queued'
    }
    // 安排下一次轮询
    pollTimer = window.setTimeout(() => {
      void pollOnce(slug, apiKey)
    }, POLL_INTERVAL_MS)
  }

  async function start(
    slug: string,
    body: Record<string, unknown>,
    apiKey: string
  ): Promise<boolean> {
    reset()
    const run = generation
    if (!slug || !apiKey) {
      phase.value = 'failed'
      errorMessage.value = 'Missing slug or API Key.'
      return false
    }
    phase.value = 'submitting'
    submittedAt.value = Date.now()
    // 预生成稳定的 internal_request_id（首选 crypto.randomUUID，未命中时退到 timestamp+random）
    // 任务终态由统一的 /requests/{id} 查询返回，不依赖上游返回的 request_id。
    const rid = generateClientRequestId()
    internalRequestId.value = rid
    startTickTimer()
    activeStorageKey = await taskStorageKey(slug, apiKey)
    if (run !== generation) return false
    requestId.value = rid
    recoveryStartedAt = Date.now()
    persist(slug)
    try {
      const s = await videoPlaygroundAPI.submit(slug, body, apiKey, { internalRequestId: rid })
      if (run !== generation) return false
      if (!s || !s.request_id) {
        phase.value = 'failed'
        errorMessage.value = 'Submit succeeded but no request_id returned.'
        stopTickTimer()
        return false
      }
      requestId.value = s.request_id
      recoveryStartedAt = 0
      persist(slug)
      phase.value = 'queued'
      // 立即触发第一次轮询
      void pollOnce(slug, apiKey)
      return true
    } catch (err) {
      if (run !== generation) return false
      const status = (err as { response?: { status?: number } }).response?.status
      if (!status || status >= 500) {
        phase.value = 'queued'
        notice.value = 'submitUnconfirmed'
        void pollOnce(slug, apiKey)
        return true
      }
      phase.value = 'failed'
      errorMessage.value = extractErrorMessage(err, 'Failed to submit task.')
      forget()
      stopTickTimer()
      return false
    }
  }

  async function taskStorageKey(slug: string, apiKey: string): Promise<string> {
    try {
      const hash = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(apiKey))
      const fingerprint = Array.from(new Uint8Array(hash), byte => byte.toString(16).padStart(2, '0')).join('')
      return `playground-task:${fingerprint}:${slug}`
    } catch { return '' }
  }
  function persist(slug: string) {
    if (!activeStorageKey) return
    try { sessionStorage.setItem(activeStorageKey, JSON.stringify({ slug, requestId: requestId.value, internalRequestId: internalRequestId.value, submittedAt: submittedAt.value, recovering: recoveryStartedAt > 0 })) } catch { /* storage is optional */ }
  }
  function forget() {
    try { if (activeStorageKey) sessionStorage.removeItem(activeStorageKey) } catch { /* storage is optional */ }
  }
  async function resume(slug: string, apiKey: string): Promise<boolean> {
    if (phase.value !== 'idle') return false
    const run = generation
    const key = await taskStorageKey(slug, apiKey)
    if (run !== generation || phase.value !== 'idle' || !key) return false
    try {
      const saved = JSON.parse(sessionStorage.getItem(key) || 'null')
      if (!saved || saved.slug !== slug || typeof saved.requestId !== 'string' || !saved.requestId || typeof saved.submittedAt !== 'number' || Date.now() - saved.submittedAt > 24 * 60 * 60 * 1000) return false
      activeStorageKey = key
      requestId.value = saved.requestId
      internalRequestId.value = saved.internalRequestId || saved.requestId
      submittedAt.value = saved.submittedAt
      recoveryStartedAt = saved.recovering ? Date.now() : 0
      phase.value = 'queued'
      startTickTimer()
      void pollOnce(slug, apiKey)
      return true
    } catch { return false }
  }

  function reset() {
    generation += 1
    activeStorageKey = ''
    recoveryStartedAt = 0
    notice.value = ''
    stopPolling()
    stopTickTimer()
    phase.value = 'idle'
    requestId.value = ''
    internalRequestId.value = ''
    errorMessage.value = ''
    statusPayload.value = null
    resultPayload.value = null
    submittedAt.value = 0
    tick.value = 0
  }

  const isTerminal = computed(
    () => phase.value === 'completed' || phase.value === 'failed' || phase.value === 'canceled'
  )
  const isBusy = computed(
    () => phase.value === 'submitting' || phase.value === 'queued' || phase.value === 'running'
  )

  return {
    // state
    phase,
    requestId,
    internalRequestId,
    errorMessage,
    notice,
    statusPayload,
    resultPayload,
    videoUrls,
    elapsedSeconds,
    displayElapsed,
    // derived
    isTerminal,
    isBusy,
    // actions
    start,
    resume,
    reset,
  }
}

/**
 * generateClientRequestId：不依赖任何外部包。
 * 优先用 crypto.randomUUID（主流现代浏览器均支持），少数环境下退到 timestamp+random，
 * 上游只当这个值为幂等键使用。
 */
function generateClientRequestId(): string {
  try {
    const g = (globalThis as unknown as { crypto?: { randomUUID?: () => string } }).crypto
    if (g && typeof g.randomUUID === 'function') {
      return g.randomUUID()
    }
  } catch {
    /* ignore */
  }
  return `pg-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}
