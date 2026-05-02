import { useState, useEffect, useCallback } from 'react'
import { api, type Incident, type AnalysisResult } from './api'
import { useWebSocket, type WSEvent } from './useWebSocket'

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

function IncidentRow({ incident, onAnalyzed, onAnalysisStart, hasResult }: {
  incident: Incident
  onAnalyzed: (name: string, runId: string) => void
  onAnalysisStart: (incidentName: string) => void
  hasResult?: boolean
}) {
  const [analysis, setAnalysis] = useState<AnalysisState>({ status: 'idle' })
  const id = incident.metadata.name
  const isResolved = incident.status.state === 'resolved'
  const { title, sub } = incidentLabel(incident)

  useEffect(() => {
    if (hasResult && analysis.status !== 'idle') {
      setAnalysis({ status: 'done' })
    }
  }, [hasResult])

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
        {!isResolved && analysis.status === 'idle' && !hasResult && (
          <button className="btn-analyze" onClick={handleAnalyze}>
            Ask KubeChan to help
          </button>
        )}
        {(analysis.status === 'pending' || analysis.status === 'collecting') && !hasResult && (
          <span className="status-text pending"><span className="spinner" /> KubeChan is on it…</span>
        )}
        {analysis.status === 'error' && (
          <span className="status-text error">✗ {analysis.error}</span>
        )}
        {hasResult && (
          <span className="status-text analyzed">✓ KubeChan analyzed</span>
        )}
      </div>
    </div>
  )
}

export interface IncidentListProps {
  onAnalysisStart: (incidentName: string) => void
  onAnalysisComplete: (result: AnalysisResult, incidentName: string) => void
}

export function IncidentList({ onAnalysisStart, onAnalysisComplete }: IncidentListProps) {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [wsConnected, setWsConnected] = useState(false)
  const [analyzedIncidents, setAnalyzedIncidents] = useState<Set<string>>(new Set())

  const load = useCallback(async () => {
    try {
      const data = await api.listIncidents()
      setIncidents(data.sort((a, b) =>
        (b.status.openedAt ?? '').localeCompare(a.status.openedAt ?? '')
      ))
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
            setAnalyzedIncidents(prev => new Set(prev).add(incidentId))
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
          hasResult={analyzedIncidents.has(inc.metadata.name)}
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
              hasResult={analyzedIncidents.has(inc.metadata.name)}
            />
          ))}
        </details>
      )}
    </div>
  )
}


