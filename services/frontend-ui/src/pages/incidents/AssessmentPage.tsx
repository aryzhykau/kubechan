import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { Alert, Box, Button, CircularProgress, Tab, Tabs } from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import OpenInNewIcon from '@mui/icons-material/OpenInNew'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import type { AnalysisProgress, AssessmentClaim, AssessmentDraft, EvidenceSource, GraphCandidate, GraphEdge, GraphNode, InvestigationGraph, NodeInsight, ResourceRef } from '../../api/index'
import './AssessmentPage.css'
import { useAnalysisProgress } from '../../analysis/useAnalysisProgress'
import { AnalysisProgressPanel } from '../../components/AnalysisProgressPanel'
import { useGetDiagnosticRunEvidenceQuery, useListDiagnosticRunsQuery } from '../../store/api/diagnosticsApi'
import { useGetDiagnosticRunAnalysisResultQuery } from '../../store/api/analysisApi'
import { investigationGraph, latestDraft, latestInvestigationScope, rootPreviewGraph } from '../../analysis/investigation'
import { useAnalyzeMutation, useGetIncidentQuery, useListIncidentsQuery } from '../../store/api/incidentsApi'

type PositionedNode = GraphNode & { x: number; y: number }
type Branch = NonNullable<AssessmentDraft['branches']>[number]
type Selection = { type: 'node'; value: GraphNode } | { type: 'edge'; value: GraphEdge } | { type: 'candidate'; value: GraphCandidate }

type InspectorSelection = Selection | { type: 'source'; value: EvidenceSource }
type AssessmentView = 'summary' | 'workspace' | 'graph'
type BranchDetailView = 'all' | 'evidence' | 'recovery'

const ASSESSMENT_VIEWS: ReadonlyArray<{ id: AssessmentView; label: string }> = [
  { id: 'summary', label: 'Summary' },
  { id: 'workspace', label: 'Workspace' },
  { id: 'graph', label: 'Graph' },
]
const BRANCH_LABELS: Record<Branch['status'], string> = {
  open: 'Queued', checking: 'Checking', confirmed: 'Blocker', ruled_out: 'Ruled out', gap: 'Unresolved',
}

function branchGraphSelection(branch: Branch, graph: InvestigationGraph): Selection | undefined {
  const edge = branch.edgeId && graph.edges.find(item => item.id === branch.edgeId)
  if (edge) return { type: 'edge', value: edge }
  const node = graph.nodes.find(item => item.id === branch.nodeId)
  if (node) return { type: 'node', value: node }
}

function branchLabel(branch: Branch, graph: InvestigationGraph): string {
  const edge = branch.edgeId && graph.edges.find(item => item.id === branch.edgeId)
  if (!edge) return branch.question
  const from = graph.nodes.find(node => node.id === edge.from)?.resource
  const to = graph.nodes.find(node => node.id === edge.to)?.resource
  if (!to) return branch.question
  if (edge.origin === 'inferred' && from) return `${from.kind}/${from.name} → ${to.kind}/${to.name}`
  return `${to.kind} / ${to.name}`
}

function activateGraphItem(event: KeyboardEvent<SVGGElement>, activate: () => void) {
  if (event.key !== 'Enter' && event.key !== ' ') return
  event.preventDefault()
  event.stopPropagation()
  activate()
}

const NODE_WIDTH = 210
const NODE_HEIGHT = 132
const COLUMN_WIDTH = 256
const ROW_HEIGHT = 172
const INSPECTOR_ATTRIBUTES = ['selector', 'templateLabels', 'replicas', 'readyReplicas', 'availableReplicas', 'readyEndpoints', 'notReadyEndpoints', 'phase', 'storageClass', 'scaleTargetRef', 'endpoint', 'status', 'totalLag', 'identityBasis']

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
  return { nodes: positioned, width: 80 + maxRank * COLUMN_WIDTH + NODE_WIDTH, height: 80 + (maxRows - 1) * ROW_HEIGHT + NODE_HEIGHT }
}

function compactLabel(value: string, maxLength: number): string {
  return value.length <= maxLength ? value : `${value.slice(0, maxLength - 1)}…`
}

function formatAttribute(value: unknown): string {
  if (value == null) return '—'
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value)
  return JSON.stringify(value)
}

function nodeMetadata(node: GraphNode): Array<{ label: string; value: string }> {
  const lines = [
    { label: 'state', value: node.state },
    { label: 'role', value: node.role || 'context' },
    { label: 'namespace', value: node.resource.namespace || 'cluster scoped' },
    { label: 'evidence', value: `${node.sourceIds?.length ?? 0} sources` },
  ]
  const attributes = node.attributes ?? {}
  for (const key of INSPECTOR_ATTRIBUTES) {
    if (!(key in attributes) || lines.length >= 8) continue
    lines.push({ label: key, value: formatAttribute(attributes[key]) })
  }
  return lines
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
  const viewport = useRef<HTMLDivElement>(null)
  const [viewportWidth, setViewportWidth] = useState(0)
  const [manualZoom, setManualZoom] = useState<number>()
  const markerId = useId()
  const fitZoom = viewportWidth ? Math.min(1, Math.max(0.15, (viewportWidth - 4) / layout.width)) : 0.65
  const zoom = manualZoom ?? Math.max(0.57, fitZoom)
  const selectedId = selected?.value.id

  useEffect(() => {
    const element = viewport.current
    if (!element) return
    const observer = new ResizeObserver(() => setViewportWidth(element.clientWidth))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    const element = viewport.current
    if (!element || !selectedId) return
    const item = Array.from(element.querySelectorAll<SVGGElement>('[data-graph-id]'))
      .find(target => target.dataset.graphId === selectedId)
    if (!item) return
    const bounds = element.getBoundingClientRect()
    const target = item.getBoundingClientRect()
    const x = target.left + target.width / 2
    const y = target.top + target.height / 2
    if (x < bounds.left + 16 || x > bounds.right - 16 || y < bounds.top + 16 || y > bounds.bottom - 16) {
      element.scrollBy(x - bounds.left - element.clientWidth / 2, y - bounds.top - element.clientHeight / 2)
    }
  }, [layout, selectedId, zoom])
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
    <div ref={viewport} className="investigation-graph__viewport">
      <div className="investigation-graph__controls" aria-label="Graph zoom controls">
        <button type="button" onClick={() => setManualZoom(Math.max(Math.min(0.3, fitZoom), zoom - 0.1))} aria-label="Zoom out">−</button>
        <span>{Math.round(zoom * 100)}%</span>
        <button type="button" onClick={() => setManualZoom(Math.min(1.5, zoom + 0.1))} aria-label="Zoom in">+</button>
        <button type="button" onClick={() => { setManualZoom(fitZoom); viewport.current?.scrollTo(0, 0) }}>Fit</button>
      </div>
      <svg className="investigation-graph" style={{ width: layout.width * zoom, height: layout.height * zoom }} viewBox={`0 0 ${layout.width} ${layout.height}`} preserveAspectRatio="xMinYMin meet" role="group" aria-label="Investigation resource graph">
        <defs>
          <marker id={`${markerId}-normal`} className="graph-marker--normal" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
          <marker id={`${markerId}-broken`} className="graph-marker--broken" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
          <marker id={`${markerId}-candidate`} className="graph-marker--candidate" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker>
        </defs>
        {graph.edges.map(edge => {
          const path = edgePath(edge.from, edge.to)
          if (!path) return null
          return <g key={edge.id} className={`graph-edge graph-edge--${edge.state} ${edge.origin === 'inferred' ? 'graph-edge--inferred' : ''} ${selected?.type === 'edge' && selected.value.id === edge.id ? 'is-selected' : ''}`} data-graph-id={edge.id} role="button" tabIndex={0} aria-pressed={selected?.type === 'edge' && selectedId === edge.id}
            aria-label={`${byId.get(edge.from)?.resource.name}: ${edge.type.replace(/_/g, ' ')} ${byId.get(edge.to)?.resource.name} · ${edge.origin === 'inferred' ? 'proposed' : edge.state}`}
            onClick={() => onSelect({ type: 'edge', value: edge })} onKeyDown={event => activateGraphItem(event, () => onSelect({ type: 'edge', value: edge }))}>
            <path className="graph-edge__hit" d={path} />
            <path className="graph-edge__line" d={path} markerEnd={`url(#${markerId}-${edge.origin === 'inferred' ? 'candidate' : edge.state === 'broken' ? 'broken' : 'normal'})`} />
            <text x={((byId.get(edge.from)?.x ?? 0) + (byId.get(edge.to)?.x ?? 0)) / 2 + NODE_WIDTH / 2} y={Math.min(byId.get(edge.from)?.y ?? 0, byId.get(edge.to)?.y ?? 0) + 30}>{edge.type.replace(/_/g, ' ')}</text>
          </g>
        })}
        {candidates.map(candidate => {
          // New snapshots point the remediation hint from the resource whose
          // configuration must change. Fall back for pre-contract snapshots.
          const sourceNodeId = candidate.sourceNodeId ?? candidate.referenceNodeId
          const path = edgePath(sourceNodeId, candidate.candidateNodeId)
          if (!path) return null
          return <g key={candidate.id} className={`graph-edge graph-edge--candidate ${selected?.type === 'candidate' && selected.value.id === candidate.id ? 'is-selected' : ''}`} data-graph-id={candidate.id} role="button" tabIndex={0} aria-pressed={selected?.type === 'candidate' && selectedId === candidate.id}
            aria-label={`Proposed target ${byId.get(candidate.candidateNodeId)?.resource.name}`}
            onClick={() => onSelect({ type: 'candidate', value: candidate })} onKeyDown={event => activateGraphItem(event, () => onSelect({ type: 'candidate', value: candidate }))}>
            <path className="graph-edge__hit" d={path} />
            <path className="graph-edge__line" d={path} markerEnd={`url(#${markerId}-candidate)`} />
            <text x={((byId.get(sourceNodeId)?.x ?? 0) + (byId.get(candidate.candidateNodeId)?.x ?? 0)) / 2 + NODE_WIDTH / 2} y={Math.min(byId.get(sourceNodeId)?.y ?? 0, byId.get(candidate.candidateNodeId)?.y ?? 0) + 70}>{candidate.strength === 'strong' ? 'should route to' : 'possible target'}</text>
          </g>
        })}
        {layout.nodes.map(node => {
          const state = node.id === rootCauseId ? 'root-cause' : node.state
          const candidate = candidateByNode.get(node.id)
          const review = insightByNode.get(node.id)
          return <g key={node.id} className={`graph-node graph-node--${state} ${node.type === 'reference' ? 'graph-node--reference' : ''} ${selected?.type === 'node' && selected.value.id === node.id ? 'is-selected' : ''}`} transform={`translate(${node.x} ${node.y})`} data-graph-id={node.id} role="button" tabIndex={0} aria-pressed={selected?.type === 'node' && selectedId === node.id}
            aria-label={`${node.resource.kind}/${node.resource.name} · ${node.state}`}
            onClick={() => onSelect({ type: 'node', value: node })} onKeyDown={event => activateGraphItem(event, () => onSelect({ type: 'node', value: node }))}>
            <title>{`${node.resource.kind}/${node.resource.name} · ${node.resource.namespace || 'cluster scoped'}`}</title>
            <rect width={NODE_WIDTH} height={NODE_HEIGHT} rx="3" />
            <text className="graph-node__kind" x="14" y="21">{compactLabel(node.resource.kind, 25)}</text>
            <text className="graph-node__name" x="14" y="44">{compactLabel(node.resource.name, 23)}</text>
            <text className="graph-node__namespace" x="14" y="66">{compactLabel(node.resource.namespace || 'cluster scoped', 31)}</text>
            <text className="graph-node__state" x={NODE_WIDTH - 14} y="20" textAnchor="end">{node.state === 'missing' ? 'not found' : node.id === rootCauseId ? 'root cause' : compactLabel(node.state, 13)}</text>
            <text className={`graph-node__review graph-node__review--${review?.relevance ?? 'context'}`} x="14" y="85">{review?.relevance === 'context' ? 'relevance not determined' : review?.relevance.replace('_', ' ') ?? 'not assessed'}</text>
            <text className="graph-node__explanation" x="14" y="102">{compactLabel(review?.comment ?? (readingNode === node.id ? 'Checking snapshot…' : readNodes?.has(node.id) ? 'Snapshot read completed' : 'No model review'), 32)}</text>
            {candidate && <g className={`graph-node__candidate ${selected?.type === 'candidate' && selected.value.id === candidate.id ? 'is-selected' : ''}`} role="button" tabIndex={0} aria-label={`Inspect ${candidate.strength} candidate`} onClick={event => { event.stopPropagation(); onSelect({ type: 'candidate', value: candidate }) }} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); event.stopPropagation(); onSelect({ type: 'candidate', value: candidate }) } }}>
              <rect x="108" y="113" width="88" height="15" rx="2" />
              <text x="152" y="124" textAnchor="middle">{candidate.strength} candidate</text>
            </g>}
          </g>
        })}
      </svg>
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
  return <aside className="assessment-inspector" aria-label="Evidence inspector">
    <div className="assessment-inspector__header"><span>{selection.type}</span><h3>{title}</h3></div>
    {selection.type === 'node' && <dl className="assessment-node-meta">{nodeMetadata(selection.value).map(({ label, value }) =>
      <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>}
    {review && <div className="candidate-reason"><strong>{review.relevance === 'context' ? 'Relevance undetermined' : review.relevance === 'not_relevant' ? 'Not relevant' : 'Relevant'}</strong><p>{review.comment}</p></div>}
    {selection.type === 'candidate' && selection.value.reasons.map(reason => <div className="candidate-reason" key={reason.code}><strong>{reason.label}</strong><span>{reason.code}</span></div>)}
    {selection.type === 'edge' && selection.value.origin === 'inferred' && <div className="candidate-reason"><strong>{selection.value.strength ?? 'possible'} inferred relationship · score {selection.value.score ?? 0}</strong><span>Not observed Kubernetes topology</span></div>}
    {selection.type === 'edge' && selection.value.reasons?.map(reason => <div className="candidate-reason" key={reason.code}><strong>{reason.label}</strong><span>{reason.code}</span></div>)}
    {resolved.length === 0 && <p className="assessment-inspector__muted">No directly addressable evidence is attached to this entity.</p>}
    {resolved.map(source => <SourceRecord key={source.id} source={source} />)}
  </aside>
}

function SourceRecord({ source }: { source: EvidenceSource }) {
  return <div className="source-record">
    <strong>{sourceLabel(source)}</strong>
    {source.path && <code>{source.path}</code>}
    <pre>{observedValue(source)}</pre>
    <small>{source.id}</small>
  </div>
}

function BranchPicker({ branches, graph, selectedId, onSelect, horizontal = false }: {
  branches: Branch[]
  graph: InvestigationGraph
  selectedId?: string
  onSelect: (branch: Branch) => void
  horizontal?: boolean
}) {
  return <div className={`assessment-branch-picker${horizontal ? ' assessment-branch-picker--horizontal' : ''}`} role="group" aria-label="Investigation branches">
    {branches.map(branch => <button key={branch.id} type="button" className="assessment-branch-choice"
      aria-pressed={selectedId === branch.id} onClick={() => onSelect(branch)}>
      <span className={`assessment-branch-status assessment-branch-status--${branch.status}`}>{BRANCH_LABELS[branch.status]}</span>
      <strong>{branchLabel(branch, graph)}</strong>
    </button>)}
  </div>
}

function BranchDetails({ branch, sources, claim, progress, view, showOutcome = true }: {
  branch: Branch
  sources: Map<string, EvidenceSource>
  claim?: AssessmentClaim
  progress?: AnalysisProgress
  view: BranchDetailView
  showOutcome?: boolean
}) {
  const activity = branch.status === 'checking'
    ? progress?.events.slice().reverse().find(event => event.branchId === branch.id && event.target)?.target
    : undefined
  const evidenceIds = new Set([...branch.sourceIds, ...(claim?.citations.map(citation => citation.sourceId) ?? [])])
  const evidence = [...evidenceIds].flatMap(id => {
    const source = sources.get(id)
    return source ? [source] : []
  })
  const outcome = branch.outcome || claim?.statement
  return <div className="assessment-branch-content">
    {showOutcome && outcome && <p className="assessment-branch-outcome">{outcome}</p>}
    {activity && <p role="status">Checking {activity.resource.kind}/{activity.resource.name}</p>}
    {!outcome && !activity && <p>{branch.status === 'checking' ? 'Investigation in progress.' : 'No outcome recorded yet.'}</p>}
    {view !== 'recovery' && <section aria-label="Branch evidence">
      <h4>Evidence</h4>
      {evidence.length > 0 ? evidence.map(source => <SourceRecord key={source.id} source={source} />) : <p>No evidence cited yet.</p>}
    </section>}
    {view !== 'evidence' && (branch.status === 'confirmed' || !!branch.runbook?.length) && <section aria-label="Branch recovery">
      <h4>Recovery</h4>
      {branch.runbook?.length ? <ol>{branch.runbook.map((step, index) => <li key={index}>
        {step.action}
        {step.verification && <p><strong>Verify</strong> · {step.verification}</p>}
      </li>)}</ol> : <p>No recovery steps recorded for this branch.</p>}
    </section>}
    {view === 'recovery' && branch.status !== 'confirmed' && !branch.runbook?.length && <p>Recovery steps are available for confirmed blockers.</p>}
  </div>
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
  const [view, setView] = useState<AssessmentView>(ASSESSMENT_VIEWS[0].id)
  const [branchId, setBranchId] = useState<string>()
  const [summaryExpanded, setSummaryExpanded] = useState(false)
  const [inspected, setInspected] = useState<InspectorSelection>()
  const [workspaceDetail, setWorkspaceDetail] = useState<BranchDetailView>('evidence')
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
    setEvidenceReady(false); setResultReady(false); setShowSnapshot(false); setInspected(undefined)
    setBranchId(undefined); setSummaryExpanded(false); setView(ASSESSMENT_VIEWS[0].id); setWorkspaceDetail('evidence')
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
      if (node) {
        setInspected({ type: 'node', value: node })
        if (ASSESSMENT_VIEWS.some(view => view.id === 'graph')) setView('graph')
      }
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
      <GraphCanvas graph={rootPreviewGraph(earlyRoot)} nodeInsights={[]} onSelect={setInspected} selected={inspected?.type === 'node' ? inspected : undefined} compact />
    </section>}
  </Box>
  const assessment = analysis?.payload.assessment ?? draft?.assessment
  const graph = evidence.payload.graph
  if (!graph) return <Box><AnalysisProgressPanel progress={progress} /><Alert severity="info">This analysis predates investigation graphs. Run the assessment again to generate one.</Alert></Box>
  const pendingInvestigation = !analysis && !terminal
  const fullPlanGraph = !!draft?.branches?.length
  const scope = liveScope ?? draft?.graph ?? (pendingInvestigation ? { nodeIds: [graph.entryResourceId], edgeIds: [], candidateIds: [], sourceIds: [] } : undefined)
  const incremental = !!scope
  const visibleGraph = scope && !showSnapshot && !fullPlanGraph ? investigationGraph(graph, scope) : graph
  const readNodes = new Set(progress?.events.filter(event => event.investigationGraph && event.target && event.phase === 'completed').map(event => event.target!.nodeId))
  const lastStep = progress?.events[progress.events.length - 1]
  const readingNode = lastStep?.phase === 'started' && lastStep.investigationGraph ? lastStep.target?.nodeId : undefined
  const readIds = scope?.sourceIds && !showSnapshot && !fullPlanGraph ? new Set([...scope.sourceIds, ...(progress?.events.flatMap(event => event.exposed?.sourceIds ?? []) ?? []), ...(analysis?.payload.agentExecution?.exposed.sourceIds ?? [])]) : undefined
  const sources = new Map((evidence.payload.sources ?? []).filter(source => !readIds || readIds.has(source.id)).map(source => [source.id, source]))
  const confidence = assessment && (analysis || draft?.hasConclusion) ? Math.round(assessment.confidence * 100) : undefined
  const root = evidence.payload.rootResource
  const branches = draft?.branches ?? []
  const claims = assessment?.claims ?? []
  const supported = claims.filter(claim => claim.verdict === 'supported')
  const blockers = branches.filter((branch, index) => branch.status === 'confirmed' &&
    (!branch.findingId || branches.findIndex(item => item.status === 'confirmed' && item.findingId === branch.findingId) === index))
  const activeBranch = branches.find(branch => branch.id === branchId)
    ?? (pendingInvestigation ? branches.find(branch => branch.status === 'checking') : undefined)
    ?? blockers[0] ?? branches[0]
  const linkedClaim = (branch: Branch) => claims.find(claim => claim.id === branch.findingId) ?? claims.find(claim => claim.statement === branch.outcome)
  const standaloneClaims = supported.filter(claim => !blockers.some(branch => linkedClaim(branch)?.id === claim.id))
  const activeAnchor = activeBranch && branchGraphSelection(activeBranch, visibleGraph)
  const rootNode = visibleGraph.nodes.find(node => node.id === visibleGraph.entryResourceId)
  const defaultGraphSelection: Selection | undefined = activeAnchor ?? (rootNode ? { type: 'node', value: rootNode } : undefined)
  const runState = analysis?.payload.agentExecution?.status ?? progress?.status ?? (analysis ? 'completed' : terminal ? 'stopped' : 'running')
  const statusLabel = { completed: 'Completed', partial: 'Partial', running: 'In progress', timed_out: 'Timed out' }[runState as string]
    ?? runState.replace(/_/g, ' ')
  const selectedSource = inspected?.type === 'source' ? inspected.value : undefined
  const sourceNode = selectedSource && visibleGraph.nodes.find(node => node.sourceIds?.includes(selectedSource.id))
  const visibleEntity = inspected?.type === 'node' ? visibleGraph.nodes.some(node => node.id === inspected.value.id)
    : inspected?.type === 'edge' ? visibleGraph.edges.some(edge => edge.id === inspected.value.id)
    : inspected?.type === 'candidate' && visibleGraph.candidates?.some(candidate => candidate.id === inspected.value.id)
  const inspectedEntity = inspected && inspected.type !== 'source' && visibleEntity ? inspected : undefined
  const graphSelection: Selection | undefined = sourceNode ? { type: 'node', value: sourceNode } : inspectedEntity ?? defaultGraphSelection
  const review = graphSelection?.type === 'node' ? assessment?.nodeInsights?.find(item => item.nodeId === graphSelection.value.id) : undefined

  function pickBranch(branch: Branch) {
    setBranchId(branch.id)
    setInspected(undefined)
  }

  function pickGraphItem(item: Selection) {
    const related = branches.filter(branch => item.type === 'edge' ? branch.edgeId === item.value.id
      : item.type === 'node' && (branch.nodeId === item.value.id || graph?.edges.some(edge => edge.id === branch.edgeId && edge.to === item.value.id)))
    if (related.length === 1) pickBranch(related[0])
    else setInspected(item)
  }

  function inspectSource(source: EvidenceSource) {
    setInspected({ type: 'source', value: source })
    if (ASSESSMENT_VIEWS.some(view => view.id === 'graph')) setView('graph')
  }

  const graphViewEnabled = ASSESSMENT_VIEWS.some(view => view.id === 'graph')
  const workspaceViewEnabled = ASSESSMENT_VIEWS.some(view => view.id === 'workspace')
  const gaps = assessment?.missingEvidence ?? []
  const hasFindings = blockers.length > 0 || standaloneClaims.length > 0

  return <Box className="assessment-page">
    <header className="assessment-page__header">
      <div className="assessment-page__breadcrumbs">
        <Button startIcon={<ArrowBackIcon />} onClick={() => navigate('/')} size="small">Incidents</Button>
        <span aria-hidden="true">/</span><span>Assessment</span>
      </div>
      <div className="assessment-page__title">
        <div className="assessment-page__identity">
          <p>{root?.kind} · {root?.namespace || 'Cluster scoped'}</p>
          <h1>{root?.name ?? 'Assessment'}</h1>
          <p>Evidence snapshot · {new Date(evidence.collectedAt).toLocaleString()}</p>
        </div>
        <div className="assessment-page__actions">
          <span className={`assessment-run-state assessment-run-state--${runState}`}>{statusLabel}</span>
          {analysis && currentIncident && currentIncident.status.state !== 'resolved' &&
            <Button size="small" variant="outlined" title="Run again without adding resources"
              disabled={startingAnalysis || incidentRunActive} onClick={() => void runAgain()}>
              {startingAnalysis ? 'Starting…' : incidentRunActive ? 'Analysis running…' : 'Run again'}
            </Button>}
        </div>
      </div>
    </header>

    {startFailed && <Alert severity="error">Could not start a new diagnostic run. Try again.</Alert>}
    {pendingInvestigation && <AnalysisProgressPanel progress={progress} compact />}
    <Tabs className="assessment-tabs" value={view} onChange={(_, next: AssessmentView) => setView(next)} aria-label="Investigation views">
      {ASSESSMENT_VIEWS.map(({ id, label }) =>
        <Tab key={id} value={id} label={label} id={`assessment-tab-${id}`} aria-controls={`assessment-view-${id}`} />)}
    </Tabs>

    <section className="assessment-view assessment-summary" role="tabpanel" id="assessment-view-summary" aria-labelledby="assessment-tab-summary" hidden={view !== 'summary'}>
      <div className="assessment-view-heading">
        <h2>{blockers.length ? `${blockers.length} confirmed blocker${blockers.length === 1 ? '' : 's'}` : 'Diagnosis'}</h2>
        {confidence !== undefined && <span>{confidence}% confidence</span>}
      </div>
      {!hasFindings && <p className="assessment-summary-text">{assessment?.summary || (pendingInvestigation ? 'Investigation is collecting and checking evidence.' : 'No assessment is available for this snapshot.')}</p>}
      {draft && !analysis && <p role="status" className="assessment-draft-note">{terminal ? 'This investigation stopped. The saved findings below are provisional.' : 'Findings are provisional while investigation runs.'}</p>}
      {blockers.length > 0 && <ol className="assessment-summary-list" aria-label="Confirmed blockers">
        {blockers.map((branch, index) => {
          const expanded = summaryExpanded && activeBranch?.id === branch.id
          const claim = linkedClaim(branch)
          const label = branchLabel(branch, graph)
          const outcome = claim?.statement || branch.outcome
          return <li key={branch.id}>
            <button className="assessment-summary-choice" type="button" aria-expanded={expanded} aria-controls={`summary-branch-${branch.id}`} onClick={() => {
              pickBranch(branch)
              setSummaryExpanded(!expanded)
            }}>
              <span className="assessment-summary-index" aria-hidden="true">{String(index + 1).padStart(2, '0')}</span>
              <span className="assessment-summary-finding">
                <strong>{label}</strong>
                {outcome && outcome !== label && <span>{outcome}</span>}
              </span>
              <ExpandMoreIcon aria-hidden="true" fontSize="small" />
            </button>
            <div className="assessment-summary-detail" id={`summary-branch-${branch.id}`} hidden={!expanded}>
              {expanded && <>
                <BranchDetails branch={branch} claim={claim} sources={sources} view="all" showOutcome={false} />
                {workspaceViewEnabled && <Button size="small" onClick={() => setView('workspace')}>Open in workspace</Button>}
                {graphViewEnabled && <Button size="small" onClick={() => { setInspected(undefined); setView('graph') }}>Show on graph</Button>}
              </>}
            </div>
          </li>
        })}
      </ol>}
      {standaloneClaims.length > 0 && <ClaimLedger claims={standaloneClaims} sources={sources} onSource={inspectSource} />}
      {!graphViewEnabled && selectedSource && <SourceRecord source={selectedSource} />}
      {gaps.length > 0 && <section className="assessment-gaps"><h3>Unresolved checks</h3>
        {gaps.map((gap, index) => <p key={index}>{gap.description}{gap.reason ? ` · ${gap.reason}` : ''}</p>)}
      </section>}
      {assessment && assessment.remediation.length > 0 && <section className="assessment-runbook">
        <div className="assessment-section-heading"><span>Recovery runbook</span><strong>{assessment.remediation.length} steps</strong></div>
        {[...assessment.remediation].sort((a, b) => a.order - b.order).map(step => <article key={`${step.order}-${step.title}`}>
          <b>{step.order}</b><div><h3>{step.title}</h3>{step.description && <p>{step.description}</p>}{step.command && <code>{step.command}</code>}
            {step.verification && <small>Verify: {step.verification}</small>}</div>
        </article>)}
      </section>}
      {hasFindings && assessment?.summary && <details className="assessment-disclosure">
        <summary>Full assessment text</summary>
        <div className="assessment-summary-text">{assessment.summary}</div>
      </details>}
      {claims.length > standaloneClaims.length && <details className="assessment-disclosure">
        <summary>All findings and citations</summary><ClaimLedger claims={claims} sources={sources} onSource={inspectSource} />
      </details>}
    </section>

    <section className="assessment-view" role="tabpanel" id="assessment-view-workspace" aria-labelledby="assessment-tab-workspace" hidden={view !== 'workspace'}>
      {activeBranch ? <div className="assessment-branch-workspace">
        <aside aria-label="Investigation plan">
          <div className="assessment-view-heading"><h2>Branches</h2><span>{branches.length} checks</span></div>
          <BranchPicker branches={branches} graph={graph} selectedId={activeBranch.id} onSelect={pickBranch} />
        </aside>
        <section className="assessment-inspector" aria-label="Selected branch">
          <div className="assessment-inspector__header">
            <span className={`assessment-branch-status assessment-branch-status--${activeBranch.status}`}>{BRANCH_LABELS[activeBranch.status]}</span>
            <h3>{activeBranch.question}</h3>
          </div>
          <div className="assessment-detail-toggle" role="group" aria-label="Branch details">
            <Button size="small" aria-pressed={workspaceDetail === 'evidence'} onClick={() => setWorkspaceDetail('evidence')}>Evidence</Button>
            <Button size="small" aria-pressed={workspaceDetail === 'recovery'} onClick={() => setWorkspaceDetail('recovery')}>Recovery</Button>
          </div>
          <BranchDetails branch={activeBranch} claim={linkedClaim(activeBranch)} sources={sources} progress={progress} view={workspaceDetail} />
          {graphViewEnabled && <Button size="small" onClick={() => { setInspected(undefined); setView('graph') }}>Show on graph</Button>}
        </section>
      </div> : <div className="assessment-empty"><h2>Investigation branches</h2>
        <p>{pendingInvestigation ? 'Branches will appear when the agent records its plan.' : 'This assessment did not record investigation branches.'}</p>
      </div>}
    </section>

    <section className="assessment-view" role="tabpanel" id="assessment-view-graph" aria-labelledby="assessment-tab-graph" hidden={view !== 'graph'}>
      <div className="assessment-view-heading"><h2>Dependency map</h2><span>{visibleGraph.nodes.length} resources · {visibleGraph.edges.length} relationships</span></div>
      {activeBranch && <BranchPicker branches={branches} graph={graph} selectedId={!inspected ? activeBranch.id : undefined} onSelect={pickBranch} horizontal />}
      <section className="assessment-workspace assessment-workspace--split" aria-label="Graph and evidence inspector">
        <div className="assessment-workspace__main">
          <div className="assessment-graph-bar">
            <div className="assessment-graph-legend" aria-label="Relationship legend">
              <span><i /> Observed</span><span><i /> Broken</span><span><i /> Proposed</span>
            </div>
            {incremental && !fullPlanGraph && <Button size="small" onClick={() => { setShowSnapshot(value => !value); setInspected(undefined) }}>{showSnapshot ? 'Show investigation' : 'Full snapshot'}</Button>}
          </div>
          {view === 'graph' && (visibleGraph.nodes.length ? <GraphCanvas graph={visibleGraph} rootCauseId={assessment?.causalPath?.rootCauseEntityId}
            nodeInsights={assessment?.nodeInsights ?? []} selected={graphSelection} compact readNodes={readNodes} readingNode={readingNode} onSelect={pickGraphItem} />
            : <div className="assessment-graph-waiting" role="status">Waiting for investigation resources.</div>)}
        </div>
        {selectedSource ? <aside className="assessment-inspector" aria-label="Evidence inspector">
          <div className="assessment-inspector__header"><span>Exact source</span><h3>{sourceLabel(selectedSource)}</h3></div>
          <SourceRecord source={selectedSource} />
          <Button size="small" onClick={() => setInspected(undefined)}>Back to graph selection</Button>
        </aside> : !inspectedEntity && activeBranch ? <aside className="assessment-inspector" aria-label="Evidence inspector">
          <div className="assessment-inspector__header">
            <span className={`assessment-branch-status assessment-branch-status--${activeBranch.status}`}>{BRANCH_LABELS[activeBranch.status]}</span>
            <h3>{branchLabel(activeBranch, graph)}</h3>
            <p>{activeBranch.question}</p>
          </div>
          <BranchDetails branch={activeBranch} claim={linkedClaim(activeBranch)} sources={sources} progress={progress} view="all" />
          {activeAnchor && <Button size="small" onClick={() => setInspected(activeAnchor)}>Inspect {activeAnchor.type === 'edge' ? 'relationship' : 'resource'}</Button>}
        </aside> : <EvidencePanel selection={graphSelection} sources={sources} review={review} />}
      </section>
    </section>

    {!pendingInvestigation && progress?.available && <details className="assessment-disclosure">
      <summary>Investigation activity</summary>
      <AnalysisProgressPanel progress={progress} compact />
    </details>}
    {analysis?.payload.tokenUsage && <details className="assessment-disclosure">
      <summary>Run details</summary>
      <p>{analysis.payload.tokenUsage.totalTokens.toLocaleString()} tokens · input {analysis.payload.tokenUsage.inputTokens.toLocaleString()} · output {analysis.payload.tokenUsage.outputTokens.toLocaleString()}</p>
    </details>}
  </Box>
}
