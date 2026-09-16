import { describe, expect, it, vi } from 'vitest'
import { runWithConcurrency } from '../concurrency'

/** A promise plus its resolver, so a test can decide when a task finishes. */
const deferred = () => {
  let resolve!: () => void
  const promise = new Promise<void>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

describe('runWithConcurrency', () => {
  it('starts tasks in parallel up to the limit', async () => {
    const gates = [deferred(), deferred(), deferred(), deferred(), deferred()]
    const started: number[] = []

    const done = runWithConcurrency([0, 1, 2, 3, 4], 3, async (index) => {
      started.push(index)
      await gates[index].promise
    })

    // Three workers pick up the first three items before anything resolves.
    await Promise.resolve()
    expect(started).toEqual([0, 1, 2])

    gates.forEach((gate) => gate.resolve())
    await done
    expect(started).toEqual([0, 1, 2, 3, 4])
  })

  it('never exceeds the limit of in-flight tasks', async () => {
    let inFlight = 0
    let peak = 0

    await runWithConcurrency(Array.from({ length: 20 }, (_, index) => index), 4, async () => {
      inFlight += 1
      peak = Math.max(peak, inFlight)
      await new Promise((resolve) => setTimeout(resolve, 1))
      inFlight -= 1
    })

    expect(peak).toBe(4)
    expect(inFlight).toBe(0)
  })

  it('lets a fast task move on without waiting for a slow earlier one', async () => {
    const slow = deferred()
    const completed: string[] = []

    const done = runWithConcurrency(['slow', 'fast', 'next'], 2, async (name) => {
      if (name === 'slow') await slow.promise
      completed.push(name)
    })

    // 'fast' and then 'next' finish while 'slow' is still pending.
    await vi.waitFor(() => expect(completed).toEqual(['fast', 'next']))

    slow.resolve()
    await done
    expect(completed).toEqual(['fast', 'next', 'slow'])
  })

  it('processes every item when the limit exceeds the item count', async () => {
    const seen: number[] = []
    await runWithConcurrency([1, 2], 10, async (item) => {
      seen.push(item)
    })
    expect(seen).toEqual([1, 2])
  })

  it('falls back to a single worker for a non-positive limit', async () => {
    let inFlight = 0
    let peak = 0

    await runWithConcurrency([1, 2, 3], 0, async () => {
      inFlight += 1
      peak = Math.max(peak, inFlight)
      await Promise.resolve()
      inFlight -= 1
    })

    expect(peak).toBe(1)
  })

  it('does nothing for an empty list', async () => {
    const task = vi.fn()
    await runWithConcurrency([], 4, task)
    expect(task).not.toHaveBeenCalled()
  })

  it('stops picking up new items once shouldContinue turns false', async () => {
    const seen: number[] = []
    let keepGoing = true

    await runWithConcurrency(
      [1, 2, 3, 4, 5, 6],
      1,
      async (item) => {
        seen.push(item)
        if (item === 2) keepGoing = false
      },
      () => keepGoing
    )

    expect(seen).toEqual([1, 2])
  })

  it('propagates a task rejection', async () => {
    await expect(
      runWithConcurrency([1, 2], 2, async (item) => {
        if (item === 2) throw new Error('boom')
      })
    ).rejects.toThrow('boom')
  })
})
