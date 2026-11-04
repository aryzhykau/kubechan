import { useState } from 'react'
import {
  Dialog, DialogTitle, DialogContent, DialogActions,
  Button, IconButton, Typography, Box, Stack, FormControlLabel, Checkbox,
  CircularProgress, Alert, Fade,
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import AddIcon from '@mui/icons-material/Add'
import HelpIcon from '@mui/icons-material/Help'
import { useAugmentIncidentMutation } from '../../store/api/incidentsApi'
import { useListNamespacesQuery } from '../../store/api/resourcesApi'
import { ResourcePicker, type ResourceEntry } from '../../components/ResourcePicker'
import type { SuggestedResource } from '../../api/index'

interface RelatedEntry { id: number; namespace: string; kind: string; apiGroup: string; name: string; evidenceSlices: string[] }

const EVIDENCE_SLICES = ['spec', 'status', 'conditions', 'events', 'labels', 'annotations', 'ownerRefs']
const WORKLOAD_KINDS = new Set(['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'Job'])

function defaultEvidenceSlices(kind: string) {
  return [...EVIDENCE_SLICES.slice(0, 5), ...(WORKLOAD_KINDS.has(kind) ? ['logs', 'metrics'] : [])]
}

export interface AugmentIncidentModalProps {
  incidentId: string
  defaultNamespace?: string
  suggestions: SuggestedResource[]
  onClose: () => void
  onAugmented: (diagnosticRunId: string, addedResources: Array<{ kind: string; name: string; namespace: string; apiGroup?: string }>) => void
}

let _rowId = 100

export function AugmentIncidentModal({ incidentId, defaultNamespace = '', suggestions, onClose, onAugmented }: AugmentIncidentModalProps) {
  const { data: namespaces = [], isLoading: loadingNS } = useListNamespacesQuery()
  const [augment] = useAugmentIncidentMutation()

  const [pendingEntry, setPendingEntry] = useState<ResourceEntry | null>(null)
  const [pickerKey, setPickerKey] = useState(0)
  const [added, setAdded] = useState<RelatedEntry[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)

  function commitResource() {
    if (!pendingEntry || added.length >= 5) return
    setAdded(prev => [...prev, { id: ++_rowId, ...pendingEntry, evidenceSlices: [...pendingEntry.evidenceSlices] }])
    setPendingEntry(null)
    setPickerKey(k => k + 1)
  }

  async function handleSubmit() {
    if (added.length === 0) return
    setSubmitting(true)
    setSubmitError(null)
    try {
      const relatedResources = added.map(r => ({
        kind: r.kind, name: r.name, namespace: r.namespace,
        apiGroup: r.apiGroup || undefined,
        evidenceSlices: r.evidenceSlices,
      }))
      const res = await augment({ incidentId, relatedResources }).unwrap()
      onAugmented(res.diagnosticRunId, relatedResources)
    } catch (e: unknown) {
      setSubmitError(String(e))
      setSubmitting(false)
    }
  }

  const canAdd = !!pendingEntry && added.length < 5

  return (
    <Dialog
      open
      onClose={onClose}
      maxWidth="md"
      fullWidth
      slots={{ transition: Fade }}
      slotProps={{ transition: { timeout: 180 } }}
    >
      <DialogTitle sx={{
        px: 3, pt: 2.5, pb: 2,
        background: 'background.paper',
        borderBottom: '1px solid',
        borderColor: 'divider',
        display: 'flex', alignItems: 'flex-start', gap: 1.5,
      }}>
        <Box sx={{
          mt: 0.2, width: 32, height: 32, borderRadius: 0,
          background: 'rgba(213,166,74,0.12)', border: '1px solid rgba(213,166,74,0.45)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          flexShrink: 0,
        }}>
          <HelpIcon sx={{ fontSize: '1rem', color: '#f0d28b' }} />
        </Box>
        <Box sx={{ flex: 1 }}>
          <Typography sx={{ fontWeight: 700, fontSize: '1rem', lineHeight: 1.25 }}>
            Add more context
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.3, display: 'block', lineHeight: 1.4 }}>
            Attach resources to this incident and trigger a new evidence snapshot and analysis
          </Typography>
        </Box>
        <IconButton size="small" onClick={onClose} sx={{ mt: 0.2 }}>
          <CloseIcon sx={{ fontSize: '1rem' }} />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ px: 3, py: 2.5, display: 'flex', flexDirection: 'column', gap: 2.5 }}>
        {suggestions.length > 0 && (
          <Box>
            <Typography variant="caption" sx={{ display: 'block', fontWeight: 700, mb: 1, textTransform: 'uppercase', letterSpacing: '0.07em' }}>
              Suggested by KubeChan
            </Typography>
            <Stack spacing={1}>
              {suggestions.map((s, i) => (
                <Box key={`${s.kind}-${i}`} sx={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) auto', gap: 1.5, alignItems: 'start', py: 0.5 }}>
                  <Box sx={{ display: 'flex', gap: 1, alignItems: 'baseline', minWidth: 0 }}>
                    <Typography variant="caption" sx={{ color: '#f0d28b', fontWeight: 700, flexShrink: 0 }}>{s.kind}</Typography>
                    <Typography variant="caption" color="text.secondary" sx={{ lineHeight: 1.45 }}>{s.reason}</Typography>
                  </Box>
                  <Button size="small" variant="outlined" disabled={added.length >= 5 || pendingEntry?.kind === s.kind} onClick={() => {
                    setPendingEntry({ namespace: defaultNamespace || namespaces[0] || '', kind: s.kind, apiGroup: '', name: '', evidenceSlices: defaultEvidenceSlices(s.kind) })
                    setPickerKey(k => k + 1)
                  }}>Choose resource</Button>
                </Box>
              ))}
            </Stack>
          </Box>
        )}

        {loadingNS ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
            <CircularProgress size={20} />
          </Box>
        ) : (
          <Box>
              <Typography variant="caption" sx={{ display: 'block', fontWeight: 700, mb: 1, textTransform: 'uppercase', letterSpacing: '0.07em' }}>
              {pendingEntry?.kind ? `Choose ${pendingEntry.kind}` : 'Add a different resource'}
            </Typography>
            <ResourcePicker
              key={pickerKey}
              value={pendingEntry}
              onChange={setPendingEntry}
              namespaces={namespaces}
              defaultNamespace={defaultNamespace}
            />
            <Box sx={{ mt: 1.5, display: 'flex', justifyContent: 'flex-end' }}>
              <Button variant="outlined" size="small" disabled={!canAdd} onClick={commitResource} startIcon={<AddIcon />}>
                Add
              </Button>
            </Box>
          </Box>
        )}

        <Box>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', mb: 1, borderTop: '1px solid', borderColor: 'divider', pt: 1.5 }}>
            <Typography variant="caption" sx={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.07em' }}>Added to next run</Typography>
            <Typography variant="caption" color="text.secondary">{added.length} / 5 resources</Typography>
          </Box>
          {added.length === 0 ? <Typography variant="caption" color="text.secondary">No resources added yet.</Typography> : (
            <Stack spacing={0.75}>
              {added.map(r => {
                const slices = [...EVIDENCE_SLICES, ...(WORKLOAD_KINDS.has(r.kind) ? ['logs', 'metrics'] : [])]
                return <Box key={r.id} sx={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) auto', gap: 1, p: 1.25, border: '1px solid', borderColor: 'divider', bgcolor: 'rgba(255,255,255,0.015)' }}>
                  <Box sx={{ minWidth: 0 }}>
                    <Typography variant="body2" sx={{ fontSize: '0.82rem', fontWeight: 700, overflowWrap: 'anywhere' }}>{r.kind}/{r.name}</Typography>
                    <Typography variant="caption" color="text.secondary">{r.namespace}{r.apiGroup ? ` · ${r.apiGroup}` : ''}</Typography>
                    <Box sx={{ mt: 0.5, borderTop: '1px solid', borderColor: 'divider', pt: 0.5 }}>
                      <Typography component="div" variant="caption" sx={{ color: '#f0d28b', fontSize: '0.7rem' }}>
                        Evidence for this resource · {r.evidenceSlices.length} selected
                      </Typography>
                      <Box sx={{ display: 'flex', flexWrap: 'wrap', columnGap: 1, rowGap: 0, mt: 0.25 }}>
                        {slices.map(slice => <FormControlLabel key={slice} sx={{ mr: 0.5, my: -0.25, '& .MuiFormControlLabel-label': { fontSize: '0.7rem', color: 'text.secondary' } }}
                          control={<Checkbox size="small" checked={r.evidenceSlices.includes(slice)} onChange={() => setAdded(prev => prev.map(entry => entry.id !== r.id ? entry : { ...entry, evidenceSlices: entry.evidenceSlices.includes(slice) ? entry.evidenceSlices.filter(value => value !== slice) : [...entry.evidenceSlices, slice] }))} sx={{ p: 0.5, '& .MuiSvgIcon-root': { fontSize: 16 } }} />}
                          label={slice} />)}
                      </Box>
                    </Box>
                  </Box>
                  <Button size="small" color="inherit" onClick={() => setAdded(prev => prev.filter(x => x.id !== r.id))} aria-label={`Remove ${r.kind}/${r.name}`}>Remove</Button>
                </Box>
              })}
            </Stack>
          )}
        </Box>

        <Alert severity="info" sx={{ fontSize: '0.78rem' }}>
          Evidence is configured independently for each resource. A new immutable run will be created; workloads are not changed. Secret values are never collected.
        </Alert>

        {submitError && <Alert severity="error" sx={{ fontSize: '0.78rem' }}>{submitError}</Alert>}
      </DialogContent>

      <DialogActions sx={{ px: 3, pb: 2.5, pt: 0, gap: 1 }}>
        <Button onClick={onClose} size="small" color="inherit">Cancel</Button>
        <Button
          variant="contained"
          size="small"
          disabled={added.length === 0 || submitting}
          onClick={handleSubmit}
        >
          {submitting ? <CircularProgress size={14} color="inherit" /> : 'Re-analyze'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
