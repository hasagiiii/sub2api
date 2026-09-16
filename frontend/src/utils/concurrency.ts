/**
 * Run `task` over `items` with at most `limit` tasks in flight.
 *
 * Workers pull from a shared cursor, so a slow item never blocks the ones
 * behind it: the next item starts as soon as any worker frees up. Resolves once
 * every item has been processed (or `shouldContinue` asked to stop early).
 *
 * `task` is expected to record its own outcome; a rejection propagates and
 * cancels the remaining work, so callers that want per-item error handling
 * should catch inside `task`.
 *
 * `shouldContinue` is checked before each item is picked up, letting callers
 * abandon a superseded run (e.g. the user restarted it) without waiting for the
 * whole queue to drain.
 */
export async function runWithConcurrency<T>(
  items: readonly T[],
  limit: number,
  task: (item: T) => Promise<void>,
  shouldContinue: () => boolean = () => true
): Promise<void> {
  const workerCount = Math.min(Math.max(1, Math.floor(limit)), items.length)
  if (workerCount === 0) return

  let cursor = 0
  const worker = async (): Promise<void> => {
    while (cursor < items.length && shouldContinue()) {
      const item = items[cursor]
      cursor += 1
      await task(item)
    }
  }

  await Promise.all(Array.from({ length: workerCount }, () => worker()))
}
