import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box, Typography, Button, IconButton, Alert, Checkbox,
  FormControlLabel, Skeleton, Stack,
  Dialog, DialogTitle, DialogContent, DialogContentText, DialogActions,
} from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutlined'
import {
  useListDiagnosticRunsQuery,
  useDeleteDiagnosticRunMutation,
  useBulkDeleteDiagnosticRunsMutation,
} from '../../store/api/diagnosticsApi'
import { ConfidenceBadge } from '../../components/shared/ConfidenceBadge'
import { StatusBadge } from '../../components/shared/StatusBadge'
import { fmtDate } from '../../components/shared/utils'
import type { DiagnosticRunSummary } from '../../api/index'
import type { ChatterEvent } from '../../hooks/useKubeChan'

interface Props {
  onAction?: (e: ChatterEvent) => void
}

export function DiagnosticsPage({ onAction }: Props) {
  const navigate = useNavigate()
  const { data: runs = [], isLoading, isError, error, refetch } = useListDiagnosticRunsQuery()
  const [deleteSingle, { isLoading: deletingSingle }] = useDeleteDiagnosticRunMutation()
  const [bulkDelete, { isLoading: deletingBulk }] = useBulkDeleteDiagnosticRunsMutation()

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirmDelete, setConfirmDelete] = useState<{ ids: string[]; label: string } | null>(null)

  const deleting = deletingSingle || deletingBulk

  const allSelected = runs.length > 0 && selected.size === runs.length

  const toggleAll = () => setSelected(allSelected ? new Set() : new Set(runs.map(r => r.diagnosticRunId)))
  const toggleOne = (id: string) =>
    setSelected(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })

  async function confirmAndDelete() {
    if (!confirmDelete) return
    const { ids } = confirmDelete
    setConfirmDelete(null)
    setSelected(prev => {
      const next = new Set(prev)
      ids.forEach(id => next.delete(id))
      return next
    })
    if (ids.length === 1) {
      await deleteSingle(ids[0])
    } else {
      await bulkDelete(ids)
    }
    onAction?.('delete-run')
  }

  // Group by incidentId
  const groups: { incidentId: string; runs: DiagnosticRunSummary[] }[] = []
  const groupIndex = new Map<string, number>()
  for (const run of runs) {
    const key = run.incidentId || '(no incident)'
    if (!groupIndex.has(key)) {
      groupIndex.set(key, groups.length)
      groups.push({ incidentId: key, runs: [] })
    }
    groups[groupIndex.get(key)!].runs.push(run)
  }

  if (isLoading) {
    return (
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
          <Typography variant="h6" sx={{ flex: 1 }}>Diagnostic Runs</Typography>
        </Box>
        <Stack spacing={1}>{[1, 2, 3].map(i => <Skeleton key={i} variant="rounded" height={72} />)}</Stack>
      </Box>
    )
  }

  if (isError) {
    return (
      <Alert severity="error" action={<Button color="inherit" size="small" onClick={refetch}>Retry</Button>}>
        Failed to load diagnostic runs: {String(error)}
      </Alert>
    )
  }

  return (
    <Box>
      {/* Header */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 3 }}>
        <Typography variant="h6" sx={{ flex: 1 }}>Diagnostic Runs</Typography>
        {selected.size > 0 && (
          <Button
            size="small"
            variant="outlined"
            color="error"
            startIcon={<DeleteOutlineIcon />}
            disabled={deleting}
            onClick={() => setConfirmDelete({ ids: [...selected], label: `${selected.size} selected run${selected.size > 1 ? 's' : ''}` })}
          >
            Delete {selected.size} selected
          </Button>
        )}
        <IconButton size="small" onClick={refetch} disabled={isLoading} title="Refresh">
          <RefreshIcon fontSize="small" />
        </IconButton>
      </Box>

      {runs.length === 0 && (
        <Box sx={{ textAlign: 'center', py: 8 }}>
          <Typography color="text.secondary">No diagnostic runs yet. Trigger an analysis from the Incidents tab.</Typography>
        </Box>
      )}

      {runs.length > 0 && (
        <>
          <Box sx={{ mb: 1.5, pl: 0.5 }}>
            <FormControlLabel
              control={<Checkbox size="small" checked={allSelected} onChange={toggleAll} indeterminate={selected.size > 0 && !allSelected} />}
              label={<Typography variant="caption">{allSelected ? 'Deselect all' : `Select all (${runs.length})`}</Typography>}
            />
          </Box>

          <Stack spacing={2}>
            {groups.map(group => (
              <Box key={group.incidentId}>
                <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.07em', mb: 1, display: 'block', pl: 0.5 }}>
                  {group.incidentId}
                </Typography>
                <Stack spacing={1}>
                  {group.runs.map(run => (
                    <Box
                      key={run.diagnosticRunId}
                      onClick={() => { navigate(`/diagnostics/${encodeURIComponent(run.diagnosticRunId)}`); onAction?.('open-run') }}
                      sx={{
                        background: selected.has(run.diagnosticRunId) ? 'rgba(99,102,241,0.08)' : 'rgba(255,255,255,0.02)',
                        border: '1px solid',
                        borderColor: selected.has(run.diagnosticRunId) ? 'rgba(99,102,241,0.3)' : '#2a2f4a',
                        borderRadius: '10px',
                        p: 1.75,
                        cursor: 'pointer',
                        transition: 'all 0.15s',
                        '&:hover': { borderColor: '#3d4470', background: 'rgba(99,102,241,0.05)' },
                      }}
                    >
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                        <Box
                          onClick={(e) => { e.stopPropagation(); toggleOne(run.diagnosticRunId) }}
                          sx={{ display: 'flex' }}
                        >
                          <Checkbox
                            size="small"
                            checked={selected.has(run.diagnosticRunId)}
                            onChange={() => toggleOne(run.diagnosticRunId)}
                            onClick={(e) => e.stopPropagation()}
                          />
                        </Box>
                        <Typography
                          variant="body2"
                          sx={{ fontFamily: 'monospace', fontSize: '0.78rem', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                          title={run.diagnosticRunId}
                        >
                          {run.diagnosticRunId}
                        </Typography>
                        <StatusBadge status={run.status} hasAnalysis={!!run.analysisResultId} />
                        <ConfidenceBadge confidence={run.confidence} />
                        <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'nowrap' }}>
                          {fmtDate(run.requestedAt)}
                        </Typography>
                        <IconButton
                          size="small"
                          color="error"
                          disabled={deleting}
                          title="Delete run"
                          onClick={(e) => {
                            e.stopPropagation()
                            setConfirmDelete({ ids: [run.diagnosticRunId], label: 'this diagnostic run' })
                          }}
                          sx={{ opacity: 0.6, '&:hover': { opacity: 1 } }}
                        >
                          <DeleteOutlineIcon sx={{ fontSize: '1rem' }} />
                        </IconButton>
                      </Box>
                      {run.likelyRootCause && (
                        <Typography variant="caption" color="text.secondary" sx={{ mt: 0.75, display: 'block', pl: 5, lineHeight: 1.45 }}>
                          {run.likelyRootCause}
                        </Typography>
                      )}
                    </Box>
                  ))}
                </Stack>
              </Box>
            ))}
          </Stack>
        </>
      )}

      {/* Confirm delete dialog */}
      <Dialog open={!!confirmDelete} onClose={() => setConfirmDelete(null)} maxWidth="xs" fullWidth>
        <DialogTitle>Delete diagnostic run{confirmDelete?.ids.length !== 1 ? 's' : ''}?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Delete {confirmDelete?.label} and all associated evidence/analysis data? This cannot be undone.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmDelete(null)}>Cancel</Button>
          <Button variant="contained" color="error" onClick={confirmAndDelete}>Delete</Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
