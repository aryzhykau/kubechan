// API base URL — in dev Vite proxies /api → backend; in prod it's same origin.
const BASE = ''

const TOKEN_KEY = 'kubechan_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

export interface CurrentUser {
  userId: string
  username: string
  role: 'admin' | 'viewer'
}
export interface ResourceRef {
  namespace?: string
  kind: string
  name: string
  apiGroup?: string
}

export interface Incident {
  metadata: { name: string; namespace: string; creationTimestamp: string }
  spec: {
    rootResource: ResourceRef
    problemCases?: string[]
    source?: 'auto' | 'manual'
    userMessage?: string
    relatedResources?: ResourceRef[]
  }
  status: { state: 'open' | 'resolved'; openedAt?: string; resolvedAt?: string; activeProblemCases?: number }
  ownerUsername?: string
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

export interface DiagnosticRunDetail {
  diagnosticRun: DiagnosticRun
  triggeredBy?: { userId: string; username: string } | null
}

export interface DiagnosticRunSummary {
  diagnosticRunId: string
  incidentId: string
  requestedAt: string
  status: string
  analysisResultId?: string
  likelyRootCause?: string
  confidence?: number
  model?: string
  analysisCreatedAt?: string
  needsMoreInfo?: boolean
  suggestedResources?: SuggestedResource[]
  suggestFalsePositive?: boolean
  suggestExclusionRule?: ExclusionRuleProposal | null
}

export interface K8sEvent {
  type: string
  reason: string
  message: string
  count: number
  lastTime: string
}

export interface PodLog {
  podName: string
  phase: string
  logs: string
  prevLogs?: string
  events: K8sEvent[]
  dependencies?: {
    configMaps: Array<{
      name: string
      missing?: boolean
      keys?: string[]
      mountPaths?: string[]
      data?: Record<string, string>
    }>
    secrets: Array<{ name: string; missing?: boolean }>
  }
}

export interface PVCInfo {
  name: string
  phase: string
  storageClass?: string
  requestedStorage?: string
  events: K8sEvent[]
}

export interface EvidencePayload {
  rootResource?: { kind: string; name: string; namespace?: string }
  rootResourceEvents?: K8sEvent[]
  workloadPodLogs?: PodLog[]
  pvcInfos?: PVCInfo[]
}

export interface Evidence {
  id: string
  diagnosticRunId: string
  incidentId: string
  collectedAt: string
  collectorVersion: string
  payload: EvidencePayload
  createdAt: string
}

export interface AnalyzeResponse {
  diagnosticRunId: string
  analysisRequestId: string
}

export interface ManualIncidentResponse {
  incidentId: string
  diagnosticRunId: string
  analysisRequestId: string
}

export interface ResourceItem {
  name: string
  namespace: string
}

export interface KindItem {
  kind: string
  apiGroup: string
}

export interface ExclusionRule {
  name: string
  spec: {
    description: string
    enabled: boolean
    detectors?: string[]
    targetResources?: Array<{ kind: string; name: string; namespace: string; apiGroup?: string }>
    selector?: { namespace?: string; kinds?: string[]; matchLabels?: Record<string, string> }
    timeWindow?: {
      timezone: string
      periods: Array<{ start: string; end: string; days: string[] }>
    }
  }
  status: {
    suppressedCount?: number
    lastMatchedAt?: string
  }
}

export interface SuggestedResource {
  kind: string
  apiGroup?: string
  reason: string
}

export interface ExclusionRuleProposal {
  reason: string
  detectors: string[]
  targetResources: Array<{ namespace: string; kind: string; name: string; apiGroup?: string }>
  timeWindow?: {
    timezone: string
    periods: Array<{ start: string; end: string; days: string[] }>
  } | null
}

export interface AnalysisResult {
  id: string
  incidentId: string
  diagnosticRunId: string
  model: string
  status: string
  likelyRootCause: string
  confidence: number
  userRating?: 'up' | 'down' | ''
  payload: {
    openingRant?: string
    likelyRootCause?: string
    evidenceChain?: string
    recommendation?: string
    closingInsult?: string
    confidence?: number
    model?: string
    thinkingBudgetUsed?: number
    needsMoreInfo?: boolean
    suggestedResources?: SuggestedResource[]
    suggestFalsePositive?: boolean
    suggestExclusionRule?: ExclusionRuleProposal | null
  }
  createdAt: string
}

async function apiFetch<T>(path: string, opts?: RequestInit): Promise<T> {
  const token = getToken()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) headers['Authorization'] = `Bearer ${token}`
  if (opts?.headers) {
    Object.assign(headers, opts.headers)
  }
  const res = await fetch(BASE + path, { ...opts, headers })
  if (res.status === 401) {
    clearToken()
    throw new Error('Unauthorized')
  }
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
    apiFetch<DiagnosticRunDetail>(`/api/v1/diagnosticruns/${id}`),

  listDiagnosticRuns: (incidentId?: string) =>
    apiFetch<DiagnosticRunSummary[]>(
      incidentId ? `/api/v1/diagnosticruns?incidentId=${encodeURIComponent(incidentId)}` : '/api/v1/diagnosticruns'
    ),

  getDiagnosticRunEvidence: (runId: string) =>
    apiFetch<Evidence>(`/api/v1/diagnosticruns/${encodeURIComponent(runId)}/evidence`),

  getDiagnosticRunAnalysisResult: (runId: string) =>
    apiFetch<AnalysisResult>(`/api/v1/diagnosticruns/${encodeURIComponent(runId)}/analysisresult`),

  deleteDiagnosticRun: (runId: string) =>
    apiFetch<void>(`/api/v1/diagnosticruns/${encodeURIComponent(runId)}`, { method: 'DELETE' }),

  bulkDeleteDiagnosticRuns: (ids: string[]) =>
    apiFetch<{ deleted: number }>('/api/v1/diagnosticruns', {
      method: 'DELETE',
      body: JSON.stringify({ ids }),
    }),

  getAnalysisResult: (id: string) =>
    apiFetch<AnalysisResult>(`/api/v1/analysisresults/${id}`),

  rateAnalysisResult: (id: string, rating: 'up' | 'down') =>
    apiFetch<{ id: string; userRating: string }>(`/api/v1/analysisresults/${id}/rate`, {
      method: 'POST',
      body: JSON.stringify({ rating }),
    }),

  getKubeChanState: () =>
    apiFetch<{ moodLevel: number; pokeCount: number }>('/api/v1/kubechan/state'),

  poke: () =>
    apiFetch<{ moodLevel: number; pokeCount: number }>('/api/v1/kubechan/poke', { method: 'POST' }),

  listNamespaces: () =>
    apiFetch<string[]>('/api/v1/namespaces'),

  listResources: (ns: string, kind: string, apiGroup?: string) =>
    apiFetch<ResourceItem[]>(
      `/api/v1/namespaces/${encodeURIComponent(ns)}/resources?kind=${encodeURIComponent(kind)}${apiGroup ? `&apiGroup=${encodeURIComponent(apiGroup)}` : ''}`
    ),

  listKinds: (ns: string, q?: string) =>
    apiFetch<KindItem[]>(
      `/api/v1/namespaces/${encodeURIComponent(ns)}/kinds${q ? `?q=${encodeURIComponent(q)}` : ''}`
    ),

  createManualIncident: (body: {
    namespace: string
    resourceKind: string
    resourceName: string
    userMessage: string
    relatedResources: { kind: string; name: string; namespace: string; apiGroup?: string; evidenceSlices?: string[] }[]
  }) =>
    apiFetch<ManualIncidentResponse>('/api/v1/incidents/manual', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  augmentIncident: (incidentId: string, relatedResources: { kind: string; name: string; namespace: string; apiGroup?: string; evidenceSlices?: string[] }[]) =>
    apiFetch<AnalyzeResponse>(`/api/v1/incidents/${encodeURIComponent(incidentId)}/augment`, {
      method: 'POST',
      body: JSON.stringify({ relatedResources }),
    }),

  resolveIncident: (id: string) =>
    apiFetch<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}/resolve`, { method: 'POST' }),

  markFalsePositive: (id: string) =>
    apiFetch<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}/false-positive`, { method: 'POST' }),

  // ── Auth ────────────────────────────────────────────────────────────────────
  login: (username: string, password: string) =>
    apiFetch<{ token: string; role: string; username: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  me: () =>
    apiFetch<CurrentUser>('/api/v1/auth/me'),

  // ── User management (admin only) ────────────────────────────────────────────
  listUsers: () =>
    apiFetch<{ id: string; username: string; role: string; createdAt: string }[]>('/api/v1/users'),

  createUser: (username: string, password: string, role: 'admin' | 'viewer') =>
    apiFetch<{ id: string; username: string; role: string }>('/api/v1/users', {
      method: 'POST',
      body: JSON.stringify({ username, password, role }),
    }),

  deleteUser: (id: string) =>
    apiFetch<void>(`/api/v1/users/${id}`, { method: 'DELETE' }),

  // ── Per-user LLM settings ────────────────────────────────────────────────────
  getLLMSettings: () =>
    apiFetch<{ provider: string; configured: boolean; credFields: Record<string, unknown> }>('/api/v1/me/llm-settings'),

  saveLLMSettings: (provider: string, credentials: Record<string, string>) =>
    apiFetch<{ status: string }>('/api/v1/me/llm-settings', {
      method: 'PUT',
      body: JSON.stringify({ provider, credentials }),
    }),

  getLLMModels: () =>
    apiFetch<{ providers: Record<string, { id: string; label: string }[]> }>('/api/v1/llm-models'),

  // ── Admin system settings ──────────────────────────────────────────────────
  getAdminSettings: () =>
    apiFetch<Record<string, unknown>>('/api/v1/settings'),

  updateAdminSettings: (patch: Record<string, unknown>) =>
    apiFetch<{ status: string }>('/api/v1/settings', {
      method: 'PUT',
      body: JSON.stringify(patch),
    }),

  // ── Exclusion Rules ───────────────────────────────────────────────────────
  listExclusionRules: () =>
    apiFetch<ExclusionRule[]>('/api/v1/exclusion-rules'),

  createExclusionRule: (name: string, spec: ExclusionRule['spec']) =>
    apiFetch<ExclusionRule>('/api/v1/exclusion-rules', {
      method: 'POST',
      body: JSON.stringify({ name, spec }),
    }),

  setExclusionRuleEnabled: (name: string, enabled: boolean) =>
    apiFetch<ExclusionRule>(`/api/v1/exclusion-rules/${encodeURIComponent(name)}`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled }),
    }),

  deleteExclusionRule: (name: string) =>
    apiFetch<void>(`/api/v1/exclusion-rules/${encodeURIComponent(name)}`, { method: 'DELETE' }),
}
