import { useState, useEffect, useCallback } from 'react'
import { api, type Incident, type AnalysisResult, type DiagnosticRunSummary } from './api'
import { useWebSocket, type WSEvent } from './useWebSocket'
import { type ChatterEvent } from './chatter'

function incidentLabel(incident: Incident): { title: string; sub: string } {
  const { rootResource } = incident.spec
  const ns = rootResource.namespace || incident.metadata.namespace || 'default'
  const affected = incident.status.activeProblemCases ?? 0
  const title = `${rootResource.kind} ${rootResource.name} in ${ns}`
  const sub = affected > 0 ? `${affected} resource${affected !== 1 ? 's' : ''} also affected` : ''
  return { title, sub }
}

function timeAgo(iso?: string): string {
  if (!iso) return '—'
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

interface AnalysisState {
  status: 'idle' | 'pending' | 'collecting' | 'done' | 'error'
  diagnosticRunId?: string
  error?: string
}

function ConfidenceBadge({ confidence }: { confidence?: number }) {
  if (confidence == null) return null
  const pct = Math.round(confidence * 100)
  const cls = pct >= 80 ? 'confidence-high' : pct >= 50 ? 'confidence-medium' : 'confidence-low'
  return <span className={`confidence-badge ${cls}`}>{pct}%</span>
}

function IncidentRow({ incident, onAnalyzed, onAnalysisStart, previousRun }: {
  incident: Incident
  onAnalyzed: (name: string, runId: string) => void
  onAnalysisStart: (incidentName: string) => void
  previousRun?: DiagnosticRunSummary
}) {
  const [analysis, setAnalysis] = useState<AnalysisState>({ status: 'idle' })
  const id = incident.metadata.name
  const isResolved = incident.status.state === 'resolved'
  const { title, sub } = incidentLabel(incident)

  // If a fresh result arrives via WS, flip to done.
  useEffect(() => {
    if (previousRun?.analysisResultId && analysis.status === 'collecting') {
      setAnalysis({ status: 'done' })
    }
  }, [previousRun?.analysisResultId])

  async function handleAnalyze() {
    setAnalysis({ status: 'pending' })
    onAnalysisStart(title)
    try {
      const res = await api.analyze(id)
      setAnalysis({ status: 'collecting', diagnosticRunId: res.diagnosticRunId })
      onAnalyzed(incident.metadata.name, res.diagnosticRunId)
    } catch (e: unknown) {
      setAnalysis({ status: 'error', error: String(e) })
    }
  }

  const isInFlight = analysis.status === 'pending' || analysis.status === 'collecting'
  const alreadyAnalyzed = !!previousRun?.analysisResultId

  return (
    <div className={`incident-row ${isResolved ? 'resolved' : 'open'}`}>
      <div className="incident-header">
        <span className={`state-badge ${incident.status.state}`}>
          {incident.status.state.toUpperCase()}
        </span>
        <div className="incident-title-block">
          <strong className="incident-name">{title}</strong>
          {sub && <span className="incident-affected">{sub}</span>}
        </div>
        <span className="incident-meta muted">
          opened {timeAgo(incident.status.openedAt)}
        </span>
      </div>

      {(incident.spec.problemCases ?? []).length > 0 && (
        <div className="problem-cases">
          {incident.spec.problemCases!.map(pc => (
            <span key={pc} className="pc-chip">{pc}</span>
          ))}
        </div>
      )}

      <div className="incident-actions">
        {isInFlight && (
          <span className="status-text pending"><span className="spinner" /> KubeChan is on it…</span>
        )}
        {analysis.status === 'error' && (
          <span className="status-text error">✗ {analysis.error}</span>
        )}
        {!isInFlight && analysis.status !== 'error' && (
          <div className="incident-actions-row">
            {alreadyAnalyzed && (
              <span className="status-text analyzed">
                ✓ analyzed
                <ConfidenceBadge confidence={previousRun!.confidence} />
              </span>
            )}
            {!isResolved && (
              <button className="btn-analyze" onClick={handleAnalyze}>
                {alreadyAnalyzed ? 'Ask KubeChan again' : 'Ask KubeChan to help'}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

export interface IncidentListProps {
  onAnalysisStart: (incidentName: string) => void
  onAnalysisComplete: (result: AnalysisResult, incidentName: string) => void
  onAction?: (event: ChatterEvent) => void
}

export function IncidentList({ onAnalysisStart, onAnalysisComplete, onAction }: IncidentListProps) {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [wsConnected, setWsConnected] = useState(false)
  // Map incidentId → most recent DiagnosticRunSummary that has an analysis result
  const [priorRuns, setPriorRuns] = useState<Map<string, DiagnosticRunSummary>>(new Map())

  const load = useCallback(async () => {
    try {
      const [incData, runData] = await Promise.all([
        api.listIncidents(),
        api.listDiagnosticRuns(),
      ])
      setIncidents(incData.sort((a, b) =>
        (b.status.openedAt ?? '').localeCompare(a.status.openedAt ?? '')
      ))
      // react to incident count
      const open = incData.filter(i => i.status.state !== 'resolved')
      if (open.length === 0) onAction?.('no-incidents')
      else if (open.length >= 5) onAction?.('many-incidents')
      // Build map: incidentId → latest run that has a completed analysis
      const map = new Map<string, DiagnosticRunSummary>()
      for (const run of runData) {
        if (!run.incidentId) continue
        const existing = map.get(run.incidentId)
        if (!existing || run.requestedAt > existing.requestedAt) {
          map.set(run.incidentId, run)
        }
      }
      setPriorRuns(map)
      setError(null)
    } catch (e: unknown) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleWS = useCallback((event: WSEvent) => {
    if (event.type === 'connection') {
      setWsConnected(true)
      return
    }
    if (event.type === 'Analysis.Completed') {
      const analysisId = event.analysisId as string
      const incidentId = event.incidentId as string
      if (analysisId) {
        api.getAnalysisResult(analysisId)
          .then(result => {
            // Update priorRuns so the row reflects the fresh result immediately
            setPriorRuns(prev => {
              const next = new Map(prev)
              next.set(incidentId, {
                diagnosticRunId: result.diagnosticRunId,
                incidentId,
                requestedAt: result.createdAt,
                status: 'completed',
                analysisResultId: result.id,
                likelyRootCause: result.likelyRootCause,
                confidence: result.confidence,
                model: result.model,
                analysisCreatedAt: result.createdAt,
              })
              return next
            })
            onAnalysisComplete(result, incidentId)
          })
          .catch(console.error)
      }
      return
    }
    if (
      event.type?.startsWith('Incident.') ||
      event.type?.startsWith('ProblemCase.')
    ) {
      load()
    }
  }, [load, onAnalysisComplete])

  useWebSocket(handleWS)

  if (loading) return <p className="loading">Loading incidents…</p>
  if (error) return <p className="error-msg">Error: {error}</p>

  const open = incidents.filter(i => i.status.state !== 'resolved')
  const resolved = incidents.filter(i => i.status.state === 'resolved')

  return (
    <div className="incident-list">
      <div className="list-header">
        <h2>Incidents</h2>
        <div className="header-right">
          <span className={`ws-dot ${wsConnected ? 'connected' : 'disconnected'}`} title={wsConnected ? 'Live' : 'Connecting…'} />
          <button className="btn-refresh" onClick={load}>↺ Refresh</button>
        </div>
      </div>

      {open.length === 0 && <p className="empty">No open incidents 🎉</p>}
      {open.map(inc => (
        <IncidentRow
          key={inc.metadata.name}
          incident={inc}
          onAnalyzed={() => load()}
          onAnalysisStart={onAnalysisStart}
          previousRun={priorRuns.get(inc.metadata.name)}
        />
      ))}

      {resolved.length > 0 && (
        <details className="resolved-section">
          <summary>{resolved.length} resolved incident{resolved.length > 1 ? 's' : ''}</summary>
          {resolved.map(inc => (
            <IncidentRow
              key={inc.metadata.name}
              incident={inc}
              onAnalyzed={() => load()}
              onAnalysisStart={onAnalysisStart}
              previousRun={priorRuns.get(inc.metadata.name)}
            />
          ))}
        </details>
      )}
    </div>
  )
}
