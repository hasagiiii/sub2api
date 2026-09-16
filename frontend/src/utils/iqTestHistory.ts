// Local persistence for GPT-6 Astra IQ test runs.
//
// The IQ test drives the per-account SSE endpoint straight from the browser and
// the backend keeps no record of a run, so history is stored client-side. Runs
// carry full HTML responses which can be hundreds of kilobytes, therefore every
// entry is trimmed before writing and quota failures shed the oldest entries
// instead of losing the newest run.

const STORAGE_KEY = 'iq_test_history'

/** Keep the history list small so a browsing session cannot fill up localStorage. */
export const MAX_HISTORY_ENTRIES = 10

/** Per-account response cap; long HTML is truncated rather than dropped. */
export const MAX_OUTPUT_CHARS = 20000

export const TRUNCATION_NOTICE = '\n… [truncated]'

export type IQTestHistoryStatus = 'success' | 'failed'

export interface IQTestHistoryResult {
  accountId: number
  accountName: string
  status: IQTestHistoryStatus
  output: string
  errorMessage: string
  inputTokens: number
  outputTokens: number
  totalTokens: number
  costUSD: number
}

export interface IQTestHistoryEntry {
  id: string
  createdAt: string
  prompt: string
  results: IQTestHistoryResult[]
}

const getStorage = (): Storage | null => {
  if (typeof window === 'undefined') return null
  try {
    return window.localStorage
  } catch {
    // Access throws in privacy modes that disable storage entirely.
    return null
  }
}

const toFiniteNumber = (value: unknown): number => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

const toText = (value: unknown): string => (typeof value === 'string' ? value : '')

export const truncateOutput = (output: string): string => {
  if (output.length <= MAX_OUTPUT_CHARS) return output
  return output.slice(0, MAX_OUTPUT_CHARS) + TRUNCATION_NOTICE
}

const normalizeResult = (value: unknown): IQTestHistoryResult | null => {
  if (!value || typeof value !== 'object') return null
  const raw = value as Record<string, unknown>
  const accountId = Number(raw.accountId)
  if (!Number.isFinite(accountId)) return null
  return {
    accountId,
    accountName: toText(raw.accountName),
    status: raw.status === 'success' ? 'success' : 'failed',
    output: truncateOutput(toText(raw.output)),
    errorMessage: toText(raw.errorMessage),
    inputTokens: toFiniteNumber(raw.inputTokens),
    outputTokens: toFiniteNumber(raw.outputTokens),
    totalTokens: toFiniteNumber(raw.totalTokens),
    costUSD: toFiniteNumber(raw.costUSD)
  }
}

const normalizeEntry = (value: unknown): IQTestHistoryEntry | null => {
  if (!value || typeof value !== 'object') return null
  const raw = value as Record<string, unknown>
  const id = toText(raw.id)
  const createdAt = toText(raw.createdAt)
  if (!id || !createdAt) return null
  const results = Array.isArray(raw.results)
    ? raw.results.map(normalizeResult).filter((result): result is IQTestHistoryResult => result !== null)
    : []
  return { id, createdAt, prompt: toText(raw.prompt), results }
}

export const loadIQTestHistory = (): IQTestHistoryEntry[] => {
  const storage = getStorage()
  if (!storage) return []
  const serialized = storage.getItem(STORAGE_KEY)
  if (!serialized) return []
  try {
    const parsed = JSON.parse(serialized)
    if (!Array.isArray(parsed)) return []
    return parsed
      .map(normalizeEntry)
      .filter((entry): entry is IQTestHistoryEntry => entry !== null)
      .slice(0, MAX_HISTORY_ENTRIES)
  } catch {
    // Corrupted payloads are discarded rather than breaking the page.
    return []
  }
}

/**
 * Persist `entries`, shedding the oldest ones when the browser rejects the
 * write because of quota limits. Returns the list that was actually stored so
 * callers stay in sync with what a later reload will read back.
 */
const persist = (entries: IQTestHistoryEntry[]): IQTestHistoryEntry[] => {
  const storage = getStorage()
  if (!storage) return entries
  let candidate = entries.slice(0, MAX_HISTORY_ENTRIES)
  while (candidate.length > 0) {
    try {
      storage.setItem(STORAGE_KEY, JSON.stringify(candidate))
      return candidate
    } catch {
      // Most likely QuotaExceededError: drop the oldest run and retry.
      candidate = candidate.slice(0, candidate.length - 1)
    }
  }
  try {
    storage.removeItem(STORAGE_KEY)
  } catch {
    // Ignore: nothing else can be done when storage refuses every operation.
  }
  return []
}

export const saveIQTestHistoryEntry = (entry: IQTestHistoryEntry): IQTestHistoryEntry[] => {
  const trimmed: IQTestHistoryEntry = {
    ...entry,
    results: entry.results.map((result) => ({ ...result, output: truncateOutput(result.output) }))
  }
  return persist([trimmed, ...loadIQTestHistory().filter((item) => item.id !== trimmed.id)])
}

export const deleteIQTestHistoryEntry = (id: string): IQTestHistoryEntry[] =>
  persist(loadIQTestHistory().filter((entry) => entry.id !== id))

export const clearIQTestHistory = (): void => {
  const storage = getStorage()
  if (!storage) return
  try {
    storage.removeItem(STORAGE_KEY)
  } catch {
    // Ignore: storage is unavailable, nothing was persisted anyway.
  }
}

export const createHistoryEntryID = (): string => {
  const cryptoRef = typeof globalThis === 'undefined' ? undefined : globalThis.crypto
  if (cryptoRef && typeof cryptoRef.randomUUID === 'function') return cryptoRef.randomUUID()
  return `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}
