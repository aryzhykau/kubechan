import { Chip } from '@mui/material'

interface Props {
  status: string
  hasAnalysis?: boolean
}

export function StatusBadge({ status, hasAnalysis }: Props) {
  const label = hasAnalysis ? 'analyzed' : status
  const color =
    hasAnalysis ? 'success'
    : status === 'pending' || status === 'collecting' ? 'warning'
    : 'default'
  return <Chip label={label} color={color} size="small" sx={{ fontSize: '0.7rem', height: 20 }} />
}
