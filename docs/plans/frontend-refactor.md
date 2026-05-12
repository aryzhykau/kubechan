# Frontend Refactor Plan

**Status:** Planning  
**Scope:** `services/frontend-ui/`  
**Phases:** 3 (Structure → State Management → Polish & MUI)

---

## Context & Motivation

The current frontend is a functional React 19 / TypeScript / Vite app but has accumulated structural debt:

- **Flat `src/`**: 19 files — pages, modals, hooks, API layer, persona logic, and styles all in one directory.
- **God component**: `App.tsx` (~350 lines) owns auth, routing, KubeChan persona state, 6 timer refs, WebSocket handling, and all event callbacks.
- **Duplicated code**: `ConfidenceBadge` and `fmtDate` exist in multiple files; chatter-timer logic copy-pasted verbatim 3 times.
- **Manual data-fetching**: Every page manually manages `loading`, `error`, and `data` state via `useState` + `useEffect`.
- **Mixed UI system**: MUI 9 is installed and used in `ExclusionRuleModal` (with an inline `ThemeProvider`), but the rest of the app uses raw HTML + CSS classes. Two design languages coexist.
- **Manual routing**: No `react-router-dom`; navigation is a `View` union in state. No deep-linking, no browser back/forward.
- **Security**: WebSocket token passed as a URL query parameter (ends up in server logs and browser history).

---

## Phase 1 — Project Structure Reorganization

**Goal:** Establish the canonical folder layout. Zero logic changes — pure file moves and import path updates. The app must build and run identically before and after.

### Target Directory Layout

```
services/frontend-ui/src/
├── main.tsx
├── App.tsx
├── app.css                        # kept; split in Phase 3
│
├── api/
│   └── index.ts                   # renamed from api.ts; no content changes
│
├── hooks/
│   └── useWebSocket.ts            # moved from src/
│
├── persona/
│   └── chatter.ts                 # moved from src/
│
├── components/
│   ├── KubeChanSidebar.tsx        # moved from src/
│   └── ResourcePicker.tsx         # moved from src/
│
└── pages/
    ├── incidents/
    │   ├── IncidentList.tsx
    │   └── AugmentIncidentModal.tsx
    ├── diagnostics/
    │   ├── DiagnosticsPage.tsx
    │   └── DiagnosticRunDetail.tsx
    ├── admin/
    │   ├── UsersPage.tsx
    │   ├── AdminSettingsPage.tsx
    │   └── ExclusionRulesPage.tsx
    ├── exclusion-rules/
    │   └── ExclusionRuleModal.tsx
    ├── manual-incident/
    │   └── ManualIncidentModal.tsx
    ├── llm/
    │   └── LLMSettingsPage.tsx
    └── login/
        └── LoginPage.tsx
```

### Folder Rationale

| Folder | Contents | Why |
|--------|----------|-----|
| `api/` | All `fetch` wrappers + TypeScript interfaces | Data layer; isolated from UI |
| `hooks/` | Custom React hooks | Different lifecycle contract from components |
| `persona/` | `chatter.ts`, future persona assets/data | KubeChan persona is a distinct product domain |
| `components/` | Reusable UI that is not page-specific | `KubeChanSidebar` appears on every view; `ResourcePicker` is used across multiple modals |
| `pages/` | One subfolder per route/domain | Collocates page component with its direct modals |

### Implementation Steps

1. Create all target directories.
2. Move each file (shell `mv`), keeping git history.
3. Update all import paths in every affected file (grep for relative imports).
4. Run `tsc -b && vite build` — must succeed with zero errors.
5. Smoke-test in dev (`tilt up` or `vite dev`).

### Files Not Moved

- `App.tsx` stays at `src/` root — it is the composition root, not a page.
- `main.tsx` stays at root — Vite entry point.
- `app.css` stays at root — global styles, split deferred to Phase 3.

---

## Phase 2 — Redux Toolkit + RTK Query + WebSocket Middleware

**Goal:** Replace manual data-fetching boilerplate and the tangled `App.tsx` state machine with a typed Redux store. Every component should read state from the store and dispatch actions rather than drilling props through multiple layers.

### New Dependencies

```json
"@reduxjs/toolkit": "^2.x",
"react-redux": "^9.x",
"react-router-dom": "^7.x"
```

### Store Layout

```
src/store/
├── index.ts                        # configureStore, RootState, AppDispatch exports
├── hooks.ts                        # typed useAppSelector / useAppDispatch
├── api/
│   ├── baseQuery.ts                # fetchBaseQuery: injects Authorization header, handles 401
│   ├── incidentsApi.ts             # incidents + problem cases endpoints
│   ├── diagnosticsApi.ts           # diagnostic runs + evidence endpoints
│   ├── analysisApi.ts              # analysis results + rating
│   ├── exclusionRulesApi.ts        # exclusion rules CRUD
│   ├── adminApi.ts                 # users + settings
│   └── kubechanApi.ts              # state + poke
└── slices/
    ├── authSlice.ts                # currentUser, initAuth thunk
    ├── kubechanSlice.ts            # pose, mood, chatter, reaction, incidentName, result
    └── uiSlice.ts                  # activeView (replaced by router in Phase 3), modal flags
```

### `baseQuery.ts`

```ts
import { fetchBaseQuery } from '@reduxjs/toolkit/query/react'
import { getToken, clearToken } from '../api'

export const baseQuery = fetchBaseQuery({
  baseUrl: '/api/v1',
  prepareHeaders: (headers) => {
    const token = getToken()
    if (token) headers.set('Authorization', `Bearer ${token}`)
    return headers
  },
})

// Wrap baseQuery to handle 401 globally: clear token, dispatch clearUser
```

### `authSlice.ts`

```ts
interface AuthState {
  currentUser: CurrentUser | null | undefined  // undefined = loading
}

// actions
setUser(user: CurrentUser)
clearUser()

// thunk
initAuth(): AppThunk   // calls api.me(), dispatches setUser or clearUser
```

### `kubechanSlice.ts`

All KubeChan state transitions that are currently scattered across `App.tsx` callbacks become Redux actions. Timer side-effects stay in `useKubeChan` hook (timers are not serializable and do not belong in Redux state).

```ts
interface KubeChanSliceState {
  pose: KubeChanPose          // 'idle' | 'thinking' | 'speaking' | 'chatter'
  moodLevel: number
  incidentName?: string
  result?: AnalysisResult
  chatterLine?: string
  reactionLine?: string
}

// actions
setIdle()
setThinking(incidentName: string)
setSpeaking({ result, incidentName })
setChatter(line: string)
clearChatter()
setReaction(line: string)
clearReaction()
setMoodLevel(n: number)
```

**`useKubeChan` hook** (replaces the ~150 lines of persona logic in `App.tsx`):

```ts
// src/hooks/useKubeChan.ts
export function useKubeChan() {
  const dispatch = useAppDispatch()
  const moodLevel = useAppSelector(selectMoodLevel)
  const moodLevelRef = useRef(moodLevel)
  // ... timer refs (chatterTimer, pokeResetTimer, silenceStage, lastInteraction)
  
  const triggerChatter = useCallback((event: ChatterEvent) => { ... dispatch(setChatter(line)) ... }, [])
  const handlePoke = useCallback(() => { ... }, [])
  // silence intervals, idle interval

  return { triggerChatter, handlePoke }
}
```

`App.tsx` calls `useKubeChan()` once and passes `triggerChatter` / `handlePoke` to children via context or props.

### `uiSlice.ts`

```ts
interface UIState {
  showManualModal: boolean
  exclusionProposal: ExclusionRuleProposal | null
}

// actions
openManualModal()
closeManualModal()
setExclusionProposal(proposal: ExclusionRuleProposal | null)
```

Routing (`view` state) moves to `react-router-dom` in Phase 3 and is removed from this slice.

### RTK Query Endpoint Map

| Current call | RTK Query hook | Cache tag |
|---|---|---|
| `api.listIncidents()` | `useListIncidentsQuery()` | `Incident` |
| `api.getIncident(id)` | `useGetIncidentQuery(id)` | `Incident` |
| `api.analyze(id)` | `useAnalyzeMutation()` | invalidates `DiagnosticRun` |
| `api.listDiagnosticRuns()` | `useListDiagnosticRunsQuery()` | `DiagnosticRun` |
| `api.getDiagnosticRun(id)` | `useGetDiagnosticRunQuery(id)` | `DiagnosticRun` |
| `api.deleteDiagnosticRun(id)` | `useDeleteDiagnosticRunMutation()` | invalidates `DiagnosticRun` |
| `api.bulkDeleteDiagnosticRuns(ids)` | `useBulkDeleteDiagnosticRunsMutation()` | invalidates `DiagnosticRun` |
| `api.getDiagnosticRunEvidence(id)` | `useGetEvidenceQuery(id)` | `Evidence` |
| `api.getDiagnosticRunAnalysisResult(id)` | `useGetAnalysisResultQuery(id, { pollingInterval: 3000, skip: !pending })` | `AnalysisResult` |
| `api.rateAnalysisResult(id, r)` | `useRateAnalysisResultMutation()` | optimistic update on `AnalysisResult` |
| `api.getKubeChanState()` | `useGetKubeChanStateQuery()` | `KubeChanState` |
| `api.poke()` | `usePokeMutation()` | invalidates `KubeChanState` |
| `api.listExclusionRules()` | `useListExclusionRulesQuery()` | `ExclusionRule` |
| `api.createExclusionRule(body)` | `useCreateExclusionRuleMutation()` | invalidates `ExclusionRule` |
| `api.listUsers()` | `useListUsersQuery()` | `User` |
| `api.createUser(body)` | `useCreateUserMutation()` | invalidates `User` |
| `api.deleteUser(id)` | `useDeleteUserMutation()` | invalidates `User` |

RTK Query eliminates all `useState<{loading, error, data}>` patterns in pages. Components get `isLoading`, `isError`, `data` from the query hook with automatic cache, deduplication, and refetch-on-focus.

**Analysis result polling** — currently a raw `setInterval` in `handleManualCreated` with a 5-minute hard cutoff and no cleanup on unmount:

```ts
// Current (leaky)
const poll = setInterval(async () => { ... }, 3000)
setTimeout(() => clearInterval(poll), 5 * 60_000)

// Phase 2: RTK Query handles this
const { data: result } = useGetAnalysisResultQuery(diagnosticRunId, {
  pollingInterval: result?.status === 'completed' ? 0 : 3000,
  skip: !diagnosticRunId,
})
```

### WebSocket → Redux Middleware

The `useWebSocket` hook moves into a Redux middleware so the entire store can react to server events, not just the one component that happens to call the hook.

```ts
// src/store/middleware/wsMiddleware.ts
export const wsMiddleware: Middleware = (store) => {
  let ws: WebSocket | null = null

  const connect = () => {
    const token = getToken()
    // Phase 3: replace with Sec-WebSocket-Protocol ticket approach
    const url = `${protocol}://${host}/ws?token=${encodeURIComponent(token ?? '')}`
    ws = new WebSocket(url)
    
    ws.onmessage = (e) => {
      const event = JSON.parse(e.data)
      if (event.type === 'Incident.Created') {
        store.dispatch(incidentsApi.util.invalidateTags(['Incident']))
        store.dispatch(setChatter(pickChatterLine('new-incident', getMoodLevel(store.getState()))))
      }
      if (event.type === 'KubeChanState.Updated') {
        store.dispatch(setMoodLevel(event.moodLevel))
      }
    }
    ws.onclose = () => setTimeout(connect, 3000)
  }

  return (next) => (action) => {
    if (action.type === 'auth/setUser') connect()
    if (action.type === 'auth/clearUser') ws?.close()
    return next(action)
  }
}
```

### `App.tsx` After Phase 2

Drops from ~350 lines to ~70 lines. It becomes a pure composition root:

```tsx
function App() {
  const dispatch = useAppDispatch()
  const currentUser = useAppSelector(selectCurrentUser)
  const view = useAppSelector(selectActiveView)
  const { triggerChatter, handlePoke } = useKubeChan()
  const kubechan = useAppSelector(selectKubeChanState)
  const moodLevel = useAppSelector(selectMoodLevel)

  useEffect(() => { dispatch(initAuth()) }, [dispatch])

  if (currentUser === undefined) return <AppLoading />
  if (currentUser === null) return <LoginPage />

  return (
    <AppShell
      kubechan={kubechan}
      moodLevel={moodLevel}
      onPoke={handlePoke}
      onRate={handleRate}
      currentUser={currentUser}
    />
  )
}
```

### Implementation Steps

1. `npm install @reduxjs/toolkit react-redux react-router-dom`
2. Create `src/store/index.ts` with `configureStore`, register slices + RTK Query reducers + WS middleware.
3. Create `src/store/hooks.ts` — `useAppSelector`, `useAppDispatch`.
4. Implement `authSlice.ts` + `initAuth` thunk. Wire into `App.tsx`. Verify login flow still works.
5. Implement `kubechanSlice.ts`. Extract `useKubeChan` hook. Remove persona logic from `App.tsx`. Verify KubeChan reactions work.
6. Implement `uiSlice.ts`. Replace `view` state with dispatched actions. Verify navigation still works.
7. Implement RTK Query service files, endpoint by endpoint, starting with `incidentsApi`. Replace manual fetch calls in `IncidentList`.
8. Repeat for `diagnosticsApi`, `analysisApi`, `exclusionRulesApi`, `adminApi`, `kubechanApi`.
9. Replace `useWebSocket` hook with `wsMiddleware`. Remove the hook file.
10. Run full build + smoke-test all pages.

---

## Phase 3 — MUI Migration, Routing, Polish & Component Split

**Goal:** Establish a unified design system (MUI + single theme), introduce URL-based routing, eliminate remaining CSS duplication, and enforce the container/presentational pattern throughout.

### 3a — Global MUI Theme

Currently `ExclusionRuleModal` defines its own inline `ThemeProvider`/`createTheme`. All other components use raw HTML + CSS variables. The theme must be defined once and applied at the root.

```ts
// src/theme.ts
import { createTheme, alpha } from '@mui/material'

export const theme = createTheme({
  palette: {
    mode: 'dark',
    background: {
      default: '#0d0f17',   // --bg
      paper:   '#161923',   // --surface
    },
    primary:   { main: '#6366f1' },   // --accent
    warning:   { main: '#f59e0b' },   // --open
    success:   { main: '#22c55e' },   // --resolved
    error:     { main: '#ef4444' },   // --error
    text: {
      primary:   '#e2e8f0',  // --text
      secondary: '#64748b',  // --muted
    },
  },
  shape:      { borderRadius: 8 },     // --radius
  typography: { fontFamily: "'Inter', system-ui, sans-serif" },
  components: {
    MuiButton: { ... },
    MuiCard:   { ... },
    MuiDialog: { ... },
    MuiChip:   { ... },
    // ... overrides to match existing visual design
  },
})
```

```tsx
// main.tsx
root.render(
  <Provider store={store}>
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <App />
    </ThemeProvider>
  </Provider>
)
```

`ExclusionRuleModal` loses its inline `createTheme` / `ThemeProvider` wrapper. The global theme applies everywhere.

### 3b — React Router

```tsx
// App.tsx routes
<RouterProvider router={createBrowserRouter([
  { path: '/',                 element: <IncidentListPage /> },
  { path: '/diagnostics',      element: <DiagnosticsPage /> },
  { path: '/diagnostics/:id',  element: <DiagnosticRunDetail /> },
  { path: '/admin/users',      element: <ProtectedRoute role="admin"><UsersPage /></ProtectedRoute> },
  { path: '/admin/settings',   element: <ProtectedRoute role="admin"><AdminSettingsPage /></ProtectedRoute> },
  { path: '/admin/exclusions', element: <ProtectedRoute role="admin"><ExclusionRulesPage /></ProtectedRoute> },
  { path: '/llm-settings',     element: <LLMSettingsPage /> },
  { path: '/login',            element: <LoginPage /> },
])} />
```

`uiSlice` is simplified to just modal flags; `view` state is removed. `navigate()` from `react-router-dom` replaces `dispatch(navigate(...))`.

### 3c — Shared Component Library

```
src/components/shared/
├── ConfidenceBadge.tsx      # de-duplicated from IncidentList + DiagnosticsPage
├── ResourcePill.tsx         # extracted from IncidentList (already a local function)
├── StatusBadge.tsx          # extracted from DiagnosticsPage
├── EventsTable.tsx          # extracted from DiagnosticRunDetail
├── ErrorBoundary.tsx        # new
└── utils.ts                 # fmtDate, timeAgo (de-duplicated from 3 files)
```

**`ConfidenceBadge`** — currently defined separately in `IncidentList.tsx` and `DiagnosticsPage.tsx` with identical logic:

```tsx
// src/components/shared/ConfidenceBadge.tsx
export function ConfidenceBadge({ confidence }: { confidence?: number }) {
  if (confidence == null) return null
  const pct = Math.round(confidence * 100)
  const color = pct >= 80 ? 'success' : pct >= 50 ? 'warning' : 'error'
  return <Chip label={`${pct}%`} color={color} size="small" />
}
```

**`ErrorBoundary`** — currently absent; a single render error crashes the entire app to a blank screen:

```tsx
// src/components/shared/ErrorBoundary.tsx
export class ErrorBoundary extends React.Component<...> { ... }

// Usage in App.tsx
<ErrorBoundary fallback={<Alert severity="error">Something went wrong.</Alert>}>
  <Outlet />
</ErrorBoundary>
```

### 3d — MUI Component Migration Map

Each native HTML element + CSS class gets replaced by its MUI equivalent. The design tokens from `app.css` are already translated into the theme in 3a, so the visual result must be identical.

| Current | MUI Replacement |
|---------|----------------|
| `<button className="btn-refresh">` | `<Button variant="outlined" size="small">` |
| `<button className="btn-back">` | `<IconButton>` + `<ArrowBackIcon>` |
| `<button className="btn-delete-run">` | `<IconButton color="error" size="small">` |
| `<button className="btn-delete-bulk">` | `<Button variant="outlined" color="error">` |
| `<button className="app-nav-btn">` | `<Button>` or `<Tab>` inside MUI `<Tabs>` |
| `<div className="incident-row">` | `<Card>` + `<CardContent>` |
| `<span className="state-badge open">` | `<Chip label="open" color="warning" size="small">` |
| `<span className="state-badge resolved">` | `<Chip label="resolved" color="success" size="small">` |
| `<span className="pc-chip">` | `<Chip variant="outlined" size="small">` |
| `<div className="error-msg">` | `<Alert severity="error">` |
| `<div className="loading">` | `<CircularProgress>` or `<LinearProgress>` |
| `<div className="empty">` | `<Typography color="text.secondary">` |
| `window.confirm(...)` | `<Dialog>` with confirm/cancel buttons |
| `window.alert(...)` | `<Snackbar>` + `<Alert>` |
| `<table className="diag-events-table">` | MUI `<Table>` + `<TableHead>` + `<TableBody>` |
| `<input type="checkbox" className="diag-checkbox">` | MUI `<Checkbox>` |
| `<details>` / `<summary>` incident details | MUI `<Accordion>` |
| `<nav className="app-nav">` header nav | MUI `<AppBar>` + `<Toolbar>` + `<Tabs>` |
| Login form inputs | MUI `<TextField>` |
| Login page card | MUI `<Card>` |

### 3e — Container / Presentational Split

Each page splits into a container (data + dispatch) and one or more presentational components (pure props → JSX).

**Example: Incidents**

```
pages/incidents/
├── IncidentListPage.tsx         # container: useListIncidentsQuery, dispatch
└── components/
    ├── IncidentList.tsx         # presentational: incidents[] → list
    └── IncidentCard.tsx         # presentational: single incident
        └── IncidentDetails.tsx  # presentational: collapsible detail section
```

**Example: Diagnostics**

```
pages/diagnostics/
├── DiagnosticsPage.tsx          # container: useListDiagnosticRunsQuery, delete mutations
└── components/
    ├── DiagnosticRunList.tsx    # presentational: grouped runs list
    └── DiagnosticRunRow.tsx     # presentational: single run row + checkbox
```

```
pages/diagnostics/
├── DiagnosticRunDetailPage.tsx  # container: useGetEvidenceQuery, useGetAnalysisResultQuery
└── components/
    ├── EvidenceTabs.tsx         # presentational: tab switcher
    ├── LogsTab.tsx
    ├── EventsTab.tsx
    ├── ConfigTab.tsx
    └── PVCsTab.tsx
```

**Rule:** A presentational component must not call `useAppDispatch`, `useAppSelector`, or any `useXxxQuery` / `useXxxMutation` hook. If it needs to trigger an action, it receives a callback prop.

### 3f — Security Fix: WebSocket Token

The current URL query-param token (`/ws?token=...`) logs the token in nginx access logs and browser history. The fix uses the WS subprotocol field (a widely-used workaround since browsers do not allow custom WS headers):

```ts
// wsMiddleware.ts
const ws = new WebSocket(url, [`bearer.${token}`])

// backend-api/ws handler — reads from r.Header.Get("Sec-WebSocket-Protocol")
// and echoes back: w.Header().Set("Sec-WebSocket-Protocol", fmt.Sprintf("bearer.%s", token))
```

Requires a matching backend change in `services/backend-api/ws/`.

### 3g — `app.css` Reduction

After all components migrate to MUI:
- Layout rules (`app`, `app-header`, `app-body`, `app-main`) → kept as minimal global layout CSS or moved to MUI `Box`/`Stack`/`AppBar`.
- KubeChan sidebar styles → co-located in `components/KubeChanSidebar.tsx` using MUI `sx` prop or `styled()`.
- All per-component classes → deleted as each component is migrated.
- Target: `app.css` shrinks to only global resets, CSS custom properties used by the persona/animation system (image transitions, shake keyframes), and layout skeleton.

### Additional Fixes in Phase 3

| Issue | Fix |
|-------|-----|
| `key={i}` array index keys in multiple components | Replace with stable IDs (`run.diagnosticRunId`, `ev.reason+ev.lastTime`, etc.) |
| Missing `useEffect` deps in `DiagnosticRunDetail` (`onAction`, `onResultLoaded`) | Stabilise callbacks with `useCallback` in parent; refs already used for most cases |
| `usePokeMutation` optimistic mood update | Use RTK Query `onQueryStarted` for optimistic increment |
| No loading skeleton on incident cards | Add MUI `<Skeleton>` while `isLoading` |

### Implementation Steps

1. Create `src/theme.ts`. Wrap app in `ThemeProvider` + `CssBaseline`. Verify existing MUI-using components (`ExclusionRuleModal`) look correct.
2. Remove inline `createTheme` from `ExclusionRuleModal`.
3. Install `react-router-dom`. Replace `view` state routing with `RouterProvider`. Verify all navigation works, browser back/forward works.
4. Create `src/components/shared/` files: `ConfidenceBadge`, `ResourcePill`, `StatusBadge`, `EventsTable`, `ErrorBoundary`, `utils.ts`. Update all imports.
5. Migrate `LoginPage` to MUI (`TextField`, `Card`, `Button`, `Alert`).
6. Migrate `AppBar` / nav header to MUI `AppBar` + `Toolbar` + `Tabs`.
7. Migrate `IncidentList` → `IncidentListPage` container + `IncidentList`/`IncidentCard`/`IncidentDetails` presentational. Replace CSS classes with MUI `Card`, `Chip`, `Accordion`.
8. Replace `window.confirm` in `DiagnosticsPage` with MUI `Dialog`. Replace `window.alert` with `Snackbar`.
9. Migrate `DiagnosticsPage` + `DiagnosticRunDetail` similarly.
10. Migrate remaining pages: `UsersPage`, `AdminSettingsPage`, `ExclusionRulesPage`, `LLMSettingsPage`.
11. Fix WS token security (requires coordinated backend change).
12. Prune `app.css` to only global/animation rules.
13. Add `<ErrorBoundary>` around route outlet.
14. Fix array-index keys throughout.
15. Final: run `tsc -b && vite build`, smoke-test all pages and flows.

---

## Dependency Summary

| Package | Phase | Reason |
|---------|-------|--------|
| `@reduxjs/toolkit` | 2 | RTK Query + slices |
| `react-redux` | 2 | `Provider`, `useSelector`, `useDispatch` |
| `react-router-dom` | 3 | URL-based routing |
| `@mui/material` | already installed | Unified component library |
| `@mui/icons-material` | already installed | Icon set |
| `@emotion/react` + `@emotion/styled` | already installed | MUI peer deps |

No new UI dependencies needed beyond what's already in `package.json`.

---

## Non-Goals

- **No test suite introduced in these phases.** Adding a test suite (Vitest + React Testing Library) is a separate initiative.
- **No SSR / Next.js migration.** The app remains a Vite SPA served by nginx.
- **No GraphQL.** RTK Query over REST matches the existing backend API shape.
- **No design overhaul.** The dark indigo visual language (`--bg #0d0f17`, `--accent #6366f1`) is preserved; only the implementation layer (CSS classes → MUI components) changes.

---

## Completion Criteria

| Phase | Done when |
|-------|-----------|
| 1 | `tsc -b && vite build` passes; all imports resolve; file tree matches target layout |
| 2 | No `useState<loading\|error>` patterns remain in page components; `App.tsx` < 80 lines; WS reconnects automatically; persona reactions work end-to-end |
| 3 | No `window.confirm`/`window.alert`; no raw `<button className="...">` in page components; `app.css` < 100 lines; browser back/forward navigates correctly; `<ErrorBoundary>` in place |
