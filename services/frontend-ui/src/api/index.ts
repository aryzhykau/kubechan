// ── Token management ──────────────────────────────────────────────────────────

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

// ── Domain types ───────────────────────────────────────────────────────────────

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

export interface User {
  id: string
  username: string
  role: string
  createdAt: string
}
