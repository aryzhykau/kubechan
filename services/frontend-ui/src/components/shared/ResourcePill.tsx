import { Box, Typography } from '@mui/material'

interface Props {
  kind: string
  name: string
  namespace?: string
}

export function ResourcePill({ kind, name, namespace }: Props) {
  return (
    <Box
      component="span"
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 0.5,
        background: 'rgba(99,102,241,0.1)',
        border: '1px solid rgba(99,102,241,0.25)',
        borderRadius: '6px',
        px: 1,
        py: 0.25,
        fontFamily: 'monospace',
        fontSize: '0.75rem',
      }}
    >
      <Typography component="span" sx={{ color: '#818cf8', fontFamily: 'monospace', fontSize: '0.7rem', fontWeight: 600 }}>
        {kind}
      </Typography>
      <Typography component="span" sx={{ color: '#e2e8f0', fontFamily: 'monospace', fontSize: '0.75rem' }}>
        {name}
      </Typography>
      {namespace && (
        <Typography component="span" sx={{ color: '#64748b', fontFamily: 'monospace', fontSize: '0.7rem' }}>
          {namespace}
        </Typography>
      )}
    </Box>
  )
}
