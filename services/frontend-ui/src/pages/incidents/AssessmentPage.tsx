import { useEffect, useMemo, useState } from 'react'
import { Alert, Box, Button, CircularProgress } from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import OpenInNewIcon from '@mui/icons-material/OpenInNew'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import type { AssessmentClaim, EvidenceSource, GraphCandidate, GraphEdge, GraphNode, InvestigationGraph, NodeInsight, ResourceRef } from '../../api/index'
import './AssessmentPage.css'
import { useAnalysisProgress } from '../../analysis/useAnalysisProgress'
import { AnalysisProgressPanel } from '../../components/AnalysisProgressPanel'
import { useGetDiagnosticRunEvidenceQuery, useListDiagnosticRunsQuery } from '../../store/api/diagnosticsApi'
import { useGetDiagnosticRunAnalysisResultQuery } from '../../store/api/analysisApi'
import { investigationGraph, latestDraft, latestInvestigationScope, rootPreviewGraph } from '../../analysis/investigation'
import { useAnalyzeMutation, useGetIncidentQuery, useListIncidentsQuery } from '../../store/api/incidentsApi'

type PositionedNode = GraphNode & { x: number; y: number }
type Selection = { type: 'node'; value: GraphNode } | { type: 'edge'; value: GraphEdge } | { type: 'candidate'; value: GraphCandidate }

const NODE_WIDTH = 210
const NODE_HEIGHT = 132
const COLUMN_WIDTH = 310
const ROW_HEIGHT = 172
const TOOLTIP_WIDTH = 640
const TOOLTIP_SAFE_ATTRIBUTES = ['selector', 'templateLabels', 'replicas', 'readyReplicas', 'availableReplicas', 'readyEndpoints', 'notReadyEndpoints', 'phase', 'storageClass', 'scaleTargetRef', 'endpoint', 'status', 'totalLag', 'identityBasis']

const KIND_RANK: Record<string, number> = {
  Ingress: 0, HTTPRoute: 0, GRPCRoute: 0,
  Service: 1,
  Deployment: 2, StatefulSet: 2, DaemonSet: 2, Job: 2, CronJob: 2,
  Pod: 3,
  ConfigMap: 4, Secret: 4, PersistentVolumeClaim: 4,
  StorageClass: 5,
}

function graphLayout(graph: InvestigationGraph, compact: boolean): { nodes: PositionedNode[]; width: number; height: number } {
  const groups = new Map<number, GraphNode[]>()
  for (const node of graph.nodes) {
    const rank = KIND_RANK[node.resource.kind] ?? 2
    groups.set(rank, [...(groups.get(rank) ?? []), node])
  }
  const positioned: PositionedNode[] = []
  for (const [column, [rank, values]] of [...groups.entries()].sort(([a], [b]) => a - b).entries()) {
    const position = compact ? column : rank
    values.sort((a, b) => a.id.localeCompare(b.id))
    values.forEach((node, index) => positioned.push({ ...node, x: 40 + position * COLUMN_WIDTH, y: 40 + index * ROW_HEIGHT }))
  }
  const maxRank = compact ? groups.size - 1 : Math.max(0, ...groups.keys())
  const maxRows = Math.max(1, ...Array.from(groups.values()).map(values => values.length))
  return { nodes: positioned, width: Math.max(1180, 80 + maxRank * COLUMN_WIDTH + NODE_WIDTH), height: Math.max(520, 80 + (maxRows - 1) * ROW_HEIGHT + NODE_HEIGHT) }
}

function compactLabel(value: string, maxLength: number): string {
  return value.length <= maxLength ? value : `${value.slice(0, maxLength - 1)}…`
}

function tooltipValue(value: unknown): string {
  if (value == null) return '—'
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value)
  return JSON.stringify(value)
}

function nodeTooltipLines(node: GraphNode): Array<{ label: string; value: string }> {
  const lines = [
    { label: 'state', value: node.state },
    { label: 'role', value: node.role || 'context' },
    { label: 'namespace', value: node.resource.namespace || 'cluster scoped' },
    { label: 'evidence', value: `${node.sourceIds?.length ?? 0} sources` },
  ]
  const attributes = node.attributes ?? {}
  for (const key of TOOLTIP_SAFE_ATTRIBUTES) {
    if (!(key in attributes) || lines.length >= 8) continue
    lines.push({ label: key, value: compactLabel(tooltipValue(attributes[key]), 38) })
  }
  return lines
}

function wrapTooltipText(value: string, maxLength = 53, maxLines = 8): string[] {
  const words = value.trim().split(/\s+/).filter(Boolean)
  const lines: string[] = []
  for (const word of words) {
    const current = lines[lines.length - 1]
    if (!current || current.length + word.length + 1 > maxLength) lines.push(word)
    else lines[lines.length - 1] = `${current} ${word}`
    if (lines.length > maxLines) break
  }
  if (lines.length > maxLines) {
    lines.length = maxLines
    lines[maxLines - 1] = compactLabel(lines[maxLines - 1], maxLength - 1) + '…'
  }
  return lines.length ? lines : ['No model comment for this snapshot.']
}

function nodeInsightImage(relevance?: NodeInsight['relevance']): string {
  if (relevance === 'relevant') return '/kubechan-facepalm-angry.png'
  if (relevance === 'not_relevant') return '/kubechan-rolleye.png'
  return '/kubechan-looking-2.png'
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

function GraphCanvas({ graph, rootCauseId, nodeInsights, selected, onSelect, compact = false, readNodes, readingNode }: {
  graph: InvestigationGraph
  rootCauseId?: string
  nodeInsights: NodeInsight[]
  selected?: Selection
  onSelect: (selection: Selection) => void
  compact?: boolean
  readNodes?: Set<string>
  readingNode?: string
}) {
  const layout = useMemo(() => graphLayout(graph, compact), [graph, compact])
  const [zoom, setZoom] = useState(0.75)
  const [hoveredNodeId, setHoveredNodeId] = useState<string>()
  const [tooltipPointer, setTooltipPointer] = useState({ x: 0, y: 0 })
  const byId = new Map(layout.nodes.map(node => [node.id, node]))
  const insightByNode = new Map(nodeInsights.map(insight => [insight.nodeId, insight]))
  const candidates = graph.candidates ?? []
  const candidateByNode = new Map<string, GraphCandidate>()
  for (const candidate of [...candidates].sort((a, b) => b.score - a.score || a.id.localeCompare(b.id))) {
    if (!candidateByNode.has(candidate.candidateNodeId)) candidateByNode.set(candidate.candidateNodeId, candidate)
  }

  function edgePath(fromId: string, toId: string): string {
    const from = byId.get(fromId); const to = byId.get(toId)
    if (!from || !to) return ''
    const x1 = from.x + NODE_WIDTH; const y1 = from.y + NODE_HEIGHT / 2
    const x2 = to.x; const y2 = to.y + NODE_HEIGHT / 2
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
      <svg className="investigation-graph" style={{ width: Math.max(compact ? 300 : 820, layout.width * zoom), height: Math.max(430, layout.height * zoom) }} viewBox={`0 0 ${layout.width} ${layout.height}`} preserveAspectRatio="xMinYMin meet" role="img" aria-label="Investigation resource graph">
        <defs>
          <marker id="arrow-normal" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
          <marker id="arrow-broken" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
          <marker id="arrow-candidate" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
        </defs>
        {graph.edges.map(edge => {
          const path = edgePath(edge.from, edge.to)
          if (!path) return null
          return <g key={edge.id} className={`graph-edge graph-edge--${edge.state} ${edge.origin === 'inferred' ? 'graph-edge--inferred' : ''} ${selected?.type === 'edge' && selected.value.id === edge.id ? 'is-selected' : ''}`} role="button" tabIndex={0} onClick={() => onSelect({ type: 'edge', value: edge })} onKeyDown={event => event.key === 'Enter' && onSelect({ type: 'edge', value: edge })}>
            <path className="graph-edge__hit" d={path} />
            <path className="graph-edge__line" d={path} markerEnd={`url(#arrow-${edge.origin === 'inferred' ? 'candidate' : edge.state === 'broken' ? 'broken' : 'normal'})`} />
            <text x={((byId.get(edge.from)?.x ?? 0) + (byId.get(edge.to)?.x ?? 0)) / 2 + NODE_WIDTH / 2} y={Math.min(byId.get(edge.from)?.y ?? 0, byId.get(edge.to)?.y ?? 0) + 30}>{edge.type.replace(/_/g, ' ')}</text>
          </g>
        })}
        {candidates.map(candidate => {
          // New snapshots point the remediation hint from the resource whose
          // configuration must change. Fall back for pre-contract snapshots.
          const sourceNodeId = candidate.sourceNodeId ?? candidate.referenceNodeId
          const path = edgePath(sourceNodeId, candidate.candidateNodeId)
          if (!path) return null
          return <g key={candidate.id} className={`graph-edge graph-edge--candidate ${selected?.type === 'candidate' && selected.value.id === candidate.id ? 'is-selected' : ''}`} role="button" tabIndex={0} onClick={() => onSelect({ type: 'candidate', value: candidate })} onKeyDown={event => event.key === 'Enter' && onSelect({ type: 'candidate', value: candidate })}>
            <path className="graph-edge__hit" d={path} />
            <path className="graph-edge__line" d={path} markerEnd="url(#arrow-candidate)" />
            <text x={((byId.get(sourceNodeId)?.x ?? 0) + (byId.get(candidate.candidateNodeId)?.x ?? 0)) / 2 + NODE_WIDTH / 2} y={Math.min(byId.get(sourceNodeId)?.y ?? 0, byId.get(candidate.candidateNodeId)?.y ?? 0) + 70}>{candidate.strength === 'strong' ? 'should route to' : 'possible target'}</text>
          </g>
        })}
        {layout.nodes.map(node => {
          const state = node.id === rootCauseId ? 'root-cause' : node.state
          const candidate = candidateByNode.get(node.id)
          const review = insightByNode.get(node.id)
          return <g key={node.id} className={`graph-node graph-node--${state} ${node.type === 'reference' ? 'graph-node--reference' : ''} ${selected?.type === 'node' && selected.value.id === node.id ? 'is-selected' : ''}`} transform={`translate(${node.x} ${node.y})`} role="button" tabIndex={0} onMouseEnter={event => { setHoveredNodeId(node.id); setTooltipPointer({ x: event.clientX, y: event.clientY }) }} onMouseMove={event => setTooltipPointer({ x: event.clientX, y: event.clientY })} onMouseLeave={() => setHoveredNodeId(undefined)} onFocus={event => { const rect = event.currentTarget.getBoundingClientRect(); setHoveredNodeId(node.id); setTooltipPointer({ x: rect.right, y: rect.top }) }} onBlur={() => setHoveredNodeId(undefined)} onClick={() => onSelect({ type: 'node', value: node })} onKeyDown={event => event.key === 'Enter' && onSelect({ type: 'node', value: node })}>
            <title>{`${node.resource.kind}/${node.resource.name} · ${node.resource.namespace || 'cluster scoped'}`}</title>
            <rect width={NODE_WIDTH} height={NODE_HEIGHT} rx="3" />
            <text className="graph-node__kind" x="14" y="21">{compactLabel(node.resource.kind, 25)}</text>
            <text className="graph-node__name" x="14" y="44">{compactLabel(node.resource.name, 23)}</text>
            <text className="graph-node__namespace" x="14" y="66">{compactLabel(node.resource.namespace || 'cluster scoped', 31)}</text>
            <text className="graph-node__state" x={NODE_WIDTH - 14} y="20" textAnchor="end">{node.type === 'reference' ? 'not found' : node.id === rootCauseId ? 'root cause' : compactLabel(node.state, 13)}</text>
            <text className={`graph-node__review graph-node__review--${review?.relevance ?? 'context'}`} x="14" y="85">{review?.relevance === 'context' ? 'relevance not determined' : review?.relevance.replace('_', ' ') ?? 'not assessed'}</text>
            <text className="graph-node__explanation" x="14" y="102">{compactLabel(review?.comment ?? (readingNode === node.id ? 'Checking snapshot…' : readNodes?.has(node.id) ? 'Snapshot read completed' : 'No model review'), 32)}</text>
            {candidate && <g className={`graph-node__candidate ${selected?.type === 'candidate' && selected.value.id === candidate.id ? 'is-selected' : ''}`} role="button" tabIndex={0} aria-label={`Inspect ${candidate.strength} candidate`} onClick={event => { event.stopPropagation(); onSelect({ type: 'candidate', value: candidate }) }} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); event.stopPropagation(); onSelect({ type: 'candidate', value: candidate }) } }}>
              <rect x="108" y="113" width="88" height="15" rx="2" />
              <text x="152" y="124" textAnchor="middle">{candidate.strength} candidate</text>
            </g>}
          </g>
        })}
      </svg>
      {(() => {
        const node = hoveredNodeId ? byId.get(hoveredNodeId) : undefined
        if (!node) return null
        const insight = insightByNode.get(node.id)
        const commentLines = wrapTooltipText(insight?.comment || '')
        const lines = nodeTooltipLines(node).slice(0, 5)
        const effectiveWidth = Math.min(TOOLTIP_WIDTH, window.innerWidth - 32)
        const estimatedHeight = Math.min(window.innerHeight - 32, Math.max(330, 128 + commentLines.length * 24 + lines.length * 26))
        const left = Math.max(16, Math.min(tooltipPointer.x + 18, window.innerWidth - effectiveWidth - 16))
        const top = Math.max(16, Math.min(tooltipPointer.y + 18, window.innerHeight - estimatedHeight - 16))
        return <aside className="graph-node-tooltip" style={{ left, top, width: effectiveWidth, maxHeight: window.innerHeight - 32 }} role="tooltip">
          <header><strong>{node.resource.kind}/{node.resource.name}</strong><span className={`graph-node-tooltip__relevance graph-node-tooltip__relevance--${insight?.relevance ?? 'unknown'}`}>{(insight?.relevance ?? 'no model insight').replace('_', ' ')}</span></header>
          <div className="graph-node-tooltip__message">
            <div className="graph-node-tooltip__avatar-bg"><img className="graph-node-tooltip__avatar" src={nodeInsightImage(insight?.relevance)} alt="KubeChan reaction" /></div>
            <div><b className="graph-node-tooltip__speaker">KUBECHAN</b><p className="graph-node-tooltip__comment">{insight?.comment || 'No model comment for this snapshot.'}</p></div>
          </div>
          <dl>{lines.map(line => <div key={line.label}><dt>{line.label}</dt><dd>{line.value}</dd></div>)}</dl>
        </aside>
      })()}
    </div>
  )
}

function EvidencePanel({ selection, sources, review }: { selection?: Selection; sources: Map<string, EvidenceSource>; review?: NodeInsight }) {
  if (!selection) return <div className="assessment-inspector assessment-inspector--empty"><span>Select a resource or relationship</span><p>Inspect the exact metadata, event, or log evidence used by the analysis.</p></div>
  const ids = selection.type === 'node' || selection.type === 'edge'
    ? selection.value.sourceIds ?? []
    : selection.value.reasons.flatMap(reason => reason.sourceIds ?? [])
  const resolved = [...new Set([...ids, ...(review?.sourceIds ?? [])])].map(id => sources.get(id)).filter(Boolean) as EvidenceSource[]
  const title = selection.type === 'node'
    ? `${selection.value.resource.kind}/${selection.value.resource.name}`
    : selection.type === 'edge' ? selection.value.type.replace(/_/g, ' ') : 'Suggested target'
  return <aside className="assessment-inspector">
    <div className="assessment-inspector__header"><span>{selection.type}</span><h3>{title}</h3></div>
    {review && <div className="candidate-reason"><strong>{review.relevance === 'context' ? 'Relevance undetermined' : review.relevance === 'not_relevant' ? 'Not relevant' : 'Relevant'}</strong><p>{review.comment}</p></div>}
    {selection.type === 'candidate' && selection.value.reasons.map(reason => <div className="candidate-reason" key={reason.code}><strong>{reason.label}</strong><span>{reason.code}</span></div>)}
    {selection.type === 'edge' && selection.value.origin === 'inferred' && <div className="candidate-reason"><strong>{selection.value.strength ?? 'possible'} inferred relationship · score {selection.value.score ?? 0}</strong><span>Not observed Kubernetes topology</span></div>}
    {selection.type === 'edge' && selection.value.reasons?.map(reason => <div className="candidate-reason" key={reason.code}><strong>{reason.label}</strong><span>{reason.code}</span></div>)}
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
  const location = useLocation()
  const { currentData: incident } = useGetIncidentQuery(incidentId, { skip: !incidentId })
  const { currentData: incidents } = useListIncidentsQuery()
  const [analyzeIncident, { isLoading: startingAnalysis, isError: startFailed, reset: resetAnalysisStart }] = useAnalyzeMutation()
  const currentIncident = incident ?? incidents?.find(item => item.metadata.name === incidentId)
  const earlyRoot = (location.state as { rootResource?: ResourceRef } | null)?.rootResource
    ?? currentIncident?.spec.rootResource
  const [evidenceReady, setEvidenceReady] = useState(false)
  const [resultReady, setResultReady] = useState(false)
  const [showSnapshot, setShowSnapshot] = useState(false)
  const [selection, setSelection] = useState<Selection>()
  const [sourceSelection, setSourceSelection] = useState<EvidenceSource>()
  const { data: progress } = useAnalysisProgress(runId)
  const { data: runs } = useListDiagnosticRunsQuery(undefined, { refetchOnMountOrArgChange: true })
  const run = runs?.find(item => item.diagnosticRunId === runId)
  const incidentRunActive = runs?.some(item => item.incidentId === incidentId && !item.analysisResultId
    && !['completed', 'failed', 'error'].includes(item.status.toLowerCase()))
  const terminal = progress?.available ? progress.status !== 'running' : ['completed', 'failed', 'error'].includes(run?.status ?? '')
  const evidenceQuery = useGetDiagnosticRunEvidenceQuery(runId, { pollingInterval: evidenceReady || terminal ? 0 : 2000, refetchOnMountOrArgChange: true })
  const resultQuery = useGetDiagnosticRunAnalysisResultQuery(runId, { pollingInterval: resultReady || terminal ? 0 : 3000, refetchOnMountOrArgChange: true })
  const evidence = evidenceQuery.currentData
  const analysis = resultQuery.currentData
  const draft = latestDraft(progress) ?? analysis?.payload.agentExecution?.draft
  const liveScope = latestInvestigationScope(progress) ?? analysis?.payload.agentExecution?.investigationGraph
  const evidenceUnavailable = evidenceQuery.error && 'status' in evidenceQuery.error && evidenceQuery.error.status !== 404

  async function runAgain() {
    try {
      const next = await analyzeIncident(incidentId).unwrap()
      navigate(`/incidents/${encodeURIComponent(incidentId)}/assessments/${encodeURIComponent(next.diagnosticRunId)}`, {
        state: { rootResource: currentIncident?.spec.rootResource },
      })
    } catch { /* The mutation exposes the error below. */ }
  }

  useEffect(() => {
    setEvidenceReady(false); setResultReady(false); setShowSnapshot(false); setSelection(undefined); setSourceSelection(undefined)
    resetAnalysisStart()
  }, [runId])
  useEffect(() => { if (evidence) setEvidenceReady(true) }, [evidence])
  useEffect(() => { if (analysis) setResultReady(true) }, [analysis])
  useEffect(() => {
    if (terminal) { void resultQuery.refetch(); void evidenceQuery.refetch() }
  }, [terminal])
  useEffect(() => {
    const select = (event: Event) => {
      const id = (event as CustomEvent<string>).detail
      const node = evidence?.payload.graph?.nodes.find(item => item.id === id)
      if (node) { setSourceSelection(undefined); setSelection({ type: 'node', value: node }) }
    }
    window.addEventListener('kubechan:select-investigation-node', select)
    return () => window.removeEventListener('kubechan:select-investigation-node', select)
  }, [evidence])

  if (!evidence) return <Box className="assessment-page">
    <Button startIcon={<ArrowBackIcon />} onClick={() => navigate('/')} size="small">Incident {incidentId}</Button>
    <AnalysisProgressPanel progress={progress} compact />
    {evidenceUnavailable && <Alert severity="error" action={<Button onClick={() => { void evidenceQuery.refetch() }}>Retry</Button>}>Could not load this run’s evidence snapshot.</Alert>}
    {terminal ? <Alert severity="info">This run has no collected evidence snapshot.</Alert> : <p role="status"><CircularProgress size={16} /> Collecting the evidence snapshot</p>}
    {earlyRoot && <section className="assessment-workspace" aria-label="Incident root">
      <div className="assessment-section-heading"><span>Incident root</span></div>
      <GraphCanvas graph={rootPreviewGraph(earlyRoot)} nodeInsights={[]} onSelect={setSelection} selected={selection} compact />
    </section>}
  </Box>
  const assessment = analysis?.payload.assessment ?? draft?.assessment
  const graph = evidence.payload.graph
  if (!graph) return <Box><AnalysisProgressPanel progress={progress} /><Alert severity="info">This analysis predates investigation graphs. Run the assessment again to generate one.</Alert></Box>
  const pendingInvestigation = !analysis && !terminal
  const scope = liveScope ?? draft?.graph ?? (pendingInvestigation ? { nodeIds: [graph.entryResourceId], edgeIds: [], candidateIds: [], sourceIds: [] } : undefined)
  const incremental = !!scope
  const visibleGraph = scope && !showSnapshot ? investigationGraph(graph, scope) : graph
  const readNodes = new Set(progress?.events.filter(event => event.investigationGraph && event.target && event.phase === 'completed').map(event => event.target!.nodeId))
  const lastStep = progress?.events[progress.events.length - 1]
  const readingNode = lastStep?.phase === 'started' && lastStep.investigationGraph ? lastStep.target?.nodeId : undefined
  const readIds = scope?.sourceIds && !showSnapshot ? new Set([...scope.sourceIds, ...(progress?.events.flatMap(event => event.exposed?.sourceIds ?? []) ?? []), ...(analysis?.payload.agentExecution?.exposed.sourceIds ?? [])]) : undefined
  const sources = new Map((evidence.payload.sources ?? []).filter(source => !readIds || readIds.has(source.id)).map(source => [source.id, source]))
  const confidence = assessment && (analysis || draft?.hasConclusion) ? Math.round(assessment.confidence * 100) : undefined
  const root = evidence.payload.rootResource
  const selectedNode = selection?.type === 'node' ? visibleGraph.nodes.find(node => node.id === selection.value.id) : undefined
  const effectiveSelection = sourceSelection
    ? ({ type: 'node', value: graph.nodes.find(node => node.sourceIds?.includes(sourceSelection.id)) ?? graph.nodes.find(node => node.id === graph.entryResourceId)! } as Selection)
    : selection?.type === 'node' ? (selectedNode ? { type: 'node' as const, value: selectedNode } : undefined) : selection
  const selectedReview = selectedNode && assessment?.nodeInsights?.find(item => item.nodeId === selectedNode.id)

  return <Box className="assessment-page">
    <header className="assessment-page__header">
      <Button startIcon={<ArrowBackIcon />} onClick={() => navigate('/')} size="small">Incident {incidentId}</Button>
      <div className="assessment-page__identity"><span>Assessment</span><h1>{root?.kind}/{root?.name}</h1><p>{root?.namespace} · Evidence snapshot {new Date(evidence.collectedAt).toLocaleString()}</p></div>
      {confidence !== undefined && <div className="assessment-confidence"><strong>{confidence}%</strong><span>confidence</span><i><b style={{ width: `${confidence}%` }} /></i></div>}
    </header>

    {analysis && currentIncident && currentIncident.status.state !== 'resolved' && (
      <Box sx={{ my: 1.5, display: 'flex', justifyContent: 'flex-end' }}>
        <Button size="small" variant="outlined" disabled={startingAnalysis || incidentRunActive} onClick={() => void runAgain()}>
          {startingAnalysis ? 'Starting…' : incidentRunActive ? 'Analysis running…' : 'Run again without adding resources'}
        </Button>
      </Box>
    )}
    {startFailed && <Alert severity="error">Could not start a new diagnostic run. Try again.</Alert>}
    <AnalysisProgressPanel progress={progress} compact />

    <section className="assessment-diagnosis"><span>{draft && !analysis ? terminal ? 'Draft · investigation stopped' : 'Draft · investigation in progress' : assessment ? 'Diagnosis' : 'Evidence graph'}</span><h2>{assessment?.summary || (draft ? 'KubeChan is investigating and recording findings.' : 'Deterministic evidence snapshot — model assessment is not available.')}</h2><div><strong>{assessment?.claims.filter(claim => claim.verdict === 'supported').length ?? 0}</strong> supported claims <b>·</b> <strong>{visibleGraph.nodes.length}</strong> resources <b>·</b> <strong>{visibleGraph.edges.length}</strong> relationships</div>{draft && !analysis && <p role="status">{terminal ? 'This investigation stopped. Its accepted draft findings are preserved and have not been finalized.' : 'Findings and resource relevance may change as new evidence is inspected.'}</p>}{analysis?.payload.tokenUsage && <small>{analysis.payload.tokenUsage.totalTokens.toLocaleString()} tokens · input {analysis.payload.tokenUsage.inputTokens.toLocaleString()} · output {analysis.payload.tokenUsage.outputTokens.toLocaleString()}{analysis.payload.tokenUsage.inputSource === 'tokenizer_estimate' || analysis.payload.tokenUsage.outputSource === 'tokenizer_estimate' ? ' · tokenizer estimate' : ' · provider reported'}</small>}</section>

    {analysis?.payload.agentExecution?.citationQuotes?.policy === 'snapshot-excerpt-v1' && <Box component="p" sx={{ color: 'text.secondary', fontSize: '.72rem', mt: 0, mb: 2 }}>Cited excerpts come directly from the saved evidence. Select a citation to inspect its complete source.</Box>}

    <section className={`assessment-workspace${incremental && !effectiveSelection ? ' assessment-workspace--graph-only' : ''}`}>
      <div className="assessment-workspace__main">
        <div className="assessment-section-heading"><span>{incremental && !showSnapshot ? 'Agent investigation' : 'Evidence snapshot'}</span><strong>{visibleGraph.nodes.length} resources shown</strong>{incremental && <Button size="small" onClick={() => { setShowSnapshot(value => !value); setSelection(undefined); setSourceSelection(undefined) }}>{showSnapshot ? 'Show investigation' : 'Inspect full snapshot'}</Button>}</div>
        {visibleGraph.nodes.length ? <GraphCanvas graph={visibleGraph} rootCauseId={assessment?.causalPath?.rootCauseEntityId} nodeInsights={assessment?.nodeInsights ?? []} selected={effectiveSelection} compact={!!scope && !showSnapshot} readNodes={readNodes} readingNode={readingNode} onSelect={value => { setSourceSelection(undefined); setSelection(value) }} /> : <div className="assessment-graph-waiting" role="status">Waiting for investigation resources.</div>}
      </div>
      {sourceSelection ? <aside className="assessment-inspector"><div className="assessment-inspector__header"><span>source</span><h3>{sourceLabel(sourceSelection)}</h3></div>{sourceSelection.path && <code>{sourceSelection.path}</code>}<div className="source-record"><pre>{observedValue(sourceSelection)}</pre><small>{sourceSelection.id}</small></div><Button size="small" onClick={() => setSourceSelection(undefined)}>Back to graph selection</Button></aside> : (!incremental || effectiveSelection) && <EvidencePanel selection={effectiveSelection} sources={sources} review={selectedReview} />}
    </section>

    {assessment && <ClaimLedger claims={assessment.claims} sources={sources} onSource={setSourceSelection} />}

    {assessment?.missingEvidence.length ? <section className="assessment-gaps"><h3>Evidence gaps</h3>{assessment.missingEvidence.map((gap, index) => <p key={index}>{gap.description}{gap.reason ? ` · ${gap.reason}` : ''}</p>)}</section> : null}
    {assessment && assessment.remediation.length > 0 && <section className="assessment-runbook"><div className="assessment-section-heading"><span>Remediation runbook</span><strong>{assessment.remediation.length} step{assessment.remediation.length === 1 ? '' : 's'}</strong></div>{[...assessment.remediation].sort((a, b) => a.order - b.order).map(step => <article key={`${step.order}-${step.title}`}><b>{step.order}</b><div><h3>{step.title}</h3><p>{step.description}</p>{step.command && <code>{step.command}</code>}<small>Verify: {step.verification}</small></div></article>)}</section>}
  </Box>
}
