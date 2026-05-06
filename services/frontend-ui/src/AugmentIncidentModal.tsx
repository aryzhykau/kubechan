import { useState, useEffect } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  IconButton,
  Typography,
  Box,
  Stack,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Chip,
  CircularProgress,
  Alert,
  ToggleButtonGroup,
  ToggleButton,
  Fade,
  ThemeProvider,
  createTheme,
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import AddIcon from '@mui/icons-material/Add'
import HelpIcon from '@mui/icons-material/Help'
import { api, type SuggestedResource, type ResourceItem } from './api'

const RELATED_KINDS = ['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'Job', 'Service', 'Ingress', 'PersistentVolumeClaim', 'ConfigMap'] as const

const modalTheme = createTheme({
  palette: {
    mode: 'dark',
    background: { paper: '#16162a', default: '#0f0f1e' },
    primary:    { main: '#6366f1', light: '#818cf8', dark: '#4f46e5' },
    secondary:  { main: '#f59e0b' },
    error:      { main: '#f87171' },
    success:    { main: '#34d399' },
    divider:    '#2a2a42',
    text:       { primary: '#e2e8f0', secondary: '#7c8498', disabled: '#4a4a62' },
  },
  shape: { borderRadius: 10 },
  typography: { fontFamily: 'inherit' },
  components: {
    MuiDialog: {
      styleOverrides: {
        paper: {
          backgroundImage: 'none',
          border: '1px solid #2a2a42',
          boxShadow: '0 32px 80px rgba(0,0,0,0.75)',
        },
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          '& .MuiOutlinedInput-notchedOutline': { borderColor: '#2a2a42' },
          '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: '#44446a' },
          '&.Mui-focused .MuiOutlinedInput-notchedOutline': { borderColor: '#6366f1', borderWidth: 1.5 },
        },
        input: { fontSize: '0.84rem' },
      },
    },
    MuiInputLabel: { styleOverrides: { root: { fontSize: '0.84rem' } } },
    MuiMenuItem: { styleOverrides: { root: { fontSize: '0.84rem' } } },
    MuiToggleButton: {
      styleOverrides: {
        root: {
          color: '#7c8498',
          borderColor: '#2a2a42',
          textTransform: 'none',
          fontSize: '0.78rem',
          fontWeight: 500,
          padding: '4px 14px',
          borderRadius: '999px !important',
          border: '1px solid #2a2a42 !important',
          '&.Mui-selected': {
            color: '#818cf8',
            background: 'rgba(99,102,241,0.15)',
            borderColor: '#6366f1 !important',
            fontWeight: 600,
          },
          '&:hover': { borderColor: '#44446a !important', color: '#e2e8f0', background: 'rgba(255,255,255,0.04)' },
        },
      },
    },
    MuiChip: {
      styleOverrides: { root: { fontFamily: 'monospace', fontSize: '0.72rem' } },
    },
  },
})

interface RelatedEntry { id: number; namespace: string; kind: string; name: string }

export interface AugmentIncidentModalProps {
  incidentId: string
  defaultNamespace?: string
  suggestions: SuggestedResource[]
  onClose: () => void
  onAugmented: (diagnosticRunId: string) => void
}

let _rowId = 100

export function AugmentIncidentModal({
  incidentId, defaultNamespace = '', suggestions, onClose, onAugmented,
}: AugmentIncidentModalProps) {
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [loadingNS, setLoadingNS] = useState(true)
  const [ns, setNs] = useState(defaultNamespace)

  const [pendingKind, setPendingKind] = useState('')
  const [pendingName, setPendingName] = useState('')
  const [pendingNameOptions, setPendingNameOptions] = useState<ResourceItem[]>([])
  const [loadingNames, setLoadingNames] = useState(false)

  const [added, setAdded] = useState<RelatedEntry[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)

  useEffect(() => {
    api.listNamespaces()
      .then(list => {
        setNamespaces(list)
        if (!defaultNamespace && list.length) setNs(list[0])
      })
      .catch(() => {})
      .finally(() => setLoadingNS(false))
  }, [defaultNamespace])

  useEffect(() => {
    if (!ns || !pendingKind) { setPendingNameOptions([]); setPendingName(''); return }
    setLoadingNames(true)
    setPendingName('')
    api.listResources(ns, pendingKind)
      .then(setPendingNameOptions)
      .catch(() => setPendingNameOptions([]))
      .finally(() => setLoadingNames(false))
  }, [ns, pendingKind])

  function commitResource() {
    if (!pendingKind || !pendingName || added.length >= 5) return
    setAdded(prev => [...prev, { id: ++_rowId, namespace: ns, kind: pendingKind, name: pendingName }])
    setPendingKind('')
    setPendingName('')
    setPendingNameOptions([])
  }

  async function handleSubmit() {
    if (added.length === 0) return
    setSubmitting(true)
    setSubmitError(null)
    try {
      const res = await api.augmentIncident(
        incidentId,
        added.map(r => ({ kind: r.kind, name: r.name, namespace: r.namespace })),
      )
      onAugmented(res.diagnosticRunId)
    } catch (e: unknown) {
      setSubmitError(String(e))
      setSubmitting(false)
    }
  }

  const canAdd = !!pendingKind && !!pendingName && added.length < 5

  return (
    <ThemeProvider theme={modalTheme}>
      <Dialog
        open
        onClose={onClose}
        maxWidth="sm"
        fullWidth
        slots={{ transition: Fade }}
        slotProps={{ transition: { timeout: 180 }, paper: { sx: { borderRadius: '14px' } } }}
      >
        <DialogTitle sx={{
          px: 3, pt: 2.5, pb: 2,
          background: 'linear-gradient(135deg, rgba(245,158,11,0.12) 0%, rgba(99,102,241,0.06) 100%)',
          borderBottom: '1px solid', borderColor: 'divider',
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
            <Typography sx={{ fontWeight: 700, fontSize: '1rem', lineHeight: 1.25, color: 'text.primary' }}>
              Add more context
            </Typography>
            <Typography sx={{ fontSize: '0.72rem', color: 'text.secondary', mt: 0.3, lineHeight: 1.4 }}>
              KubeChan asked for more evidence — add resources and trigger a new analysis
            </Typography>
          </Box>
          <IconButton size="small" onClick={onClose} sx={{ color: 'text.disabled', mt: 0.2, '&:hover': { color: 'text.secondary', background: 'rgba(255,255,255,0.06)' } }}>
            <CloseIcon sx={{ fontSize: '1rem' }} />
          </IconButton>
        </DialogTitle>

        <DialogContent sx={{ px: 3, py: 3, display: 'flex', flexDirection: 'column', gap: 2.5 }}>

          {/* LLM suggestions */}
          {suggestions.length > 0 && (
            <Box>
              <Typography sx={{ fontSize: '0.7rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.secondary', mb: 1 }}>
                KubeChan suggests
              </Typography>
              <Stack spacing={1}>
                {suggestions.map((s, i) => (
                  <Box key={i} sx={{ display: 'flex', gap: 1.5, alignItems: 'flex-start' }}>
                    <Chip
                      label={s.kind}
                      size="small"
                      sx={{ background: 'rgba(245,158,11,0.12)', color: '#fbbf24', border: '1px solid rgba(245,158,11,0.3)', flexShrink: 0 }}
                    />
                    <Typography sx={{ fontSize: '0.78rem', color: 'text.secondary', lineHeight: 1.45 }}>
                      {s.reason}
                    </Typography>
                  </Box>
                ))}
              </Stack>
            </Box>
          )}

          {/* Namespace picker */}
          <FormControl size="small" fullWidth>
            <InputLabel>Namespace</InputLabel>
            <Select
              value={ns}
              label="Namespace"
              onChange={e => setNs(e.target.value)}
              disabled={loadingNS}
              endAdornment={loadingNS ? <CircularProgress size={13} sx={{ mr: 1.5, color: 'text.disabled' }} /> : null}
            >
              {namespaces.map(n => <MenuItem key={n} value={n}>{n}</MenuItem>)}
            </Select>
          </FormControl>

          {/* Kind picker */}
          <Box>
            <Typography sx={{ fontSize: '0.72rem', color: 'text.secondary', mb: 1, fontWeight: 600, letterSpacing: '0.04em' }}>
              Resource kind
            </Typography>
            <ToggleButtonGroup
              exclusive value={pendingKind}
              onChange={(_, v) => { if (v) setPendingKind(v) }}
              size="small"
              sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.6, '& .MuiToggleButtonGroup-grouped': { margin: 0 } }}
            >
              {RELATED_KINDS.map(k => (
                <ToggleButton key={k} value={k}>{k}</ToggleButton>
              ))}
            </ToggleButtonGroup>
          </Box>

          {/* Name picker */}
          {pendingKind && (
            <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
              <FormControl size="small" sx={{ flex: 1 }}>
                <InputLabel>Name</InputLabel>
                <Select
                  value={pendingName}
                  label="Name"
                  onChange={e => setPendingName(e.target.value)}
                  disabled={loadingNames}
                  endAdornment={loadingNames ? <CircularProgress size={13} sx={{ mr: 1.5, color: 'text.disabled' }} /> : null}
                >
                  {pendingNameOptions.map(o => <MenuItem key={o.name} value={o.name}>{o.name}</MenuItem>)}
                  {!loadingNames && pendingNameOptions.length === 0 && (
                    <MenuItem value="" disabled>No {pendingKind}s found</MenuItem>
                  )}
                </Select>
              </FormControl>
              <Button
                variant="outlined"
                size="small"
                disabled={!canAdd}
                onClick={commitResource}
                startIcon={<AddIcon />}
                sx={{ whiteSpace: 'nowrap', borderColor: '#2a2a42', color: 'text.secondary', '&:hover': { borderColor: '#6366f1', color: '#818cf8' } }}
              >
                Add
              </Button>
            </Stack>
          )}

          {/* Added chips */}
          {added.length > 0 && (
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.8 }}>
              {added.map(r => (
                <Chip
                  key={r.id}
                  label={`${r.kind}/${r.name}`}
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
          <Button onClick={onClose} size="small" sx={{ color: 'text.secondary' }}>
            Cancel
          </Button>
          <Button
            variant="contained"
            size="small"
            disabled={added.length === 0 || submitting}
            onClick={handleSubmit}
            sx={{
              background: 'linear-gradient(135deg, #6366f1, #7c3aed)',
              fontWeight: 700,
              '&:hover': { background: 'linear-gradient(135deg, #818cf8, #8b5cf6)' },
            }}
          >
            {submitting ? <CircularProgress size={14} sx={{ color: 'inherit' }} /> : 'Re-analyze'}
          </Button>
        </DialogActions>
      </Dialog>
    </ThemeProvider>
  )
}
