import { Chip } from '@mui/material'

interface Props { confidence?: number }

export function ConfidenceBadge({ confidence }: Props) {
  if (confidence == null) return null
  const pct = Math.round(confidence * 100)
  const color = pct >= 80 ? 'success' : pct >= 50 ? 'warning' : 'error'
  return (
    <Chip
      label={`${pct}%`}
      color={color}
      size="small"
      sx={{ fontWeight: 700, fontSize: '0.7rem', height: 20 }}
    />
  )
}
