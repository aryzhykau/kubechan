import { useState } from 'react'
import {
  Box, Typography, Stack, Chip, Button, IconButton,
  Switch, CircularProgress, Alert, Tooltip, Paper,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutlined'
import ShieldOutlinedIcon from '@mui/icons-material/ShieldOutlined'
import {
  useListExclusionRulesQuery,
  useSetExclusionRuleEnabledMutation,
  useDeleteExclusionRuleMutation,
} from '../../store/api/exclusionRulesApi'
import type { ExclusionRule, ExclusionRuleProposal } from '../../api/index'
import { ExclusionRuleModal } from '../exclusion-rules/ExclusionRuleModal'

// ── Rule Card ─────────────────────────────────────────────────────────────────

interface RuleCardProps {
  rule: ExclusionRule
}

function RuleCard({ rule }: RuleCardProps) {
  const [setEnabled] = useSetExclusionRuleEnabledMutation()
  const [deleteRule] = useDeleteExclusionRuleMutation()
  const [confirmDelete, setConfirmDelete] = useState(false)

  const targets    = rule.spec.targetResources ?? []
  const detectors  = rule.spec.detectors ?? []
  const selector   = rule.spec.selector
  const timeWindow = rule.spec.timeWindow

  return (
    <Paper elevation={0} sx={{ border: '1px solid #2a2f4a', borderRadius: '12px', p: 2.5, background: rule.spec.enabled ? 'rgba(99,102,241,0.04)' : 'rgba(255,255,255,0.01)' }}>
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 2 }}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
            <Typography sx={{ fontWeight: 700, fontSize: '0.9rem', color: rule.spec.enabled ? 'text.primary' : 'text.disabled', fontFamily: 'monospace' }}>
              {rule.name}
            </Typography>
            {!rule.spec.enabled && <Chip label="disabled" size="small" sx={{ fontSize: '0.65rem', height: 18 }} />}
          </Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5, lineHeight: 1.45 }}>
            {rule.spec.description}
          </Typography>

          {targets.length > 0 && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>Suppressed resources</Typography>
              <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', gap: '4px !important' }}>
                {targets.map((t, i) => (
                  <Chip key={i} label={`${t.apiGroup ? t.apiGroup + '/' : ''}${t.kind}/${t.namespace}/${t.name}`} size="small"
                    sx={{ fontFamily: 'monospace', fontSize: '0.7rem', background: 'rgba(99,102,241,0.1)', color: '#818cf8', border: '1px solid rgba(99,102,241,0.25)' }} />
                ))}
              </Stack>
            </Box>
          )}

          {detectors.length > 0 && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>Suppressed detectors</Typography>
              <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', gap: '4px !important' }}>
                {detectors.map((d, i) => (
                  <Chip key={i} label={d} size="small"
                    sx={{ fontFamily: 'monospace', fontSize: '0.7rem', background: 'rgba(251,191,36,0.1)', color: '#fbbf24', border: '1px solid rgba(251,191,36,0.25)' }} />
                ))}
              </Stack>
            </Box>
          )}

          {selector && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>Selector</Typography>
              <Typography sx={{ fontSize: '0.76rem', color: 'text.secondary', fontFamily: 'monospace' }}>
                {[
                  selector.namespace && `ns:${selector.namespace}`,
                  selector.kinds?.length && `kinds:[${selector.kinds.join(', ')}]`,
                  selector.matchLabels && Object.entries(selector.matchLabels).map(([k, v]) => `${k}=${v}`).join(', '),
                ].filter(Boolean).join('  ·  ')}
              </Typography>
            </Box>
          )}

          {timeWindow && (
            <Box sx={{ mb: 1 }}>
              <Typography sx={{ fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase', color: 'text.disabled', mb: 0.6 }}>Active window ({timeWindow.timezone})</Typography>
              <Stack spacing={0.4}>
                {timeWindow.periods.map((p, i) => (
                  <Typography key={i} sx={{ fontSize: '0.76rem', color: 'text.secondary', fontFamily: 'monospace' }}>
                    {p.days.join(', ')}  {p.start}–{p.end}
                  </Typography>
                ))}
              </Stack>
            </Box>
          )}

          {(rule.status.suppressedCount || rule.status.lastMatchedAt) && (
            <Typography sx={{ fontSize: '0.72rem', color: 'text.disabled', mt: 0.5 }}>
              {rule.status.suppressedCount ? `Suppressed ${rule.status.suppressedCount}×` : ''}
              {rule.status.lastMatchedAt ? `  ·  last match ${new Date(rule.status.lastMatchedAt).toLocaleString()}` : ''}
            </Typography>
          )}
        </Box>

        <Stack spacing={0.5} sx={{ alignItems: 'flex-end', flexShrink: 0 }}>
          <Tooltip title={rule.spec.enabled ? 'Disable rule' : 'Enable rule'}>
            <Switch
              size="small"
              checked={rule.spec.enabled}
              onChange={() => setEnabled({ name: rule.name, enabled: !rule.spec.enabled })}
            />
          </Tooltip>
          {confirmDelete ? (
            <Stack direction="row" spacing={0.5}>
              <Button size="small" onClick={() => setConfirmDelete(false)} sx={{ fontSize: '0.7rem', minWidth: 0, px: 0.75, color: 'text.disabled' }}>Cancel</Button>
              <Button size="small" color="error" onClick={async () => { await deleteRule(rule.name); setConfirmDelete(false) }} sx={{ fontSize: '0.7rem', minWidth: 0, px: 0.75 }}>
                Delete
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

interface Props {
  initialProposal?: ExclusionRuleProposal | null
}

export function ExclusionRulesPage({ initialProposal }: Props) {
  const { data: rules = [], isLoading, isError, error } = useListExclusionRulesQuery()
  const [showCreate, setShowCreate] = useState(false)

  return (
    <Box sx={{ maxWidth: 860, mx: 'auto' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Box sx={{ width: 38, height: 38, borderRadius: '10px', background: 'linear-gradient(135deg, #6366f1, #7c3aed)', display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: '0 4px 14px rgba(99,102,241,0.35)' }}>
            <ShieldOutlinedIcon sx={{ fontSize: '1.2rem', color: '#fff' }} />
          </Box>
          <Box>
            <Typography sx={{ fontWeight: 700, fontSize: '1.15rem', lineHeight: 1.2 }}>Exclusion Rules</Typography>
            <Typography variant="caption" color="text.secondary">Suppress false-positive incidents for known, expected conditions</Typography>
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

      {isLoading && <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}><CircularProgress size={28} /></Box>}
      {isError && <Alert severity="error" sx={{ mb: 2 }}>{String(error)}</Alert>}

      {!isLoading && rules.length === 0 && !isError && (
        <Paper elevation={0} sx={{ border: '1px dashed #2a2f4a', borderRadius: '12px', p: 5, textAlign: 'center' }}>
          <ShieldOutlinedIcon sx={{ fontSize: '2.5rem', color: 'text.disabled', mb: 1.5 }} />
          <Typography color="text.secondary">No exclusion rules yet</Typography>
          <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.5 }}>
            Create one to suppress false positives — like scaled-to-zero deployments during off-hours
          </Typography>
        </Paper>
      )}

      <Stack spacing={1.5}>
        {rules.map(rule => <RuleCard key={rule.name} rule={rule} />)}
      </Stack>

      <ExclusionRuleModal
        open={showCreate || !!initialProposal}
        onClose={() => setShowCreate(false)}
        proposal={initialProposal ?? null}
        onCreated={() => setShowCreate(false)}
      />
    </Box>
  )
}
