import { Chip } from '@mui/material'

interface Props {
  status: string
  hasAnalysis?: boolean
}

export function StatusBadge({ status, hasAnalysis }: Props) {
  const label = hasAnalysis ? 'analyzed' : status === 'in_progress' ? 'analyzing' : status
  const color =
    hasAnalysis || status === 'completed' ? 'success'
    : status === 'failed' || status === 'error' ? 'error'
    : status === 'pending' || status === 'dispatched' || status === 'collecting' || status === 'in_progress' ? 'warning'
    : 'default'
  return <Chip label={label} color={color} size="small" sx={{ fontSize: '0.7rem', height: 20 }} />
}
