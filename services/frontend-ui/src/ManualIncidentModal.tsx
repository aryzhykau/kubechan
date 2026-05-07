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
  TextField,
  ToggleButtonGroup,
  ToggleButton,
  CircularProgress,
  Alert,
  Chip,
  Divider,
  Tooltip,
  Fade,
  ThemeProvider,
  createTheme,
  alpha,
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import AddIcon from '@mui/icons-material/Add'
import BugReportOutlinedIcon from '@mui/icons-material/BugReportOutlined'
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome'
import { api, type ResourceItem } from './api'
import { ResourcePicker, type ResourceEntry } from './ResourcePicker'

const ROOT_KINDS = ['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'Job'] as const

// ── MUI dark theme matching the app palette ───────────────────────────────────
const modalTheme = createTheme({
  palette: {
    mode: 'dark',
    background: { paper: '#16162a', default: '#0f0f1e' },
    primary:    { main: '#6366f1', light: '#818cf8', dark: '#4f46e5' },
    secondary:  { main: '#7c3aed' },
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
          boxShadow: '0 32px 80px rgba(0,0,0,0.75), 0 0 0 1px rgba(99,102,241,0.08)',
        },
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          '& .MuiOutlinedInput-notchedOutline': { borderColor: '#2a2a42' },
          '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: '#44446a' },
          '&.Mui-focused .MuiOutlinedInput-notchedOutline': { borderColor: '#6366f1', borderWidth: 1.5 },
          '&.Mui-disabled .MuiOutlinedInput-notchedOutline': { borderColor: '#1e1e36' },
        },
        input: { fontSize: '0.84rem' },
      },
    },
    MuiInputLabel: {
      styleOverrides: { root: { fontSize: '0.84rem' } },
    },
    MuiMenuItem: {
      styleOverrides: { root: { fontSize: '0.84rem' } },
    },
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
          transition: 'all 0.15s',
          '&.Mui-selected': {
            color: '#818cf8',
            background: 'rgba(99,102,241,0.15)',
            borderColor: '#6366f1 !important',
            fontWeight: 600,
          },
          '&:hover': { borderColor: '#44446a !important', color: '#e2e8f0', background: 'rgba(255,255,255,0.04)' },
          '&.Mui-selected:hover': { background: 'rgba(99,102,241,0.22)' },
        },
      },
    },
    MuiChip: {
      styleOverrides: { root: { fontFamily: 'monospace', fontSize: '0.72rem' } },
    },
  },
})

// ── Section header with numbered badge ───────────────────────────────────────
function SectionLabel({ num, children, optional }: { num: number; children: string; optional?: boolean }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25, mb: 2 }}>
      <Box sx={{
        width: 22, height: 22, borderRadius: '50%',
        background: 'linear-gradient(135deg, #6366f1 0%, #818cf8 100%)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        fontSize: '0.62rem', fontWeight: 800, color: '#fff', flexShrink: 0,
        boxShadow: '0 2px 8px rgba(99,102,241,0.4)',
      }}>
        {num}
      </Box>
      <Typography sx={{ fontWeight: 700, fontSize: '0.7rem', letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.secondary' }}>
        {children}
      </Typography>
      {optional && (
        <Typography component="span" sx={{ fontSize: '0.66rem', color: 'text.disabled', fontStyle: 'italic' }}>
          optional
        </Typography>
      )}
    </Box>
  )
}

// ── Types ─────────────────────────────────────────────────────────────────────
interface RelatedEntry { id: number; namespace: string; kind: string; apiGroup: string; name: string; evidenceSlices: string[] }

export interface ManualIncidentModalProps {
  onClose: () => void
  onCreated: (incidentId: string, diagnosticRunId: string) => void
}

let _rowId = 0

// ── Component ─────────────────────────────────────────────────────────────────
export function ManualIncidentModal({ onClose, onCreated }: ManualIncidentModalProps) {
  // Root resource
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [loadingNS, setLoadingNS] = useState(true)
  const [rootNS, setRootNS] = useState('')
  const [rootKind, setRootKind] = useState('')
  const [rootName, setRootName] = useState('')
  const [rootNameOptions, setRootNameOptions] = useState<ResourceItem[]>([])
  const [loadingRootNames, setLoadingRootNames] = useState(false)

  // Related resources (committed chips)
  const [related, setRelated] = useState<RelatedEntry[]>([])

  // Pending related row (the "add" form)
  const [pendingEntry, setPendingEntry] = useState<ResourceEntry | null>(null)

  // Description
  const [userMessage, setUserMessage] = useState('')

  // Submit
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)

  // Load namespaces
  useEffect(() => {
    api.listNamespaces()
      .then(ns => { setNamespaces(ns); if (ns.length) { setRootNS(ns[0]) } })
      .catch(() => {})
      .finally(() => setLoadingNS(false))
  }, [])

  // Load root names when ns+kind change
  useEffect(() => {
    if (!rootNS || !rootKind) { setRootNameOptions([]); setRootName(''); return }
    setLoadingRootNames(true); setRootName('')
    api.listResources(rootNS, rootKind)
      .then(setRootNameOptions).catch(() => setRootNameOptions([]))
      .finally(() => setLoadingRootNames(false))
  }, [rootNS, rootKind])

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
      const res = await api.createManualIncident({
        namespace: rootNS, resourceKind: rootKind, resourceName: rootName,
        userMessage: msg,
        relatedResources: related.map(r => ({
          kind: r.kind, name: r.name, namespace: r.namespace || rootNS,
          apiGroup: r.apiGroup || undefined,
          evidenceSlices: r.evidenceSlices,
        })),
      })
      onCreated(res.incidentId, res.diagnosticRunId)
    } catch (e: unknown) {
      setSubmitError(String(e)); setSubmitting(false)
    }
  }

  const msgLen = userMessage.trim().length
  const canSubmit = !!rootKind && !!rootName && msgLen >= 10 && !submitting
  const canAddRelated = !!pendingEntry && related.length < 5

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
        {/* ── Title bar ──────────────────────────────────────────────────── */}
        <DialogTitle sx={{
          px: 3, pt: 2.5, pb: 2,
          background: 'linear-gradient(135deg, rgba(99,102,241,0.14) 0%, rgba(124,58,237,0.07) 100%)',
          borderBottom: '1px solid', borderColor: 'divider',
          display: 'flex', alignItems: 'flex-start', gap: 1.5,
        }}>
          <Box sx={{
            mt: 0.2,
            width: 36, height: 36, borderRadius: '10px',
            background: 'linear-gradient(135deg, #6366f1, #7c3aed)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 4px 14px rgba(99,102,241,0.45)', flexShrink: 0,
          }}>
            <BugReportOutlinedIcon sx={{ fontSize: '1.15rem', color: '#fff' }} />
          </Box>
          <Box sx={{ flex: 1 }}>
            <Typography sx={{ fontWeight: 700, fontSize: '1rem', lineHeight: 1.25, color: 'text.primary' }}>
              Report an issue
            </Typography>
            <Typography sx={{ fontSize: '0.72rem', color: 'text.secondary', mt: 0.3, lineHeight: 1.4 }}>
              Describe what you're seeing — KubeChan will investigate
            </Typography>
          </Box>
          <IconButton size="small" onClick={onClose} sx={{ color: 'text.disabled', mt: 0.2, '&:hover': { color: 'text.secondary', background: 'rgba(255,255,255,0.06)' } }}>
            <CloseIcon sx={{ fontSize: '1rem' }} />
          </IconButton>
        </DialogTitle>

        {/* ── Body ───────────────────────────────────────────────────────── */}
        <DialogContent sx={{ px: 3, py: 3, display: 'flex', flexDirection: 'column', gap: 0 }}>

          {/* 1 — Root resource */}
          <Box>
            <SectionLabel num={1}>Root resource</SectionLabel>
            <Stack spacing={2}>

              {/* Namespace */}
              <FormControl size="small" fullWidth>
                <InputLabel>Namespace</InputLabel>
                <Select
                  value={rootNS} label="Namespace"
                  onChange={e => setRootNS(e.target.value)}
                  disabled={loadingNS}
                  endAdornment={loadingNS ? <CircularProgress size={13} sx={{ mr: 1.5, color: 'text.disabled' }} /> : null}
                >
                  {namespaces.map(ns => <MenuItem key={ns} value={ns}>{ns}</MenuItem>)}
                  {namespaces.length === 0 && <MenuItem value="" disabled>No namespaces found</MenuItem>}
                </Select>
              </FormControl>

              {/* Kind — pill toggle group */}
              <Box>
                <Typography sx={{ fontSize: '0.72rem', color: 'text.secondary', mb: 1, fontWeight: 600, letterSpacing: '0.04em' }}>
                  Kind
                </Typography>
                <ToggleButtonGroup
                  exclusive value={rootKind}
                  onChange={(_, v) => { if (v) setRootKind(v) }}
                  size="small"
                  sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.6, '& .MuiToggleButtonGroup-grouped': { margin: 0 } }}
                >
                  {ROOT_KINDS.map(k => (
                    <ToggleButton key={k} value={k}>{k}</ToggleButton>
                  ))}
                </ToggleButtonGroup>
              </Box>

              {/* Resource name */}
              {rootKind && (
                loadingRootNames ? (
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, color: 'text.disabled', fontSize: '0.8rem', py: 0.5 }}>
                    <CircularProgress size={13} color="inherit" />
                    <span>Loading {rootKind}s…</span>
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
                  <TextField
                    size="small" fullWidth label="Resource name"
                    placeholder={`Enter ${rootKind} name…`}
                    value={rootName}
                    onChange={e => setRootName(e.target.value)}
                  />
                )
              )}
            </Stack>
          </Box>

          <Divider sx={{ my: 2.5 }} />

          {/* 2 — Related resources */}
          <Box>
            <SectionLabel num={2} optional>Related resources</SectionLabel>

            {/* Committed chips */}
            {related.length > 0 && (
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mb: 1.5 }}>
                {related.map(r => (
                  <Chip
                    key={r.id}
                    label={`${r.apiGroup ? r.apiGroup + '/' : ''}${r.kind}/${r.name}`}
                    size="small"
                    onDelete={() => setRelated(prev => prev.filter(x => x.id !== r.id))}
                    sx={{
                      background: alpha('#6366f1', 0.13),
                      border: `1px solid ${alpha('#6366f1', 0.35)}`,
                      color: '#818cf8',
                      '& .MuiChip-deleteIcon': {
                        color: alpha('#818cf8', 0.5),
                        '&:hover': { color: '#f87171' },
                      },
                    }}
                  />
                ))}
              </Box>
            )}

            {/* Add row */}
            {related.length < 5 && (
              <Box sx={{ border: '1px solid', borderColor: 'divider', borderRadius: 2, p: 1.5, background: alpha('#fff', 0.015) }}>
                <ResourcePicker
                  value={pendingEntry}
                  onChange={setPendingEntry}
                  namespaces={namespaces}
                  defaultNamespace={rootNS}
                />
                <Box sx={{ mt: 1.5, display: 'flex', justifyContent: 'flex-end' }}>
                  <Tooltip title={canAddRelated ? 'Add resource' : 'Select kind and name first'} placement="top">
                    <span>
                      <IconButton
                        size="small"
                        onClick={commitRelated}
                        disabled={!canAddRelated}
                        sx={{
                          border: '1px solid',
                          borderRadius: 1.5,
                          borderColor: canAddRelated ? 'primary.main' : 'divider',
                          color: canAddRelated ? 'primary.light' : 'text.disabled',
                          background: canAddRelated ? alpha('#6366f1', 0.12) : 'transparent',
                          transition: 'all 0.15s',
                          '&:hover:not(:disabled)': { background: alpha('#6366f1', 0.22) },
                        }}
                      >
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
              value={userMessage}
              onChange={e => setUserMessage(e.target.value)}
              sx={{ '& .MuiInputBase-input': { fontSize: '0.84rem', lineHeight: 1.6 } }}
              helperText={
                <Box component="span" sx={{ display: 'flex', justifyContent: 'space-between', width: '100%' }}>
                  <span>{msgLen < 10 ? `${10 - msgLen} more character${10 - msgLen !== 1 ? 's' : ''} needed` : '✓ ready'}</span>
                  <span>{userMessage.length}</span>
                </Box>
              }
              slotProps={{
                formHelperText: {
                  sx: {
                    color: msgLen >= 10 ? 'success.main' : 'text.disabled',
                    fontSize: '0.68rem',
                    mx: 0,
                    transition: 'color 0.2s',
                  },
                },
              }}
            />
          </Box>

          {submitError && (
            <Alert
              severity="error"
              sx={{ mt: 2, fontSize: '0.8rem', borderRadius: 2, border: `1px solid ${alpha('#f87171', 0.3)}` }}
            >
              {submitError}
            </Alert>
          )}
        </DialogContent>

        {/* ── Footer ─────────────────────────────────────────────────────── */}
        <DialogActions sx={{
          px: 3, py: 2,
          borderTop: '1px solid', borderColor: 'divider',
          background: alpha('#fff', 0.01),
          gap: 1,
        }}>
          <Button
            variant="outlined" size="small" onClick={onClose} disabled={submitting}
            sx={{ color: 'text.secondary', borderColor: 'divider', fontWeight: 500, '&:hover': { borderColor: 'text.secondary', background: alpha('#fff', 0.04) } }}
          >
            Cancel
          </Button>
          <Button
            variant="contained" size="small" onClick={handleSubmit} disabled={!canSubmit}
            startIcon={submitting
              ? <CircularProgress size={13} color="inherit" />
              : <AutoAwesomeIcon sx={{ fontSize: '0.9rem !important' }} />
            }
            sx={{
              fontWeight: 700,
              px: 2.5,
              background: canSubmit
                ? 'linear-gradient(135deg, #6366f1 0%, #818cf8 100%)'
                : undefined,
              boxShadow: canSubmit ? '0 3px 12px rgba(99,102,241,0.4)' : 'none',
              transition: 'all 0.2s',
              '&:hover:not(:disabled)': {
                background: 'linear-gradient(135deg, #4f46e5 0%, #6e78e8 100%)',
                boxShadow: '0 5px 20px rgba(99,102,241,0.55)',
                transform: 'translateY(-1px)',
              },
              '&:active:not(:disabled)': { transform: 'translateY(0)' },
            }}
          >
            {submitting ? 'Submitting…' : 'Submit to KubeChan'}
          </Button>
        </DialogActions>
      </Dialog>
    </ThemeProvider>
  )
}
