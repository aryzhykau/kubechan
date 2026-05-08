import { useState } from 'react'
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
  TextField,
  Chip,
  Checkbox,
  FormControlLabel,
  Alert,
  CircularProgress,
  Fade,
  ThemeProvider,
  createTheme,
  alpha,
  Divider,
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import AddIcon from '@mui/icons-material/Add'
import ShieldOutlinedIcon from '@mui/icons-material/ShieldOutlined'
import DeleteOutlinedIcon from '@mui/icons-material/DeleteOutlined'
import { api, type ExclusionRuleProposal } from './api'

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
  },
})

const ALL_DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

interface Period { start: string; end: string; days: string[] }
interface TargetRow { namespace: string; kind: string; name: string }

export interface ExclusionRuleModalProps {
  open: boolean
  onClose: () => void
  proposal?: ExclusionRuleProposal | null
  onCreated: () => void
}

export function ExclusionRuleModal({ open, onClose, proposal, onCreated }: ExclusionRuleModalProps) {
  const [description, setDescription] = useState(proposal?.reason ?? '')
  const [detectors, setDetectors] = useState<string[]>(proposal?.detectors ?? [])
  const [detectorInput, setDetectorInput] = useState('')
  const [targets, setTargets] = useState<TargetRow[]>(
    proposal?.targetResources?.map(r => ({
      namespace: r.namespace ?? '',
      kind: r.kind ?? '',
      name: r.name ?? '',
    })) ?? [{ namespace: '', kind: '', name: '' }]
  )
  const [useTimeWindow, setUseTimeWindow] = useState(!!proposal?.timeWindow)
  const [timezone, setTimezone] = useState(proposal?.timeWindow?.timezone ?? 'UTC')
  const [periods, setPeriods] = useState<Period[]>(
    proposal?.timeWindow?.periods ?? [{ start: '08:00', end: '18:00', days: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri'] }]
  )
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)

  function addDetector() {
    const d = detectorInput.trim()
    if (!d || detectors.includes(d)) { setDetectorInput(''); return }
    setDetectors(prev => [...prev, d])
    setDetectorInput('')
  }

  function removeDetector(d: string) {
    setDetectors(prev => prev.filter(x => x !== d))
  }

  function updateTarget(idx: number, field: keyof TargetRow, val: string) {
    setTargets(prev => prev.map((t, i) => i === idx ? { ...t, [field]: val } : t))
  }

  function addTarget() {
    setTargets(prev => [...prev, { namespace: '', kind: '', name: '' }])
  }

  function removeTarget(idx: number) {
    setTargets(prev => prev.filter((_, i) => i !== idx))
  }

  function addPeriod() {
    setPeriods(prev => [...prev, { start: '08:00', end: '18:00', days: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri'] }])
  }

  function removePeriod(idx: number) {
    setPeriods(prev => prev.filter((_, i) => i !== idx))
  }

  function updatePeriod(idx: number, field: 'start' | 'end', val: string) {
    setPeriods(prev => prev.map((p, i) => i === idx ? { ...p, [field]: val } : p))
  }

  function toggleDay(periodIdx: number, day: string) {
    setPeriods(prev => prev.map((p, i) => {
      if (i !== periodIdx) return p
      return {
        ...p,
        days: p.days.includes(day) ? p.days.filter(d => d !== day) : [...p.days, day],
      }
    }))
  }

  async function handleSubmit() {
    if (!description.trim()) { setSubmitError('Description is required.'); return }
    const validTargets = targets.filter(t => t.kind && t.name)
    if (validTargets.length === 0) { setSubmitError('At least one target resource (kind + name) is required.'); return }

    const name = `rule-${Date.now()}`
    const spec: Parameters<typeof api.createExclusionRule>[1] = {
      description: description.trim(),
      enabled: true,
      detectors: detectors.length > 0 ? detectors : undefined,
      targetResources: validTargets.map(t => ({
        namespace: t.namespace,
        kind: t.kind,
        name: t.name,
      })),
      timeWindow: useTimeWindow && periods.length > 0
        ? { timezone, periods }
        : undefined,
    }

    setSubmitting(true)
    setSubmitError(null)
    try {
      await api.createExclusionRule(name, spec)
      onCreated()
      onClose()
    } catch (e: unknown) {
      setSubmitError(String(e))
      setSubmitting(false)
    }
  }

  return (
    <ThemeProvider theme={modalTheme}>
      <Dialog
        open={open}
        onClose={onClose}
        maxWidth="sm"
        fullWidth
        slots={{ transition: Fade }}
        slotProps={{ transition: { timeout: 180 }, paper: { sx: { borderRadius: '14px' } } }}
      >
        <DialogTitle sx={{
          px: 3, pt: 2.5, pb: 2,
          background: 'linear-gradient(135deg, rgba(245,158,11,0.1) 0%, rgba(99,102,241,0.06) 100%)',
          borderBottom: '1px solid', borderColor: 'divider',
          display: 'flex', alignItems: 'flex-start', gap: 1.5,
        }}>
          <Box sx={{
            mt: 0.2, width: 36, height: 36, borderRadius: '10px',
            background: 'linear-gradient(135deg, #f59e0b, #d97706)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 4px 14px rgba(245,158,11,0.35)', flexShrink: 0,
          }}>
            <ShieldOutlinedIcon sx={{ fontSize: '1.15rem', color: '#fff' }} />
          </Box>
          <Box sx={{ flex: 1 }}>
            <Typography sx={{ fontWeight: 700, fontSize: '1rem', lineHeight: 1.25, color: 'text.primary' }}>
              Create exclusion rule
            </Typography>
            <Typography sx={{ fontSize: '0.72rem', color: 'text.secondary', mt: 0.3 }}>
              Suppress specific detectors for known-good behaviour
            </Typography>
          </Box>
          <IconButton size="small" onClick={onClose} sx={{ color: 'text.disabled', mt: 0.2, '&:hover': { color: 'text.secondary' } }}>
            <CloseIcon sx={{ fontSize: '1rem' }} />
          </IconButton>
        </DialogTitle>

        <DialogContent sx={{ px: 3, py: 3, display: 'flex', flexDirection: 'column', gap: 2.5 }}>

          {/* Description */}
          <TextField
            label="Description"
            fullWidth
            size="small"
            multiline
            rows={2}
            value={description}
            onChange={e => setDescription(e.target.value)}
            placeholder="Why is this behaviour expected?"
            sx={{ mt: 3, '& .MuiInputBase-input': { fontSize: '0.84rem' } }}
          />

          {/* Detectors */}
          <Box>
            <Typography sx={{ fontSize: '0.7rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.secondary', mb: 1 }}>
              Detectors to suppress
            </Typography>
            <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75, mb: 1 }}>
              {detectors.map(d => (
                <Chip
                  key={d}
                  label={d}
                  size="small"
                  onDelete={() => removeDetector(d)}
                  sx={{ fontFamily: 'monospace', fontSize: '0.72rem', background: alpha('#6366f1', 0.15), color: '#818cf8', border: `1px solid ${alpha('#6366f1', 0.3)}` }}
                />
              ))}
            </Stack>
            <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
              <TextField
                size="small"
                placeholder="e.g. ServiceNoEndpoints"
                value={detectorInput}
                onChange={e => setDetectorInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addDetector() } }}
                sx={{ flex: 1, '& .MuiInputBase-input': { fontSize: '0.82rem' } }}
              />
              <IconButton size="small" onClick={addDetector} sx={{ border: '1px solid', borderColor: 'divider', borderRadius: 1.5, color: 'text.secondary' }}>
                <AddIcon sx={{ fontSize: '1rem' }} />
              </IconButton>
            </Stack>
          </Box>

          <Divider />

          {/* Target resources */}
          <Box>
            <Typography sx={{ fontSize: '0.7rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.secondary', mb: 1 }}>
              Target resources
            </Typography>
            {targets.map((t, i) => (
              <Stack key={i} direction="row" spacing={1} sx={{ mb: 1, alignItems: 'center' }}>
                <TextField size="small" label="Namespace" value={t.namespace} onChange={e => updateTarget(i, 'namespace', e.target.value)}
                  sx={{ minWidth: 100, flex: 1, '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }} />
                <TextField size="small" label="Kind" value={t.kind} onChange={e => updateTarget(i, 'kind', e.target.value)}
                  sx={{ minWidth: 120, flex: 1, '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }} />
                <TextField size="small" label="Name" value={t.name} onChange={e => updateTarget(i, 'name', e.target.value)}
                  sx={{ minWidth: 120, flex: 2, '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }} />
                <IconButton size="small" onClick={() => removeTarget(i)} disabled={targets.length === 1}
                  sx={{ color: 'text.disabled', '&:hover': { color: '#f87171' } }}>
                  <DeleteOutlinedIcon sx={{ fontSize: '1rem' }} />
                </IconButton>
              </Stack>
            ))}
            <Button size="small" startIcon={<AddIcon />} onClick={addTarget}
              sx={{ color: 'text.secondary', fontSize: '0.78rem', textTransform: 'none', borderColor: 'divider' }}>
              Add target
            </Button>
          </Box>

          <Divider />

          {/* Time window */}
          <Box>
            <FormControlLabel
              control={<Checkbox checked={useTimeWindow} onChange={e => setUseTimeWindow(e.target.checked)} size="small" />}
              label={<Typography sx={{ fontSize: '0.82rem', color: 'text.secondary' }}>Only suppress during specific time windows</Typography>}
            />
            {useTimeWindow && (
              <Box sx={{ mt: 1.5, pl: 1 }}>
                <TextField
                  size="small" label="Timezone (IANA)" value={timezone}
                  onChange={e => setTimezone(e.target.value)}
                  sx={{ mb: 2, width: 220, '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }}
                />
                {periods.map((p, i) => (
                  <Box key={i} sx={{ mb: 2, p: 1.5, border: '1px solid', borderColor: 'divider', borderRadius: 2 }}>
                    <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1.5 }}>
                      <TextField size="small" label="Start" value={p.start} onChange={e => updatePeriod(i, 'start', e.target.value)}
                        sx={{ width: 90, '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }} />
                      <TextField size="small" label="End" value={p.end} onChange={e => updatePeriod(i, 'end', e.target.value)}
                        sx={{ width: 90, '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }} />
                      <Box sx={{ flex: 1 }} />
                      <IconButton size="small" onClick={() => removePeriod(i)} disabled={periods.length === 1}
                        sx={{ color: 'text.disabled', '&:hover': { color: '#f87171' } }}>
                        <DeleteOutlinedIcon sx={{ fontSize: '0.9rem' }} />
                      </IconButton>
                    </Stack>
                    <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5 }}>
                      {ALL_DAYS.map(day => (
                        <Chip
                          key={day}
                          label={day}
                          size="small"
                          onClick={() => toggleDay(i, day)}
                          variant={p.days.includes(day) ? 'filled' : 'outlined'}
                          sx={{
                            fontSize: '0.7rem', height: 22, cursor: 'pointer',
                            ...(p.days.includes(day)
                              ? { bgcolor: alpha('#6366f1', 0.25), color: '#818cf8', borderColor: 'transparent' }
                              : { color: '#4a4a62', borderColor: '#2a2a42' }),
                          }}
                        />
                      ))}
                    </Stack>
                  </Box>
                ))}
                <Button size="small" startIcon={<AddIcon />} onClick={addPeriod}
                  sx={{ color: 'text.secondary', fontSize: '0.78rem', textTransform: 'none' }}>
                  Add period
                </Button>
              </Box>
            )}
          </Box>

          {submitError && <Alert severity="error" sx={{ fontSize: '0.78rem' }}>{submitError}</Alert>}
        </DialogContent>

        <DialogActions sx={{ px: 3, pb: 2.5, pt: 0, gap: 1 }}>
          <Button onClick={onClose} size="small" sx={{ color: 'text.secondary' }}>
            Cancel
          </Button>
          <Button
            variant="contained"
            size="small"
            disabled={submitting}
            onClick={handleSubmit}
            sx={{
              background: 'linear-gradient(135deg, #f59e0b, #d97706)',
              fontWeight: 700,
              '&:hover': { background: 'linear-gradient(135deg, #fbbf24, #f59e0b)' },
            }}
          >
            {submitting ? <CircularProgress size={14} sx={{ color: 'inherit' }} /> : 'Create Rule'}
          </Button>
        </DialogActions>
      </Dialog>
    </ThemeProvider>
  )
}
