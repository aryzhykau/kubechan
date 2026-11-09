import type { AnalysisProgress, AssessmentDraft, InvestigationGraph, InvestigationScope, ResourceRef } from '../api/index'

export function assessmentPath(incidentId: string, runId: string): string {
  return `/incidents/${encodeURIComponent(incidentId)}/assessments/${encodeURIComponent(runId)}`
}

export function latestDraft(progress?: AnalysisProgress): AssessmentDraft | undefined {
  return progress?.events.reduce<AssessmentDraft | undefined>((current, event) => {
    if (event.attemptId !== progress.attemptId || !event.draft || event.draft.version !== 1) return current
    return !current || event.draft.revision > current.revision ? event.draft : current
  }, undefined)
}

export function latestInvestigationScope(progress?: AnalysisProgress): InvestigationScope | undefined {
  for (let i = (progress?.events.length ?? 0) - 1; progress && i >= 0; i--) {
    const event = progress.events[i]
    if (event.attemptId === progress.attemptId && event.investigationGraph) return event.investigationGraph
  }
  return undefined
}

export function rootPreviewGraph(resource: ResourceRef): InvestigationGraph {
  const id = 'pending-root'
  return { schemaVersion: 2, entryResourceId: id, nodes: [{ id, type: 'resource', resource, state: 'collecting', role: 'root' }], edges: [], candidates: [] }
}

export function investigationGraph(graph: InvestigationGraph, draft: AssessmentDraft | InvestigationScope): InvestigationGraph {
  const scope = 'graph' in draft ? draft.graph : draft
  const selected = new Set(scope.nodeIds)
  const read = scope.sourceIds ? new Set(scope.sourceIds) : undefined
  const edges = new Set(scope.edgeIds)
  const nodes = graph.nodes.filter(node => selected.has(node.id)).map(node => read
    ? { ...node, attributes: undefined, sourceIds: [...new Set([
      ...(node.sourceIds ?? []),
      ...graph.edges.filter(edge => node.type === 'reference' && edge.to === node.id && edges.has(edge.id) && edge.origin === 'observed' && edge.state === 'broken').flatMap(edge => edge.sourceIds ?? []),
    ].filter(id => read.has(id)))] }
    : node)
  const available = new Set(nodes.map(node => node.id))
  const candidates = new Set(scope.candidateIds)
  return { ...graph, nodes,
    edges: graph.edges.filter(edge => edges.has(edge.id) && available.has(edge.from) && available.has(edge.to)),
    candidates: graph.candidates?.filter(candidate => candidates.has(candidate.id) && [candidate.sourceNodeId, candidate.referenceNodeId, candidate.candidateNodeId].every(id => !!id && available.has(id))),
  }
}
