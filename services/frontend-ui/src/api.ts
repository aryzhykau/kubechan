// API base URL — in dev Vite proxies /api → backend; in prod it's same origin.
const BASE = ''

export interface ResourceRef {
  namespace?: string
  kind: string
  name: string
}

export interface Incident {
  metadata: { name: string; namespace: string; creationTimestamp: string }
  spec: { rootResource: ResourceRef; problemCases?: string[] }
  status: { state: 'open' | 'resolved'; openedAt?: string; resolvedAt?: string; activeProblemCases?: number }
}

export interface ProblemCase {
  metadata: { name: string; namespace: string }
  spec: { affectedResource: ResourceRef; detector: string; severity: string; symptoms?: string[] }
  status: { state: string; firstSeen?: string; lastSeen?: string }
}

export interface DiagnosticRun {
  metadata: { name: string; namespace: string }
  spec: { incidentRef?: string; requestedAt?: string }
  status: { state: string; evidenceRef?: string; collectionErrors?: string[] }
}

export interface AnalyzeResponse {
  diagnosticRunId: string
  analysisRequestId: string
}

export interface AnalysisResult {
  id: string
  incidentId: string
  diagnosticRunId: string
  model: string
  status: string
  likelyRootCause: string
  confidence: number
  payload: {
    openingRant?: string
    likelyRootCause?: string
    evidenceChain?: string
    recommendation?: string
    closingInsult?: string
    confidence?: number
    model?: string
    thinkingBudgetUsed?: number
  }
  createdAt: string
}

async function apiFetch<T>(path: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json', ...opts?.headers },
    ...opts,
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`${res.status} ${res.statusText}: ${body}`)
  }
  return res.json() as Promise<T>
}

export const api = {
  listIncidents: (ns = 'kubechan') =>
    apiFetch<Incident[]>(`/api/v1/incidents?namespace=${ns}`),

  getIncident: (id: string) =>
    apiFetch<Incident>(`/api/v1/incidents/${id}`),

  analyze: (id: string) =>
    apiFetch<AnalyzeResponse>(`/api/v1/incidents/${id}/analyze`, { method: 'POST' }),

  listProblemCases: (ns = 'kubechan') =>
    apiFetch<ProblemCase[]>(`/api/v1/problemcases?namespace=${ns}`),

  getDiagnosticRun: (id: string) =>
    apiFetch<DiagnosticRun>(`/api/v1/diagnosticruns/${id}`),

  getAnalysisResult: (id: string) =>
    apiFetch<AnalysisResult>(`/api/v1/analysisresults/${id}`),
}
