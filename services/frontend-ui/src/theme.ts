import { createTheme, alpha } from '@mui/material/styles'

export const theme = createTheme({
  palette: {
    mode: 'dark',
    background: {
      default: '#0d0f17',
      paper: '#161923',
    },
    primary: { main: '#6366f1', light: '#818cf8', dark: '#4f46e5' },
    secondary: { main: '#f59e0b' },
    warning: { main: '#f59e0b' },
    success: { main: '#22c55e' },
    error: { main: '#ef4444' },
    divider: '#2a2f4a',
    text: {
      primary: '#e2e8f0',
      secondary: '#64748b',
      disabled: '#4a4a62',
    },
  },
  shape: { borderRadius: 8 },
  typography: {
    fontFamily: "'Inter', system-ui, sans-serif",
    h5: { fontWeight: 700 },
    h6: { fontWeight: 700 },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          background: '#0d0f17',
          scrollbarColor: '#2a2f4a transparent',
          '&::-webkit-scrollbar': { width: 8 },
          '&::-webkit-scrollbar-track': { background: 'transparent' },
          '&::-webkit-scrollbar-thumb': { background: '#2a2f4a', borderRadius: 4 },
        },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: { backgroundImage: 'none' },
      },
    },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: {
          textTransform: 'none',
          fontWeight: 600,
          borderRadius: 8,
          '&.MuiButton-containedPrimary': {
            background: 'linear-gradient(135deg, #6366f1, #4f46e5)',
            '&:hover': { background: 'linear-gradient(135deg, #818cf8, #6366f1)' },
          },
        },
      },
    },
    MuiChip: {
      styleOverrides: {
        root: { fontWeight: 600, fontSize: '0.72rem' },
      },
    },
    MuiDialog: {
      styleOverrides: {
        paper: {
          backgroundImage: 'none',
          border: '1px solid #2a2f4a',
          boxShadow: '0 32px 80px rgba(0,0,0,0.75)',
        },
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          '& .MuiOutlinedInput-notchedOutline': { borderColor: '#2a2f4a' },
          '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: '#3d4470' },
          '&.Mui-focused .MuiOutlinedInput-notchedOutline': { borderColor: '#6366f1', borderWidth: 1.5 },
          '&.Mui-disabled .MuiOutlinedInput-notchedOutline': { borderColor: '#1e1e36' },
        },
        input: { fontSize: '0.875rem' },
      },
    },
    MuiInputLabel: { styleOverrides: { root: { fontSize: '0.875rem' } } },
    MuiMenuItem: { styleOverrides: { root: { fontSize: '0.875rem' } } },
    MuiCard: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          border: '1px solid #2a2f4a',
          borderRadius: 12,
        },
      },
    },
    MuiTableCell: {
      styleOverrides: {
        root: { borderBottom: '1px solid #2a2f4a', fontSize: '0.84rem' },
        head: { fontWeight: 700, color: '#64748b', background: '#161923' },
      },
    },
    MuiAlert: {
      styleOverrides: {
        root: {
          borderRadius: 8,
          '&.MuiAlert-standardError': { background: alpha('#ef4444', 0.12), color: '#fca5a5' },
          '&.MuiAlert-standardSuccess': { background: alpha('#22c55e', 0.12), color: '#86efac' },
          '&.MuiAlert-standardWarning': { background: alpha('#f59e0b', 0.12), color: '#fcd34d' },
          '&.MuiAlert-standardInfo': { background: alpha('#6366f1', 0.12), color: '#a5b4fc' },
        },
      },
    },
    MuiAccordion: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          background: '#1e2235',
          border: '1px solid #2a2f4a',
          borderRadius: '8px !important',
          '&:before': { display: 'none' },
          '&.Mui-expanded': { margin: 0 },
        },
      },
    },
    MuiAccordionSummary: {
      styleOverrides: {
        root: { minHeight: 44, '&.Mui-expanded': { minHeight: 44 } },
        content: { '&.Mui-expanded': { margin: '12px 0' } },
      },
    },
    MuiToggleButton: {
      styleOverrides: {
        root: {
          color: '#64748b',
          borderColor: '#2a2f4a',
          textTransform: 'none',
          fontSize: '0.78rem',
          fontWeight: 500,
          padding: '4px 14px',
          borderRadius: '999px !important',
          border: '1px solid #2a2f4a !important',
          '&.Mui-selected': {
            color: '#818cf8',
            background: alpha('#6366f1', 0.15),
            borderColor: '#6366f1 !important',
            fontWeight: 600,
          },
          '&:hover': { borderColor: '#3d4470 !important', color: '#e2e8f0', background: alpha('#ffffff', 0.04) },
        },
      },
    },
    MuiLinearProgress: {
      styleOverrides: {
        root: { borderRadius: 4, background: alpha('#6366f1', 0.15) },
        bar: { borderRadius: 4, background: '#6366f1' },
      },
    },
    MuiSkeleton: {
      styleOverrides: {
        root: { background: '#1e2235' },
      },
    },
  },
})
