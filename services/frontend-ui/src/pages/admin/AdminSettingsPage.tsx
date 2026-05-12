import { useState } from 'react'
import { Box, Typography, Alert, TextField, Button, CircularProgress, Paper, Divider } from '@mui/material'
import { useGetAdminSettingsQuery, useUpdateAdminSettingsMutation } from '../../store/api/adminApi'

export function AdminSettingsPage() {
  const { data: settings, isLoading, error: loadErr } = useGetAdminSettingsQuery()
  const [updateSettings, { isLoading: saving }] = useUpdateAdminSettingsMutation()
  const [success, setSuccess] = useState('')
  const [saveErr, setSaveErr] = useState('')

  const [fields, setFields] = useState({
    debounce:    '',
    pending:     '',
    unavailable: '',
  })
  const [fieldsInit, setFieldsInit] = useState(false)

  if (settings && !fieldsInit) {
    setFields({
      debounce:    String(settings['detector.debounce_window_secs']       ?? 30),
      pending:     String(settings['detector.pending_threshold_secs']     ?? 300),
      unavailable: String(settings['detector.unavailable_threshold_secs'] ?? 300),
    })
    setFieldsInit(true)
  }

  async function handleSave() {
    setSaveErr(''); setSuccess('')
    try {
      await updateSettings({
        'detector.debounce_window_secs':       Number(fields.debounce),
        'detector.pending_threshold_secs':     Number(fields.pending),
        'detector.unavailable_threshold_secs': Number(fields.unavailable),
      }).unwrap()
      setSuccess('Settings saved.')
    } catch (e) {
      setSaveErr(String(e))
    }
  }

  function field(f: keyof typeof fields) {
    return (e: React.ChangeEvent<HTMLInputElement>) => setFields(p => ({ ...p, [f]: e.target.value }))
  }

  return (
    <Box sx={{ p: 3, maxWidth: 640 }}>
      <Typography variant="h5" gutterBottom>Detector Settings</Typography>
      {isLoading && <CircularProgress size={20} />}
      {loadErr && <Alert severity="error" sx={{ mb: 2 }}>Failed to load settings.</Alert>}
      {saveErr && <Alert severity="error" sx={{ mb: 2 }}>{saveErr}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Paper sx={{ p: 3, bgcolor: 'background.paper' }}>
        <Typography variant="subtitle2" gutterBottom sx={{ color: 'text.secondary' }}>Timing Thresholds (seconds)</Typography>
        <Divider sx={{ mb: 2 }} />

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField
            label="Debounce Window (secs)"
            type="number"
            size="small"
            value={fields.debounce}
            onChange={field('debounce')}
            helperText="How long to wait before triggering detection after a change."
          />
          <TextField
            label="Pending Threshold (secs)"
            type="number"
            size="small"
            value={fields.pending}
            onChange={field('pending')}
            helperText="Seconds before a pending pod is flagged as an issue."
          />
          <TextField
            label="Unavailable Threshold (secs)"
            type="number"
            size="small"
            value={fields.unavailable}
            onChange={field('unavailable')}
            helperText="Seconds before an unavailable deployment is flagged."
          />
        </Box>

        <Box sx={{ mt: 3 }}>
          <Button
            variant="contained"
            onClick={handleSave}
            disabled={saving || isLoading}
            startIcon={saving ? <CircularProgress size={16} /> : null}
          >
            {saving ? 'Saving…' : 'Save Settings'}
          </Button>
        </Box>
      </Paper>
    </Box>
  )
}
