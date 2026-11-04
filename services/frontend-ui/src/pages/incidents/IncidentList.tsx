import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box, Typography, Button, IconButton, Alert, Stack,
  CircularProgress, Skeleton, Tooltip,
  TextField, InputAdornment, FormControl, Select, MenuItem,
  Checkbox,
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ShieldOutlinedIcon from '@mui/icons-material/ShieldOutlined'
import SearchIcon from '@mui/icons-material/Search'
import CloseIcon from '@mui/icons-material/Close'
import {
  useListIncidentsQuery,
  useListProblemCasesQuery,
  useAnalyzeMutation,
  useResolveIncidentMutation,
  useMarkFalsePositiveMutation,
} from '../../store/api/incidentsApi'
import { useGetDiagnosticRunEvidenceQuery, useListDiagnosticRunsQuery } from '../../store/api/diagnosticsApi'
import { useAppDispatch } from '../../store/hooks'
import { useKubeChan } from '../../hooks/useKubeChan'
import { setExclusionProposal } from '../../store/slices/uiSlice'
import { ResourcePill } from '../../components/shared/ResourcePill'
import { timeAgo } from '../../components/shared/utils'
import { AugmentIncidentModal } from './AugmentIncidentModal'
import { waitForAnalysis } from '../../analysis/tracker'
import type { Incident, ProblemCase, DiagnosticRunSummary, ExclusionRuleProposal, AnalysisResult } from '../../api/index'

// ── Helpers ───────────────────────────────────────────────────────────────────

function incidentLabel(incident: Incident): { title: string; sub: string } {
  const { rootResource } = incident.spec
  const ns = rootResource.namespace || incident.metadata.namespace || 'default'
  const affected = incident.status.activeProblemCases ?? 0
  const title = `${rootResource.kind} ${rootResource.name} in ${ns}`
  const sub = affected > 0
    ? `${affected} active symptom${affected !== 1 ? 's' : ''}`
    : incident.spec.source === 'manual' && incident.spec.userMessage
      ? incident.spec.userMessage.slice(0, 80) + (incident.spec.userMessage.length > 80 ? '…' : '')
      : ''
  return { title, sub }
}

// ── IncidentDetails accordion ─────────────────────────────────────────────────

function IncidentDetails({ incident, problemCases }: {
  incident: Incident
  problemCases: ProblemCase[]
}) {
  const isManual = incident.spec.source === 'manual'
  const related = incident.spec.relatedResources ?? []
  const pcs = incident.spec.problemCases ?? []
  const pcByName = new Map(problemCases.map(pc => [pc.metadata.name, pc]))
  const incidentPCs = pcs.map(name => pcByName.get(name)).filter((pc): pc is ProblemCase => !!pc)
  const activePCs = incidentPCs.filter(pc => pc.status.state !== 'resolved')
  const previousPCs = incidentPCs.filter(pc => pc.status.state === 'resolved')

  function isRootResource(pc: ProblemCase) {
    const affected = pc.spec.affectedResource
    const root = incident.spec.rootResource
    return affected.kind.toLowerCase() === root.kind.toLowerCase()
      && affected.name === root.name
      && (affected.namespace || incident.metadata.namespace) === (root.namespace || incident.metadata.namespace)
  }

  function renderProblemCase(pc: ProblemCase) {
    const correlated = !isRootResource(pc)
    const severity = pc.spec.severity.toLowerCase()
    const signalColor = severity === 'critical' ? '#d96b68' : severity === 'high' ? '#d9975f' : '#d5a64a'
    const resource = pc.spec.affectedResource
    return (
      <Box
        key={pc.metadata.name}
        sx={{
          px: 0.85,
          py: 0.65,
          borderLeft: '2px solid',
          borderLeftColor: signalColor,
          borderBottom: '1px solid #303832',
          background: '#141815',
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.5 }}>
          <Typography sx={{ fontSize: '0.7rem', lineHeight: 1.25, fontWeight: 700, minWidth: 0, overflowWrap: 'anywhere' }}>{pc.spec.detector.replace(/([a-z])([A-Z])/g, '$1 $2')}</Typography>
          <Typography sx={{ ml: 'auto', flexShrink: 0, fontSize: '0.56rem', fontWeight: 750, textTransform: 'uppercase', color: signalColor }}>{pc.spec.severity}</Typography>
        </Box>
        <Typography sx={{ mt: 0.2, fontSize: '0.63rem', lineHeight: 1.3, color: 'text.secondary', overflowWrap: 'anywhere' }}>
          {resource.kind}/{resource.name}{resource.namespace ? ` · ${resource.namespace}` : ''}{correlated ? ' · related' : ''} · {timeAgo(pc.status.lastSeen ?? pc.status.firstSeen)}
        </Typography>
        {(pc.spec.symptoms?.length ? pc.spec.symptoms : ['No symptom description recorded']).map((symptom, index) => (
          <Typography key={`${pc.metadata.name}-symptom-${index}`} sx={{ mt: 0.3, fontSize: '0.65rem', lineHeight: 1.35, color: 'text.primary', overflowWrap: 'anywhere' }}>{symptom}</Typography>
        ))}
      </Box>
    )
  }

  return (
    <Box>
        <Stack spacing={1}>
          {isManual && incident.spec.userMessage && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>User description</Typography>
              <Typography variant="body2" sx={{ color: 'text.primary', fontSize: '0.76rem', lineHeight: 1.4 }}>
                {incident.spec.userMessage}
              </Typography>
            </Box>
          )}

          {isManual && related.length > 0 && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Related resources</Typography>
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                {related.map((r, i) => (
                  <ResourcePill key={`${r.kind}-${r.name}-${i}`} kind={r.kind} name={r.name} namespace={r.namespace} />
                ))}
              </Box>
            </Box>
          )}

          {!isManual && activePCs.length > 0 && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, fontSize: '0.68rem', fontWeight: 700 }}>Active signals <Box component="span" sx={{ color: 'text.disabled', fontWeight: 500 }}>· {activePCs.length}</Box></Typography>
              <Stack spacing={0.5}>{activePCs.map(renderProblemCase)}</Stack>
            </Box>
          )}

          {!isManual && previousPCs.length > 0 && (
            <Typography variant="caption" color="text.disabled">{previousPCs.length} previous signals</Typography>
          )}
        </Stack>
    </Box>
  )
}

function IncidentAnalysis({ previousRun, onRate }: {
  previousRun: DiagnosticRunSummary
  onRate?: (runId: string, rating: 'up' | 'down', confidence: number) => void
}) {
  const [analysisResult, setAnalysisResult] = useState<AnalysisResult | null>(null)
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const [localRating, setLocalRating] = useState<'up' | 'down' | null>(null)
  const [evidenceOpen, setEvidenceOpen] = useState(false)
  const [recommendationOpen, setRecommendationOpen] = useState(true)
  const { data: evidence } = useGetDiagnosticRunEvidenceQuery(previousRun.diagnosticRunId)

  async function fetchAnalysis() {
    if (analysisResult || analysisLoading) return
    setAnalysisLoading(true)
    try {
      const { getToken } = await import('../../api/index')
      const token = getToken()
      const headers: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {}
      const res = await fetch(
        `/api/v1/diagnosticruns/${encodeURIComponent(previousRun.diagnosticRunId)}/analysisresult`,
        { headers },
      )
      if (res.ok) setAnalysisResult(await res.json() as AnalysisResult)
    } catch { /* ignore */ } finally {
      setAnalysisLoading(false)
    }
  }

  useEffect(() => {
    void fetchAnalysis()
    // The diagnostic run id is the identity of this analysis panel.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [previousRun.diagnosticRunId])

  function handleRate(rating: 'up' | 'down') {
    if (!analysisResult?.id || localRating || analysisResult.userRating) return
    setLocalRating(rating)
    const confidence = analysisResult.confidence ?? analysisResult.payload?.confidence ?? 0
    onRate?.(analysisResult.id, rating, confidence)
  }

  const effectiveRating = localRating ?? analysisResult?.userRating
  const confidence = analysisResult
    ? (analysisResult.confidence ?? analysisResult.payload?.confidence ?? 0)
    : (previousRun.confidence ?? 0)
  const pct = Math.round(confidence * 100)
  const confColor = confidence >= 0.8 ? '#4ade80' : confidence >= 0.5 ? '#fbbf24' : '#f87171'
  const assessment = analysisResult?.payload?.assessment
  const sourcesById = new Map((evidence?.payload.sources ?? []).map(source => [source.id, source]))

  return (
    <Box sx={{ mt: 1.25, border: '1px solid', borderColor: 'rgba(213,166,74,0.28)', background: 'rgba(213,166,74,0.025)' }}>
      <Box sx={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 1.5, alignItems: 'start', px: 1.5, py: 1.25, borderBottom: '1px solid', borderColor: 'rgba(213,166,74,0.18)' }}>
        <Box>
          <Typography sx={{ fontSize: '0.62rem', fontWeight: 800, letterSpacing: '0.1em', textTransform: 'uppercase', color: 'primary.main' }}>
            KubeChan assessment
          </Typography>
          <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.25 }}>
            Automated diagnosis · {timeAgo(previousRun.analysisCreatedAt ?? previousRun.requestedAt)}
          </Typography>
        </Box>
        <Box sx={{ minWidth: 88, textAlign: 'right' }}>
          <Typography sx={{ color: confColor, fontSize: '1.05rem', fontWeight: 750, lineHeight: 1 }}>{pct}%</Typography>
          <Typography variant="caption" color="text.disabled">confidence</Typography>
          <Box sx={{ mt: 0.55, ml: 'auto', width: 72, height: 3, background: 'rgba(255,255,255,0.08)' }}>
            <Box sx={{ width: `${pct}%`, height: '100%', background: confColor }} />
          </Box>
        </Box>
      </Box>

      {analysisLoading && (
        <Stack spacing={0.75} sx={{ p: 1.5 }}>
          <Skeleton variant="text" width="32%" />
          <Skeleton variant="text" width="92%" />
          <Skeleton variant="text" width="74%" />
        </Stack>
      )}

      {!analysisLoading && analysisResult && (
        <>
          {assessment && (
            <>
              <Box sx={{ px: 1.5, py: 1.5 }}>
                <Typography sx={{ fontSize: '0.62rem', color: 'text.disabled', fontWeight: 750, letterSpacing: '0.08em', textTransform: 'uppercase', mb: 0.6 }}>Diagnosis</Typography>
                <Typography sx={{ fontSize: '0.84rem', lineHeight: 1.55, fontWeight: 600 }}>{assessment.summary}</Typography>
              </Box>
              <Box sx={{ borderTop: '1px solid', borderColor: 'divider' }}>
                {assessment.claims.map(claim => (
                  <Box key={claim.id} sx={{ px: 1.5, py: 1.25, borderBottom: '1px solid', borderColor: 'divider' }}>
                    <Typography sx={{ fontSize: '0.58rem', fontWeight: 800, letterSpacing: '0.08em', textTransform: 'uppercase', color: claim.verdict === 'supported' ? 'success.main' : claim.verdict === 'partially_supported' ? 'warning.main' : 'error.main' }}>{claim.verdict.replace('_', ' ')}</Typography>
                    <Typography variant="body2" sx={{ mt: 0.35, fontSize: '0.8rem', fontWeight: 550 }}>{claim.statement}</Typography>
                    {claim.citations.map(citation => (
                      <Box key={citation.sourceId} sx={{ mt: 0.75, pl: 1, borderLeft: '2px solid', borderColor: 'primary.dark' }}>
                        {(() => {
                          const source = sourcesById.get(citation.sourceId)
                          if (!source) return <Typography sx={{ fontFamily: 'monospace', fontSize: '0.65rem', color: 'error.main' }}>Unavailable source: {citation.sourceId}</Typography>
                          const observed = source.line?.text ?? source.event?.message ?? (typeof source.value === 'string' ? source.value : JSON.stringify(source.value))
                          return (
                            <>
                              <Typography sx={{ fontSize: '0.68rem', color: 'primary.light', fontWeight: 700 }}>
                                {source.resource.kind} · {source.resource.namespace}/{source.resource.name}
                              </Typography>
                              {source.path && <Typography sx={{ fontFamily: 'monospace', fontSize: '0.62rem', color: 'text.disabled' }}>{source.path}</Typography>}
                              {observed && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.2, whiteSpace: 'pre-wrap' }}>{observed}</Typography>}
                              {(source.truncated || source.redacted) && <Typography variant="caption" color="warning.main">{source.truncated ? 'truncated ' : ''}{source.redacted ? 'redacted' : ''}</Typography>}
                            </>
                          )
                        })()}
                        {citation.quote && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.2 }}>“{citation.quote}”</Typography>}
                        {citation.relevance && <Typography variant="caption" color="text.disabled" sx={{ display: 'block' }}>{citation.relevance}</Typography>}
                      </Box>
                    ))}
                  </Box>
                ))}
              </Box>
              {assessment.remediation.length > 0 && (
                <Box sx={{ px: 1.5, py: 1.25, borderTop: '1px solid', borderColor: 'divider' }}>
                  <Typography sx={{ fontSize: '0.62rem', color: 'text.disabled', fontWeight: 750, letterSpacing: '0.08em', textTransform: 'uppercase', mb: 1 }}>Remediation runbook</Typography>
                  {assessment.remediation.sort((a, b) => a.order - b.order).map(step => (
                    <Box key={`${step.order}-${step.title}`} sx={{ display: 'grid', gridTemplateColumns: '22px 1fr', gap: 1, mb: 1.25 }}>
                      <Typography sx={{ color: 'primary.main', fontWeight: 800 }}>{step.order}</Typography>
                      <Box>
                        <Typography variant="body2" sx={{ fontSize: '0.8rem', fontWeight: 650 }}>{step.title}</Typography>
                        {step.description && <Typography variant="caption" color="text.secondary">{step.description}</Typography>}
                        {step.command && <Box component="code" sx={{ display: 'block', mt: 0.6, p: 0.75, overflowX: 'auto', background: '#101311', border: '1px solid', borderColor: 'divider', fontSize: '0.68rem', color: '#c9d2cb' }}>{step.command}</Box>}
                        {step.verification && <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.5 }}>Verify: {step.verification}</Typography>}
                      </Box>
                    </Box>
                  ))}
                </Box>
              )}
              {analysisResult.payload.citationValidation && !analysisResult.payload.citationValidation.valid && (
                <Alert severity="warning" sx={{ m: 1.5 }}>Some model citations were rejected because their source IDs were not present in the evidence snapshot.</Alert>
              )}
            </>
          )}

          {!assessment && (
            <>
          <Box sx={{ px: 1.5, py: 1.5 }}>
            <Typography sx={{ fontSize: '0.62rem', color: 'text.disabled', fontWeight: 750, letterSpacing: '0.08em', textTransform: 'uppercase', mb: 0.6 }}>
              Likely root cause
            </Typography>
            <Typography sx={{ fontSize: '0.84rem', lineHeight: 1.55, fontWeight: 550 }}>
              {analysisResult.likelyRootCause || analysisResult.payload?.likelyRootCause || 'No root cause was identified.'}
            </Typography>
          </Box>

          {analysisResult.payload?.recommendation && (
            <Box sx={{ borderTop: '1px solid', borderColor: 'divider' }}>
              <Button
                fullWidth
                onClick={() => setRecommendationOpen(open => !open)}
                endIcon={<ExpandMoreIcon sx={{ transform: recommendationOpen ? 'rotate(180deg)' : 'none', transition: 'transform 120ms ease' }} />}
                sx={{ justifyContent: 'space-between', px: 1.5, py: 1, color: 'text.primary', fontSize: '0.72rem', letterSpacing: '0.05em', textTransform: 'uppercase', borderRadius: 0 }}
              >
                Recommended action
              </Button>
              {recommendationOpen && (
                <Box sx={{ mx: 1.5, mb: 1.5, pl: 1.25, borderLeft: '2px solid', borderColor: 'primary.main' }}>
                  <Typography variant="body2" sx={{ fontSize: '0.8rem', lineHeight: 1.6, whiteSpace: 'pre-wrap' }}>
                    {analysisResult.payload.recommendation}
                  </Typography>
                </Box>
              )}
            </Box>
          )}

          {analysisResult.payload?.evidenceChain && (
            <Box sx={{ borderTop: '1px solid', borderColor: 'divider' }}>
              <Button
                fullWidth
                onClick={() => setEvidenceOpen(open => !open)}
                endIcon={<ExpandMoreIcon sx={{ transform: evidenceOpen ? 'rotate(180deg)' : 'none', transition: 'transform 120ms ease' }} />}
                sx={{ justifyContent: 'space-between', px: 1.5, py: 1, color: 'text.secondary', fontSize: '0.72rem', letterSpacing: '0.05em', textTransform: 'uppercase', borderRadius: 0 }}
              >
                Evidence chain
              </Button>
              {evidenceOpen && (
                <Typography variant="body2" sx={{ px: 1.5, pb: 1.5, fontSize: '0.78rem', lineHeight: 1.6, color: 'text.secondary', whiteSpace: 'pre-wrap' }}>
                  {analysisResult.payload.evidenceChain}
                </Typography>
              )}
            </Box>
          )}
            </>
          )}

          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 1.5, py: 1, borderTop: '1px solid', borderColor: 'divider', background: 'rgba(0,0,0,0.12)' }}>
            <Typography variant="caption" color="text.disabled" sx={{ fontFamily: 'monospace', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {analysisResult.model}
            </Typography>
            {analysisResult.payload.tokenUsage && (
              <Tooltip title={`Input: ${analysisResult.payload.tokenUsage.inputTokens.toLocaleString()} (${analysisResult.payload.tokenUsage.inputSource === 'provider' ? 'provider reported' : 'estimated'}) · Output: ${analysisResult.payload.tokenUsage.outputTokens.toLocaleString()} (${analysisResult.payload.tokenUsage.outputSource === 'provider' ? 'provider reported' : 'estimated'})`}>
                <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'nowrap' }}>
                  {analysisResult.payload.tokenUsage.totalTokens.toLocaleString()} tokens
                  {analysisResult.payload.tokenUsage.inputSource === 'tokenizer_estimate' || analysisResult.payload.tokenUsage.outputSource === 'tokenizer_estimate' ? ' est.' : ''}
                </Typography>
              </Tooltip>
            )}
            {onRate && (
              <>
                <Typography variant="caption" color="text.secondary">{effectiveRating ? 'Feedback recorded' : 'Useful?'}</Typography>
                <Button size="small" variant={effectiveRating === 'up' ? 'contained' : 'text'} color="success" disabled={!!effectiveRating} onClick={() => handleRate('up')} sx={{ minWidth: 0, px: 0.75 }}>Yes</Button>
                <Button size="small" variant={effectiveRating === 'down' ? 'contained' : 'text'} color="error" disabled={!!effectiveRating} onClick={() => handleRate('down')} sx={{ minWidth: 0, px: 0.75 }}>No</Button>
              </>
            )}
          </Box>
        </>
      )}
    </Box>
  )
}

// ── IncidentCard ──────────────────────────────────────────────────────────────

interface IncidentCardProps {
  incident: Incident
  previousRun?: DiagnosticRunSummary
  analysisSummary?: string
  problemCases: ProblemCase[]
  onAction?: () => void
  onResolved?: () => void
  onSuggestRule?: (p: ExclusionRuleProposal) => void
  onMarkFalsePositive?: () => void
  onRefresh: () => void
}

function IncidentCard({
  incident, previousRun, analysisSummary, problemCases,
  onResolved, onSuggestRule, onMarkFalsePositive, onRefresh,
  onRegisterAugment,
}: IncidentCardProps & { onRegisterAugment?: (open: () => void) => void }) {
  void IncidentAnalysis
  const [resolveApi] = useResolveIncidentMutation()
  const [fpApi] = useMarkFalsePositiveMutation()
  const { handleAnalysisStart, handleAnalysisComplete, handleAnalysisFailed } = useKubeChan()

  const [isAnalyzing, setIsAnalyzing] = useState(false)
  const [resolveStep, setResolveStep] = useState<'idle' | 'confirm'>('idle')
  const [fpStep, setFpStep] = useState<'idle' | 'confirm'>('idle')
  const [augmentOpen, setAugmentOpen] = useState(false)

  const id = incident.metadata.name
  const isResolved = incident.status.state === 'resolved'
  const isManual = incident.spec.source === 'manual'
  const alreadyAnalyzed = !!previousRun?.analysisResultId
  const needsMoreInfo = alreadyAnalyzed && !!previousRun?.needsMoreInfo
  const { title } = incidentLabel(incident)
  const rootNS = incident.spec.rootResource.namespace || incident.metadata.namespace || ''

  useEffect(() => { onRegisterAugment?.(() => setAugmentOpen(true)) }, [onRegisterAugment])

  async function fetchAnalysisResult(runId: string): Promise<AnalysisResult | null> {
    const { getToken } = await import('../../api/index')
    const token = getToken()
    const headers: Record<string, string> = {}
    if (token) headers['Authorization'] = `Bearer ${token}`
    try {
      const res = await fetch(`/api/v1/diagnosticruns/${encodeURIComponent(runId)}/analysisresult`, { headers })
      if (res.ok) return await res.json() as AnalysisResult
    } catch { /* ignore */ }
    return null
  }

  async function handleResolve() {
    setResolveStep('idle')
    onResolved?.()
    await resolveApi(id)
    onRefresh()
  }

  async function handleFP() {
    setFpStep('idle')
    onMarkFalsePositive?.()
    onResolved?.()
    await fpApi(id)
    onRefresh()
  }

  function handleAugmented(runId: string) {
    setAugmentOpen(false)
    setIsAnalyzing(true)
    handleAnalysisStart(title)
    waitForAnalysis(runId).then(ok => {
      if (ok) {
        fetchAnalysisResult(runId).then(result => {
          if (result) handleAnalysisComplete(result, incident.metadata.name)
          onRefresh()
        })
      } else {
        handleAnalysisFailed(title)
        onRefresh()
      }
    }).finally(() => setIsAnalyzing(false))
  }

  return (
    <Box
      sx={{
        background: '#171b18',
        p: { xs: 1.1, md: 1.5 },
      }}
    >
      <Typography component="h2" sx={{ fontWeight: 650, fontSize: '1rem', lineHeight: 1.35, mb: 0.3, color: isResolved ? 'text.secondary' : 'text.primary', letterSpacing: '-0.01em' }}>
        {incident.spec.rootResource.name}
      </Typography>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontSize: '0.66rem' }}>
        {incident.spec.rootResource.kind} · {rootNS} · {isManual ? `manual${incident.ownerUsername ? ` by ${incident.ownerUsername}` : ''}` : 'auto-detected'}
      </Typography>

      <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr', mt: 1, border: '1px solid #343c35', background: '#141815' }}>
        {[
          ['Active symptoms', `${incident.status.activeProblemCases ?? 0}`],
          ['Latest event', timeAgo(incident.status.lastOccurrenceAt ?? incident.status.openedAt)],
          ['First detected', timeAgo(incident.status.openedAt)],
          ['Occurrences', `${incident.status.occurrenceCount ?? 1}`],
        ].map(([label, value], index) => <Box key={label} sx={{ px: 0.9, py: 0.7, borderLeft: index % 2 ? '1px solid #343c35' : 0, borderTop: index > 1 ? '1px solid #343c35' : 0 }}>
          <Typography sx={{ fontSize: '0.54rem', lineHeight: 1.2, color: 'text.disabled', textTransform: 'uppercase', letterSpacing: '.08em' }}>{label}</Typography>
          <Typography sx={{ mt: 0.2, fontSize: '0.72rem', lineHeight: 1.25, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}>{value}</Typography>
        </Box>)}
      </Box>

      <Box component="section" sx={{ mt: 1.1, pb: 1.1, borderBottom: '1px solid #3b443c' }}>
        <IncidentDetails incident={incident} problemCases={problemCases} />
      </Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', mt: 1.1, mb: 0.65 }}>
        <Typography sx={{ fontSize: '0.68rem', fontWeight: 700 }}>Diagnostic analysis</Typography>
        <Typography variant="caption" color="text.disabled" sx={{ fontSize: '0.6rem' }}>{previousRun ? `run · ${timeAgo(previousRun.analysisCreatedAt ?? previousRun.requestedAt)}` : 'not collected'}</Typography>
      </Box>
      {previousRun?.analysisResultId && (
        <Box sx={{ p: 1, border: '1px solid', borderColor: 'divider', background: 'rgba(255,255,255,0.015)' }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 0.35 }}>
            <Typography sx={{ fontSize: '0.62rem', fontWeight: 800, letterSpacing: '.08em', textTransform: 'uppercase', color: previousRun.needsMoreInfo ? 'warning.main' : 'success.main' }}>
              {previousRun.needsMoreInfo ? 'More context needed' : 'Analysis complete'}
            </Typography>
            <Typography sx={{ ml: 'auto', fontSize: '0.78rem', fontWeight: 750, color: (previousRun.confidence ?? 0) >= .8 ? 'success.main' : (previousRun.confidence ?? 0) >= .5 ? 'warning.main' : 'error.main' }}>
              {Math.round((previousRun.confidence ?? 0) * 100)}%
            </Typography>
          </Box>
          <Typography variant="body2" sx={{ fontSize: '0.72rem', lineHeight: 1.4 }}>{analysisSummary || previousRun.likelyRootCause || 'Analysis finished without a summary.'}</Typography>
          {needsMoreInfo && !!previousRun.suggestedResources?.length && <Box sx={{ mt: 0.75, pt: 0.75, borderTop: '1px solid', borderColor: 'divider' }}>
            <Typography sx={{ fontSize: '0.6rem', color: 'warning.main', textTransform: 'uppercase', letterSpacing: '.07em' }}>Requested next</Typography>
            {previousRun.suggestedResources.slice(0, 3).map((suggestion, index) => <Typography key={`${suggestion.kind}-${index}`} variant="caption" sx={{ display: 'block', mt: 0.35 }}><strong>{suggestion.kind}</strong> · {suggestion.reason}</Typography>)}
            {previousRun.suggestedResources.some(suggestion => suggestion.kind.toLowerCase() === 'secret') && <Typography variant="caption" color="text.disabled">Secret values are never collected.</Typography>}
          </Box>}
        </Box>
      )}
      {!previousRun && <Box sx={{ mt: 0.75, py: 0.75, borderTop: '1px solid', borderColor: 'divider' }}>
        <Typography sx={{ fontSize: '0.65rem', color: 'text.disabled', textTransform: 'uppercase', letterSpacing: '.08em' }}>Not analyzed</Typography>
        <Typography variant="caption" color="text.secondary">Evidence has not been collected yet.</Typography>
      </Box>}
      {previousRun && !previousRun.analysisResultId && <Box sx={{ mt: 0.75, py: 0.75, borderTop: '1px solid', borderColor: 'divider' }}>
        <Typography sx={{ fontSize: '0.65rem', fontWeight: 750, color: /fail|error/i.test(previousRun.status) ? 'error.main' : '#7fa8c9', textTransform: 'uppercase', letterSpacing: '.08em' }}>
          {/fail|error/i.test(previousRun.status) ? 'Analysis failed' : /collect/i.test(previousRun.status) ? 'Collecting evidence' : 'Analysis running'}
        </Typography>
        <Typography variant="caption" color="text.secondary">{timeAgo(previousRun.requestedAt)}</Typography>
      </Box>}

      {(isAnalyzing || (!isResolved && isManual)) && <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', mt: 1.5, pt: 1.5, borderTop: '1px solid', borderColor: 'divider' }}>
        {isAnalyzing && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <CircularProgress size={14} />
            <Typography variant="caption" color="warning.main">KubeChan is on it…</Typography>
          </Box>
        )}
        {!isResolved && isManual && resolveStep === 'idle' && !isAnalyzing && (
          <Button size="small" variant="outlined" color="success" onClick={() => setResolveStep('confirm')} sx={{ fontSize: '0.78rem' }}>
            Resolve
          </Button>
        )}
        {!isResolved && isManual && resolveStep === 'confirm' && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            <Typography variant="caption" color="text.secondary">Mark as resolved?</Typography>
            <Button size="small" variant="contained" color="success" onClick={handleResolve} sx={{ fontSize: '0.72rem' }}>Yes</Button>
            <Button size="small" variant="text" onClick={() => setResolveStep('idle')} sx={{ fontSize: '0.72rem' }}>Cancel</Button>
          </Box>
        )}
      </Box>}

      {/* Exclusion rule suggestion banner */}
      {previousRun?.suggestExclusionRule && onSuggestRule && (
        <Box sx={{ mt: 1.5, p: 1.5, background: 'rgba(213,166,74,0.06)', borderRadius: '4px', border: '1px solid rgba(213,166,74,0.2)', display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <ShieldOutlinedIcon sx={{ color: 'primary.main', fontSize: '1.1rem', flexShrink: 0 }} />
          <Box sx={{ flex: 1 }}>
            <Typography variant="caption" sx={{ fontWeight: 700, color: 'primary.light', display: 'block' }}>Expected behaviour — not a real incident</Typography>
            <Typography variant="caption" color="text.secondary">{previousRun.suggestExclusionRule.reason}</Typography>
          </Box>
          <Button size="small" variant="outlined" color="primary" onClick={() => onSuggestRule(previousRun!.suggestExclusionRule!)} sx={{ fontSize: '0.75rem', whiteSpace: 'nowrap' }}>
            Create Rule
          </Button>
        </Box>
      )}

      {/* False positive suggestion banner */}
      {previousRun?.suggestFalsePositive && isManual && !isResolved && (
        <Box sx={{ mt: 1.5, p: 1.5, background: 'rgba(100,116,139,0.1)', borderRadius: '8px', border: '1px solid rgba(100,116,139,0.2)', display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Typography sx={{ fontSize: '1rem', flexShrink: 0 }}>🤷</Typography>
          <Box sx={{ flex: 1 }}>
            <Typography variant="caption" sx={{ fontWeight: 700, display: 'block' }}>This looks like expected behaviour</Typography>
            <Typography variant="caption" color="text.secondary">KubeChan thinks this was never a real incident.</Typography>
          </Box>
          {fpStep === 'idle' && (
            <Button size="small" variant="outlined" onClick={() => setFpStep('confirm')} sx={{ fontSize: '0.75rem', whiteSpace: 'nowrap' }}>
              Mark as False Positive
            </Button>
          )}
          {fpStep === 'confirm' && (
            <Box sx={{ display: 'flex', gap: 0.75 }}>
              <Button size="small" variant="contained" color="error" onClick={handleFP} sx={{ fontSize: '0.72rem' }}>Yes</Button>
              <Button size="small" variant="text" onClick={() => setFpStep('idle')} sx={{ fontSize: '0.72rem' }}>Cancel</Button>
            </Box>
          )}
        </Box>
      )}

      {augmentOpen && (
        <AugmentIncidentModal
          incidentId={id}
          defaultNamespace={rootNS}
          suggestions={previousRun?.suggestedResources ?? []}
          onClose={() => setAugmentOpen(false)}
          onAugmented={handleAugmented}
        />
      )}
    </Box>
  )
}

// ── IncidentList page ─────────────────────────────────────────────────────────

type InboxView = 'active' | 'resolved' | 'all'
type IncidentSort = 'updated' | 'severity' | 'resource' | 'namespace'
type AnalysisFilter = 'any' | 'not_analyzed' | 'needs_context' | 'completed' | 'running' | 'failed'

const severityRank: Record<string, number> = { critical: 4, high: 3, medium: 2, low: 1 }
const toolbarSelectSx = {
  height: 30,
  fontSize: '0.68rem',
  color: 'text.secondary',
  '& .MuiSelect-select': { py: 0.5, pl: 0.75, pr: '19px !important', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  '& .MuiSelect-icon': { fontSize: '1rem' },
  '&:hover': { bgcolor: 'rgba(255,255,255,0.035)' },
  '&.Mui-focused': { outline: '2px solid #d5a64a', outlineOffset: 1 },
}

function incidentSeverity(incident: Incident, problemCases: ProblemCase[]): string {
  const names = new Set(incident.spec.problemCases ?? [])
  return problemCases
    .filter(pc => names.has(pc.metadata.name))
    .map(pc => pc.spec.severity.toLowerCase())
    .sort((a, b) => (severityRank[b] ?? 0) - (severityRank[a] ?? 0))[0] ?? 'unknown'
}

function activeProblemCases(incident: Incident, problemCases: ProblemCase[]): ProblemCase[] {
  const names = new Set(incident.spec.problemCases ?? [])
  return problemCases.filter(pc => names.has(pc.metadata.name) && pc.status.state !== 'resolved')
    .sort((a, b) => (severityRank[b.spec.severity.toLowerCase()] ?? 0) - (severityRank[a.spec.severity.toLowerCase()] ?? 0))
}

function IncidentInboxRow({ incident, severity, selected, expanded, checked, run, signal, symptoms, onClick, onCheck }: {
  incident: Incident
  severity: string
  selected: boolean
  expanded: boolean
  checked: boolean
  run?: DiagnosticRunSummary
  signal: string
  symptoms: string
  onClick: () => void
  onCheck: () => void
}) {
  const { rootResource } = incident.spec
  const isResolved = incident.status.state === 'resolved'
  const occurrences = incident.status.occurrenceCount ?? 1
  const namespace = rootResource.namespace ?? incident.metadata.namespace
  const severityColor: Record<string, string> = {
    critical: '#d96b68', high: '#d9975f', medium: '#d5a64a', low: '#7da399', unknown: '#687169',
  }
  const analysisState = isResolved ? 'Resolved' : run?.needsMoreInfo ? 'Needs context' : run?.analysisResultId ? 'Completed' : run ? run.status === 'failed' ? 'Failed' : run.status === 'collecting' ? 'Collecting evidence' : 'Analysis running' : 'Not analyzed'
  const analysisColor = isResolved ? '#7da399' : run?.needsMoreInfo ? '#d5a64a' : run?.analysisResultId ? '#6da985' : run && /fail|error/i.test(run.status) ? '#d96b68' : run ? '#7fa8c9' : '#7b837d'
  return (
    <Box
      component="div"
      role="button"
      data-incident-id={incident.metadata.name}
      tabIndex={0}
      onClick={onClick}
      onKeyDown={event => { if (event.target === event.currentTarget && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); onClick() } }}
      aria-pressed={selected}
      aria-expanded={expanded}
      aria-controls="incident-drawer"
      aria-label={`${rootResource.name}, ${severity} severity, ${isResolved ? 'resolved' : 'open'}, ${analysisState}${run?.confidence != null ? `, ${Math.round(run.confidence * 100)} percent confidence` : ''}`}
      sx={{
        width: '100%', textAlign: 'left', color: 'inherit', cursor: 'pointer',
        display: 'grid',
        gridTemplateColumns: { xs: '28px 4px minmax(105px, 1fr) minmax(70px, .6fr) 58px', sm: '28px 4px minmax(130px, 1.3fr) minmax(100px, .8fr) minmax(100px, .9fr) minmax(120px, 1fr) 62px' },
        alignItems: 'center', gap: 1.25, minHeight: 64, px: 0, py: 0,
        border: 0, borderBottom: '1px solid', borderColor: 'divider',
        background: selected ? 'rgba(213,166,74,0.075)' : 'transparent',
        boxShadow: selected ? 'inset 2px 0 0 #d5a64a' : 'none',
        '&:hover': { background: selected ? 'rgba(213,166,74,0.095)' : 'rgba(255,255,255,0.025)' },
        '&:focus-visible': { outline: '2px solid #d5a64a', outlineOffset: -2 },
      }}
    >
      <Checkbox
        size="small"
        checked={checked}
        onClick={event => event.stopPropagation()}
        onChange={onCheck}
        slotProps={{ input: { 'aria-label': `Select incident ${rootResource.name}` } }}
        sx={{ p: 0.5, ml: 0.25 }}
      />
      <Box sx={{ alignSelf: 'stretch', background: isResolved ? '#4b544d' : severityColor[severity] ?? severityColor.unknown }} />
      <Box sx={{ minWidth: 0, py: 1 }}>
        <Typography sx={{ fontWeight: 650, fontSize: '0.78rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {rootResource.name}
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.15, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {namespace} · {rootResource.kind}{incident.spec.source === 'manual' ? ' · manual' : ''}
        </Typography>
      </Box>
      <Box>
        <Typography sx={{ fontSize: '0.65rem', fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase', color: isResolved ? 'success.main' : severityColor[severity] }}>
          {isResolved ? 'Resolved' : severity}
        </Typography>
        <Typography variant="caption" color="text.disabled" title={signal} sx={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {signal || (incident.spec.source === 'manual' ? 'User reported' : `${occurrences} events`)}
        </Typography>
      </Box>
      <Box sx={{ minWidth: 0, display: { xs: 'none', sm: 'block' } }}>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>{incident.spec.source === 'manual' ? 'Manual' : `${incident.status.activeProblemCases ?? 0} active`}</Typography>
        <Typography variant="caption" color="text.disabled" title={symptoms} sx={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{symptoms || 'No symptom detail'}</Typography>
      </Box>
      <Box sx={{ display: { xs: 'none', sm: 'block' }, minWidth: 0 }}>
        <Typography variant="caption" sx={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: analysisColor, fontWeight: 650 }}>
          {analysisState}{run?.confidence != null && <Box component="span" sx={{ ml: 0.5, fontWeight: 750 }}>{Math.round(run.confidence * 100)}%</Box>}
        </Typography>
        <Typography variant="caption" color="text.disabled">{run ? timeAgo(run.analysisCreatedAt ?? run.requestedAt) : 'No diagnostic run'}</Typography>
      </Box>
      <Typography variant="caption" color="text.disabled" sx={{ textAlign: 'right', pr: 1.25 }}>
        {timeAgo(incident.status.lastOccurrenceAt ?? incident.status.resolvedAt ?? incident.status.openedAt)}
      </Typography>
    </Box>
  )
}

export interface IncidentListProps {
  onAction?: () => void
  onResolved?: () => void
  onMarkFalsePositive?: () => void
}

export function IncidentList({ onAction, onResolved, onMarkFalsePositive }: IncidentListProps = {}) {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const initialParams = new URLSearchParams(window.location.search)
  const initialView = initialParams.get('view')
  const initialSort = initialParams.get('sort')
  const initialAnalysis = initialParams.get('analysis')
  const [view, setView] = useState<InboxView>(initialView === 'resolved' || initialView === 'all' ? initialView : 'active')
  const [search, setSearch] = useState(initialParams.get('q') ?? '')
  const [namespace, setNamespace] = useState(initialParams.get('namespace') ?? 'all')
  const [severity, setSeverity] = useState(initialParams.get('severity') ?? 'all')
  const [sort, setSort] = useState<IncidentSort>(initialSort === 'severity' || initialSort === 'resource' || initialSort === 'namespace' ? initialSort : 'updated')
  const [analysisFilter, setAnalysisFilter] = useState<AnalysisFilter>(initialAnalysis === 'not_analyzed' || initialAnalysis === 'needs_context' || initialAnalysis === 'completed' || initialAnalysis === 'running' || initialAnalysis === 'failed' ? initialAnalysis : 'any')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [checkedIds, setCheckedIds] = useState<Set<string>>(new Set())
  const [bulkConfirm, setBulkConfirm] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [visibleCount, setVisibleCount] = useState(20)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [drawerIncidentId, setDrawerIncidentId] = useState<string | null>(null)
  const [switching, setSwitching] = useState(false)
  const switchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const switchToken = useRef(0)
  const tableRef = useRef<HTMLDivElement | null>(null)
  const controlsRef = useRef<HTMLDivElement | null>(null)
  const drawerBodyRef = useRef<HTMLDivElement | null>(null)
  const [tableHeight, setTableHeight] = useState(480)

  const { data: incidents = [], isLoading, isError, error, refetch } = useListIncidentsQuery()
  const { data: problemCases = [], refetch: refetchProblemCases } = useListProblemCasesQuery()
  const { data: runs = [], refetch: refetchRuns } = useListDiagnosticRunsQuery()
  const [analyzeIncident] = useAnalyzeMutation()
  const [resolveSelected] = useResolveIncidentMutation()

  useEffect(() => {
    const params = new URLSearchParams()
    if (view !== 'active') params.set('view', view)
    if (search.trim()) params.set('q', search.trim())
    if (namespace !== 'all') params.set('namespace', namespace)
    if (severity !== 'all') params.set('severity', severity)
    if (analysisFilter !== 'any') params.set('analysis', analysisFilter)
    if (sort !== 'updated') params.set('sort', sort)
    const query = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`)
  }, [view, search, namespace, severity, analysisFilter, sort])

  function refetchAll() { refetch(); refetchProblemCases(); refetchRuns() }

  useEffect(() => {
    const refresh = () => { refetch(); refetchProblemCases(); refetchRuns() }
    window.addEventListener('kubechan:refresh-incidents', refresh)
    return () => window.removeEventListener('kubechan:refresh-incidents', refresh)
  }, [refetch, refetchProblemCases, refetchRuns])

  // Build incidentId → latest run map
  const priorRuns = runs.reduce<Record<string, DiagnosticRunSummary>>((acc, run) => {
    if (!run.incidentId) return acc
    const existing = acc[run.incidentId]
    if (!existing || run.requestedAt > existing.requestedAt) acc[run.incidentId] = run
    return acc
  }, {})

  const open = (incidents as Incident[]).filter(i => i.status.state !== 'resolved')
    .sort((a, b) => (b.status.openedAt ?? '').localeCompare(a.status.openedAt ?? ''))
  const resolved = (incidents as Incident[]).filter(i => i.status.state === 'resolved')
    .sort((a, b) => (b.status.resolvedAt ?? '').localeCompare(a.status.resolvedAt ?? ''))
  const allIncidents = [...open, ...resolved]
  const namespaces = Array.from(new Set(allIncidents.map(i => i.spec.rootResource.namespace || i.metadata.namespace))).sort()
  const normalizedSearch = search.trim().toLowerCase()
  const filtered = allIncidents.filter(incident => {
    if (view === 'active' && incident.status.state === 'resolved') return false
    if (view === 'resolved' && incident.status.state !== 'resolved') return false
    const ns = incident.spec.rootResource.namespace || incident.metadata.namespace
    if (namespace !== 'all' && ns !== namespace) return false
    const currentSeverity = incidentSeverity(incident, problemCases)
    if (severity !== 'all' && currentSeverity !== severity) return false
    const latestRun = priorRuns[incident.metadata.name]
    if (analysisFilter === 'not_analyzed' && latestRun) return false
    if (analysisFilter === 'needs_context' && !latestRun?.needsMoreInfo) return false
    if (analysisFilter === 'completed' && (!latestRun?.analysisResultId || latestRun.needsMoreInfo)) return false
    if (analysisFilter === 'running' && (!latestRun || latestRun.analysisResultId || /fail|error/i.test(latestRun.status))) return false
    if (analysisFilter === 'failed' && (!latestRun || !/fail|error/i.test(latestRun.status))) return false
    if (normalizedSearch) {
      const haystack = [incident.metadata.name, incident.spec.rootResource.kind, incident.spec.rootResource.name, ns].join(' ').toLowerCase()
      if (!haystack.includes(normalizedSearch)) return false
    }
    return true
  }).sort((a, b) => {
    if (sort === 'severity') return (severityRank[incidentSeverity(b, problemCases)] ?? 0) - (severityRank[incidentSeverity(a, problemCases)] ?? 0)
    if (sort === 'resource') return a.spec.rootResource.name.localeCompare(b.spec.rootResource.name)
    if (sort === 'namespace') {
      const ans = a.spec.rootResource.namespace || a.metadata.namespace
      const bns = b.spec.rootResource.namespace || b.metadata.namespace
      return ans.localeCompare(bns)
    }
    const aTime = a.status.lastOccurrenceAt ?? a.status.resolvedAt ?? a.status.openedAt ?? ''
    const bTime = b.status.lastOccurrenceAt ?? b.status.resolvedAt ?? b.status.openedAt ?? ''
    return bTime.localeCompare(aTime)
  })
  const selected = filtered.find(i => i.metadata.name === selectedId) ?? filtered[0]
  const drawerIncident = allIncidents.find(i => i.metadata.name === drawerIncidentId)
  const drawerRun = drawerIncident ? priorRuns[drawerIncident.metadata.name] : undefined
  const drawerRunActive = !!drawerRun && !drawerRun.analysisResultId && !['completed', 'failed', 'error'].includes(drawerRun.status.toLowerCase())
  const { data: drawerEvidence, isLoading: evidenceLoading, isError: evidenceError } = useGetDiagnosticRunEvidenceQuery(drawerRun?.diagnosticRunId ?? '', { skip: !drawerOpen || !drawerRun?.analysisResultId })
  const [drawerAnalysis, setDrawerAnalysis] = useState<AnalysisResult | null>(null)
  const drawerNeedsMoreInfo = drawerAnalysis && drawerAnalysis.diagnosticRunId === drawerRun?.diagnosticRunId
    ? (drawerAnalysis.payload.assessment?.needsMoreInfo ?? drawerAnalysis.payload.needsMoreInfo ?? drawerRun?.needsMoreInfo ?? false)
    : (drawerRun?.needsMoreInfo ?? false)
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const [openAugment, setOpenAugment] = useState<(() => void) | null>(null)
  const registerAugment = useCallback((open: () => void) => { setOpenAugment(() => open) }, [])
  const [startingIds, setStartingIds] = useState<Set<string>>(new Set())
  const footerBusy = !!drawerIncident && startingIds.has(drawerIncident.metadata.name)
  const [footerError, setFooterError] = useState<string | null>(null)
  const { handleAnalysisStart, handleAnalysisComplete, handleAnalysisFailed } = useKubeChan()

  useEffect(() => {
    let current = true
    setDrawerAnalysis(null)
    if (drawerOpen && drawerRun?.analysisResultId) {
      setAnalysisLoading(true)
      import('../../api/index').then(({ getToken }) => {
        const token = getToken()
        const headers: Record<string, string> = {}
        if (token) headers.Authorization = `Bearer ${token}`
        return fetch(`/api/v1/diagnosticruns/${encodeURIComponent(drawerRun.diagnosticRunId)}/analysisresult`, { headers })
      }).then(async response => response.ok ? await response.json() as AnalysisResult : null)
        .then(result => { if (current) setDrawerAnalysis(result) })
        .catch(() => { if (current) setDrawerAnalysis(null) })
        .finally(() => { if (current) setAnalysisLoading(false) })
    } else setAnalysisLoading(false)
    return () => { current = false }
  }, [drawerOpen, drawerRun?.diagnosticRunId, drawerRun?.analysisResultId])

  useEffect(() => () => { if (switchTimer.current) clearTimeout(switchTimer.current) }, [])

  function closeDrawer() {
    const focusWasInDrawer = !!document.activeElement?.closest('#incident-drawer')
    ++switchToken.current
    if (switchTimer.current) clearTimeout(switchTimer.current)
    setSwitching(false)
    setDrawerOpen(false)
    if (focusWasInDrawer) requestAnimationFrame(() => {
      const row = Array.from(document.querySelectorAll<HTMLElement>('[data-incident-id]'))
        .find(element => element.dataset.incidentId === drawerIncidentId)
      row?.focus()
    })
  }

  async function startAnalysis(incident: Incident) {
    const incidentId = incident.metadata.name
    if (startingIds.has(incidentId)) return
    setStartingIds(previous => new Set(previous).add(incidentId))
    setFooterError(null)
    const label = incidentLabel(incident).title
    handleAnalysisStart(label)
    onAction?.()
    try {
      const response = await analyzeIncident(incidentId).unwrap()
      void waitForAnalysis(response.diagnosticRunId).then(async completed => {
        if (completed) {
          const { getToken } = await import('../../api/index')
          const token = getToken()
          const headers: Record<string, string> = {}
          if (token) headers.Authorization = `Bearer ${token}`
          const resultResponse = await fetch(`/api/v1/diagnosticruns/${encodeURIComponent(response.diagnosticRunId)}/analysisresult`, { headers })
          if (resultResponse.ok) handleAnalysisComplete(await resultResponse.json() as AnalysisResult, incidentId)
        } else {
          handleAnalysisFailed(label)
        }
        refetchAll()
      }).catch(() => { refetchAll() })
    } catch {
      handleAnalysisFailed(label)
      setFooterError(incidentId)
    } finally {
      setStartingIds(previous => {
        const next = new Set(previous)
        next.delete(incidentId)
        return next
      })
    }
  }

  function selectIncident(incident: Incident) {
    setSelectedId(incident.metadata.name)
    setFooterError(null)
    if (drawerOpen && drawerIncidentId === incident.metadata.name) { closeDrawer(); return }
    const token = ++switchToken.current
    if (switchTimer.current) clearTimeout(switchTimer.current)
    if (drawerOpen) {
      setSwitching(true)
      switchTimer.current = setTimeout(() => {
        if (token !== switchToken.current) return
        setDrawerIncidentId(incident.metadata.name)
        requestAnimationFrame(() => requestAnimationFrame(() => { if (token === switchToken.current) setSwitching(false) }))
      }, 220)
    } else {
      setDrawerIncidentId(incident.metadata.name)
      setDrawerOpen(true)
    }
  }

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) { if (event.key === 'Escape' && drawerOpen) closeDrawer() }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [drawerOpen])
  const visibleIncidents = filtered.slice(0, visibleCount)

  useLayoutEffect(() => {
    const updateHeight = () => {
      const top = tableRef.current?.getBoundingClientRect().top
      if (top != null) setTableHeight(Math.max(260, Math.min(720, window.innerHeight - top - 12)))
    }
    updateHeight()
    window.addEventListener('resize', updateHeight)
    const observer = new ResizeObserver(updateHeight)
    if (tableRef.current) observer.observe(tableRef.current)
    if (controlsRef.current) observer.observe(controlsRef.current)
    return () => { window.removeEventListener('resize', updateHeight); observer.disconnect() }
  }, [isLoading, filtered.length, checkedIds.size])

  useLayoutEffect(() => { if (drawerBodyRef.current) drawerBodyRef.current.scrollTop = 0 }, [drawerIncidentId])

  function changeView(next: InboxView) {
    setView(next)
    setSelectedId(null)
    closeDrawer()
    setVisibleCount(20)
    setCheckedIds(new Set())
  }

  useEffect(() => {
    function handleKeyboard(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null
      if (target?.closest('input, textarea, select, button, [role="dialog"], [contenteditable="true"]')) return
      if (!filtered.length) return
      const index = Math.max(0, filtered.findIndex(incident => incident.metadata.name === selected?.metadata.name))
      if (event.key === 'ArrowDown' || event.key === 'j') {
        event.preventDefault()
        setSelectedId(filtered[Math.min(index + 1, filtered.length - 1)].metadata.name)
      }
      if (event.key === 'ArrowUp' || event.key === 'k') {
        event.preventDefault()
        setSelectedId(filtered[Math.max(index - 1, 0)].metadata.name)
      }
    }
    window.addEventListener('keydown', handleKeyboard)
    return () => window.removeEventListener('keydown', handleKeyboard)
  }, [filtered, selected])

  function toggleChecked(id: string) {
    setCheckedIds(current => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id); else next.add(id)
      return next
    })
    setBulkConfirm(false)
  }

  async function bulkResolve() {
    const eligible = filtered.filter(incident => checkedIds.has(incident.metadata.name) && incident.status.state !== 'resolved')
    if (!eligible.length) return
    setBulkBusy(true)
    try {
      await Promise.all(eligible.map(incident => resolveSelected(incident.metadata.name).unwrap()))
      setCheckedIds(new Set())
      setBulkConfirm(false)
      onResolved?.()
      refetchAll()
    } finally {
      setBulkBusy(false)
    }
  }

  function renderIncidentCard(incident: Incident) {
    return (
      <IncidentCard
        key={incident.metadata.name}
        incident={incident}
        previousRun={priorRuns[incident.metadata.name]}
        analysisSummary={drawerAnalysis?.diagnosticRunId === priorRuns[incident.metadata.name]?.diagnosticRunId ? drawerAnalysis.payload.assessment?.summary : undefined}
        problemCases={problemCases}
        onAction={onAction}
        onResolved={onResolved}
        onSuggestRule={(p) => dispatch(setExclusionProposal(p))}
        onMarkFalsePositive={onMarkFalsePositive}
        onRefresh={refetchAll}
        onRegisterAugment={registerAugment}
      />
    )
  }

  if (isLoading) {
    return (
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 1 }}>
          <Typography variant="h6">Incidents</Typography>
        </Box>
        {[1, 2, 3, 4, 5].map(i => <Skeleton key={i} variant="rectangular" height={58} />)}
      </Box>
    )
  }

  if (isError) {
    return (
      <Alert severity="error" action={<Button color="inherit" size="small" onClick={refetch}>Retry</Button>}>
        Failed to load incidents: {String(error)}
      </Alert>
    )
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'flex-end', flexWrap: { xs: 'wrap', sm: 'nowrap' }, gap: 1.5, mb: 2.25 }}>
        <Box sx={{ flex: { xs: '1 1 100%', sm: 1 }, minWidth: 0 }}>
          <Typography component="h1" sx={{ fontSize: '1.35rem', fontWeight: 650, letterSpacing: '-0.02em' }}>Incidents</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.25, fontSize: '0.68rem' }}>
            Compare active signals and review diagnostic outcomes.
          </Typography>
        </Box>
        <Box sx={{ display: 'flex', width: { xs: '100%', sm: 'auto' }, gap: 1.1, mr: 0.5, color: 'text.secondary', whiteSpace: 'nowrap' }}>
          <Typography variant="caption" sx={{ fontSize: '0.62rem' }}><strong>{open.length}</strong> active</Typography>
          <Typography variant="caption" sx={{ fontSize: '0.62rem' }}><strong>{open.filter(incident => ['critical', 'high'].includes(incidentSeverity(incident, problemCases))).length}</strong> high priority</Typography>
          <Typography variant="caption" sx={{ fontSize: '0.62rem' }}><strong>{open.filter(incident => priorRuns[incident.metadata.name]?.needsMoreInfo).length}</strong> need context</Typography>
        </Box>
      </Box>

      <Box ref={controlsRef} sx={{ mb: 1, py: 0.5, borderTop: '1px solid', borderBottom: '1px solid', borderColor: 'divider' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.4, flexWrap: 'wrap' }}>
          <Box role="group" aria-label="Incident status" sx={{ display: 'flex', alignItems: 'center', gap: 0.65, flexShrink: 0 }}>
            {([['active', open.length], ['resolved', resolved.length], ['all', allIncidents.length]] as const).map(([option, count]) => (
              <Box component="button" type="button" key={option} aria-pressed={view === option} onClick={() => changeView(option)}
                sx={{ height: 30, px: 0.7, border: '1px solid', borderColor: view === option ? '#8e6c2d' : 'divider', borderRadius: 0, background: view === option ? 'rgba(213,166,74,.08)' : 'transparent', color: view === option ? '#f0d28b' : 'text.secondary', font: 'inherit', fontSize: '0.63rem', whiteSpace: 'nowrap', cursor: 'pointer', '&:hover': { borderColor: '#8e6c2d', color: 'text.primary' }, '&:focus-visible': { outline: '2px solid #d5a64a', outlineOffset: 2 } }}>
                {option === 'active' ? 'Active' : option === 'resolved' ? 'Resolved' : 'All'} {count}
              </Box>
            ))}
          </Box>
          <FormControl variant="standard" size="small" sx={{ width: 115, flex: '0 0 115px' }}>
            <Select value={namespace} onChange={event => { setNamespace(event.target.value); setSelectedId(null); closeDrawer(); setVisibleCount(20) }} displayEmpty disableUnderline aria-label="Filter by namespace" renderValue={value => value === 'all' ? 'Namespace: all' : `Namespace: ${value}`} sx={toolbarSelectSx}>
              <MenuItem value="all">All namespaces</MenuItem>
              {namespaces.map(ns => <MenuItem key={ns} value={ns}>{ns}</MenuItem>)}
            </Select>
          </FormControl>
          <FormControl variant="standard" size="small" sx={{ width: 95, flex: '0 0 95px' }}>
            <Select value={severity} onChange={event => { setSeverity(event.target.value); setSelectedId(null); closeDrawer(); setVisibleCount(20) }} displayEmpty disableUnderline aria-label="Filter by severity" renderValue={value => value === 'all' ? 'Severity: all' : `Severity: ${value}`} sx={toolbarSelectSx}>
              <MenuItem value="all">All severity</MenuItem>
              {['critical', 'high', 'medium', 'low'].map(level => <MenuItem key={level} value={level}>{level}</MenuItem>)}
            </Select>
          </FormControl>
          <FormControl variant="standard" size="small" sx={{ width: 105, flex: '0 0 105px' }}>
            <Select value={analysisFilter} onChange={event => { setAnalysisFilter(event.target.value as AnalysisFilter); setSelectedId(null); closeDrawer(); setVisibleCount(20) }} displayEmpty disableUnderline aria-label="Filter by analysis state" renderValue={value => value === 'any' ? 'Analysis: any' : `Analysis: ${String(value).replace('_', ' ')}`} sx={toolbarSelectSx}>
              <MenuItem value="any">Any analysis</MenuItem>
              <MenuItem value="not_analyzed">Not analyzed</MenuItem>
              <MenuItem value="needs_context">Needs context</MenuItem>
              <MenuItem value="completed">Completed</MenuItem>
              <MenuItem value="running">Running</MenuItem>
              <MenuItem value="failed">Failed</MenuItem>
            </Select>
          </FormControl>
          <TextField
            id="incident-search"
            variant="standard"
            size="small"
            value={search}
            onChange={event => { setSearch(event.target.value); setSelectedId(null); closeDrawer(); setVisibleCount(20) }}
            placeholder="Search"
            aria-label="Search incidents"
            sx={{ width: 70, flex: '0 1 70px', minWidth: 68, px: 0.4, '& .MuiInputBase-root': { height: 30, fontSize: '0.68rem' } }}
            slotProps={{ input: { disableUnderline: true, startAdornment: <InputAdornment position="start"><SearchIcon sx={{ fontSize: '0.85rem' }} /></InputAdornment> } }}
          />
          <FormControl variant="standard" size="small" sx={{ width: 145, flex: '0 0 145px', ml: 'auto' }}>
            <Select value={sort} onChange={event => setSort(event.target.value as IncidentSort)} displayEmpty disableUnderline aria-label="Sort incidents" renderValue={value => value === 'updated' ? 'Sort: recent activity' : `Sort: ${value}`} sx={toolbarSelectSx}>
              <MenuItem value="updated">Newest activity</MenuItem>
              <MenuItem value="severity">Highest severity</MenuItem>
              <MenuItem value="resource">Resource name</MenuItem>
              <MenuItem value="namespace">Namespace</MenuItem>
            </Select>
          </FormControl>
        </Box>
      </Box>

      {checkedIds.size > 0 && <Box sx={{ display: 'flex', alignItems: 'center', minHeight: 34, mb: 1, gap: 1 }}>
        <Typography variant="caption" sx={{ color: 'primary.light', fontWeight: 700 }}>{checkedIds.size} selected</Typography>
        {!bulkConfirm ? (
          <Button size="small" variant="outlined" color="success" onClick={() => setBulkConfirm(true)}>Resolve selected</Button>
        ) : (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
            <Typography variant="caption" color="text.secondary">Resolve open incidents?</Typography>
            <Button size="small" variant="contained" color="success" disabled={bulkBusy} onClick={bulkResolve}>{bulkBusy ? 'Resolving…' : 'Confirm'}</Button>
            <Button size="small" variant="text" onClick={() => setBulkConfirm(false)}>Cancel</Button>
          </Box>
        )}
        <Button size="small" variant="text" color="inherit" onClick={() => setCheckedIds(new Set())}>Clear</Button>
      </Box>}

      {filtered.length === 0 ? (
        <Box sx={{ textAlign: 'center', py: 7, border: '1px dashed', borderColor: 'divider', borderRadius: 2 }}>
          <Typography sx={{ fontSize: '1.6rem', mb: 0.75 }}>{view === 'active' ? '🎉' : '⌕'}</Typography>
          <Typography color="text.secondary">No incidents match this view</Typography>
        </Box>
      ) : (
        <Box ref={tableRef} sx={{ position: 'relative', border: '1px solid', borderColor: 'divider', height: tableHeight, overflow: 'hidden' }}>
          <Box sx={{ height: 'calc(100% - 42px)', display: 'flex', flexDirection: 'column', minWidth: 0 }}>
            <Box sx={{ position: 'sticky', top: 0, zIndex: 2, display: 'grid', gridTemplateColumns: { xs: '28px 4px minmax(105px,1fr) minmax(70px,.6fr) 58px', sm: '28px 4px minmax(130px,1.3fr) minmax(100px,.8fr) minmax(100px,.9fr) minmax(120px,1fr) 62px' }, gap: 1.25, px: 0, height: 35, alignItems: 'center', borderBottom: '1px solid', borderColor: 'divider', background: '#141815' }}>
              <Checkbox
                size="small"
                checked={filtered.length > 0 && filtered.every(incident => checkedIds.has(incident.metadata.name))}
                indeterminate={checkedIds.size > 0 && !filtered.every(incident => checkedIds.has(incident.metadata.name))}
                onChange={() => {
                  if (filtered.every(incident => checkedIds.has(incident.metadata.name))) setCheckedIds(new Set())
                  else setCheckedIds(new Set(filtered.map(incident => incident.metadata.name)))
                  setBulkConfirm(false)
                }}
                slotProps={{ input: { 'aria-label': 'Select all visible incidents' } }}
                sx={{ p: 0.5, ml: 0.25 }}
              />
              <Box />
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem' }}>Incident / resource</Typography>
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem' }}>Signal</Typography>
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem', display: { xs: 'none', sm: 'block' } }}>Symptoms</Typography>
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem', display: { xs: 'none', sm: 'block' } }}>Latest analysis</Typography>
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem', textAlign: 'right', pr: 1.25 }}>Updated</Typography>
            </Box>
          <Stack spacing={0} onScroll={event => {
            const list = event.currentTarget
            if (list.scrollHeight - list.scrollTop - list.clientHeight < 100 && visibleCount < filtered.length) setVisibleCount(count => Math.min(count + 20, filtered.length))
          }} sx={{ flex: 1, minHeight: 0, overflowY: 'auto', pb: 0.5 }}>
            {visibleIncidents.map(incident => (
              <Box key={incident.metadata.name}>
                <IncidentInboxRow
                  incident={incident}
                  severity={incidentSeverity(incident, problemCases)}
                  selected={incident.metadata.name === selected?.metadata.name}
                  expanded={drawerOpen && incident.metadata.name === drawerIncidentId}
                  checked={checkedIds.has(incident.metadata.name)}
                  run={runs.filter(run => run.incidentId === incident.metadata.name).sort((a, b) => b.requestedAt.localeCompare(a.requestedAt))[0]}
                  signal={activeProblemCases(incident, problemCases)[0]?.spec.detector.replace(/([a-z])([A-Z])/g, '$1 $2') ?? ''}
                  symptoms={activeProblemCases(incident, problemCases)[0]?.spec.symptoms?.[0] ?? ''}
                  onClick={() => selectIncident(incident)}
                  onCheck={() => toggleChecked(incident.metadata.name)}
                />
              </Box>
            ))}
          </Stack>
          </Box>
          <Box sx={{ position: 'absolute', inset: 0, bottom: 42, pointerEvents: 'none', overflow: 'hidden', zIndex: 5 }}>
            <Box component="aside" id="incident-drawer" role="dialog" aria-modal="false" aria-label="Incident details" aria-hidden={!drawerOpen} inert={!drawerOpen || switching}
              sx={{ pointerEvents: drawerOpen ? 'auto' : 'none', position: { xs: 'fixed', sm: 'absolute' }, right: 0, top: { xs: 60, sm: 0 }, bottom: 0, width: { xs: 'calc(100vw - 55px)', sm: 360 }, maxWidth: '100%', display: 'flex', flexDirection: 'column', bgcolor: '#171b18', borderLeft: '1px solid', borderColor: 'divider', boxShadow: '-16px 0 36px rgba(0,0,0,.42)', transform: !drawerOpen || switching ? 'translateX(105%)' : 'translateX(0)', transition: 'transform 220ms ease', '@media (prefers-reduced-motion: reduce)': { transition: 'none' } }}>
              <Box ref={drawerBodyRef} sx={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, px: 1.5, py: 0.5, minHeight: 42, borderBottom: '1px solid', borderColor: 'divider', position: 'sticky', top: 0, zIndex: 2, bgcolor: '#171b18' }}>
                  <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: drawerIncident?.status.state === 'resolved' ? 'success.main' : 'error.main' }} />
                  <Typography sx={{ fontSize: '0.62rem', fontWeight: 800, textTransform: 'uppercase', letterSpacing: '.08em', color: drawerIncident?.status.state === 'resolved' ? 'success.main' : 'error.main' }}>
                    {drawerIncident?.status.state ?? 'Incident'} · {drawerIncident ? incidentSeverity(drawerIncident, problemCases) : ''}
                  </Typography>
                  <Typography sx={{ ml: 'auto', minWidth: 0, maxWidth: 150, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: '0.61rem', color: 'text.disabled', fontFamily: 'monospace' }}>{drawerIncident?.metadata.name}</Typography>
                  <IconButton aria-label="Close incident details" onClick={closeDrawer} size="small"><CloseIcon fontSize="small" /></IconButton>
                </Box>
                {drawerIncident && renderIncidentCard(drawerIncident)}
                {drawerIncident && drawerRun?.analysisResultId && (
                  <Box component="section" sx={{ px: 1.1, pt: 1.1, pb: 1.5, mt: 0.3, borderTop: '1px solid #3b443c' }} aria-live="polite">
                    <Typography sx={{ px: 0.9, mb: 0.6, fontSize: '0.68rem', fontWeight: 700 }}>Evidence used</Typography>
                    {(evidenceLoading || analysisLoading) && <Typography variant="caption" sx={{ display: 'block' }}>Loading evidence and assessment…</Typography>}
                    {(evidenceError || (!analysisLoading && !drawerAnalysis && !evidenceLoading)) && <Alert severity="warning" sx={{ mt: 0.5 }}>Run result or evidence is unavailable.</Alert>}
                    {drawerAnalysis?.diagnosticRunId === drawerRun?.diagnosticRunId && drawerEvidence?.diagnosticRunId === drawerRun?.diagnosticRunId && (() => {
                      const sourceMap = new Map((drawerEvidence.payload.sources ?? []).map(source => [source.id, source]))
                      const assessment = drawerAnalysis.payload.assessment
                      const seenSources = new Set<string>()
                      const citations = (assessment?.claims.flatMap(claim => claim.citations) ?? [])
                        .filter(citation => {
                          if (!sourceMap.has(citation.sourceId) || seenSources.has(citation.sourceId)) return false
                          seenSources.add(citation.sourceId)
                          return true
                        })
                      return <Box>
                        <Typography variant="caption" color="text.disabled" sx={{ display: 'block', px: 0.9, mb: 0.7, fontSize: '0.61rem' }}>{citations.length ? `${Math.min(2, citations.length)} of ${citations.length} citations from this run` : 'No addressable citations in this run'}</Typography>
                        {citations.slice(0, 2).map((citation, index) => {
                          const source = sourceMap.get(citation.sourceId)!
                          const fieldValue = typeof source.value === 'string' ? source.value : source.value == null ? '' : JSON.stringify(source.value)
                          const value = source.line?.text ?? source.event?.message ?? fieldValue ?? 'Source record available in full assessment'
                          const sourceLabel = source.type === 'resource_field' ? 'RESOURCE FIELD' : source.type === 'log_line' ? 'LOG' : source.type === 'collection_error' ? 'COLLECTION ERROR' : 'EVENT'
                          const sourceColor = source.type === 'collection_error' ? '#d96b68' : source.type === 'resource_field' ? '#d5a64a' : source.type === 'log_line' ? '#6da985' : '#80a5bd'
                          return <Box key={`${citation.sourceId}-${index}`} sx={{ mb: 0.55, px: 0.9, py: 0.8, border: '1px solid #343c35', borderLeft: `2px solid ${sourceColor}`, background: '#141815' }}>
                            <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.65, minWidth: 0 }}>
                              <Typography sx={{ flexShrink: 0, fontSize: '0.56rem', fontWeight: 800, letterSpacing: '.06em', color: sourceColor }}>{sourceLabel}</Typography>
                              <Typography sx={{ minWidth: 0, fontSize: '0.6rem', color: 'text.secondary', overflowWrap: 'anywhere' }}>{source.resource.kind}/{source.resource.name}</Typography>
                            </Box>
                            {source.path && <Typography sx={{ mt: 0.35, fontFamily: 'monospace', fontSize: '0.61rem', lineHeight: 1.35, color: 'text.secondary', overflowWrap: 'anywhere' }}>{source.path}</Typography>}
                            <Typography sx={{ mt: 0.4, fontFamily: source.type === 'resource_field' ? 'monospace' : 'inherit', fontSize: '0.65rem', lineHeight: 1.4, color: 'text.primary', overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' }}>{String(value) || 'Source record available in full assessment'}</Typography>
                            {(source.redacted || source.truncated) && <Typography sx={{ mt: 0.35, fontSize: '0.57rem', color: 'warning.main' }}>{source.redacted ? 'Redacted' : ''}{source.redacted && source.truncated ? ' · ' : ''}{source.truncated ? 'Truncated' : ''}</Typography>}
                          </Box>
                        })}
                      </Box>
                    })()}
                  </Box>
                )}
              </Box>
              {drawerIncident && <Box sx={{ px: 1.25, py: 1, borderTop: '1px solid', borderColor: 'divider', bgcolor: '#151916', display: 'flex', gap: 0.75, alignItems: 'center', zIndex: 2, flexWrap: 'wrap' }}>
                {drawerNeedsMoreInfo && drawerIncident.status.state !== 'resolved' && <Button size="small" variant="contained" disabled={footerBusy} onClick={() => openAugment?.()}>Add context &amp; re-analyze</Button>}
                {drawerRun?.analysisResultId && <Button size="small" variant={drawerNeedsMoreInfo ? 'outlined' : 'contained'} onClick={() => navigate(`/incidents/${encodeURIComponent(drawerIncident.metadata.name)}/assessments/${encodeURIComponent(drawerRun.diagnosticRunId)}`)}>Full assessment</Button>}
                {drawerIncident.status.state !== 'resolved' && !drawerNeedsMoreInfo && <Button size="small" variant={drawerRun?.analysisResultId ? 'outlined' : 'contained'} disabled={footerBusy || drawerRunActive} onClick={() => startAnalysis(drawerIncident)}>{footerBusy ? 'Starting…' : drawerRunActive ? 'Analysis running…' : drawerRun?.analysisResultId ? 'Run again' : 'Run diagnostic analysis'}</Button>}
                {footerError === drawerIncident.metadata.name && <Typography role="alert" variant="caption" color="error.main">Could not start analysis. Try again.</Typography>}
              </Box>}
            </Box>
          </Box>
          <Box sx={{ position: 'absolute', left: 0, right: 0, bottom: 0, zIndex: 3, minHeight: 42, px: 1.5, display: 'flex', alignItems: 'center', gap: 1, bgcolor: '#141815', borderTop: '1px solid', borderColor: 'divider' }}>
            <Typography variant="caption" color="text.secondary" aria-live="polite">Showing {Math.min(visibleCount, filtered.length)} of {filtered.length} incidents</Typography>
            <Box sx={{ flex: 1 }} />
            {visibleCount < filtered.length && <Button size="small" variant="outlined" onClick={() => setVisibleCount(count => Math.min(count + 20, filtered.length))}>Load more</Button>}
            <Typography variant="caption" color="text.disabled" sx={{ display: { xs: 'none', sm: 'block' }, fontSize: '0.64rem' }}>{visibleCount < filtered.length ? 'Scroll list to browse' : 'All loaded'}</Typography>
          </Box>
        </Box>
      )}
    </Box>
  )
}
