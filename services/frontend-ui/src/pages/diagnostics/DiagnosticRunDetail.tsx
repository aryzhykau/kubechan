import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Box, Typography, Button, Tab, Tabs, Chip,
  Table, TableHead, TableBody, TableRow, TableCell,
  Paper, Skeleton,
} from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import { useGetDiagnosticRunEvidenceQuery } from '../../store/api/diagnosticsApi'
import { useGetDiagnosticRunAnalysisResultQuery } from '../../store/api/analysisApi'
import { fmtDate } from '../../components/shared/utils'
import type { Evidence, K8sEvent, PodLog } from '../../api/index'
import type { ChatterEvent } from '../../hooks/useKubeChan'

const TABS = [
  { id: 'logs', label: 'Logs' },
  { id: 'events', label: 'Events' },
  { id: 'config', label: 'Config' },
  { id: 'pvcs', label: 'PVCs' },
]

// ── Sub-components ────────────────────────────────────────────────────────────

function EventsTable({ events }: { events: K8sEvent[] }) {
  if (!events?.length) return <Typography variant="body2" color="text.secondary" sx={{ py: 2 }}>No events.</Typography>
  return (
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>Type</TableCell>
          <TableCell>Reason</TableCell>
          <TableCell>Count</TableCell>
          <TableCell>Last Seen</TableCell>
          <TableCell>Message</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {events.map((ev, i) => (
          <TableRow key={`${ev.reason}-${ev.lastTime}-${i}`} sx={{ '&:last-child td': { border: 0 } }}>
            <TableCell>
              <Chip label={ev.type} size="small" color={ev.type === 'Warning' ? 'warning' : 'default'} sx={{ fontSize: '0.65rem', height: 18 }} />
            </TableCell>
            <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.78rem' }}>{ev.reason}</TableCell>
            <TableCell>{ev.count}</TableCell>
            <TableCell sx={{ whiteSpace: 'nowrap', color: 'text.secondary', fontSize: '0.78rem' }}>{fmtDate(ev.lastTime, true)}</TableCell>
            <TableCell sx={{ fontSize: '0.82rem', maxWidth: 400, wordBreak: 'break-word' }}>{ev.message}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function LogsTab({ evidence }: { evidence: Evidence | null }) {
  const pods = evidence?.payload?.workloadPodLogs ?? []
  if (!pods.length) return <Typography variant="body2" color="text.secondary" sx={{ py: 2 }}>No pod logs collected.</Typography>
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
      {pods.map((pod: PodLog) => (
        <Box key={pod.podName}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <Typography sx={{ fontFamily: 'monospace', fontWeight: 700, fontSize: '0.85rem' }}>{pod.podName}</Typography>
            <Chip label={pod.phase} size="small" sx={{ fontSize: '0.65rem', height: 18 }} />
          </Box>
          {pod.logs
            ? <Paper component="pre" elevation={0} sx={{ p: 2, fontFamily: 'monospace', fontSize: '0.75rem', overflowX: 'auto', background: '#0d0f17', border: '1px solid #2a2f4a', borderRadius: '8px', whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 400, overflowY: 'auto' }}>{pod.logs}</Paper>
            : <Typography variant="body2" color="text.secondary">No logs.</Typography>
          }
          {pod.prevLogs && (
            <>
              <Typography variant="caption" color="text.secondary" sx={{ mt: 1.5, mb: 0.5, display: 'block', fontWeight: 600 }}>Previous container logs</Typography>
              <Paper component="pre" elevation={0} sx={{ p: 2, fontFamily: 'monospace', fontSize: '0.75rem', overflowX: 'auto', background: '#0d0f17', border: '1px solid #2a2f4a', borderRadius: '8px', opacity: 0.7, whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 300, overflowY: 'auto' }}>{pod.prevLogs}</Paper>
            </>
          )}
        </Box>
      ))}
    </Box>
  )
}

function EventsTab({ evidence }: { evidence: Evidence | null }) {
  const rootEvents = evidence?.payload?.rootResourceEvents ?? []
  const pods = evidence?.payload?.workloadPodLogs ?? []
  const root = evidence?.payload?.rootResource

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
      {rootEvents.length > 0 && (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 1, textTransform: 'uppercase', letterSpacing: '0.07em' }}>
            {root ? `${root.kind}/${root.name}` : 'Root resource events'}
          </Typography>
          <EventsTable events={rootEvents} />
        </Box>
      )}
      {pods.map((pod: PodLog) =>
        pod.events?.length > 0 && (
          <Box key={pod.podName}>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 1, textTransform: 'uppercase', letterSpacing: '0.07em' }}>
              Pod: {pod.podName}
            </Typography>
            <EventsTable events={pod.events} />
          </Box>
        )
      )}
      {rootEvents.length === 0 && pods.every(p => !p.events?.length) && (
        <Typography variant="body2" color="text.secondary">No events collected.</Typography>
      )}
    </Box>
  )
}

function ConfigTab({ evidence }: { evidence: Evidence | null }) {
  const pods = evidence?.payload?.workloadPodLogs ?? []
  const allCMs = pods.flatMap(p => (p.dependencies?.configMaps ?? []).map(cm => ({ pod: p.podName, ...cm })))
  const allSecrets = pods.flatMap(p => (p.dependencies?.secrets ?? []).map(s => ({ pod: p.podName, ...s })))

  if (!allCMs.length && !allSecrets.length) {
    return <Typography variant="body2" color="text.secondary">No ConfigMap or Secret dependencies collected.</Typography>
  }
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
      {allCMs.length > 0 && (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 1.5, textTransform: 'uppercase', letterSpacing: '0.07em' }}>ConfigMaps</Typography>
          {allCMs.map((cm, i) => (
            <Box key={i} sx={{ mb: 1.5, background: '#161923', border: '1px solid #2a2f4a', borderRadius: '8px', p: 1.5 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: cm.data ? 1 : 0 }}>
                <Typography sx={{ fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 600 }}>{cm.name}</Typography>
                {cm.missing && <Chip label="missing" size="small" color="error" sx={{ fontSize: '0.65rem', height: 18 }} />}
                {(cm.mountPaths?.length ?? 0) > 0 && (
                  <Typography variant="caption" color="text.secondary">{cm.mountPaths?.join(', ')}</Typography>
                )}
              </Box>
              {cm.data && Object.keys(cm.data).length > 0 && (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                  {Object.entries(cm.data).map(([k, v]) => (
                    <Box key={k}>
                      <Typography variant="caption" sx={{ color: '#818cf8', fontFamily: 'monospace', display: 'block', mb: 0.25 }}>{k}</Typography>
                      <Paper component="pre" elevation={0} sx={{ p: 1, fontFamily: 'monospace', fontSize: '0.75rem', background: '#0d0f17', border: '1px solid #2a2f4a', borderRadius: '6px', m: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{v}</Paper>
                    </Box>
                  ))}
                </Box>
              )}
            </Box>
          ))}
        </Box>
      )}
      {allSecrets.length > 0 && (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 1.5, textTransform: 'uppercase', letterSpacing: '0.07em' }}>Secrets</Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>
            {allSecrets.map((s, i) => (
              <Chip
                key={i}
                label={s.name}
                size="small"
                color={s.missing ? 'error' : 'default'}
                variant="outlined"
                sx={{ fontFamily: 'monospace', fontSize: '0.75rem' }}
              />
            ))}
          </Box>
        </Box>
      )}
    </Box>
  )
}

function PVCsTab({ evidence }: { evidence: Evidence | null }) {
  const pvcs = evidence?.payload?.pvcInfos ?? []
  if (!pvcs.length) return <Typography variant="body2" color="text.secondary">No PVC data collected.</Typography>
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
      {pvcs.map(pvc => (
        <Box key={pvc.name} sx={{ background: '#161923', border: '1px solid #2a2f4a', borderRadius: '8px', p: 1.5 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: pvc.events?.length ? 1 : 0 }}>
            <Typography sx={{ fontFamily: 'monospace', fontWeight: 600, fontSize: '0.85rem' }}>{pvc.name}</Typography>
            <Chip label={pvc.phase} size="small" sx={{ fontSize: '0.65rem', height: 18 }} />
            {pvc.storageClass && <Typography variant="caption" color="text.secondary">{pvc.storageClass}</Typography>}
            {pvc.requestedStorage && <Typography variant="caption" color="text.secondary">{pvc.requestedStorage}</Typography>}
          </Box>
          {pvc.events?.length > 0 && <EventsTable events={pvc.events} />}
        </Box>
      ))}
    </Box>
  )
}

// ── DiagnosticRunDetail page ──────────────────────────────────────────────────

interface Props {
  onResultLoaded?: (result: import('../../api/index').AnalysisResult | null, runId: string) => void
  onAction?: (e: ChatterEvent) => void
}

export function DiagnosticRunDetail({ onResultLoaded, onAction }: Props) {
  const { id: runId } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState(0)

  const { data: evidence, isLoading: evidenceLoading } = useGetDiagnosticRunEvidenceQuery(runId!, { skip: !runId })
  const { data: analysisResult, isLoading: analysisLoading } = useGetDiagnosticRunAnalysisResultQuery(runId!, { skip: !runId })

  useEffect(() => {
    if (!evidenceLoading && !analysisLoading) {
      onResultLoaded?.(analysisResult ?? null, runId!)
      onAction?.('open-run')
    }
  }, [evidenceLoading, analysisLoading])

  const isLoading = evidenceLoading || analysisLoading

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 3 }}>
        <Button
          startIcon={<ArrowBackIcon />}
          onClick={() => navigate('/diagnostics')}
          variant="text"
          size="small"
          sx={{ color: 'text.secondary' }}
        >
          Back
        </Button>
        <Typography
          sx={{ fontFamily: 'monospace', fontSize: '0.85rem', color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 400 }}
          title={runId}
        >
          {runId}
        </Typography>
      </Box>

      {isLoading ? (
        <Box>
          <Skeleton variant="rectangular" height={44} sx={{ borderRadius: '8px', mb: 2 }} />
          <Skeleton variant="rectangular" height={300} sx={{ borderRadius: '8px' }} />
        </Box>
      ) : (
        <>
          <Tabs
            value={activeTab}
            onChange={(_, v) => setActiveTab(v)}
            sx={{
              mb: 2,
              borderBottom: '1px solid #2a2f4a',
              '& .MuiTab-root': { textTransform: 'none', fontWeight: 600, fontSize: '0.875rem', minHeight: 44, minWidth: 80 },
            }}
          >
            {TABS.map((tab, i) => <Tab key={tab.id} label={tab.label} value={i} />)}
          </Tabs>
          <Box sx={{ py: 1 }}>
            {activeTab === 0 && <LogsTab evidence={evidence ?? null} />}
            {activeTab === 1 && <EventsTab evidence={evidence ?? null} />}
            {activeTab === 2 && <ConfigTab evidence={evidence ?? null} />}
            {activeTab === 3 && <PVCsTab evidence={evidence ?? null} />}
          </Box>
        </>
      )}
    </Box>
  )
}
