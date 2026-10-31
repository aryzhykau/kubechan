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
        background: 'rgba(255,255,255,0.025)',
        border: '1px solid #343b35',
        borderRadius: '3px',
        px: 1,
        py: 0.25,
        fontFamily: 'monospace',
        fontSize: '0.75rem',
      }}
    >
      <Typography component="span" sx={{ color: '#d5a64a', fontFamily: 'monospace', fontSize: '0.7rem', fontWeight: 600 }}>
        {kind}
      </Typography>
      <Typography component="span" sx={{ color: '#d9ddd9', fontFamily: 'monospace', fontSize: '0.75rem' }}>
        {name}
      </Typography>
      {namespace && (
        <Typography component="span" sx={{ color: '#7f8981', fontFamily: 'monospace', fontSize: '0.7rem' }}>
          {namespace}
        </Typography>
      )}
    </Box>
  )
}
