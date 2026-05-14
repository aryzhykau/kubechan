import { useState } from 'react'
import {
  Box, Typography, Button, IconButton, Alert, Chip, Stack,
  CircularProgress, Skeleton, Accordion, AccordionSummary, AccordionDetails,
} from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import AddCommentIcon from '@mui/icons-material/AddComment'
import ShieldOutlinedIcon from '@mui/icons-material/ShieldOutlined'
import HelpOutlineIcon from '@mui/icons-material/HelpOutlined'
import {
  useListIncidentsQuery,
  useAnalyzeMutation,
  useResolveIncidentMutation,
  useMarkFalsePositiveMutation,
} from '../../store/api/incidentsApi'
import { useListDiagnosticRunsQuery } from '../../store/api/diagnosticsApi'
import { useAppDispatch } from '../../store/hooks'
import { useKubeChan } from '../../hooks/useKubeChan'
import { openManualModal, setExclusionProposal } from '../../store/slices/uiSlice'
import { ConfidenceBadge } from '../../components/shared/ConfidenceBadge'
import { ResourcePill } from '../../components/shared/ResourcePill'
import { timeAgo } from '../../components/shared/utils'
import { AugmentIncidentModal } from './AugmentIncidentModal'
import { waitForAnalysis } from '../../analysis/tracker'
import type { Incident, DiagnosticRunSummary, ExclusionRuleProposal, AnalysisResult } from '../../api/index'

// ── Helpers ───────────────────────────────────────────────────────────────────

function incidentLabel(incident: Incident): { title: string; sub: string } {
  const { rootResource } = incident.spec
  const ns = rootResource.namespace || incident.metadata.namespace || 'default'
  const affected = incident.status.activeProblemCases ?? 0
  const title = `${rootResource.kind} ${rootResource.name} in ${ns}`
  const sub = affected > 0
    ? `${affected} resource${affected !== 1 ? 's' : ''} also affected`
    : incident.spec.source === 'manual' && incident.spec.userMessage
      ? incident.spec.userMessage.slice(0, 80) + (incident.spec.userMessage.length > 80 ? '…' : '')
      : ''
  return { title, sub }
}

// ── IncidentDetails accordion ─────────────────────────────────────────────────

const sectionAccordionSx = {
  background: 'rgba(255,255,255,0.02)',
  border: '1px solid rgba(255,255,255,0.08)',
  borderRadius: '8px !important',
  mt: 1,
  '&:before': { display: 'none' },
}
const sectionSummarySx = {
  px: 1.5,
  minHeight: 40,
  '.MuiAccordionSummary-content': { my: 0 },
  '&:hover': { background: 'rgba(255,255,255,0.03)' },
  borderRadius: '8px',
}

function IncidentDetails({ incident, previousRun }: {
  incident: Incident
  previousRun?: DiagnosticRunSummary
}) {
  const isManual = incident.spec.source === 'manual'
  const related = incident.spec.relatedResources ?? []
  const pcs = incident.spec.problemCases ?? []
  const suggestions = previousRun?.suggestedResources ?? []
  const needsMoreInfo = !!previousRun?.needsMoreInfo && suggestions.length > 0

  return (
    <Accordion disableGutters elevation={0} sx={sectionAccordionSx}>
      <AccordionSummary
        expandIcon={<ExpandMoreIcon sx={{ fontSize: '0.9rem', color: 'text.secondary' }} />}
        sx={sectionSummarySx}
      >
        <Typography variant="body2" sx={{ fontWeight: 600, fontSize: '0.8rem' }}>Details</Typography>
      </AccordionSummary>
      <AccordionDetails sx={{ px: 1.5, pt: 0.5, pb: 1.5 }}>
        <Stack spacing={1.5}>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Root resource</Typography>
            <ResourcePill
              kind={incident.spec.rootResource.kind}
              name={incident.spec.rootResource.name}
              namespace={incident.spec.rootResource.namespace}
            />
          </Box>

          {isManual && incident.spec.userMessage && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>User description</Typography>
              <Typography variant="body2" sx={{ color: 'text.primary', fontSize: '0.82rem', lineHeight: 1.5 }}>
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

          {!isManual && pcs.length > 0 && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Problem cases</Typography>
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                {pcs.map(pc => (
                  <Chip key={pc} label={pc} size="small" variant="outlined" sx={{ fontSize: '0.7rem', height: 20, fontFamily: 'monospace' }} />
                ))}
              </Box>
            </Box>
          )}

          {needsMoreInfo && (
            <Box>
              <Typography variant="caption" sx={{ color: 'warning.main', display: 'block', mb: 0.5 }}>KubeChan suggested</Typography>
              <Stack spacing={0.75}>
                {suggestions.map((s, i) => (
                  <Box key={i} sx={{ display: 'flex', gap: 1, alignItems: 'flex-start' }}>
                    <Chip label={s.kind} size="small" sx={{ background: 'rgba(245,158,11,0.1)', color: '#fbbf24', flexShrink: 0 }} />
                    <Typography variant="caption" color="text.secondary">{s.reason}</Typography>
                  </Box>
                ))}
              </Stack>
            </Box>
          )}
        </Stack>
      </AccordionDetails>
    </Accordion>
  )
}

function IncidentAnalysis({ previousRun, onRate }: {
  previousRun: DiagnosticRunSummary
  onRate?: (runId: string, rating: 'up' | 'down', confidence: number) => void
}) {
  const [analysisResult, setAnalysisResult] = useState<AnalysisResult | null>(null)
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const [localRating, setLocalRating] = useState<'up' | 'down' | null>(null)

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

  return (
    <Accordion
      disableGutters elevation={0}
      onChange={(_, expanded) => { if (expanded) fetchAnalysis() }}
      sx={{ ...sectionAccordionSx, borderColor: 'rgba(99,102,241,0.25)', background: 'rgba(99,102,241,0.04)' }}
    >
      <AccordionSummary
        expandIcon={<ExpandMoreIcon sx={{ fontSize: '0.9rem', color: 'primary.light' }} />}
        sx={sectionSummarySx}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="body2" sx={{ fontWeight: 700, fontSize: '0.8rem', color: 'primary.light' }}>
            Analysis result
          </Typography>
          {previousRun.confidence != null && (
            <Typography variant="caption" sx={{ color: confColor, fontWeight: 700, fontSize: '0.75rem' }}>
              {pct}%
            </Typography>
          )}
        </Box>
      </AccordionSummary>
      <AccordionDetails sx={{ px: 1.5, pt: 0.5, pb: 1.5 }}>
        {analysisLoading && (
          <Stack spacing={0.75}>
            <Skeleton variant="text" width="80%" />
            <Skeleton variant="text" width="60%" />
            <Skeleton variant="text" width="70%" />
          </Stack>
        )}
        {!analysisLoading && analysisResult && (
          <Stack spacing={1.5}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
              <Typography variant="caption" sx={{ px: 1, py: 0.25, borderRadius: '4px', background: `${confColor}22`, color: confColor, fontWeight: 700, fontSize: '0.7rem' }}>
                {pct}% confidence
              </Typography>
              <Typography variant="caption" color="text.disabled" sx={{ fontFamily: 'monospace', fontSize: '0.7rem' }}>{analysisResult.model}</Typography>
            </Box>

            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, fontWeight: 600 }}>Root cause</Typography>
              <Typography variant="body2" sx={{ fontSize: '0.82rem', lineHeight: 1.5 }}>
                {analysisResult.likelyRootCause || analysisResult.payload?.likelyRootCause}
              </Typography>
            </Box>

            {analysisResult.payload?.evidenceChain && (
              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, fontWeight: 600 }}>Evidence</Typography>
                <Typography variant="body2" sx={{ fontSize: '0.82rem', lineHeight: 1.5, color: 'text.secondary' }}>
                  {analysisResult.payload.evidenceChain}
                </Typography>
              </Box>
            )}

            {analysisResult.payload?.recommendation && (
              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, fontWeight: 600 }}>Fix it</Typography>
                <Typography variant="body2" sx={{ fontSize: '0.82rem', lineHeight: 1.5, whiteSpace: 'pre-wrap' }}>
                  {analysisResult.payload.recommendation}
                </Typography>
              </Box>
            )}

            {onRate && (
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, pt: 0.5, borderTop: '1px solid rgba(255,255,255,0.06)' }}>
                <Typography variant="caption" color="text.secondary" sx={{ flex: 1 }}>
                  {effectiveRating ? 'Feedback recorded.' : 'Was this correct?'}
                </Typography>
                <Button size="small" variant={effectiveRating === 'up' ? 'contained' : 'outlined'} color="success"
                  disabled={!!effectiveRating} onClick={() => handleRate('up')} sx={{ minWidth: 0, px: 1, fontSize: '0.75rem' }}>
                  👍 Correct
                </Button>
                <Button size="small" variant={effectiveRating === 'down' ? 'contained' : 'outlined'} color="error"
                  disabled={!!effectiveRating} onClick={() => handleRate('down')} sx={{ minWidth: 0, px: 1, fontSize: '0.75rem' }}>
                  👎 Wrong
                </Button>
              </Box>
            )}
          </Stack>
        )}
      </AccordionDetails>
    </Accordion>
  )
}

// ── IncidentCard ──────────────────────────────────────────────────────────────

interface IncidentCardProps {
  incident: Incident
  previousRun?: DiagnosticRunSummary
  onAction?: () => void
  onResolved?: () => void
  onSuggestRule?: (p: ExclusionRuleProposal) => void
  onMarkFalsePositive?: () => void
  onRefresh: () => void
}

function IncidentCard({
  incident, previousRun,
  onAction, onResolved, onSuggestRule, onMarkFalsePositive, onRefresh,
}: IncidentCardProps) {
  const [analyze] = useAnalyzeMutation()
  const [resolveApi] = useResolveIncidentMutation()
  const [fpApi] = useMarkFalsePositiveMutation()
  const { handleAnalysisStart, handleAnalysisComplete, handleRate } = useKubeChan()

  const [isAnalyzing, setIsAnalyzing] = useState(false)
  const [resolveStep, setResolveStep] = useState<'idle' | 'confirm'>('idle')
  const [fpStep, setFpStep] = useState<'idle' | 'confirm'>('idle')
  const [augmentOpen, setAugmentOpen] = useState(false)

  const id = incident.metadata.name
  const isResolved = incident.status.state === 'resolved'
  const isManual = incident.spec.source === 'manual'
  const alreadyAnalyzed = !!previousRun?.analysisResultId
  const needsMoreInfo = alreadyAnalyzed && !!previousRun?.needsMoreInfo && (previousRun.suggestedResources?.length ?? 0) > 0
  const { title, sub } = incidentLabel(incident)
  const rootNS = incident.spec.rootResource.namespace || incident.metadata.namespace || ''

  async function handleAnalyze() {
    setIsAnalyzing(true)
    handleAnalysisStart(title)
    onAction?.()
    try {
      const res = await analyze(id).unwrap()
      const ok = await waitForAnalysis(res.diagnosticRunId)
      if (ok) {
        const result = await fetchAnalysisResult(res.diagnosticRunId)
        if (result) handleAnalysisComplete(result, incident.metadata.name)
      }
      onRefresh()
    } catch {
      // error handled below
    } finally {
      setIsAnalyzing(false)
    }
  }

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
        onRefresh()
      }
    }).finally(() => setIsAnalyzing(false))
  }

  return (
    <Box
      sx={{
        background: isResolved ? 'rgba(255,255,255,0.02)' : 'rgba(99,102,241,0.04)',
        border: `1px solid`,
        borderColor: isResolved ? '#2a2f4a' : 'rgba(99,102,241,0.2)',
        borderRadius: '12px',
        p: 2.5,
        transition: 'border-color 0.2s',
        '&:hover': { borderColor: isResolved ? '#3d4470' : 'rgba(99,102,241,0.35)' },
      }}
    >
      {/* Header row */}
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 1 }}>
        <Chip
          label={incident.status.state.toUpperCase()}
          size="small"
          color={isResolved ? 'success' : 'warning'}
          sx={{ fontWeight: 700, fontSize: '0.65rem', height: 20 }}
        />
        {isManual && (
          <Chip label="Manual" size="small" variant="outlined" sx={{ fontSize: '0.65rem', height: 20 }} />
        )}
        {isManual && incident.ownerUsername && (
          <Typography variant="caption" color="text.secondary">by {incident.ownerUsername}</Typography>
        )}
        <Box sx={{ flex: 1 }} />
        <Typography variant="caption" color="text.secondary">{timeAgo(incident.status.openedAt)}</Typography>
      </Box>

      {/* Title */}
      <Typography sx={{ fontWeight: 700, fontSize: '0.9rem', mb: 0.5, color: isResolved ? 'text.secondary' : 'text.primary' }}>
        {title}
      </Typography>
      {sub && <Typography variant="caption" color="text.secondary">{sub}</Typography>}

      {/* Details accordion */}
      <IncidentDetails incident={incident} previousRun={previousRun} />
      {previousRun?.analysisResultId && (
        <IncidentAnalysis previousRun={previousRun} onRate={handleRate} />
      )}

      {/* Actions row */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', mt: 1 }}>
        {isAnalyzing && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <CircularProgress size={14} />
            <Typography variant="caption" color="warning.main">KubeChan is on it…</Typography>
          </Box>
        )}
        {alreadyAnalyzed && !isAnalyzing && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            <Typography variant="caption" color="success.main" sx={{ fontWeight: 600 }}>✓ analyzed</Typography>
            <ConfidenceBadge confidence={previousRun?.confidence} />
          </Box>
        )}
        {!isResolved && !isAnalyzing && (
          <Button
            size="small"
            variant={alreadyAnalyzed ? 'text' : 'contained'}
            onClick={handleAnalyze}
            sx={{ fontSize: '0.78rem', minWidth: 0 }}
          >
            {alreadyAnalyzed ? 'Ask again' : 'Ask KubeChan to help'}
          </Button>
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
      </Box>

      {/* Needs more info banner */}
      {needsMoreInfo && (
        <Box sx={{ mt: 1.5, p: 1.5, background: 'rgba(245,158,11,0.08)', borderRadius: '8px', border: '1px solid rgba(245,158,11,0.2)', display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <HelpOutlineIcon sx={{ color: 'warning.main', fontSize: '1.1rem', flexShrink: 0 }} />
          <Box sx={{ flex: 1 }}>
            <Typography variant="caption" sx={{ fontWeight: 700, color: 'warning.main', display: 'block' }}>KubeChan needs more evidence</Typography>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, mt: 0.5 }}>
              {previousRun!.suggestedResources!.map((s, i) => (
                <Chip key={i} label={s.kind} size="small" sx={{ background: 'rgba(245,158,11,0.1)', color: '#fbbf24', fontSize: '0.7rem', height: 18 }} />
              ))}
            </Box>
          </Box>
          <Button size="small" variant="outlined" color="warning" onClick={() => setAugmentOpen(true)} sx={{ fontSize: '0.75rem', whiteSpace: 'nowrap' }}>
            Add context
          </Button>
        </Box>
      )}

      {/* Exclusion rule suggestion banner */}
      {previousRun?.suggestExclusionRule && onSuggestRule && (
        <Box sx={{ mt: 1.5, p: 1.5, background: 'rgba(99,102,241,0.08)', borderRadius: '8px', border: '1px solid rgba(99,102,241,0.2)', display: 'flex', alignItems: 'center', gap: 1.5 }}>
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

export interface IncidentListProps {
  onAction?: () => void
  onResolved?: () => void
  onMarkFalsePositive?: () => void
}

export function IncidentList({ onAction, onResolved, onMarkFalsePositive }: IncidentListProps = {}) {
  const dispatch = useAppDispatch()

  const { data: incidents = [], isLoading, isError, error, refetch } = useListIncidentsQuery()
  const { data: runs = [], refetch: refetchRuns } = useListDiagnosticRunsQuery()

  function refetchAll() { refetch(); refetchRuns() }

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

  if (isLoading) {
    return (
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 1 }}>
          <Typography variant="h6">Incidents</Typography>
        </Box>
        {[1, 2, 3].map(i => <Skeleton key={i} variant="rounded" height={120} sx={{ borderRadius: '12px' }} />)}
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
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 3 }}>
        <Typography variant="h6" sx={{ flex: 1 }}>Incidents</Typography>
        <IconButton size="small" onClick={refetch} title="Refresh">
          <RefreshIcon fontSize="small" />
        </IconButton>
        <Button
          variant="contained"
          size="small"
          startIcon={<AddCommentIcon sx={{ fontSize: '0.9rem' }} />}
          onClick={() => dispatch(openManualModal())}
          sx={{ fontSize: '0.8rem' }}
        >
          Report an issue
        </Button>
      </Box>

      {open.length === 0 && !isLoading && (
        <Box sx={{ textAlign: 'center', py: 8 }}>
          <Typography sx={{ fontSize: '2rem', mb: 1 }}>🎉</Typography>
          <Typography color="text.secondary">No open incidents</Typography>
        </Box>
      )}

      <Stack spacing={1.5}>
        {open.map(inc => (
          <IncidentCard
            key={inc.metadata.name}
            incident={inc}
            previousRun={priorRuns[inc.metadata.name]}
            onAction={onAction}
            onResolved={onResolved}
            onSuggestRule={(p) => dispatch(setExclusionProposal(p))}
            onMarkFalsePositive={onMarkFalsePositive}
            onRefresh={refetchAll}
          />
        ))}
      </Stack>

      {resolved.length > 0 && (
        <Accordion sx={{ mt: 2 }}>
          <AccordionSummary expandIcon={<ExpandMoreIcon />}>
            <Typography variant="body2" color="text.secondary">
              {resolved.length} resolved incident{resolved.length > 1 ? 's' : ''}
            </Typography>
          </AccordionSummary>
          <AccordionDetails sx={{ p: 0 }}>
            <Stack spacing={1.5} sx={{ p: 2 }}>
              {resolved.map(inc => (
                <IncidentCard
                  key={inc.metadata.name}
                  incident={inc}
                  previousRun={priorRuns[inc.metadata.name]}
                  onAction={onAction}
                  onResolved={onResolved}
                  onSuggestRule={(p) => dispatch(setExclusionProposal(p))}
                  onMarkFalsePositive={onMarkFalsePositive}
                  onRefresh={refetchAll}
                />
              ))}
            </Stack>
          </AccordionDetails>
        </Accordion>
      )}
    </Box>
  )
}
