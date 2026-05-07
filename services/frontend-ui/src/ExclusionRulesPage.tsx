import { useState, useEffect, useCallback } from 'react'
import {
  Box,
  Typography,
  Stack,
  Chip,
  Button,
  IconButton,
  Switch,
  CircularProgress,
  Alert,
  Tooltip,
  Paper,
  ThemeProvider,
  createTheme,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutlined'
import ShieldOutlinedIcon from '@mui/icons-material/ShieldOutlined'
import { api, type ExclusionRule } from './api'
import { ExclusionRuleModal } from './ExclusionRuleModal'

const pageTheme = createTheme({
  palette: {
    mode: 'dark',
    background: { paper: '#16162a', default: '#0f0f1e' },
    primary:    { main: '#6366f1', light: '#818cf8', dark: '#4f46e5' },
    error:      { main: '#f87171' },
    success:    { main: '#34d399' },
    divider:    '#2a2a42',
    text:       { primary: '#e2e8f0', secondary: '#7c8498', disabled: '#4a4a62' },
  },
  shape: { borderRadius: 10 },
  typography: { fontFamily: 'inherit' },
  components: {
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
  },
})

// ── Rule Card ─────────────────────────────────────────────────────────────────

interface RuleCardProps {
  rule: ExclusionRule
  onToggle: (name: string, enabled: boolean) => void
  onDelete: (name: string) => void
}

function RuleCard({ rule, onToggle, onDelete }: RuleCardProps) {
  const [deleting, setDeleting] = useState(false)
  const [toggling, setToggling] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  async function handleToggle() {
    setToggling(true)
    try {
      await api.setExclusionRuleEnabled(rule.name, !rule.spec.enabled)
      onToggle(rule.name, !rule.spec.enabled)
    } catch {
      // ignore
    } finally {
      setToggling(false)
    }
  }

  async function handleDelete() {
    setDeleting(true)
    try {
      await api.deleteExclusionRule(rule.name)
      onDelete(rule.name)
    } catch {
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  const targets = rule.spec.targetResources || []
  const detectors = rule.spec.detectors || []
  const selector = rule.spec.selector
  const timeWindow = rule.spec.timeWindow

  return (
    <Paper elevation={0} sx={{ border: '1px solid #2a2a42', borderRadius: '12px', p: 2.5, background: rule.spec.enabled ? 'rgba(99,102,241,0.04)' : 'rgba(255,255,255,0.01)' }}>
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 2 }}>

        {/* Left: info */}
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
            <Typography sx={{ fontWeight: 700, fontSize: '0.9rem', color: rule.spec.enabled ? 'text.primary' : 'text.disabled', fontFamily: 'monospace' }}>
              {rule.name}
            </Typography>
            {!rule.spec.enabled && (
              <Chip label="disabled" size="small" sx={{ fontSize: '0.65rem', height: 18, background: 'rgba(255,255,255,0.06)', color: 'text.disabled' }} />
            )}
          </Box>
          <Typography sx={{ fontSize: '0.8rem', color: 'text.secondary', mb: 1.5, lineHeight: 1.45 }}>
            {rule.spec.description}
          </Typography>

          {/* Target resources */}
          {targets.length > 0 && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>
                Suppressed resources
              </Typography>
              <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap' }}>
                {targets.map((t, i) => (
                  <Chip
                    key={i}
                    label={`${t.apiGroup ? t.apiGroup + '/' : ''}${t.kind}/${t.namespace}/${t.name}`}
                    size="small"
                    sx={{ fontFamily: 'monospace', fontSize: '0.7rem', background: 'rgba(99,102,241,0.1)', color: '#818cf8', border: '1px solid rgba(99,102,241,0.25)' }}
                  />
                ))}
              </Stack>
            </Box>
          )}

          {/* Detectors */}
          {detectors.length > 0 && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>
                Suppressed detectors
              </Typography>
              <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap' }}>
                {detectors.map((d, i) => (
                  <Chip
                    key={i}
                    label={d}
                    size="small"
                    sx={{ fontFamily: 'monospace', fontSize: '0.7rem', background: 'rgba(251,191,36,0.1)', color: '#fbbf24', border: '1px solid rgba(251,191,36,0.25)' }}
                  />
                ))}
              </Stack>
            </Box>
          )}

          {/* Selector */}
          {selector && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>
                Selector
              </Typography>
              <Typography sx={{ fontSize: '0.76rem', color: 'text.secondary', fontFamily: 'monospace' }}>
                {[
                  selector.namespace && `ns:${selector.namespace}`,
                  selector.kinds?.length && `kinds:[${selector.kinds.join(', ')}]`,
                  selector.matchLabels && Object.entries(selector.matchLabels).map(([k, v]) => `${k}=${v}`).join(', '),
                ].filter(Boolean).join('  ·  ')}
              </Typography>
            </Box>
          )}

          {/* Time window */}
          {timeWindow && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>
                Active window ({timeWindow.timezone})
              </Typography>
              <Stack spacing={0.4}>
                {timeWindow.periods.map((p, i) => (
                  <Typography key={i} sx={{ fontSize: '0.76rem', color: 'text.secondary', fontFamily: 'monospace' }}>
                    {p.days.join(', ')}  {p.start}–{p.end}
                  </Typography>
                ))}
              </Stack>
            </Box>
          )}

          {/* Stats */}
          {(rule.status.suppressedCount || rule.status.lastMatchedAt) && (
            <Typography sx={{ fontSize: '0.72rem', color: 'text.disabled', mt: 0.5 }}>
              {rule.status.suppressedCount ? `Suppressed ${rule.status.suppressedCount}×` : ''}
              {rule.status.lastMatchedAt ? `  ·  last match ${new Date(rule.status.lastMatchedAt).toLocaleString()}` : ''}
            </Typography>
          )}
        </Box>

        {/* Right: controls */}
        <Stack spacing={0.5} sx={{ alignItems: 'flex-end', flexShrink: 0 }}>
          <Tooltip title={rule.spec.enabled ? 'Disable rule' : 'Enable rule'}>
            <Switch
              size="small"
              checked={rule.spec.enabled}
              disabled={toggling}
              onChange={handleToggle}
              sx={{
                '& .MuiSwitch-switchBase.Mui-checked': { color: '#6366f1' },
                '& .MuiSwitch-switchBase.Mui-checked + .MuiSwitch-track': { backgroundColor: '#6366f1' },
              }}
            />
          </Tooltip>
          {confirmDelete ? (
            <Stack direction="row" spacing={0.5}>
              <Button size="small" onClick={() => setConfirmDelete(false)} sx={{ fontSize: '0.7rem', color: 'text.disabled', minWidth: 0, px: 0.75 }}>Cancel</Button>
              <Button size="small" color="error" onClick={handleDelete} disabled={deleting} sx={{ fontSize: '0.7rem', minWidth: 0, px: 0.75 }}>
                {deleting ? <CircularProgress size={11} color="error" /> : 'Delete'}
              </Button>
            </Stack>
          ) : (
            <Tooltip title="Delete rule">
              <IconButton size="small" onClick={() => setConfirmDelete(true)} sx={{ color: 'text.disabled', '&:hover': { color: 'error.main' } }}>
                <DeleteOutlineIcon sx={{ fontSize: '1rem' }} />
              </IconButton>
            </Tooltip>
          )}
        </Stack>
      </Box>
    </Paper>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

export function ExclusionRulesPage() {
  const [rules, setRules] = useState<ExclusionRule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    api.listExclusionRules()
      .then(ruleList => { setRules(ruleList); setError(null) })
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { load() }, [load])

  function handleToggle(name: string, enabled: boolean) {
    setRules(prev => prev.map(r => r.name === name ? { ...r, spec: { ...r.spec, enabled } } : r))
  }

  function handleDelete(name: string) {
    setRules(prev => prev.filter(r => r.name !== name))
  }

  function handleCreated() {
    setShowCreate(false)
    load()
  }

  return (
    <ThemeProvider theme={pageTheme}>
      <Box sx={{ maxWidth: 860, mx: 'auto', py: 3, px: 2 }}>

        {/* Header */}
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
            <Box sx={{
              width: 38, height: 38, borderRadius: '10px',
              background: 'linear-gradient(135deg, #6366f1, #7c3aed)',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              boxShadow: '0 4px 14px rgba(99,102,241,0.35)',
            }}>
              <ShieldOutlinedIcon sx={{ fontSize: '1.2rem', color: '#fff' }} />
            </Box>
            <Box>
              <Typography sx={{ fontWeight: 700, fontSize: '1.15rem', lineHeight: 1.2 }}>
                Exclusion Rules
              </Typography>
              <Typography sx={{ fontSize: '0.75rem', color: 'text.secondary', mt: 0.2 }}>
                Suppress false-positive incidents for known, expected conditions
              </Typography>
            </Box>
          </Box>
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={() => setShowCreate(true)}
            sx={{ background: 'linear-gradient(135deg, #6366f1, #7c3aed)', '&:hover': { background: 'linear-gradient(135deg, #4f46e5, #6d28d9)' }, textTransform: 'none', fontWeight: 600, fontSize: '0.84rem' }}
          >
            New rule
          </Button>
        </Box>

        {/* Content */}
        {loading && (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
            <CircularProgress size={28} sx={{ color: '#6366f1' }} />
          </Box>
        )}

        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

        {!loading && rules.length === 0 && !error && (
          <Paper elevation={0} sx={{ border: '1px dashed #2a2a42', borderRadius: '12px', p: 5, textAlign: 'center' }}>
            <ShieldOutlinedIcon sx={{ fontSize: '2.5rem', color: 'text.disabled', mb: 1.5 }} />
            <Typography sx={{ color: 'text.secondary', fontSize: '0.9rem' }}>
              No exclusion rules yet
            </Typography>
            <Typography sx={{ color: 'text.disabled', fontSize: '0.78rem', mt: 0.5 }}>
              Create one to suppress false positives — like scaled-to-zero deployments during off-hours
            </Typography>
          </Paper>
        )}

        <Stack spacing={1.5}>
          {rules.map(rule => (
            <RuleCard key={rule.name} rule={rule} onToggle={handleToggle} onDelete={handleDelete} />
          ))}
        </Stack>
      </Box>

      <ExclusionRuleModal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        proposal={null}
        onCreated={handleCreated}
      />
    </ThemeProvider>
  )
}
