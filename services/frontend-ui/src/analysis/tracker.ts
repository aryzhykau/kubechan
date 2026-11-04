/**
 * tracker.ts — resolves analysis lifecycle events, with an API fallback if WebSocket is missed.
 *
 * Usage:
 *   const ok = await waitForAnalysis(runId)   // called by the component
 *   notifyAnalysisDone(runId)                  // called by wsMiddleware
 */

import { getToken } from '../api/index'

const TIMEOUT_MS = 5 * 60_000 // 5 min safety net

type Resolve = (ok: boolean) => void

/** Components waiting for a specific run to complete. */
const pending = new Map<string, Resolve>()

/**
 * WS event arrived before the component started waiting — keep it for
 * a short window so the component can still pick it up immediately.
 */
const done = new Map<string, boolean>()

/**
 * Returns a promise that resolves to `true` when the Analysis.Completed
 * WS event arrives for `runId`, or `false` if the 5-minute timeout elapses.
 */
export function waitForAnalysis(runId: string): Promise<boolean> {
  if (done.has(runId)) {
    const ok = done.get(runId)!
    done.delete(runId)
    return Promise.resolve(ok)
  }
  return new Promise<boolean>(resolve => {
    let polling = false
    const poll = setInterval(async () => {
      if (!pending.has(runId) || polling) return
      polling = true
      try {
        const token = getToken()
        const headers: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {}
        const response = await fetch(`/api/v1/diagnosticruns/${encodeURIComponent(runId)}`, { headers })
        if (!response.ok) return
        const detail = await response.json() as { diagnosticRun?: { status?: { analysisState?: string } } }
        if (!pending.has(runId)) return
        const state = detail.diagnosticRun?.status?.analysisState
        if (state === 'completed') notifyAnalysisDone(runId)
        if (state === 'failed') notifyAnalysisFailed(runId)
      } catch { /* WebSocket or a later poll can still settle the run. */ }
      finally { polling = false }
    }, 10_000)
    const timer = setTimeout(() => {
      pending.delete(runId)
      clearInterval(poll)
      resolve(false)
    }, TIMEOUT_MS)
    pending.set(runId, (ok: boolean) => {
      clearTimeout(timer)
      clearInterval(poll)
      resolve(ok)
    })
  })
}

/** Called by wsMiddleware when an Analysis.Completed event arrives. */
export function notifyAnalysisDone(runId: string): void {
  notifyAnalysisSettled(runId, true)
}

export function notifyAnalysisFailed(runId: string): void {
  notifyAnalysisSettled(runId, false)
}

function notifyAnalysisSettled(runId: string, ok: boolean): void {
  const resolve = pending.get(runId)
  if (resolve) {
    pending.delete(runId)
    resolve(ok)
  } else {
    // Store briefly in case the component calls waitForAnalysis just after.
    done.set(runId, ok)
    setTimeout(() => done.delete(runId), 30_000)
  }
}
