import { createTheme, alpha } from '@mui/material/styles'

export const theme = createTheme({
  palette: {
    mode: 'dark',
    background: {
      default: '#101311',
      paper: '#171b18',
    },
    primary: { main: '#d5a64a', light: '#e8c879', dark: '#9f762c' },
    secondary: { main: '#7da399' },
    warning: { main: '#d9a441' },
    success: { main: '#6da985' },
    error: { main: '#d96b68' },
    divider: '#2b322d',
    text: {
      primary: '#e5e7e3',
      secondary: '#929b94',
      disabled: '#626a64',
    },
  },
  shape: { borderRadius: 5 },
  typography: {
    fontFamily: "'IBM Plex Sans', 'Segoe UI', system-ui, sans-serif",
    h5: { fontWeight: 700 },
    h6: { fontWeight: 700 },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
            background: '#101311',
            scrollbarColor: '#303832 transparent',
          '&::-webkit-scrollbar': { width: 8 },
          '&::-webkit-scrollbar-track': { background: 'transparent' },
          '&::-webkit-scrollbar-thumb': { background: '#303832', borderRadius: 2 },
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
          borderRadius: 5,
          '&.MuiButton-containedPrimary': {
            background: '#d5a64a',
            color: '#15130d',
            '&:hover': { background: '#e0b65e' },
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
          border: '1px solid #2b322d',
          boxShadow: '0 32px 80px rgba(0,0,0,0.75)',
        },
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          '& .MuiOutlinedInput-notchedOutline': { borderColor: '#343b35' },
          '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: '#586159' },
          '&.Mui-focused .MuiOutlinedInput-notchedOutline': { borderColor: '#d5a64a', borderWidth: 1.5 },
          '&.Mui-disabled .MuiOutlinedInput-notchedOutline': { borderColor: '#252b27' },
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
          border: '1px solid #2b322d',
          borderRadius: 5,
        },
      },
    },
    MuiTableCell: {
      styleOverrides: {
        root: { borderBottom: '1px solid #2b322d', fontSize: '0.84rem' },
        head: { fontWeight: 700, color: '#7f8981', background: '#141815' },
      },
    },
    MuiAlert: {
      styleOverrides: {
        root: {
          borderRadius: 8,
          '&.MuiAlert-standardError': { background: alpha('#ef4444', 0.12), color: '#fca5a5' },
          '&.MuiAlert-standardSuccess': { background: alpha('#22c55e', 0.12), color: '#86efac' },
          '&.MuiAlert-standardWarning': { background: alpha('#f59e0b', 0.12), color: '#fcd34d' },
          '&.MuiAlert-standardInfo': { background: alpha('#7da399', 0.12), color: '#a9c4bd' },
        },
      },
    },
    MuiAccordion: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          background: '#1a1f1b',
          border: '1px solid #303832',
          borderRadius: '4px !important',
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
          color: '#8c968e',
          borderColor: '#343b35',
          textTransform: 'none',
          fontSize: '0.78rem',
          fontWeight: 500,
          padding: '4px 14px',
          borderRadius: '3px !important',
          border: '1px solid #343b35 !important',
          '&.Mui-selected': {
            color: '#e8c879',
            background: alpha('#d5a64a', 0.12),
            borderColor: '#d5a64a !important',
            fontWeight: 600,
          },
          '&:hover': { borderColor: '#69736b !important', color: '#e5e7e3', background: alpha('#ffffff', 0.04) },
        },
      },
    },
    MuiLinearProgress: {
      styleOverrides: {
        root: { borderRadius: 2, background: alpha('#d5a64a', 0.15) },
        bar: { borderRadius: 2, background: '#d5a64a' },
      },
    },
    MuiSkeleton: {
      styleOverrides: {
        root: { background: '#202620' },
      },
    },
  },
})
