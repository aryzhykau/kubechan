import { useState, useEffect } from 'react'
import { api, type Evidence, type AnalysisResult, type K8sEvent, type PodLog } from './api'
import { type ChatterEvent } from './chatter'

interface Props {
  runId: string
  onBack: () => void
  onResultLoaded?: (result: AnalysisResult | null, runId: string) => void
  onAction?: (event: ChatterEvent) => void
}

type Tab = 'logs' | 'events' | 'config' | 'pvcs'

// ── helpers ──────────────────────────────────────────────────────────────────

function fmtDate(iso: string): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    })
  } catch {
    return iso
  }
}

function EventsTable({ events }: { events: K8sEvent[] }) {
  if (!events || events.length === 0) return <p className="diag-empty-section">No events.</p>
  return (
    <table className="diag-events-table">
      <thead>
        <tr>
          <th>Type</th><th>Reason</th><th>Count</th><th>Last Seen</th><th>Message</th>
        </tr>
      </thead>
      <tbody>
        {events.map((ev, i) => (
          <tr key={i} className={ev.type === 'Warning' ? 'ev-warning' : ''}>
            <td>{ev.type}</td>
            <td><code>{ev.reason}</code></td>
            <td>{ev.count}</td>
            <td className="ev-time">{fmtDate(ev.lastTime)}</td>
            <td>{ev.message}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

// ── tab contents ─────────────────────────────────────────────────────────────

function LogsTab({ evidence }: { evidence: Evidence | null }) {
  const pods = evidence?.payload?.workloadPodLogs ?? []
  if (pods.length === 0) return <p className="diag-empty-section">No pod logs collected.</p>

  return (
    <div className="diag-logs-panel">
      {pods.map((pod: PodLog) => (
        <div key={pod.podName} className="diag-pod-section">
          <div className="diag-pod-header">
            <span className="diag-pod-name">{pod.podName}</span>
            <span className="diag-pod-phase">{pod.phase}</span>
          </div>
          {pod.logs ? (
            <pre className="diag-log-block">{pod.logs}</pre>
          ) : (
            <p className="diag-empty-section">No logs.</p>
          )}
          {pod.prevLogs && (
            <>
              <div className="diag-sub-label">Previous container logs</div>
              <pre className="diag-log-block diag-log-prev">{pod.prevLogs}</pre>
            </>
          )}
        </div>
      ))}
    </div>
  )
}

function EventsTab({ evidence }: { evidence: Evidence | null }) {
  const rootEvents = evidence?.payload?.rootResourceEvents ?? []
  const pods = evidence?.payload?.workloadPodLogs ?? []

  return (
    <div className="diag-events-panel">
      {rootEvents.length > 0 && (
        <div className="diag-pod-section">
          <div className="diag-sub-label">
            {evidence?.payload?.rootResource
              ? `${evidence.payload.rootResource.kind}/${evidence.payload.rootResource.name}`
              : 'Root resource events'}
          </div>
          <EventsTable events={rootEvents} />
        </div>
      )}
      {pods.map((pod: PodLog) => (
        pod.events && pod.events.length > 0 && (
          <div key={pod.podName} className="diag-pod-section">
            <div className="diag-sub-label">Pod: {pod.podName}</div>
            <EventsTable events={pod.events} />
          </div>
        )
      ))}
      {rootEvents.length === 0 && pods.every(p => !p.events?.length) && (
        <p className="diag-empty-section">No events collected.</p>
      )}
    </div>
  )
}

function ConfigTab({ evidence }: { evidence: Evidence | null }) {
  const pods = evidence?.payload?.workloadPodLogs ?? []
  const allCMs = pods.flatMap(p => (p.dependencies?.configMaps ?? []).map(cm => ({ pod: p.podName, ...cm })))
  const allSecrets = pods.flatMap(p => (p.dependencies?.secrets ?? []).map(s => ({ pod: p.podName, ...s })))

  if (allCMs.length === 0 && allSecrets.length === 0) {
    return <p className="diag-empty-section">No ConfigMap or Secret dependencies collected.</p>
  }

  return (
    <div className="diag-config-panel">
      {allCMs.length > 0 && (
        <>
          <div className="diag-sub-label">ConfigMaps</div>
          {allCMs.map((cm, i) => (
            <div key={i} className="diag-config-item">
              <div className="diag-config-header">
                <code>{cm.name}</code>
                {cm.missing && <span className="diag-badge-missing">missing</span>}
                {cm.mountPaths && cm.mountPaths.length > 0 && (
                  <span className="diag-config-mounts">{cm.mountPaths.join(', ')}</span>
                )}
              </div>
              {cm.data && Object.keys(cm.data).length > 0 && (
                <div className="diag-config-data">
                  {Object.entries(cm.data).map(([k, v]) => (
                    <div key={k} className="diag-config-entry">
                      <span className="diag-config-key">{k}</span>
                      <pre className="diag-config-value">{v}</pre>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </>
      )}
      {allSecrets.length > 0 && (
        <>
          <div className="diag-sub-label">Secrets</div>
          {allSecrets.map((s, i) => (
            <div key={i} className="diag-config-item">
              <code>{s.name}</code>
              {s.missing && <span className="diag-badge-missing">missing</span>}
              <span className="diag-config-mounts">(values redacted)</span>
            </div>
          ))}
        </>
      )}
    </div>
  )
}

function PVCsTab({ evidence }: { evidence: Evidence | null }) {
  const pvcs = evidence?.payload?.pvcInfos ?? []
  if (pvcs.length === 0) return <p className="diag-empty-section">No PVC data collected.</p>

  return (
    <div className="diag-pvcs-panel">
      {pvcs.map((pvc, i) => (
        <div key={i} className="diag-pod-section">
          <div className="diag-pod-header">
            <span className="diag-pod-name">{pvc.name}</span>
            <span className="diag-pod-phase">{pvc.phase}</span>
            {pvc.storageClass && <span className="diag-config-mounts">{pvc.storageClass}</span>}
            {pvc.requestedStorage && <span className="diag-config-mounts">{pvc.requestedStorage}</span>}
          </div>
          <EventsTable events={pvc.events ?? []} />
        </div>
      ))}
    </div>
  )
}

// ── main component ────────────────────────────────────────────────────────────

const TABS: { id: Tab; label: string }[] = [
  { id: 'logs',     label: 'Logs' },
  { id: 'events',   label: 'Events' },
  { id: 'config',   label: 'Config' },
  { id: 'pvcs',     label: 'PVCs' },
]

export function DiagnosticRunDetail({ runId, onBack, onResultLoaded, onAction }: Props) {
  const [activeTab, setActiveTab] = useState<Tab>('logs')
  const [evidence, setEvidence] = useState<Evidence | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    onAction?.('open-run')
    setLoading(true)
    setError(null)
    setEvidence(null)

    Promise.allSettled([
      api.getDiagnosticRunEvidence(runId),
      api.getDiagnosticRunAnalysisResult(runId),
    ]).then(([evidenceRes, resultRes]) => {
      if (evidenceRes.status === 'fulfilled') setEvidence(evidenceRes.value)
      const result = resultRes.status === 'fulfilled' ? resultRes.value : null
      onResultLoaded?.(result, runId)
      if (evidenceRes.status === 'rejected' && resultRes.status === 'rejected') {
        setError('No evidence or analysis found for this run.')
      }
      setLoading(false)
    })
  }, [runId])

  return (
    <div>
      <div className="diag-detail-header">
        <button className="btn-back" onClick={onBack}>← Back</button>
        <span className="diag-run-id-large" title={runId}>{runId}</span>
      </div>

      {loading && <div className="loading">Loading…</div>}
      {error && !loading && <div className="error-msg">{error}</div>}

      {!loading && (
        <>
          <div className="diag-tabs">
            {TABS.map(tab => (
              <button
                key={tab.id}
                className={`diag-tab${activeTab === tab.id ? ' active' : ''}`}
                onClick={() => setActiveTab(tab.id)}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <div className="diag-tab-content">
            {activeTab === 'logs'     && <LogsTab evidence={evidence} />}
            {activeTab === 'events'   && <EventsTab evidence={evidence} />}
            {activeTab === 'config'   && <ConfigTab evidence={evidence} />}
            {activeTab === 'pvcs'     && <PVCsTab evidence={evidence} />}
          </div>
        </>
      )}
    </div>
  )
}
