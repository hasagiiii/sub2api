import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  clearIQTestHistory,
  createHistoryEntryID,
  deleteIQTestHistoryEntry,
  loadIQTestHistory,
  MAX_HISTORY_ENTRIES,
  MAX_OUTPUT_CHARS,
  saveIQTestHistoryEntry,
  TRUNCATION_NOTICE,
  truncateOutput
} from '../iqTestHistory'
import type { IQTestHistoryEntry } from '../iqTestHistory'

const STORAGE_KEY = 'iq_test_history'

const buildEntry = (overrides: Partial<IQTestHistoryEntry> = {}): IQTestHistoryEntry => ({
  id: 'entry-1',
  createdAt: '2026-09-16T10:00:00.000Z',
  prompt: 'draw a pelican',
  results: [
    {
      accountId: 7,
      accountName: 'Account 7',
      status: 'success',
      output: '<html></html>',
      errorMessage: '',
      inputTokens: 100,
      outputTokens: 50,
      totalTokens: 150,
      costUSD: 0.25
    }
  ],
  ...overrides
})

describe('iqTestHistory', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('returns an empty list when nothing was ever stored', () => {
    expect(loadIQTestHistory()).toEqual([])
  })

  it('persists a run and reads it back after a reload', () => {
    const stored = saveIQTestHistoryEntry(buildEntry())

    expect(stored).toHaveLength(1)
    expect(loadIQTestHistory()).toEqual(stored)
    expect(loadIQTestHistory()[0].results[0].costUSD).toBe(0.25)
  })

  it('keeps the newest run first and replaces an entry reusing the same id', () => {
    saveIQTestHistoryEntry(buildEntry({ id: 'old', createdAt: '2026-09-16T09:00:00.000Z' }))
    saveIQTestHistoryEntry(buildEntry({ id: 'new', createdAt: '2026-09-16T11:00:00.000Z' }))
    saveIQTestHistoryEntry(buildEntry({ id: 'old', prompt: 'updated prompt' }))

    const history = loadIQTestHistory()
    expect(history.map((entry) => entry.id)).toEqual(['old', 'new'])
    expect(history[0].prompt).toBe('updated prompt')
  })

  it('caps the history at MAX_HISTORY_ENTRIES, dropping the oldest run', () => {
    for (let index = 0; index < MAX_HISTORY_ENTRIES + 3; index += 1) {
      saveIQTestHistoryEntry(buildEntry({ id: `entry-${index}` }))
    }

    const history = loadIQTestHistory()
    expect(history).toHaveLength(MAX_HISTORY_ENTRIES)
    expect(history[0].id).toBe(`entry-${MAX_HISTORY_ENTRIES + 2}`)
    expect(history.some((entry) => entry.id === 'entry-0')).toBe(false)
  })

  it('truncates oversized responses instead of dropping the run', () => {
    const output = 'a'.repeat(MAX_OUTPUT_CHARS + 500)
    expect(truncateOutput(output)).toBe('a'.repeat(MAX_OUTPUT_CHARS) + TRUNCATION_NOTICE)

    saveIQTestHistoryEntry(buildEntry({ results: [{ ...buildEntry().results[0], output }] }))

    expect(loadIQTestHistory()[0].results[0].output).toHaveLength(MAX_OUTPUT_CHARS + TRUNCATION_NOTICE.length)
  })

  it('sheds the oldest runs when the browser rejects the write for quota', () => {
    saveIQTestHistoryEntry(buildEntry({ id: 'first' }))
    saveIQTestHistoryEntry(buildEntry({ id: 'second' }))

    // Reject the first two attempts (3 entries, then 2) so only the newest run fits.
    const originalSetItem = Storage.prototype.setItem
    let failures = 2
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, key: string, value: string) {
      if (failures > 0) {
        failures -= 1
        throw new DOMException('quota', 'QuotaExceededError')
      }
      originalSetItem.call(this, key, value)
    })

    const stored = saveIQTestHistoryEntry(buildEntry({ id: 'third' }))
    setItem.mockRestore()

    expect(stored.map((entry) => entry.id)).toEqual(['third'])
    expect(loadIQTestHistory().map((entry) => entry.id)).toEqual(['third'])
  })

  it('deletes a single run and keeps the rest', () => {
    saveIQTestHistoryEntry(buildEntry({ id: 'keep' }))
    saveIQTestHistoryEntry(buildEntry({ id: 'drop' }))

    const remaining = deleteIQTestHistoryEntry('drop')
    expect(remaining.map((entry) => entry.id)).toEqual(['keep'])
    expect(loadIQTestHistory().map((entry) => entry.id)).toEqual(['keep'])
  })

  it('clears every stored run', () => {
    saveIQTestHistoryEntry(buildEntry())
    clearIQTestHistory()
    expect(loadIQTestHistory()).toEqual([])
  })

  it('discards corrupted payloads rather than throwing', () => {
    localStorage.setItem(STORAGE_KEY, '{not json')
    expect(loadIQTestHistory()).toEqual([])

    localStorage.setItem(STORAGE_KEY, JSON.stringify({ nope: true }))
    expect(loadIQTestHistory()).toEqual([])
  })

  it('drops malformed entries and normalizes partial result records', () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify([
        { id: '', createdAt: '2026-09-16T10:00:00.000Z' },
        {
          id: 'partial',
          createdAt: '2026-09-16T10:00:00.000Z',
          results: [{ accountId: 3 }, { accountName: 'missing id' }]
        }
      ])
    )

    const history = loadIQTestHistory()
    expect(history).toHaveLength(1)
    expect(history[0]).toMatchObject({ id: 'partial', prompt: '' })
    expect(history[0].results).toEqual([
      {
        accountId: 3,
        accountName: '',
        status: 'failed',
        output: '',
        errorMessage: '',
        inputTokens: 0,
        outputTokens: 0,
        totalTokens: 0,
        costUSD: 0
      }
    ])
  })

  it('creates unique ids', () => {
    expect(createHistoryEntryID()).not.toBe(createHistoryEntryID())
  })
})
