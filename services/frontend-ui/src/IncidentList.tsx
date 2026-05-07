import { useState, useEffect, useCallback } from 'react'
import { api, type Incident, type AnalysisResult, type DiagnosticRunSummary } from './api'
import { useWebSocket, type WSEvent } from './useWebSocket'
import { type ChatterEvent } from './chatter'
import { AugmentIncidentModal } from './AugmentIncidentModal'

function incidentLabel(incident: Incident): { title: string; sub: string } {
  const { rootResource } = incident.spec
  const ns = rootResource.namespace || incident.metadata.namespace || 'default'
  const affected = incident.status.activeProblemCases ?? 0
  const title = `${rootResource.kind} ${rootResource.name} in ${ns}`
  // For manual incidents use the userMessage as the sub-label if no affected count.
  const sub = affected > 0
    ? `${affected} resource${affected !== 1 ? 's' : ''} also affected`
    : (incident.spec.source === 'manual' && incident.spec.userMessage)
      ? incident.spec.userMessage.slice(0, 80) + (incident.spec.userMessage.length > 80 ? '…' : '')
      : ''
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

function ResourcePill({ kind, name, namespace }: { kind: string; name: string; namespace?: string }) {
  return (
    <span className="inc-detail-pill">
      <span className="inc-detail-pill-kind">{kind}</span>
      <span className="inc-detail-pill-name">{name}</span>
      {namespace && <span className="inc-detail-pill-ns">{namespace}</span>}
    </span>
  )
}

function IncidentDetails({ incident, previousRun }: {
  incident: Incident
  previousRun?: DiagnosticRunSummary
}) {
  const isManual = incident.spec.source === 'manual'
  const related = incident.spec.relatedResources ?? []
  const pcs = incident.spec.problemCases ?? []
  const suggestions = previousRun?.suggestedResources ?? []
  const needsMoreInfo = !!previousRun?.needsMoreInfo && suggestions.length > 0

  return (
    <details className="incident-details">
      <summary className="incident-details-toggle">Details</summary>
      <div className="incident-details-body">

        {/* Root resource — always */}
        <div className="inc-detail-row">
          <span className="inc-detail-label">Root resource</span>
          <ResourcePill
            kind={incident.spec.rootResource.kind}
            name={incident.spec.rootResource.name}
            namespace={incident.spec.rootResource.namespace}
          />
        </div>

        {/* User prompt — manual only */}
        {isManual && incident.spec.userMessage && (
          <div className="inc-detail-row inc-detail-row--col">
            <span className="inc-detail-label">User description</span>
            <p className="inc-detail-message">{incident.spec.userMessage}</p>
          </div>
        )}

        {/* Related resources — manual */}
        {isManual && related.length > 0 && (
          <div className="inc-detail-row inc-detail-row--wrap">
            <span className="inc-detail-label">Related resources</span>
            <div className="inc-detail-pills">
              {related.map((r, i) => (
                <ResourcePill key={i} kind={r.kind} name={r.name} namespace={r.namespace} />
              ))}
            </div>
          </div>
        )}

        {/* Problem cases — auto */}
        {!isManual && pcs.length > 0 && (
          <div className="inc-detail-row inc-detail-row--wrap">
            <span className="inc-detail-label">Problem cases</span>
            <div className="inc-detail-pills">
              {pcs.map(pc => (
                <span key={pc} className="inc-detail-pill inc-detail-pill--pc">{pc}</span>
              ))}
            </div>
          </div>
        )}

        {/* KubeChan's suggestions */}
        {needsMoreInfo && (
          <div className="inc-detail-row inc-detail-row--col">
            <span className="inc-detail-label inc-detail-label--warn">KubeChan suggested</span>
            <div className="inc-detail-suggestions">
              {suggestions.map((s, i) => (
                <div key={i} className="inc-detail-suggestion">
                  <span className="inc-detail-pill inc-detail-pill--suggest">{s.kind}</span>
                  <span className="inc-detail-suggestion-reason">{s.reason}</span>
                </div>
              ))}
            </div>
          </div>
        )}

      </div>
    </details>
  )
}

function IncidentRow({ incident, onAnalyzed, onAnalysisStart, previousRun, onResolved, onResourcesPatched }: {
  incident: Incident
  onAnalyzed: (name: string, runId: string) => void
  onAnalysisStart: (incidentName: string) => void
  previousRun?: DiagnosticRunSummary
  onResolved?: () => void
  onResourcesPatched: (incidentName: string, resources: Array<{ kind: string; name: string; namespace: string }>) => void
}) {
  const [analysis, setAnalysis] = useState<AnalysisState>({ status: 'idle' })
  const [showAugment, setShowAugment] = useState(false)
  const [resolveState, setResolveState] = useState<'idle' | 'confirm' | 'resolving' | 'error'>('idle')
  const [resolveError, setResolveError] = useState('')
  const id = incident.metadata.name
  const isResolved = incident.status.state === 'resolved'
  const isManual = incident.spec.source === 'manual'
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

  function handleAugmented(diagnosticRunId: string, resources: Array<{ kind: string; name: string; namespace: string }>) {
    setShowAugment(false)
    onResourcesPatched(id, resources)
    onAnalysisStart(title)
    setAnalysis({ status: 'collecting', diagnosticRunId })
    onAnalyzed(id, diagnosticRunId)
  }

  async function handleResolve() {
    setResolveState('resolving')
    onResolved?.()
    try {
      await api.resolveIncident(id)
      setResolveState('idle')
    } catch (e: unknown) {
      setResolveError(String(e))
      setResolveState('error')
    }
  }

  const isInFlight = analysis.status === 'pending' || analysis.status === 'collecting'
  const alreadyAnalyzed = !!previousRun?.analysisResultId
  const needsMoreInfo = !isInFlight && alreadyAnalyzed && !!previousRun?.needsMoreInfo
  const suggestions = previousRun?.suggestedResources ?? []
  const rootNS = incident.spec.rootResource.namespace || incident.metadata.namespace || ''

  return (
    <div className={`incident-row ${isResolved ? 'resolved' : 'open'}`}>
      <div className="incident-header">
        <span className={`state-badge ${incident.status.state}`}>
          {incident.status.state.toUpperCase()}
        </span>
        {incident.spec.source === 'manual' && (
          <span className="manual-badge">Manual</span>
        )}
        {incident.spec.source === 'manual' && incident.ownerUsername && (
          <span className="owner-badge">by {incident.ownerUsername}</span>
        )}
        <div className="incident-title-block">
          <strong className="incident-name">{title}</strong>
          {sub && <span className="incident-affected">{sub}</span>}
        </div>
        <span className="incident-meta muted">
          opened {timeAgo(incident.status.openedAt)}
        </span>
      </div>

      <IncidentDetails incident={incident} previousRun={previousRun} />

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
            {!isResolved && isManual && resolveState === 'idle' && (
              <button className="btn-resolve" onClick={() => setResolveState('confirm')}>
                Resolve
              </button>
            )}
            {!isResolved && isManual && resolveState === 'confirm' && (
              <span className="resolve-confirm">
                <span className="resolve-confirm-text">Mark as resolved?</span>
                <button className="btn-resolve-yes" onClick={handleResolve}>Yes</button>
                <button className="btn-resolve-cancel" onClick={() => setResolveState('idle')}>Cancel</button>
              </span>
            )}
            {!isResolved && isManual && resolveState === 'resolving' && (
              <span className="status-text pending"><span className="spinner" /> Resolving…</span>
            )}
            {resolveState === 'error' && (
              <span className="status-text error" title={resolveError}>✗ Resolve failed</span>
            )}
          </div>
        )}
      </div>

      {needsMoreInfo && suggestions.length > 0 && (
        <div className="needs-more-info-banner">
          <div className="nmi-body">
            <span className="nmi-icon">🔍</span>
            <div className="nmi-text">
              <strong>KubeChan needs more evidence</strong>
              <span className="nmi-suggestions">
                {suggestions.map((s, i) => (
                  <span key={i} className="nmi-chip" title={s.reason}>{s.kind}</span>
                ))}
              </span>
            </div>
          </div>
          <button className="btn-augment" onClick={() => setShowAugment(true)}>
            Add context &amp; Re-analyze
          </button>
        </div>
      )}

      {showAugment && (
        <AugmentIncidentModal
          incidentId={id}
          defaultNamespace={rootNS}
          suggestions={suggestions}
          onClose={() => setShowAugment(false)}
          onAugmented={handleAugmented}
        />
      )}
    </div>
  )
}

export interface IncidentListProps {
  onAnalysisStart: (incidentName: string) => void
  onAnalysisComplete: (result: AnalysisResult, incidentName: string) => void
  onAction?: (event: ChatterEvent) => void
  onResolved?: () => void
  onReportManual?: () => void
}

export function IncidentList({ onAnalysisStart, onAnalysisComplete, onAction, onResolved, onReportManual }: IncidentListProps) {
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

  function patchIncidentResources(incidentName: string, resources: Array<{ kind: string; name: string; namespace: string }>) {
    setIncidents(prev => prev.map(inc => {
      if (inc.metadata.name !== incidentName) return inc
      const existing = inc.spec.relatedResources ?? []
      const extra = resources.filter(r =>
        !existing.some(e => e.kind === r.kind && e.name === r.name && e.namespace === r.namespace)
      )
      if (extra.length === 0) return inc
      return { ...inc, spec: { ...inc.spec, relatedResources: [...existing, ...extra] } }
    }))
  }

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
                needsMoreInfo: result.payload?.needsMoreInfo,
                suggestedResources: result.payload?.suggestedResources,
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
          {onReportManual && (
            <button className="btn-report-manual" onClick={onReportManual}>+ Report an issue</button>
          )}
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
          onResolved={onResolved}
          onResourcesPatched={patchIncidentResources}
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
              onResolved={onResolved}
              onResourcesPatched={patchIncidentResources}
            />
          ))}
        </details>
      )}
    </div>
  )
}
