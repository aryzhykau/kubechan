import { useEffect, useState } from 'react'
import {
  Box, Typography, Button, IconButton, Alert, Chip, Stack,
  CircularProgress, Skeleton, Accordion, AccordionSummary, AccordionDetails, Tooltip,
  TextField, InputAdornment, ToggleButton, ToggleButtonGroup, FormControl, Select, MenuItem,
  Checkbox, Tabs, Tab,
} from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import AddCommentIcon from '@mui/icons-material/AddComment'
import ShieldOutlinedIcon from '@mui/icons-material/ShieldOutlined'
import HelpOutlineIcon from '@mui/icons-material/HelpOutlined'
import HistoryIcon from '@mui/icons-material/History'
import LinkIcon from '@mui/icons-material/Link'
import SearchIcon from '@mui/icons-material/Search'
import {
  useListIncidentsQuery,
  useListProblemCasesQuery,
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

function IncidentDetails({ incident, previousRun, problemCases, runs }: {
  incident: Incident
  previousRun?: DiagnosticRunSummary
  problemCases: ProblemCase[]
  runs: DiagnosticRunSummary[]
}) {
  const isManual = incident.spec.source === 'manual'
  const related = incident.spec.relatedResources ?? []
  const pcs = incident.spec.problemCases ?? []
  const suggestions = previousRun?.suggestedResources ?? []
  const needsMoreInfo = !!previousRun?.needsMoreInfo && suggestions.length > 0
  const pcByName = new Map(problemCases.map(pc => [pc.metadata.name, pc]))
  const incidentPCs = pcs.map(name => pcByName.get(name)).filter((pc): pc is ProblemCase => !!pc)
  const activePCs = incidentPCs.filter(pc => pc.status.state !== 'resolved')
  const previousPCs = incidentPCs.filter(pc => pc.status.state === 'resolved')
  const occurrenceCount = incident.status.occurrenceCount ?? 1

  function isRootResource(pc: ProblemCase) {
    const affected = pc.spec.affectedResource
    const root = incident.spec.rootResource
    return affected.kind.toLowerCase() === root.kind.toLowerCase()
      && affected.name === root.name
      && (affected.namespace || incident.metadata.namespace) === (root.namespace || incident.metadata.namespace)
  }

  function renderProblemCase(pc: ProblemCase) {
    const runCount = runs.filter(run => run.problemCaseRef === pc.metadata.name).length
    const isResolved = pc.status.state === 'resolved'
    const correlated = !isRootResource(pc)
    return (
      <Box
        key={pc.metadata.name}
        sx={{
          p: 1.25,
          borderRadius: 1.5,
          border: '1px solid',
          borderColor: isResolved ? 'rgba(255,255,255,0.07)' : 'rgba(245,158,11,0.2)',
          background: isResolved ? 'rgba(255,255,255,0.015)' : 'rgba(245,158,11,0.035)',
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
          <Chip label={pc.spec.detector} size="small" color={isResolved ? 'default' : 'warning'} variant="outlined" sx={{ fontSize: '0.7rem', height: 22 }} />
          <Chip label={pc.spec.severity} size="small" variant="outlined" sx={{ fontSize: '0.65rem', height: 20, textTransform: 'uppercase', opacity: 0.8 }} />
          <Box sx={{ flex: 1 }} />
          <Typography variant="caption" color="text.disabled">{timeAgo(pc.status.lastSeen ?? pc.status.firstSeen)}</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap', mt: 1 }}>
          <ResourcePill kind={pc.spec.affectedResource.kind} name={pc.spec.affectedResource.name} namespace={pc.spec.affectedResource.namespace} />
          {correlated && (
            <Tooltip title={`This ${pc.spec.affectedResource.kind} was grouped under ${incident.spec.rootResource.kind} ${incident.spec.rootResource.name} because they belong to the same workload path.`} arrow>
              <Chip
                icon={<LinkIcon sx={{ fontSize: '0.8rem !important' }} />}
                label="part of this workload"
                size="small"
                variant="outlined"
                sx={{ fontSize: '0.65rem', height: 21, color: 'text.secondary' }}
              />
            </Tooltip>
          )}
          {runCount > 0 && <Typography variant="caption" color="text.secondary">{runCount} diagnostic{runCount === 1 ? '' : 's'}</Typography>}
        </Box>
        {(pc.spec.symptoms?.length ?? 0) > 0 && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.75, lineHeight: 1.45 }}>
            {pc.spec.symptoms!.join(' · ')}
          </Typography>
        )}
      </Box>
    )
  }

  return (
    <Accordion defaultExpanded={incident.status.state !== 'resolved'} disableGutters elevation={0} sx={sectionAccordionSx}>
      <AccordionSummary
        expandIcon={<ExpandMoreIcon sx={{ fontSize: '0.9rem', color: 'text.secondary' }} />}
        sx={sectionSummarySx}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, width: '100%', pr: 1 }}>
          <Typography variant="body2" sx={{ fontWeight: 600, fontSize: '0.8rem' }}>Incident details</Typography>
          {activePCs.length > 0 && <Chip label={`${activePCs.length} active`} size="small" color="warning" variant="outlined" sx={{ height: 19, fontSize: '0.62rem' }} />}
          {previousPCs.length > 0 && <Chip label={`${previousPCs.length} previous`} size="small" variant="outlined" sx={{ height: 19, fontSize: '0.62rem', opacity: 0.7 }} />}
        </Box>
      </AccordionSummary>
      <AccordionDetails sx={{ px: 1.5, pt: 0.5, pb: 1.5 }}>
        <Stack spacing={1.5}>
          {occurrenceCount > 1 && (
            <Box
              sx={{
                display: 'grid',
                gridTemplateColumns: { xs: '1fr', sm: 'auto 1fr 1fr' },
                alignItems: 'center',
                gap: 1.25,
                p: 1.25,
                borderRadius: 1.5,
                background: 'rgba(245,158,11,0.06)',
                border: '1px solid rgba(245,158,11,0.16)',
              }}
            >
              <HistoryIcon sx={{ color: 'warning.main', fontSize: '1.15rem' }} />
              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>First detected</Typography>
                <Typography variant="body2" sx={{ fontSize: '0.78rem', fontWeight: 600 }}>{timeAgo(incident.status.openedAt)}</Typography>
              </Box>
              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>Latest occurrence</Typography>
                <Typography variant="body2" sx={{ fontSize: '0.78rem', fontWeight: 600 }}>
                  {timeAgo(incident.status.lastOccurrenceAt)} · occurrence #{occurrenceCount}
                </Typography>
              </Box>
            </Box>
          )}

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

          {!isManual && activePCs.length > 0 && (
            <Box>
              <Typography variant="caption" color="warning.main" sx={{ display: 'block', mb: 0.75, fontWeight: 700 }}>Active symptoms</Typography>
              <Stack spacing={0.75}>{activePCs.map(renderProblemCase)}</Stack>
            </Box>
          )}

          {!isManual && previousPCs.length > 0 && (
            <Accordion
              disableGutters
              elevation={0}
              sx={{
                background: 'transparent',
                border: '1px solid rgba(255,255,255,0.07)',
                borderRadius: '8px !important',
                '&:before': { display: 'none' },
              }}
            >
              <AccordionSummary
                expandIcon={<ExpandMoreIcon sx={{ fontSize: '0.9rem', color: 'text.secondary' }} />}
                sx={{
                  minHeight: 38,
                  px: 1.25,
                  '.MuiAccordionSummary-content': { my: 0.5, alignItems: 'center', gap: 0.75 },
                }}
              >
                <HistoryIcon sx={{ fontSize: '0.95rem', color: 'text.secondary' }} />
                <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700 }}>
                  History
                </Typography>
                <Chip label={`${previousPCs.length} previous`} size="small" variant="outlined" sx={{ height: 19, fontSize: '0.62rem', opacity: 0.7 }} />
              </AccordionSummary>
              <AccordionDetails sx={{ px: 1.25, pt: 0.25, pb: 1.25 }}>
                <Stack spacing={0.75}>{previousPCs.map(renderProblemCase)}</Stack>
              </AccordionDetails>
            </Accordion>
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
  const [evidenceOpen, setEvidenceOpen] = useState(false)
  const [recommendationOpen, setRecommendationOpen] = useState(true)

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

          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 1.5, py: 1, borderTop: '1px solid', borderColor: 'divider', background: 'rgba(0,0,0,0.12)' }}>
            <Typography variant="caption" color="text.disabled" sx={{ fontFamily: 'monospace', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {analysisResult.model}
            </Typography>
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
  problemCases: ProblemCase[]
  runs: DiagnosticRunSummary[]
  onAction?: () => void
  onResolved?: () => void
  onSuggestRule?: (p: ExclusionRuleProposal) => void
  onMarkFalsePositive?: () => void
  onRefresh: () => void
}

function IncidentCard({
  incident, previousRun, problemCases, runs,
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
  const [detailTab, setDetailTab] = useState<'overview' | 'timeline' | 'diagnostics'>('overview')

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

  const incidentRuns = [...runs].sort((a, b) => (b.requestedAt ?? '').localeCompare(a.requestedAt ?? ''))
  const timeline = [
    incident.status.resolvedAt ? { at: incident.status.resolvedAt, label: 'Incident resolved', tone: 'success.main' } : null,
    ...incidentRuns.map(run => ({ at: run.requestedAt, label: `Diagnostic run ${run.status.toLowerCase()}`, tone: 'primary.main' })),
    incident.status.lastOccurrenceAt && incident.status.lastOccurrenceAt !== incident.status.openedAt
      ? { at: incident.status.lastOccurrenceAt, label: `Occurrence ${incident.status.occurrenceCount ?? 1} detected`, tone: 'warning.main' }
      : null,
    incident.status.openedAt ? { at: incident.status.openedAt, label: 'Incident opened', tone: 'warning.main' } : null,
  ].filter((entry): entry is { at: string; label: string; tone: string } => !!entry)
    .sort((a, b) => b.at.localeCompare(a.at))

  return (
    <Box
      sx={{
        background: '#171b18',
        p: { xs: 1.75, md: 2.5 },
        minHeight: { lg: 'calc(100vh - 235px)' },
      }}
    >
      {/* Header row */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25, pb: 1.5, mb: 2, borderBottom: '1px solid', borderColor: 'divider' }}>
        <Typography sx={{ fontSize: '0.64rem', lineHeight: 1, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: isResolved ? 'success.main' : 'warning.main' }}>
          {incident.status.state}
        </Typography>
        {isManual && <Typography variant="caption" color="text.secondary">Manual report</Typography>}
        {isManual && incident.ownerUsername && (
          <Typography variant="caption" color="text.secondary">by {incident.ownerUsername}</Typography>
        )}
        {(incident.status.occurrenceCount ?? 1) > 1 && (
          <Typography variant="caption" color="warning.main">{incident.status.occurrenceCount} occurrences</Typography>
        )}
        <Box sx={{ flex: 1 }} />
        <Typography variant="caption" color="text.secondary">
          {(incident.status.occurrenceCount ?? 1) > 1 ? 'Last occurrence ' : ''}
          {timeAgo(incident.status.lastOccurrenceAt ?? incident.status.openedAt)}
        </Typography>
      </Box>

      {/* Title */}
      <Typography component="h2" sx={{ fontWeight: 650, fontSize: '1.05rem', lineHeight: 1.35, mb: 0.55, color: isResolved ? 'text.secondary' : 'text.primary', letterSpacing: '-0.01em' }}>
        {title}
      </Typography>
      {sub && <Typography variant="body2" color="text.secondary" sx={{ fontSize: '0.78rem', mb: 1.5 }}>{sub}</Typography>}

      <Tabs
        value={detailTab}
        onChange={(_, value) => setDetailTab(value)}
        aria-label="Incident detail sections"
        sx={{ mt: 1.5, mb: 1.25, minHeight: 34, borderBottom: '1px solid', borderColor: 'divider', '& .MuiTab-root': { minHeight: 34, px: 1.25, py: 0.5, fontSize: '0.7rem', letterSpacing: '0.04em' } }}
      >
        <Tab value="overview" label="Overview" />
        <Tab value="timeline" label={`Timeline ${timeline.length}`} />
        <Tab value="diagnostics" label={`Diagnostics ${incidentRuns.length}`} />
      </Tabs>

      {detailTab === 'overview' && (
        <>
          <IncidentDetails incident={incident} previousRun={previousRun} problemCases={problemCases} runs={runs} />
          {previousRun?.analysisResultId && (
            <IncidentAnalysis key={previousRun.diagnosticRunId} previousRun={previousRun} onRate={handleRate} />
          )}
        </>
      )}

      {detailTab === 'timeline' && (
        <Box sx={{ py: 0.5 }}>
          {timeline.map((entry, index) => (
            <Box key={`${entry.at}-${entry.label}`} sx={{ display: 'grid', gridTemplateColumns: '86px 12px 1fr', gap: 1, minHeight: 48 }}>
              <Typography variant="caption" color="text.disabled" sx={{ pt: 0.15, textAlign: 'right' }}>{timeAgo(entry.at)}</Typography>
              <Box sx={{ position: 'relative', display: 'flex', justifyContent: 'center' }}>
                {index < timeline.length - 1 && <Box sx={{ position: 'absolute', top: 9, bottom: -9, width: '1px', background: 'divider' }} />}
                <Box sx={{ mt: 0.4, width: 7, height: 7, borderRadius: '50%', background: entry.tone, zIndex: 1 }} />
              </Box>
              <Box>
                <Typography variant="body2" sx={{ fontSize: '0.78rem', fontWeight: 600 }}>{entry.label}</Typography>
                <Typography variant="caption" color="text.disabled">{new Date(entry.at).toLocaleString()}</Typography>
              </Box>
            </Box>
          ))}
        </Box>
      )}

      {detailTab === 'diagnostics' && (
        <Box sx={{ borderTop: incidentRuns.length ? '1px solid' : 0, borderColor: 'divider' }}>
          {incidentRuns.length === 0 ? (
            <Box sx={{ py: 4, textAlign: 'center' }}>
              <Typography variant="body2" color="text.secondary">No diagnostic runs yet</Typography>
              <Typography variant="caption" color="text.disabled">Run KubeChan analysis to collect evidence.</Typography>
            </Box>
          ) : incidentRuns.map(run => (
            <Box key={run.diagnosticRunId} sx={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 1, py: 1.25, borderBottom: '1px solid', borderColor: 'divider' }}>
              <Box sx={{ minWidth: 0 }}>
                <Typography sx={{ fontFamily: 'monospace', fontSize: '0.72rem', overflow: 'hidden', textOverflow: 'ellipsis' }}>{run.diagnosticRunId}</Typography>
                <Typography variant="caption" color="text.secondary">{timeAgo(run.requestedAt)}{run.model ? ` · ${run.model}` : ''}</Typography>
              </Box>
              <Box sx={{ textAlign: 'right' }}>
                <Typography sx={{ fontSize: '0.64rem', fontWeight: 800, letterSpacing: '0.06em', textTransform: 'uppercase', color: run.status === 'completed' ? 'success.main' : 'warning.main' }}>{run.status}</Typography>
                {run.confidence != null && <Typography variant="caption" color="text.secondary">{Math.round(run.confidence * 100)}% confidence</Typography>}
              </Box>
            </Box>
          ))}
        </Box>
      )}

      {/* Actions row */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', mt: 1.5, pt: 1.5, borderTop: '1px solid', borderColor: 'divider' }}>
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

const severityRank: Record<string, number> = { critical: 4, high: 3, medium: 2, low: 1 }

function incidentSeverity(incident: Incident, problemCases: ProblemCase[]): string {
  const names = new Set(incident.spec.problemCases ?? [])
  return problemCases
    .filter(pc => names.has(pc.metadata.name))
    .map(pc => pc.spec.severity.toLowerCase())
    .sort((a, b) => (severityRank[b] ?? 0) - (severityRank[a] ?? 0))[0] ?? 'unknown'
}

function IncidentInboxRow({ incident, severity, selected, checked, onClick, onCheck }: {
  incident: Incident
  severity: string
  selected: boolean
  checked: boolean
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
  return (
    <Box
      component="button"
      onClick={onClick}
      aria-pressed={selected}
      sx={{
        width: '100%', textAlign: 'left', color: 'inherit', cursor: 'pointer',
        display: 'grid',
        gridTemplateColumns: '32px 5px minmax(150px, 1fr) 88px 74px',
        alignItems: 'center', gap: 1.25, minHeight: 58, px: 0, py: 0,
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
          {namespace} / {rootResource.kind}
        </Typography>
      </Box>
      <Box>
        <Typography sx={{ fontSize: '0.65rem', fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase', color: isResolved ? 'success.main' : severityColor[severity] }}>
          {isResolved ? 'Resolved' : severity}
        </Typography>
        <Typography variant="caption" color="text.disabled">
          {occurrences > 1 ? `${occurrences} events` : `${incident.status.activeProblemCases ?? 0} active`}
        </Typography>
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
  const initialParams = new URLSearchParams(window.location.search)
  const initialView = initialParams.get('view')
  const initialSort = initialParams.get('sort')
  const [view, setView] = useState<InboxView>(initialView === 'resolved' || initialView === 'all' ? initialView : 'active')
  const [search, setSearch] = useState(initialParams.get('q') ?? '')
  const [namespace, setNamespace] = useState(initialParams.get('namespace') ?? 'all')
  const [severity, setSeverity] = useState(initialParams.get('severity') ?? 'all')
  const [sort, setSort] = useState<IncidentSort>(initialSort === 'severity' || initialSort === 'resource' || initialSort === 'namespace' ? initialSort : 'updated')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [checkedIds, setCheckedIds] = useState<Set<string>>(new Set())
  const [bulkConfirm, setBulkConfirm] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [visibleCount, setVisibleCount] = useState(20)

  const { data: incidents = [], isLoading, isError, error, refetch } = useListIncidentsQuery()
  const { data: problemCases = [] } = useListProblemCasesQuery()
  const { data: runs = [], refetch: refetchRuns } = useListDiagnosticRunsQuery()
  const [resolveSelected] = useResolveIncidentMutation()

  useEffect(() => {
    const params = new URLSearchParams()
    if (view !== 'active') params.set('view', view)
    if (search.trim()) params.set('q', search.trim())
    if (namespace !== 'all') params.set('namespace', namespace)
    if (severity !== 'all') params.set('severity', severity)
    if (sort !== 'updated') params.set('sort', sort)
    const query = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`)
  }, [view, search, namespace, severity, sort])

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
  const visibleIncidents = filtered.slice(0, visibleCount)

  function changeView(next: InboxView) {
    setView(next)
    setSelectedId(null)
    setVisibleCount(20)
    setCheckedIds(new Set())
  }

  useEffect(() => {
    function handleKeyboard(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null
      if (target?.matches('input, textarea, select, [contenteditable="true"]')) return
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
        problemCases={problemCases}
        runs={runs.filter(run => run.incidentId === incident.metadata.name)}
        onAction={onAction}
        onResolved={onResolved}
        onSuggestRule={(p) => dispatch(setExclusionProposal(p))}
        onMarkFalsePositive={onMarkFalsePositive}
        onRefresh={refetchAll}
      />
    )
  }

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
      <Box sx={{ display: 'flex', alignItems: 'flex-end', gap: 1.5, mb: 2.25 }}>
        <Box sx={{ flex: 1 }}>
          <Typography component="h1" sx={{ fontSize: '1.35rem', fontWeight: 650, letterSpacing: '-0.02em' }}>Incident operations</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.35, fontSize: '0.78rem' }}>
            Triage cluster failures, review evidence, and coordinate resolution.
          </Typography>
        </Box>
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

      <Box sx={{ mb: 1.5, borderTop: '1px solid', borderBottom: '1px solid', borderColor: 'divider', background: 'rgba(255,255,255,0.01)' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
          <ToggleButtonGroup exclusive size="small" value={view} onChange={(_, value: InboxView | null) => value && changeView(value)} sx={{ alignSelf: 'stretch' }}>
            <ToggleButton value="active">Active {open.length}</ToggleButton>
            <ToggleButton value="resolved">Resolved {resolved.length}</ToggleButton>
            <ToggleButton value="all">All {allIncidents.length}</ToggleButton>
          </ToggleButtonGroup>
          <TextField
            size="small"
            value={search}
            onChange={event => { setSearch(event.target.value); setSelectedId(null); setVisibleCount(20) }}
            placeholder="Search incidents"
            sx={{ flex: '1 1 180px', minWidth: 0, my: 0.75 }}
            slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchIcon sx={{ fontSize: '1rem' }} /></InputAdornment> } }}
          />
          <FormControl size="small" sx={{ minWidth: 130, my: 0.75 }}>
            <Select value={namespace} onChange={event => { setNamespace(event.target.value); setSelectedId(null); setVisibleCount(20) }} displayEmpty>
              <MenuItem value="all">All namespaces</MenuItem>
              {namespaces.map(ns => <MenuItem key={ns} value={ns}>{ns}</MenuItem>)}
            </Select>
          </FormControl>
          <FormControl size="small" sx={{ minWidth: 110, my: 0.75, mr: 0.75 }}>
            <Select value={severity} onChange={event => { setSeverity(event.target.value); setSelectedId(null); setVisibleCount(20) }} displayEmpty>
              <MenuItem value="all">All severity</MenuItem>
              {['critical', 'high', 'medium', 'low'].map(level => <MenuItem key={level} value={level}>{level}</MenuItem>)}
            </Select>
          </FormControl>
        </Box>
      </Box>

      <Box sx={{ display: 'flex', alignItems: 'center', minHeight: 34, mb: 1, gap: 1 }}>
        <Typography variant="caption" color="text.secondary">
          {filtered.length} incident{filtered.length === 1 ? '' : 's'}
        </Typography>
        <Typography variant="caption" color="text.disabled" sx={{ display: { xs: 'none', md: 'block' } }}>
          · Navigate with ↑ ↓ or J K
        </Typography>
        <Box sx={{ flex: 1 }} />
        {checkedIds.size === 0 && (
          <FormControl size="small" sx={{ minWidth: 140 }}>
            <Select value={sort} onChange={event => setSort(event.target.value as IncidentSort)} displayEmpty aria-label="Sort incidents" sx={{ height: 30, fontSize: '0.75rem' }}>
              <MenuItem value="updated">Newest activity</MenuItem>
              <MenuItem value="severity">Highest severity</MenuItem>
              <MenuItem value="resource">Resource name</MenuItem>
              <MenuItem value="namespace">Namespace</MenuItem>
            </Select>
          </FormControl>
        )}
        {checkedIds.size > 0 && (
          <>
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
          </>
        )}
      </Box>

      {filtered.length === 0 ? (
        <Box sx={{ textAlign: 'center', py: 7, border: '1px dashed', borderColor: 'divider', borderRadius: 2 }}>
          <Typography sx={{ fontSize: '1.6rem', mb: 0.75 }}>{view === 'active' ? '🎉' : '⌕'}</Typography>
          <Typography color="text.secondary">No incidents match this view</Typography>
        </Box>
      ) : (
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', lg: 'minmax(360px, 0.82fr) minmax(460px, 1.18fr)' }, border: '1px solid', borderColor: 'divider', minHeight: 520, alignItems: 'start' }}>
          <Box sx={{ borderRight: { lg: '1px solid' }, borderColor: { lg: 'divider' }, minWidth: 0 }}>
            <Box sx={{ position: 'sticky', top: 0, zIndex: 2, display: 'grid', gridTemplateColumns: '32px 5px minmax(150px, 1fr) 88px 74px', gap: 1.25, px: 0, height: 32, alignItems: 'center', borderBottom: '1px solid', borderColor: 'divider', background: '#141815' }}>
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
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem' }}>Resource</Typography>
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem' }}>Signal</Typography>
              <Typography variant="caption" color="text.disabled" sx={{ textTransform: 'uppercase', letterSpacing: '0.08em', fontSize: '0.6rem', textAlign: 'right', pr: 1.25 }}>Age</Typography>
            </Box>
          <Stack spacing={0} sx={{ maxHeight: { lg: 'calc(100vh - 235px)' }, overflowY: { lg: 'auto' } }}>
            {visibleIncidents.map(incident => (
              <Box key={incident.metadata.name}>
                <IncidentInboxRow
                  incident={incident}
                  severity={incidentSeverity(incident, problemCases)}
                  selected={incident.metadata.name === selected?.metadata.name}
                  checked={checkedIds.has(incident.metadata.name)}
                  onClick={() => setSelectedId(incident.metadata.name)}
                  onCheck={() => toggleChecked(incident.metadata.name)}
                />
                {incident.metadata.name === selected?.metadata.name && (
                  <Box sx={{ display: { xs: 'block', lg: 'none' }, borderBottom: '1px solid', borderColor: 'divider' }}>
                    {renderIncidentCard(incident)}
                  </Box>
                )}
              </Box>
            ))}
            {visibleCount < filtered.length && (
              <Button
                variant="outlined"
                size="small"
                onClick={() => setVisibleCount(count => count + 20)}
                sx={{ mt: 0.5 }}
              >
                Load 20 more · {filtered.length - visibleCount} remaining
              </Button>
            )}
          </Stack>
          </Box>
          {selected && (
            <Box sx={{ display: { xs: 'none', lg: 'block' }, position: 'sticky', top: 0, minWidth: 0 }}>
              {renderIncidentCard(selected)}
            </Box>
          )}
        </Box>
      )}
    </Box>
  )
}
