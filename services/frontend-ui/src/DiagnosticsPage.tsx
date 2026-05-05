import { useState, useEffect, useCallback } from 'react'
import { api, type DiagnosticRunSummary } from './api'
import { type ChatterEvent } from './chatter'

interface Props {
  onSelectRun: (runId: string) => void
  onAction?: (event: ChatterEvent) => void
}

function fmtDate(iso: string): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit',
    })
  } catch {
    return iso
  }
}

function ConfidenceBadge({ confidence }: { confidence?: number }) {
  if (confidence == null) return null
  const pct = Math.round(confidence * 100)
  const cls = pct >= 80 ? 'confidence-high' : pct >= 50 ? 'confidence-medium' : 'confidence-low'
  return <span className={`confidence-badge ${cls}`}>{pct}%</span>
}

function StatusBadge({ status, hasAnalysis }: { status: string; hasAnalysis: boolean }) {
  const label = hasAnalysis ? 'analyzed' : status
  const cls = hasAnalysis ? 'analyzed' : status === 'pending' ? 'pending' : 'done'
  return <span className={`status-text ${cls}`}>{label}</span>
}

export function DiagnosticsPage({ onSelectRun, onAction }: Props) {
  const [runs, setRuns] = useState<DiagnosticRunSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    setSelected(new Set())
    try {
      const data = await api.listDiagnosticRuns()
      setRuns(data)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const allSelected = runs.length > 0 && selected.size === runs.length

  const toggleAll = () => {
    if (allSelected) {
      setSelected(new Set())
    } else {
      setSelected(new Set(runs.map(r => r.diagnosticRunId)))
    }
  }

  const toggleOne = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const deleteSingle = async (e: React.MouseEvent, runId: string) => {
    e.stopPropagation()
    if (!confirm('Delete this diagnostic run and all its data?')) return
    setDeleting(true)
    try {
      await api.deleteDiagnosticRun(runId)
      setRuns(prev => prev.filter(r => r.diagnosticRunId !== runId))
      setSelected(prev => { const n = new Set(prev); n.delete(runId); return n })
      onAction?.('delete-run')
    } catch (e) {
      alert(String(e))
    } finally {
      setDeleting(false)
    }
  }

  const deleteSelected = async () => {
    if (selected.size === 0) return
    if (!confirm(`Delete ${selected.size} diagnostic run(s) and all their data?`)) return
    setDeleting(true)
    try {
      await api.bulkDeleteDiagnosticRuns([...selected])
      setRuns(prev => prev.filter(r => !selected.has(r.diagnosticRunId)))
      setSelected(new Set())
      onAction?.('delete-run')
    } catch (e) {
      alert(String(e))
    } finally {
      setDeleting(false)
    }
  }

  // Group runs by incidentId, preserving order of first appearance.
  const groups: { incidentId: string; runs: DiagnosticRunSummary[] }[] = []
  const groupIndex = new Map<string, number>()
  for (const run of runs) {
    const key = run.incidentId || '(no incident)'
    if (!groupIndex.has(key)) {
      groupIndex.set(key, groups.length)
      groups.push({ incidentId: key, runs: [] })
    }
    groups[groupIndex.get(key)!].runs.push(run)
  }

  return (
    <div>
      <div className="list-header">
        <h2>Diagnostic Runs</h2>
        <div className="header-right">
          {selected.size > 0 && (
            <button
              className="btn-delete-bulk"
              onClick={deleteSelected}
              disabled={deleting}
            >
              {deleting ? '…' : `Delete ${selected.size} selected`}
            </button>
          )}
          <button className="btn-refresh" onClick={load} disabled={loading || deleting}>↺ Refresh</button>
        </div>
      </div>

      {loading && <div className="loading">Loading…</div>}
      {error && <div className="error-msg">{error}</div>}

      {!loading && !error && runs.length === 0 && (
        <div className="empty">No diagnostic runs yet. Trigger an analysis from the Incidents tab.</div>
      )}

      {!loading && runs.length > 0 && (
        <div className="diag-select-bar">
          <label className="diag-checkbox-label">
            <input
              type="checkbox"
              className="diag-checkbox"
              checked={allSelected}
              onChange={toggleAll}
            />
            {allSelected ? 'Deselect all' : `Select all (${runs.length})`}
          </label>
        </div>
      )}

      {groups.map(group => (
        <div key={group.incidentId} className="diag-incident-group">
          <div className="diag-incident-group-header">{group.incidentId}</div>
          {group.runs.map(run => (
            <div
              key={run.diagnosticRunId}
              className={`diag-run-row${selected.has(run.diagnosticRunId) ? ' selected' : ''}`}
              onClick={() => onSelectRun(run.diagnosticRunId)}
            >
              <div className="diag-run-header">
                <span
                  className="diag-run-check"
                  onClick={e => { e.stopPropagation(); toggleOne(run.diagnosticRunId) }}
                >
                  <input
                    type="checkbox"
                    className="diag-checkbox"
                    checked={selected.has(run.diagnosticRunId)}
                    onChange={() => toggleOne(run.diagnosticRunId)}
                    onClick={e => e.stopPropagation()}
                  />
                </span>
                <span className="diag-run-id" title={run.diagnosticRunId}>
                  {run.diagnosticRunId}
                </span>
                <StatusBadge status={run.status} hasAnalysis={!!run.analysisResultId} />
                <ConfidenceBadge confidence={run.confidence} />
                <span className="diag-run-time">{fmtDate(run.requestedAt)}</span>
                <button
                  className="btn-delete-run"
                  title="Delete run"
                  disabled={deleting}
                  onClick={e => deleteSingle(e, run.diagnosticRunId)}
                >
                  ✕
                </button>
              </div>
              {run.likelyRootCause && (
                <div className="diag-run-rootcause">{run.likelyRootCause}</div>
              )}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}
