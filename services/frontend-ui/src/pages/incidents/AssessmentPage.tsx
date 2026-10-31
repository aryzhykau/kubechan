import { useEffect, useMemo, useState } from 'react'
import { Alert, Box, Button, CircularProgress } from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import OpenInNewIcon from '@mui/icons-material/OpenInNew'
import { useNavigate, useParams } from 'react-router-dom'
import { getToken } from '../../api/index'
import type { AnalysisResult, AssessmentClaim, Evidence, EvidenceSource, GraphCandidate, GraphEdge, GraphNode, InvestigationGraph } from '../../api/index'
import './AssessmentPage.css'

type PositionedNode = GraphNode & { x: number; y: number }
type Selection = { type: 'node'; value: GraphNode } | { type: 'edge'; value: GraphEdge } | { type: 'candidate'; value: GraphCandidate }

const KIND_RANK: Record<string, number> = {
  Ingress: 0, HTTPRoute: 0, GRPCRoute: 0,
  Service: 1,
  Deployment: 2, StatefulSet: 2, DaemonSet: 2, Job: 2, CronJob: 2,
  Pod: 3,
  ConfigMap: 4, Secret: 4, PersistentVolumeClaim: 4,
}

function graphLayout(graph: InvestigationGraph): { nodes: PositionedNode[]; width: number; height: number } {
  const groups = new Map<number, GraphNode[]>()
  for (const node of graph.nodes) {
    const rank = KIND_RANK[node.resource.kind] ?? 2
    groups.set(rank, [...(groups.get(rank) ?? []), node])
  }
  const columnWidth = 250
  const rowHeight = 126
  const positioned: PositionedNode[] = []
  for (const [rank, values] of groups) {
    values.sort((a, b) => a.id.localeCompare(b.id))
    values.forEach((node, index) => positioned.push({ ...node, x: 34 + rank * columnWidth, y: 34 + index * rowHeight }))
  }
  const maxRank = Math.max(0, ...Array.from(groups.keys()))
  const maxRows = Math.max(1, ...Array.from(groups.values()).map(values => values.length))
  return { nodes: positioned, width: 34 + (maxRank + 1) * columnWidth, height: 58 + maxRows * rowHeight }
}

function observedValue(source: EvidenceSource): string {
  if (source.line) return source.line.text
  if (source.event) return `${source.event.reason}: ${source.event.message}`
  if (typeof source.value === 'string') return source.value
  return JSON.stringify(source.value)
}

function sourceLabel(source: EvidenceSource): string {
  return `${source.resource.kind} · ${source.resource.namespace ?? 'cluster'}/${source.resource.name}`
}

function GraphCanvas({ graph, rootCauseId, selected, onSelect }: {
  graph: InvestigationGraph
  rootCauseId?: string
  selected?: Selection
  onSelect: (selection: Selection) => void
}) {
  const layout = useMemo(() => graphLayout(graph), [graph])
  const [zoom, setZoom] = useState(0.75)
  const byId = new Map(layout.nodes.map(node => [node.id, node]))
  const candidates = graph.candidates ?? []

  function edgePath(fromId: string, toId: string): string {
    const from = byId.get(fromId); const to = byId.get(toId)
    if (!from || !to) return ''
    const x1 = from.x + 190; const y1 = from.y + 42
    const x2 = to.x; const y2 = to.y + 42
    const bend = Math.max(36, Math.abs(x2 - x1) * 0.45)
    return `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`
  }

  return (
    <div className="investigation-graph__viewport">
      <div className="investigation-graph__controls" aria-label="Graph zoom controls">
        <button type="button" onClick={() => setZoom(value => Math.max(0.55, value - 0.1))} aria-label="Zoom out">−</button>
        <span>{Math.round(zoom * 100)}%</span>
        <button type="button" onClick={() => setZoom(value => Math.min(1.25, value + 0.1))} aria-label="Zoom in">+</button>
        <button type="button" onClick={() => setZoom(0.75)}>Fit</button>
      </div>
      <svg className="investigation-graph" style={{ width: Math.max(820, layout.width * zoom), height: Math.max(430, layout.height * zoom) }} viewBox={`0 0 ${layout.width} ${layout.height}`} preserveAspectRatio="xMinYMin meet" role="img" aria-label="Investigation resource graph">
        <defs>
          <marker id="arrow-normal" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
          <marker id="arrow-broken" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
          <marker id="arrow-candidate" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
        </defs>
        {graph.edges.map(edge => {
          const path = edgePath(edge.from, edge.to)
          if (!path) return null
          return <g key={edge.id} className={`graph-edge graph-edge--${edge.state} ${selected?.type === 'edge' && selected.value.id === edge.id ? 'is-selected' : ''}`} role="button" tabIndex={0} onClick={() => onSelect({ type: 'edge', value: edge })} onKeyDown={event => event.key === 'Enter' && onSelect({ type: 'edge', value: edge })}>
            <path className="graph-edge__hit" d={path} />
            <path className="graph-edge__line" d={path} markerEnd={`url(#arrow-${edge.state === 'broken' ? 'broken' : 'normal'})`} />
            <text x={((byId.get(edge.from)?.x ?? 0) + (byId.get(edge.to)?.x ?? 0)) / 2 + 95} y={Math.min(byId.get(edge.from)?.y ?? 0, byId.get(edge.to)?.y ?? 0) + 30}>{edge.type.replace(/_/g, ' ')}</text>
          </g>
        })}
        {candidates.map(candidate => {
          const path = edgePath(candidate.referenceNodeId, candidate.candidateNodeId)
          if (!path) return null
          return <g key={candidate.id} className={`graph-edge graph-edge--candidate ${selected?.type === 'candidate' && selected.value.id === candidate.id ? 'is-selected' : ''}`} role="button" tabIndex={0} onClick={() => onSelect({ type: 'candidate', value: candidate })} onKeyDown={event => event.key === 'Enter' && onSelect({ type: 'candidate', value: candidate })}>
            <path className="graph-edge__hit" d={path} />
            <path className="graph-edge__line" d={path} markerEnd="url(#arrow-candidate)" />
            <text x={((byId.get(candidate.referenceNodeId)?.x ?? 0) + (byId.get(candidate.candidateNodeId)?.x ?? 0)) / 2 + 90} y={Math.min(byId.get(candidate.referenceNodeId)?.y ?? 0, byId.get(candidate.candidateNodeId)?.y ?? 0) + 70}>suggested target</text>
          </g>
        })}
        {layout.nodes.map(node => {
          const state = node.id === rootCauseId ? 'root-cause' : node.state
          return <g key={node.id} className={`graph-node graph-node--${state} ${node.type === 'reference' ? 'graph-node--reference' : ''} ${selected?.type === 'node' && selected.value.id === node.id ? 'is-selected' : ''}`} transform={`translate(${node.x} ${node.y})`} role="button" tabIndex={0} onClick={() => onSelect({ type: 'node', value: node })} onKeyDown={event => event.key === 'Enter' && onSelect({ type: 'node', value: node })}>
            <rect width="190" height="84" rx="3" />
            <text className="graph-node__kind" x="14" y="21">{node.resource.kind}</text>
            <text className="graph-node__name" x="14" y="44">{node.resource.name}</text>
            <text className="graph-node__namespace" x="14" y="64">{node.resource.namespace || 'cluster scoped'}</text>
            <text className="graph-node__state" x="176" y="20" textAnchor="end">{node.type === 'reference' ? 'not found' : node.id === rootCauseId ? 'root cause' : node.state}</text>
          </g>
        })}
      </svg>
    </div>
  )
}

function EvidencePanel({ selection, sources }: { selection?: Selection; sources: Map<string, EvidenceSource> }) {
  if (!selection) return <div className="assessment-inspector assessment-inspector--empty"><span>Select a resource or relationship</span><p>Inspect the exact metadata, event, or log evidence used by the analysis.</p></div>
  const ids = selection.type === 'node' || selection.type === 'edge'
    ? selection.value.sourceIds ?? []
    : selection.value.reasons.flatMap(reason => reason.sourceIds ?? [])
  const resolved = ids.map(id => sources.get(id)).filter(Boolean) as EvidenceSource[]
  const title = selection.type === 'node'
    ? `${selection.value.resource.kind}/${selection.value.resource.name}`
    : selection.type === 'edge' ? selection.value.type.replace(/_/g, ' ') : 'Suggested target'
  return <aside className="assessment-inspector">
    <div className="assessment-inspector__header"><span>{selection.type}</span><h3>{title}</h3></div>
    {selection.type === 'candidate' && selection.value.reasons.map(reason => <div className="candidate-reason" key={reason.code}><strong>{reason.label}</strong><span>{reason.code}</span></div>)}
    {resolved.length === 0 && <p className="assessment-inspector__muted">No directly addressable evidence is attached to this entity.</p>}
    {resolved.map(source => <div className="source-record" key={source.id}>
      <strong>{sourceLabel(source)}</strong>
      {source.path && <code>{source.path}</code>}
      <pre>{observedValue(source)}</pre>
      <small>{source.id}</small>
    </div>)}
  </aside>
}

function ClaimLedger({ claims, sources, onSource }: { claims: AssessmentClaim[]; sources: Map<string, EvidenceSource>; onSource: (source: EvidenceSource) => void }) {
  return <section className="evidence-ledger">
    <div className="assessment-section-heading"><span>Evidence chain</span><strong>{claims.filter(claim => claim.verdict === 'supported').length}/{claims.length} supported</strong></div>
    {claims.map((claim, index) => <article className="evidence-claim" key={claim.id || index}>
      <div className="evidence-claim__index">{String(index + 1).padStart(2, '0')}</div>
      <div className="evidence-claim__body">
        <div className="evidence-claim__title"><h3>{claim.statement}</h3><span className={`claim-verdict claim-verdict--${claim.verdict}`}>{claim.verdict.replace('_', ' ')}</span></div>
        {claim.citations.map(citation => {
          const source = sources.get(citation.sourceId)
          if (!source) return null
          return <button className="evidence-reference" type="button" key={citation.sourceId} onClick={() => onSource(source)}>
            <span>{sourceLabel(source)}</span><code>{source.path || source.type}</code><OpenInNewIcon />
          </button>
        })}
      </div>
    </article>)}
  </section>
}

export function AssessmentPage() {
  const { incidentId = '', runId = '' } = useParams()
  const navigate = useNavigate()
  const [analysis, setAnalysis] = useState<AnalysisResult | null>(null)
  const [evidence, setEvidence] = useState<Evidence | null>(null)
  const [error, setError] = useState('')
  const [selection, setSelection] = useState<Selection>()
  const [sourceSelection, setSourceSelection] = useState<EvidenceSource>()

  useEffect(() => {
    const headers: Record<string, string> = {}
    const token = getToken(); if (token) headers.Authorization = `Bearer ${token}`
    Promise.all([
      fetch(`/api/v1/diagnosticruns/${encodeURIComponent(runId)}/analysisresult`, { headers }),
      fetch(`/api/v1/diagnosticruns/${encodeURIComponent(runId)}/evidence`, { headers }),
    ]).then(async ([analysisResponse, evidenceResponse]) => {
      if (!analysisResponse.ok || !evidenceResponse.ok) throw new Error('Assessment data is unavailable')
      setAnalysis(await analysisResponse.json() as AnalysisResult)
      setEvidence(await evidenceResponse.json() as Evidence)
    }).catch(cause => setError(cause instanceof Error ? cause.message : 'Unable to load assessment'))
  }, [runId])

  if (error) return <Alert severity="error">{error}</Alert>
  if (!analysis || !evidence) return <Box sx={{ minHeight: 420, display: 'grid', placeItems: 'center' }}><CircularProgress size={28} /></Box>
  const assessment = analysis.payload.assessment
  const graph = evidence.payload.graph
  if (!assessment || !graph) return <Alert severity="info">This analysis predates investigation graphs. Run the assessment again to generate one.</Alert>
  const sources = new Map((evidence.payload.sources ?? []).map(source => [source.id, source]))
  const confidence = Math.round(assessment.confidence * 100)
  const root = evidence.payload.rootResource
  const effectiveSelection = sourceSelection
    ? ({ type: 'node', value: graph.nodes.find(node => node.sourceIds?.includes(sourceSelection.id)) ?? graph.nodes.find(node => node.id === graph.entryResourceId)! } as Selection)
    : selection

  return <Box className="assessment-page">
    <header className="assessment-page__header">
      <Button startIcon={<ArrowBackIcon />} onClick={() => navigate('/')} size="small">Incident {incidentId}</Button>
      <div className="assessment-page__identity"><span>Assessment</span><h1>{root?.kind}/{root?.name}</h1><p>{root?.namespace} · Evidence snapshot {new Date(evidence.collectedAt).toLocaleString()}</p></div>
      <div className="assessment-confidence"><strong>{confidence}%</strong><span>confidence</span><i><b style={{ width: `${confidence}%` }} /></i></div>
    </header>

    <section className="assessment-diagnosis"><span>Diagnosis</span><h2>{assessment.summary}</h2><div><strong>{assessment.claims.filter(claim => claim.verdict === 'supported').length}</strong> supported claims <b>·</b> <strong>{graph.nodes.length}</strong> resources <b>·</b> <strong>{graph.edges.length}</strong> relationships</div></section>

    <section className="assessment-workspace">
      <div className="assessment-workspace__main">
        <div className="assessment-section-heading"><span>Investigation scope</span><strong>Resources seen by the model</strong></div>
        <GraphCanvas graph={graph} rootCauseId={assessment.causalPath?.rootCauseEntityId} selected={effectiveSelection} onSelect={value => { setSourceSelection(undefined); setSelection(value) }} />
      </div>
      {sourceSelection ? <aside className="assessment-inspector"><div className="assessment-inspector__header"><span>source</span><h3>{sourceLabel(sourceSelection)}</h3></div>{sourceSelection.path && <code>{sourceSelection.path}</code>}<div className="source-record"><pre>{observedValue(sourceSelection)}</pre><small>{sourceSelection.id}</small></div><Button size="small" onClick={() => setSourceSelection(undefined)}>Back to graph selection</Button></aside> : <EvidencePanel selection={selection} sources={sources} />}
    </section>

    <ClaimLedger claims={assessment.claims} sources={sources} onSource={setSourceSelection} />

    {assessment.remediation.length > 0 && <section className="assessment-runbook"><div className="assessment-section-heading"><span>Remediation runbook</span><strong>{assessment.remediation.length} step{assessment.remediation.length === 1 ? '' : 's'}</strong></div>{assessment.remediation.sort((a, b) => a.order - b.order).map(step => <article key={`${step.order}-${step.title}`}><b>{step.order}</b><div><h3>{step.title}</h3><p>{step.description}</p>{step.command && <code>{step.command}</code>}<small>Verify: {step.verification}</small></div></article>)}</section>}
  </Box>
}
