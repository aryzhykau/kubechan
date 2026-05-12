import { useState } from 'react'
import {
  Dialog, DialogTitle, DialogContent, DialogActions,
  Button, IconButton, Typography, Box, Stack,
  FormControl, InputLabel, Select, MenuItem,
  TextField, ToggleButtonGroup, ToggleButton,
  CircularProgress, Alert, Chip, Divider, Tooltip, Fade, alpha,
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import AddIcon from '@mui/icons-material/Add'
import BugReportOutlinedIcon from '@mui/icons-material/BugReportOutlined'
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome'
import { useCreateManualIncidentMutation } from '../../store/api/incidentsApi'
import { useListNamespacesQuery, useListResourcesQuery } from '../../store/api/resourcesApi'
import { ResourcePicker, type ResourceEntry } from '../../components/ResourcePicker'

const ROOT_KINDS = ['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'Job'] as const

function SectionLabel({ num, children, optional }: { num: number; children: string; optional?: boolean }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25, mb: 2 }}>
      <Box sx={{ width: 22, height: 22, borderRadius: '50%', background: 'linear-gradient(135deg, #6366f1 0%, #818cf8 100%)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '0.62rem', fontWeight: 800, color: '#fff', flexShrink: 0, boxShadow: '0 2px 8px rgba(99,102,241,0.4)' }}>{num}</Box>
      <Typography sx={{ fontWeight: 700, fontSize: '0.7rem', letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.secondary' }}>{children}</Typography>
      {optional && <Typography component="span" sx={{ fontSize: '0.66rem', color: 'text.disabled', fontStyle: 'italic' }}>optional</Typography>}
    </Box>
  )
}

interface RelatedEntry { id: number; namespace: string; kind: string; apiGroup: string; name: string; evidenceSlices: string[] }

export interface ManualIncidentModalProps {
  onClose: () => void
  onCreated: (incidentId: string, diagnosticRunId: string) => void
}

let _rowId = 0

export function ManualIncidentModal({ onClose, onCreated }: ManualIncidentModalProps) {
  const [createManualIncident] = useCreateManualIncidentMutation()

  const [rootNS, setRootNS]   = useState('')
  const [rootKind, setRootKind] = useState('')
  const [rootName, setRootName] = useState('')
  const [related, setRelated] = useState<RelatedEntry[]>([])
  const [pendingEntry, setPendingEntry] = useState<ResourceEntry | null>(null)
  const [userMessage, setUserMessage] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)

  const { data: namespaces = [], isLoading: loadingNS } = useListNamespacesQuery()
  const { data: rootNameOptions = [], isLoading: loadingRootNames } = useListResourcesQuery(
    { ns: rootNS, kind: rootKind },
    { skip: !rootNS || !rootKind }
  )

  function commitRelated() {
    if (!pendingEntry || related.length >= 5) return
    setRelated(prev => [...prev, { id: ++_rowId, ...pendingEntry }])
    setPendingEntry(null)
  }

  async function handleSubmit() {
    if (!rootKind || !rootName || !rootNS) return
    const msg = userMessage.trim()
    if (msg.length < 10) { setSubmitError('Description must be at least 10 characters.'); return }
    setSubmitting(true); setSubmitError(null)
    try {
      const res = await createManualIncident({
        namespace: rootNS, resourceKind: rootKind, resourceName: rootName,
        userMessage: msg,
        relatedResources: related.map(r => ({
          kind: r.kind, name: r.name, namespace: r.namespace || rootNS,
          apiGroup: r.apiGroup || undefined,
          evidenceSlices: r.evidenceSlices,
        })),
      }).unwrap()
      onCreated(res.incidentId, res.diagnosticRunId)
    } catch (e) {
      setSubmitError(String(e)); setSubmitting(false)
    }
  }

  const msgLen = userMessage.trim().length
  const canSubmit = !!rootKind && !!rootName && msgLen >= 10 && !submitting
  const canAddRelated = !!pendingEntry && related.length < 5

  return (
    <Dialog
      open
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      slots={{ transition: Fade }}
      slotProps={{ transition: { timeout: 180 }, paper: { sx: { borderRadius: '14px' } } }}
    >
      <DialogTitle sx={{ px: 3, pt: 2.5, pb: 2, background: 'linear-gradient(135deg, rgba(99,102,241,0.14) 0%, rgba(124,58,237,0.07) 100%)', borderBottom: '1px solid', borderColor: 'divider', display: 'flex', alignItems: 'flex-start', gap: 1.5 }}>
        <Box sx={{ mt: 0.2, width: 36, height: 36, borderRadius: '10px', background: 'linear-gradient(135deg, #6366f1, #7c3aed)', display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: '0 4px 14px rgba(99,102,241,0.45)', flexShrink: 0 }}>
          <BugReportOutlinedIcon sx={{ fontSize: '1.15rem', color: '#fff' }} />
        </Box>
        <Box sx={{ flex: 1 }}>
          <Typography sx={{ fontWeight: 700, fontSize: '1rem', lineHeight: 1.25 }}>Report an issue</Typography>
          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.3, display: 'block' }}>Describe what you're seeing — KubeChan will investigate</Typography>
        </Box>
        <IconButton size="small" onClick={onClose} sx={{ color: 'text.disabled', mt: 0.2, '&:hover': { color: 'text.secondary' } }}>
          <CloseIcon sx={{ fontSize: '1rem' }} />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ px: 3, py: 3, display: 'flex', flexDirection: 'column', gap: 0 }}>

        {/* 1 — Root resource */}
        <Box>
          <SectionLabel num={1}>Root resource</SectionLabel>
          <Stack spacing={2}>
            <FormControl size="small" fullWidth>
              <InputLabel>Namespace</InputLabel>
              <Select value={rootNS} label="Namespace" onChange={e => setRootNS(e.target.value)} disabled={loadingNS}
                endAdornment={loadingNS && <CircularProgress size={13} sx={{ mr: 1.5, color: 'text.disabled' }} />}>
                {namespaces.map(ns => <MenuItem key={ns} value={ns}>{ns}</MenuItem>)}
              </Select>
            </FormControl>

            <Box>
              <Typography sx={{ fontSize: '0.72rem', color: 'text.secondary', mb: 1, fontWeight: 600 }}>Kind</Typography>
              <ToggleButtonGroup exclusive value={rootKind} onChange={(_, v) => { if (v) setRootKind(v) }} size="small"
                sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.6, '& .MuiToggleButtonGroup-grouped': { margin: 0 } }}>
                {ROOT_KINDS.map(k => (
                  <ToggleButton key={k} value={k} sx={{ textTransform: 'none', fontSize: '0.78rem', borderRadius: '999px !important', border: '1px solid #2a2a42 !important' }}>
                    {k}
                  </ToggleButton>
                ))}
              </ToggleButtonGroup>
            </Box>

            {rootKind && (
              loadingRootNames ? (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, color: 'text.disabled', fontSize: '0.8rem', py: 0.5 }}>
                  <CircularProgress size={13} color="inherit" /><span>Loading {rootKind}s…</span>
                </Box>
              ) : rootNameOptions.length > 0 ? (
                <FormControl size="small" fullWidth>
                  <InputLabel>Resource name</InputLabel>
                  <Select value={rootName} label="Resource name" onChange={e => setRootName(e.target.value)}>
                    <MenuItem value=""><em style={{ color: '#7c8498' }}>— select —</em></MenuItem>
                    {rootNameOptions.map(r => <MenuItem key={r.name} value={r.name}>{r.name}</MenuItem>)}
                  </Select>
                </FormControl>
              ) : (
                <TextField size="small" fullWidth label="Resource name" placeholder={`Enter ${rootKind} name…`}
                  value={rootName} onChange={e => setRootName(e.target.value)} />
              )
            )}
          </Stack>
        </Box>

        <Divider sx={{ my: 2.5 }} />

        {/* 2 — Related resources */}
        <Box>
          <SectionLabel num={2} optional>Related resources</SectionLabel>
          {related.length > 0 && (
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mb: 1.5 }}>
              {related.map(r => (
                <Chip key={r.id}
                  label={`${r.apiGroup ? r.apiGroup + '/' : ''}${r.kind}/${r.name}`}
                  size="small"
                  onDelete={() => setRelated(prev => prev.filter(x => x.id !== r.id))}
                  sx={{ background: alpha('#6366f1', 0.13), border: `1px solid ${alpha('#6366f1', 0.35)}`, color: '#818cf8' }} />
              ))}
            </Box>
          )}
          {related.length < 5 && (
            <Box sx={{ border: '1px solid', borderColor: 'divider', borderRadius: 2, p: 1.5 }}>
              <ResourcePicker value={pendingEntry} onChange={setPendingEntry} namespaces={namespaces} defaultNamespace={rootNS} />
              <Box sx={{ mt: 1.5, display: 'flex', justifyContent: 'flex-end' }}>
                <Tooltip title={canAddRelated ? 'Add resource' : 'Select kind and name first'} placement="top">
                  <span>
                    <IconButton size="small" onClick={commitRelated} disabled={!canAddRelated}
                      sx={{ border: '1px solid', borderRadius: 1.5, borderColor: canAddRelated ? 'primary.main' : 'divider', color: canAddRelated ? 'primary.light' : 'text.disabled', background: canAddRelated ? alpha('#6366f1', 0.12) : 'transparent' }}>
                      <AddIcon sx={{ fontSize: '1rem' }} />
                    </IconButton>
                  </span>
                </Tooltip>
              </Box>
            </Box>
          )}
        </Box>

        <Divider sx={{ my: 2.5 }} />

        {/* 3 — Description */}
        <Box>
          <SectionLabel num={3}>Describe the problem</SectionLabel>
          <TextField
            multiline rows={4} fullWidth size="small"
            placeholder="What are you seeing? Include timeline, affected behaviour, recent changes…"
            value={userMessage} onChange={e => setUserMessage(e.target.value)}
            helperText={
              <Box component="span" sx={{ display: 'flex', justifyContent: 'space-between', width: '100%' }}>
                <span>{msgLen < 10 ? `${10 - msgLen} more character${10 - msgLen !== 1 ? 's' : ''} needed` : '✓ ready'}</span>
                <span>{userMessage.length}</span>
              </Box>
            }
            slotProps={{ formHelperText: { sx: { color: msgLen >= 10 ? 'success.main' : 'text.disabled', fontSize: '0.68rem', mx: 0 } } }}
          />
        </Box>

        {submitError && (
          <Alert severity="error" sx={{ mt: 2, fontSize: '0.8rem', borderRadius: 2 }}>{submitError}</Alert>
        )}
      </DialogContent>

      <DialogActions sx={{ px: 3, py: 2, borderTop: '1px solid', borderColor: 'divider', gap: 1 }}>
        <Button variant="outlined" size="small" onClick={onClose} disabled={submitting}
          sx={{ color: 'text.secondary', borderColor: 'divider' }}>
          Cancel
        </Button>
        <Button
          variant="contained" size="small" onClick={handleSubmit} disabled={!canSubmit}
          startIcon={submitting ? <CircularProgress size={13} color="inherit" /> : <AutoAwesomeIcon sx={{ fontSize: '0.9rem !important' }} />}
          sx={{ fontWeight: 700, px: 2.5, background: canSubmit ? 'linear-gradient(135deg, #6366f1 0%, #818cf8 100%)' : undefined,
            boxShadow: canSubmit ? '0 3px 12px rgba(99,102,241,0.4)' : 'none',
            '&:hover:not(:disabled)': { background: 'linear-gradient(135deg, #4f46e5 0%, #6e78e8 100%)', boxShadow: '0 5px 20px rgba(99,102,241,0.55)' } }}>
          {submitting ? 'Submitting…' : 'Submit to KubeChan'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
