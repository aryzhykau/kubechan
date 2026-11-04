import { useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useAppDispatch } from '../../store/hooks'
import { setSpeaking, setIdle } from '../../store/slices/kubechanSlice'
import {
  Box, Typography, Button, Chip,
  Table, TableHead, TableBody, TableRow, TableCell,
  Paper, Skeleton,
  Accordion, AccordionSummary, AccordionDetails,
} from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import { useGetDiagnosticRunEvidenceQuery } from '../../store/api/diagnosticsApi'
import { useGetDiagnosticRunAnalysisResultQuery } from '../../store/api/analysisApi'
import { fmtDate } from '../../components/shared/utils'
import type { K8sEvent } from '../../api/index'
import type { ChatterEvent } from '../../hooks/useKubeChan'
import { buildResourceCards } from './buildResourceCards'
import type { FieldEntry, ResourceCard } from './buildResourceCards'

// ── Field sub-components ──────────────────────────────────────────────────────

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

function FieldRenderer({ field }: { field: FieldEntry }) {
  switch (field.type) {
    case 'text':
      return (
        <Box sx={{ display: 'flex', gap: 1.5, alignItems: 'baseline' }}>
          <Typography variant="caption" sx={{ color: '#818cf8', fontFamily: 'monospace', minWidth: 130, flexShrink: 0 }}>{field.key}</Typography>
          <Typography sx={{ fontFamily: 'monospace', fontSize: '0.82rem' }}>{field.value}</Typography>
        </Box>
      )
    case 'logs':
      return (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 600, mb: 0.5, textTransform: 'uppercase', letterSpacing: '0.07em' }}>{field.key}</Typography>
          <Paper component="pre" elevation={0} sx={{ p: 2, fontFamily: 'monospace', fontSize: '0.75rem', overflowX: 'auto', background: '#0d0f17', border: '1px solid #2a2f4a', borderRadius: '8px', whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 400, overflowY: 'auto', m: 0 }}>{field.value}</Paper>
        </Box>
      )
    case 'events':
      return (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 1, textTransform: 'uppercase', letterSpacing: '0.07em' }}>{field.key}</Typography>
          <EventsTable events={field.value} />
        </Box>
      )
    case 'kv':
      return (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 0.75, textTransform: 'uppercase', letterSpacing: '0.07em' }}>{field.key}</Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
            {Object.entries(field.value).map(([k, v]) => (
              <Box key={k}>
                <Typography variant="caption" sx={{ color: '#818cf8', fontFamily: 'monospace', display: 'block', mb: 0.25 }}>{k}</Typography>
                <Paper component="pre" elevation={0} sx={{ p: 1, fontFamily: 'monospace', fontSize: '0.75rem', background: '#0d0f17', border: '1px solid #2a2f4a', borderRadius: '6px', m: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{v}</Paper>
              </Box>
            ))}
          </Box>
        </Box>
      )
    case 'chips':
      return (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 0.75, textTransform: 'uppercase', letterSpacing: '0.07em' }}>{field.key}</Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>
            {field.value.map((s, i) => (
              <Chip key={i} label={s} size="small" variant="outlined" sx={{ fontFamily: 'monospace', fontSize: '0.75rem' }} />
            ))}
          </Box>
        </Box>
      )
    case 'json':
      return (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 700, mb: 0.5, textTransform: 'uppercase', letterSpacing: '0.07em' }}>{field.key}</Typography>
          <Paper component="pre" elevation={0} sx={{ p: 2, fontFamily: 'monospace', fontSize: '0.75rem', overflowX: 'auto', background: '#0d0f17', border: '1px solid #2a2f4a', borderRadius: '8px', whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 500, overflowY: 'auto', m: 0 }}>{JSON.stringify(field.value, null, 2)}</Paper>
        </Box>
      )
  }
}

function ResourceCardList({ cards }: { cards: ResourceCard[] }) {
  if (!cards.length) {
    return <Typography variant="body2" color="text.secondary" sx={{ py: 4, textAlign: 'center' }}>No evidence collected.</Typography>
  }
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
      {cards.map((card, i) => (
        <Accordion
          key={card.id}
          defaultExpanded={i === 0}
          disableGutters
          sx={{
            background: 'rgba(255,255,255,0.02)',
            border: '1px solid #2a2f4a',
            borderRadius: '10px !important',
            boxShadow: 'none',
            '&:before': { display: 'none' },
            '&.Mui-expanded': { borderColor: '#3d4470' },
          }}
        >
          <AccordionSummary
            expandIcon={<ExpandMoreIcon sx={{ color: 'text.secondary', fontSize: '1rem' }} />}
            sx={{ minHeight: 48, '& .MuiAccordionSummary-content': { alignItems: 'center', gap: 1, my: 0 } }}
          >
            <Chip
              label={card.kind}
              size="small"
              sx={{ fontFamily: 'monospace', fontSize: '0.65rem', height: 18, background: 'rgba(99,102,241,0.15)', color: '#818cf8', border: '1px solid rgba(99,102,241,0.3)' }}
            />
            <Typography sx={{ fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 600 }}>{card.name}</Typography>
            {card.namespace && (
              <Typography variant="caption" color="text.secondary">{card.namespace}</Typography>
            )}
            <Typography variant="caption" color="text.secondary" sx={{ ml: 'auto', mr: 0.5 }}>
              {card.fields.length} {card.fields.length === 1 ? 'field' : 'fields'}
            </Typography>
          </AccordionSummary>
          <AccordionDetails sx={{ pt: 0, pb: 2, px: 2 }}>
            {card.fields.length === 0
              ? <Typography variant="body2" color="text.secondary">No data collected.</Typography>
              : (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  {card.fields.map((f, fi) => <FieldRenderer key={`${f.type}-${f.key}-${fi}`} field={f} />)}
                </Box>
              )
            }
          </AccordionDetails>
        </Accordion>
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
  const dispatch = useAppDispatch()

  const { data: evidence, isLoading: evidenceLoading } = useGetDiagnosticRunEvidenceQuery(runId!, { skip: !runId })
  const { data: analysisResult, isLoading: analysisLoading } = useGetDiagnosticRunAnalysisResultQuery(runId!, { skip: !runId })

  useEffect(() => {
    if (!evidenceLoading && !analysisLoading) {
      if (analysisResult) {
        dispatch(setSpeaking({ result: analysisResult, incidentName: analysisResult.incidentId || runId! }))
      } else {
        dispatch(setIdle())
      }
      onResultLoaded?.(analysisResult ?? null, runId!)
      onAction?.('open-run')
    }
  }, [evidenceLoading, analysisLoading])

  const isLoading = evidenceLoading || analysisLoading
  const cards = buildResourceCards(evidence?.payload ?? {})

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
          <Skeleton variant="rectangular" height={48} sx={{ borderRadius: '10px', mb: 1 }} />
          <Skeleton variant="rectangular" height={48} sx={{ borderRadius: '10px', mb: 1 }} />
          <Skeleton variant="rectangular" height={48} sx={{ borderRadius: '10px' }} />
        </Box>
      ) : (
        <ResourceCardList cards={cards} />
      )}
    </Box>
  )
}
