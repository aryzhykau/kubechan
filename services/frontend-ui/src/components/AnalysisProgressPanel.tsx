import { useState } from 'react'
import { Button } from '@mui/material'
import type { AnalysisProgress } from '../api/index'
import { analysisActivityEvents, analysisEventLabel, analysisProgressLabel, analysisProgressReason, analysisProgressTitle, analysisValidationReason } from '../analysis/progress'
import { useCancelAnalysisMutation } from '../store/api/diagnosticsApi'
import './AnalysisProgressPanel.css'

export function AnalysisProgressPanel({ progress, compact = false }: { progress?: AnalysisProgress; compact?: boolean }) {
  const [cancelAnalysis, { isLoading: cancelling }] = useCancelAnalysisMutation()
  const [cancelError, setCancelError] = useState('')
  if (!progress?.available) return null
  const running = progress.status === 'running'
  const successful = progress.status === 'completed' || progress.status === 'partial'
  const reason = analysisProgressReason(progress)
  const validation = analysisValidationReason(progress)
  const events = analysisActivityEvents(progress)
  const turn = Math.max(0, ...progress.events.map(event => event.turn ?? 0))
  async function cancel() {
    setCancelError('')
    try { await cancelAnalysis(progress!.runId).unwrap() }
    catch { setCancelError('Could not cancel the analysis. Refresh its status and try again.') }
  }
  return <section className={`analysis-activity${!running && !successful ? ' analysis-activity--stopped' : ''}`} aria-label="Analysis activity">
    <div className="analysis-activity__heading">
      <strong>{running ? 'Agent activity' : analysisProgressTitle(progress)}</strong>
      {turn > 0 && <small>{turn} model request{turn === 1 ? '' : 's'}</small>}
      {running && <Button size="small" color="inherit" disabled={cancelling} onClick={() => { void cancel() }}>Cancel analysis</Button>}
    </div>
    <div role="status" aria-live="polite" aria-atomic="true" className="analysis-activity__current">
      {running ? analysisProgressLabel(progress) : reason || analysisProgressTitle(progress)}
    </div>
    {!successful && validation && validation !== reason && <p className="analysis-activity__validation">{running ? 'Response correction' : 'Last response rejected'}: {validation}</p>}
    {cancelError && <p role="alert">{cancelError}</p>}
    {events.length > 0 && <details className="analysis-activity__history" open={running && !compact}>
      <summary>Activity history · {events.length} steps</summary>
      <ol>{events.map(event => <li key={`${event.attemptId}-${event.seq}`} className={event.phase === 'failed' ? 'analysis-activity__event--failed' : ''}>
        <time dateTime={event.recordedAt}>{new Date(event.recordedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}</time>
        <span>{analysisEventLabel(event)}</span>
      </li>)}</ol>
    </details>}
  </section>
}
