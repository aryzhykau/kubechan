/**
 * tracker.ts — promise-based resolver for Analysis.Completed WS events.
 *
 * Usage:
 *   const ok = await waitForAnalysis(runId)   // called by the component
 *   notifyAnalysisDone(runId)                  // called by wsMiddleware
 */

const TIMEOUT_MS = 5 * 60_000 // 5 min safety net

type Resolve = (ok: boolean) => void

/** Components waiting for a specific run to complete. */
const pending = new Map<string, Resolve>()

/**
 * WS event arrived before the component started waiting — keep it for
 * a short window so the component can still pick it up immediately.
 */
const done = new Set<string>()

/**
 * Returns a promise that resolves to `true` when the Analysis.Completed
 * WS event arrives for `runId`, or `false` if the 5-minute timeout elapses.
 */
export function waitForAnalysis(runId: string): Promise<boolean> {
  if (done.has(runId)) {
    done.delete(runId)
    return Promise.resolve(true)
  }
  return new Promise<boolean>(resolve => {
    const timer = setTimeout(() => {
      pending.delete(runId)
      resolve(false)
    }, TIMEOUT_MS)
    pending.set(runId, (ok: boolean) => {
      clearTimeout(timer)
      resolve(ok)
    })
  })
}

/** Called by wsMiddleware when an Analysis.Completed event arrives. */
export function notifyAnalysisDone(runId: string): void {
  const resolve = pending.get(runId)
  if (resolve) {
    pending.delete(runId)
    resolve(true)
  } else {
    // Store briefly in case the component calls waitForAnalysis just after.
    done.add(runId)
    setTimeout(() => done.delete(runId), 30_000)
  }
}
