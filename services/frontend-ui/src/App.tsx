import { useEffect } from 'react'
import { createBrowserRouter, RouterProvider, Navigate, Outlet, useNavigate, NavLink } from 'react-router-dom'
import { Box, CircularProgress, Button, Tooltip, Avatar, Chip } from '@mui/material'
import BugReportOutlinedIcon from '@mui/icons-material/BugReportOutlined'
import ListAltIcon from '@mui/icons-material/ListAlt'
import PeopleOutlinedIcon from '@mui/icons-material/PeopleOutlined'
import TuneIcon from '@mui/icons-material/Tune'
import ShieldOutlinedIcon from '@mui/icons-material/ShieldOutlined'
import SmartToyOutlinedIcon from '@mui/icons-material/SmartToyOutlined'
import LogoutIcon from '@mui/icons-material/Logout'
import { KubeChanSidebar } from './components/KubeChanSidebar'
import { IncidentList } from './pages/incidents/IncidentList'
import { DiagnosticsPage } from './pages/diagnostics/DiagnosticsPage'
import { DiagnosticRunDetail } from './pages/diagnostics/DiagnosticRunDetail'
import { UsersPage } from './pages/admin/UsersPage'
import { AdminSettingsPage } from './pages/admin/AdminSettingsPage'
import { ExclusionRulesPage } from './pages/admin/ExclusionRulesPage'
import { LLMSettingsPage } from './pages/llm/LLMSettingsPage'
import { LoginPage } from './pages/login/LoginPage'
import { ManualIncidentModal } from './pages/manual-incident/ManualIncidentModal'
import { ExclusionRuleModal } from './pages/exclusion-rules/ExclusionRuleModal'
import { useAppDispatch, useAppSelector } from './store/hooks'
import { setUser, clearUser, selectCurrentUser } from './store/slices/authSlice'
import { selectKubeChan, selectMoodLevel } from './store/slices/kubechanSlice'
import { closeManualModal, selectUI } from './store/slices/uiSlice'
import { useMeQuery } from './store/api/authApi'
import { incidentsApi } from './store/api/incidentsApi'
import { useKubeChan } from './hooks/useKubeChan'
import './app.css'

// ── Auth bootstrap inside Provider ───────────────────────────────────────────

function AuthGate({ children }: { children: React.ReactNode }) {
  const dispatch  = useAppDispatch()
  const current   = useAppSelector(selectCurrentUser)
  const { data: me, isLoading, isError } = useMeQuery()

  useEffect(() => {
    if (me)      dispatch(setUser(me))
    if (isError) dispatch(clearUser())
  }, [me, isError, dispatch])

  if (isLoading || current === undefined) {
    return (
      <Box sx={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <CircularProgress size={32} />
      </Box>
    )
  }

  if (current === null) return <Navigate to="/login" replace />
  return <>{children}</>
}

// ── Shell layout ──────────────────────────────────────────────────────────────

const NAV_ITEMS = [
  { to: '/',                 label: 'Incidents',       icon: <ListAltIcon sx={{ fontSize: '1rem' }} />,        end: true  },
  { to: '/diagnostics',      label: 'Diagnostics',     icon: <BugReportOutlinedIcon sx={{ fontSize: '1rem' }} />, end: false },
  { to: '/admin/exclusions', label: 'Exclusion Rules', icon: <ShieldOutlinedIcon sx={{ fontSize: '1rem' }} />,  end: false },
  { to: '/admin/users',      label: 'Users',           icon: <PeopleOutlinedIcon sx={{ fontSize: '1rem' }} />,  end: false },
  { to: '/admin/settings',   label: 'Settings',        icon: <TuneIcon sx={{ fontSize: '1rem' }} />,            end: false },
  { to: '/llm-settings',     label: 'LLM',             icon: <SmartToyOutlinedIcon sx={{ fontSize: '1rem' }} />,end: false },
]

function AppShell() {
  const dispatch    = useAppDispatch()
  const kubechan    = useAppSelector(selectKubeChan)
  const moodLevel   = useAppSelector(selectMoodLevel)
  const ui          = useAppSelector(selectUI)
  const navigate    = useNavigate()
  const currentUser = useAppSelector(selectCurrentUser)

  const { triggerChatter, handleAnalysisStart, handleRunResultLoaded, handleRate } = useKubeChan()

  return (
    <div className="app">
      {/* ── Top header ── */}
      <header className="app-header">
        <img src="/kubechan-idle-1.png" alt="KubeChan" className="app-logo" />
        <h1>KubeChan</h1>

        {/* Nav links */}
        <Box component="nav" sx={{ display: 'flex', alignItems: 'center', gap: 0.5, ml: 3 }}>
          {NAV_ITEMS.map(({ to, label, icon, end }) => (
            <NavLink key={to} to={to} end={end} style={{ textDecoration: 'none' }}>
              {({ isActive }) => (
                <Button
                  size="small"
                  startIcon={icon}
                  sx={{
                    fontSize: '0.78rem',
                    fontWeight: isActive ? 700 : 400,
                    color: isActive ? 'primary.light' : 'text.secondary',
                    bgcolor: isActive ? 'rgba(99,102,241,0.12)' : 'transparent',
                    borderBottom: isActive ? '2px solid' : '2px solid transparent',
                    borderColor: isActive ? 'primary.main' : 'transparent',
                    borderRadius: '6px 6px 0 0',
                    px: 1.5, py: 0.5,
                    minWidth: 0,
                    '&:hover': { bgcolor: 'rgba(255,255,255,0.05)', color: 'text.primary' },
                  }}
                >
                  {label}
                </Button>
              )}
            </NavLink>
          ))}
        </Box>

        {/* Spacer */}
        <Box sx={{ flex: 1 }} />

        {/* User chip + logout */}
        {currentUser && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Chip
              avatar={<Avatar sx={{ bgcolor: 'primary.dark', fontSize: '0.65rem !important' }}>{currentUser.username[0].toUpperCase()}</Avatar>}
              label={currentUser.username}
              size="small"
              variant="outlined"
              sx={{ fontSize: '0.75rem', borderColor: 'divider', color: 'text.secondary' }}
            />
            <Tooltip title="Logout">
              <Box
                component="button"
                onClick={() => { dispatch(clearUser()); navigate('/login') }}
                sx={{
                  display: 'flex', alignItems: 'center', gap: 0.5,
                  background: 'none', border: '1px solid', borderColor: 'divider',
                  borderRadius: 1, px: 1, py: 0.5, cursor: 'pointer',
                  color: 'text.secondary', fontSize: '0.75rem',
                  '&:hover': { borderColor: 'primary.main', color: 'text.primary' },
                }}
              >
                <LogoutIcon sx={{ fontSize: '0.9rem' }} />
              </Box>
            </Tooltip>
          </Box>
        )}
      </header>

      {/* ── Body: main + right sidebar ── */}
      <div className="app-body">
        <main className="app-main">
          <Outlet context={{ triggerChatter, handleAnalysisStart, handleRunResultLoaded, handleRate }} />
        </main>

        {/* KubeChan character sidebar — right side */}
        <KubeChanSidebar
          state={kubechan}
          moodLevel={moodLevel}
          onRate={handleRate}
        />
      </div>

      {ui.showManualModal && (
        <ManualIncidentModal
          onClose={() => dispatch(closeManualModal())}
          onCreated={(incidentId, diagnosticRunId) => {
            dispatch(closeManualModal())
            handleAnalysisStart(incidentId)
            navigate(`/diagnostics/${encodeURIComponent(diagnosticRunId)}`)
          }}
        />
      )}

      {ui.exclusionProposal && (
        <ExclusionRuleModal
          open={!!ui.exclusionProposal}
          onClose={() => dispatch({ type: 'ui/setExclusionProposal', payload: null })}
          proposal={ui.exclusionProposal}
          onCreated={() => {
            dispatch({ type: 'ui/setExclusionProposal', payload: null })
            dispatch(incidentsApi.util.invalidateTags([{ type: 'Incident', id: 'LIST' }]))
          }}
        />
      )}
    </div>
  )
}

// ── Router ────────────────────────────────────────────────────────────────────

const router = createBrowserRouter([
  {
    path: '/login',
    element: <LoginPage />,
  },
  {
    path: '/',
    element: (
      <AuthGate>
        <AppShell />
      </AuthGate>
    ),
    children: [
      { index: true,              element: <IncidentList /> },
      { path: 'diagnostics',      element: <DiagnosticsPage /> },
      { path: 'diagnostics/:id',  element: <DiagnosticRunDetail /> },
      { path: 'admin/users',      element: <UsersPage /> },
      { path: 'admin/settings',   element: <AdminSettingsPage /> },
      { path: 'admin/exclusions', element: <ExclusionRulesPage /> },
      { path: 'llm-settings',     element: <LLMSettingsPage /> },
      { path: '*',                element: <Navigate to="/" replace /> },
    ],
  },
])

// ── Root export ───────────────────────────────────────────────────────────────

function App() {
  return <RouterProvider router={router} />
}

export default App
