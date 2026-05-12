import { useState } from 'react'
import {
  Dialog, DialogTitle, DialogContent, DialogActions,
  Button, IconButton, Typography, Box, Stack, Chip,
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
    setAdded(prev => [...prev, { id: ++_rowId, ...pendingEntry }])
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
      maxWidth="sm"
      fullWidth
      slots={{ transition: Fade }}
      slotProps={{ transition: { timeout: 180 } }}
    >
      <DialogTitle sx={{
        px: 3, pt: 2.5, pb: 2,
        background: 'linear-gradient(135deg, rgba(245,158,11,0.12) 0%, rgba(99,102,241,0.06) 100%)',
        borderBottom: '1px solid',
        borderColor: 'divider',
        display: 'flex', alignItems: 'flex-start', gap: 1.5,
      }}>
        <Box sx={{
          mt: 0.2, width: 36, height: 36, borderRadius: '10px',
          background: 'linear-gradient(135deg, #f59e0b, #d97706)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          boxShadow: '0 4px 14px rgba(245,158,11,0.35)', flexShrink: 0,
        }}>
          <HelpIcon sx={{ fontSize: '1.15rem', color: '#fff' }} />
        </Box>
        <Box sx={{ flex: 1 }}>
          <Typography sx={{ fontWeight: 700, fontSize: '1rem', lineHeight: 1.25 }}>
            Add more context
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.3, display: 'block', lineHeight: 1.4 }}>
            KubeChan asked for more evidence — add resources and trigger a new analysis
          </Typography>
        </Box>
        <IconButton size="small" onClick={onClose} sx={{ mt: 0.2 }}>
          <CloseIcon sx={{ fontSize: '1rem' }} />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ px: 3, py: 3, display: 'flex', flexDirection: 'column', gap: 2.5 }}>
        {suggestions.length > 0 && (
          <Box>
            <Typography variant="caption" sx={{ display: 'block', fontWeight: 700, mb: 1, textTransform: 'uppercase', letterSpacing: '0.07em' }}>
              KubeChan suggests
            </Typography>
            <Stack spacing={1}>
              {suggestions.map((s, i) => (
                <Box key={i} sx={{ display: 'flex', gap: 1.5, alignItems: 'flex-start' }}>
                  <Chip label={s.kind} size="small" sx={{ background: 'rgba(245,158,11,0.12)', color: '#fbbf24', flexShrink: 0 }} />
                  <Typography variant="caption" color="text.secondary" sx={{ lineHeight: 1.45 }}>{s.reason}</Typography>
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
              Add resource
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

        {added.length > 0 && (
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.8 }}>
            {added.map(r => (
              <Chip
                key={r.id}
                label={`${r.apiGroup ? r.apiGroup + '/' : ''}${r.kind}/${r.name}`}
                size="small"
                onDelete={() => setAdded(prev => prev.filter(x => x.id !== r.id))}
                sx={{ background: 'rgba(99,102,241,0.12)', color: '#818cf8', border: '1px solid rgba(99,102,241,0.3)' }}
              />
            ))}
          </Box>
        )}

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
