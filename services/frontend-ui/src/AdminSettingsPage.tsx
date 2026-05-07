import { useState, useEffect, useCallback } from 'react'
import {
  Box,
  Typography,
  Alert,
  TextField,
  Button,
  CircularProgress,
  Paper,
  ThemeProvider,
  createTheme,
  Divider,
} from '@mui/material'
import { api } from './api'

const pageTheme = createTheme({
  palette: {
    mode: 'dark',
    background: { paper: '#16162a', default: '#0f0f1e' },
    primary:    { main: '#6366f1', light: '#818cf8', dark: '#4f46e5' },
    secondary:  { main: '#f59e0b' },
    error:      { main: '#f87171' },
    success:    { main: '#34d399' },
    divider:    '#2a2a42',
  },
})

interface DetectorFields {
  debounce: string
  pending: string
  unavailable: string
}

export function AdminSettingsPage() {
  const [loading, setLoading]   = useState(true)
  const [saving, setSaving]     = useState(false)
  const [error, setError]       = useState('')
  const [success, setSuccess]   = useState('')
  const [fields, setFields]     = useState<DetectorFields>({ debounce: '30', pending: '300', unavailable: '300' })

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const s = await api.getAdminSettings()
      setFields({
        debounce:    String(s['detector.debounce_window_secs']       ?? 30),
        pending:     String(s['detector.pending_threshold_secs']     ?? 300),
        unavailable: String(s['detector.unavailable_threshold_secs'] ?? 300),
      })
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  async function handleSave() {
    setSaving(true)
    setError('')
    setSuccess('')
    try {
      const debounce    = parseInt(fields.debounce,    10)
      const pending     = parseInt(fields.pending,     10)
      const unavailable = parseInt(fields.unavailable, 10)
      if ([debounce, pending, unavailable].some(v => isNaN(v) || v <= 0)) {
        setError('All threshold values must be positive integers.')
        setSaving(false)
        return
      }
      await api.updateAdminSettings({
        'detector.debounce_window_secs':       debounce,
        'detector.pending_threshold_secs':     pending,
        'detector.unavailable_threshold_secs': unavailable,
      })
      setSuccess('Settings saved. The cluster-watcher will pick up the new thresholds within 60 seconds.')
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  function set(field: keyof DetectorFields) {
    return (e: React.ChangeEvent<HTMLInputElement>) =>
      setFields(prev => ({ ...prev, [field]: e.target.value }))
  }

  return (
    <ThemeProvider theme={pageTheme}>
      <Box sx={{ p: 3, maxWidth: 680, mx: 'auto' }}>
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 0.5 }}>
          System Settings
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          Admin-only configuration applied across all users and cluster-watchers.
        </Typography>

        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
            <CircularProgress size={32} />
          </Box>
        ) : (
          <>
            {error   && <Alert severity="error"   sx={{ mb: 2 }}>{error}</Alert>}
            {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

            <Paper elevation={2} sx={{ p: 3, mb: 3, bgcolor: 'background.paper' }}>
              <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
                Detector Thresholds
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                How long a problem must persist before it triggers an incident.
                The cluster-watcher polls these values every 60 s — no restart needed.
              </Typography>
              <Divider sx={{ mb: 2 }} />

              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
                <TextField
                  label="Pending Pod threshold (seconds)"
                  helperText="A Pod must be stuck in Pending for at least this long before an incident is raised. Default: 300"
                  type="number"
                  slotProps={{ htmlInput: { min: 1 } }}
                  value={fields.pending}
                  onChange={set('pending')}
                  fullWidth
                  size="small"
                />
                <TextField
                  label="Deployment unavailable threshold (seconds)"
                  helperText="A Deployment must have unavailable replicas for at least this long before an incident is raised. Default: 300"
                  type="number"
                  slotProps={{ htmlInput: { min: 1 } }}
                  value={fields.unavailable}
                  onChange={set('unavailable')}
                  fullWidth
                  size="small"
                />
                <TextField
                  label="Debounce window (seconds)"
                  helperText="Quiet period after a symptom is detected before the ProblemCase is actually created. Reduces noise from transient flaps. Default: 30"
                  type="number"
                  slotProps={{ htmlInput: { min: 1 } }}
                  value={fields.debounce}
                  onChange={set('debounce')}
                  fullWidth
                  size="small"
                />
              </Box>
            </Paper>

            <Box sx={{ display: 'flex', gap: 2 }}>
              <Button
                variant="contained"
                onClick={handleSave}
                disabled={saving}
                startIcon={saving ? <CircularProgress size={16} color="inherit" /> : null}
              >
                {saving ? 'Saving…' : 'Save Settings'}
              </Button>
              <Button variant="outlined" onClick={load} disabled={loading || saving}>
                Reset
              </Button>
            </Box>
          </>
        )}
      </Box>
    </ThemeProvider>
  )
}
