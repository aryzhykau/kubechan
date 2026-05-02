# Phase 4 — frontend-ui

## Prerequisites (from other services)
- `GET /api/v1/problemcases`, `GET /api/v1/problemcases/:id`, `GET /api/v1/diagnosticruns/:id`, `GET /api/v1/analysisresults/:id` live (Phase 2B)
- `GET /ws` WebSocket endpoint live (Phase 2B.12)
- `POST /api/v1/problemcases/:id/analyze` live (Phase 2B.6)
- `GET /api/v1/settings`, `PUT /api/v1/settings` live (Phase 2B.7)
- `GET /api/v1/persona/idle-message` live (Phase 3B.4)

---

## Tasks (ordered)

### [4.1] Scaffold + infrastructure

**Task 4.1.1** — Project scaffold (~3h)
- `npm create vite@latest frontend-ui -- --template react-ts`
- Install: `react-router-dom@7`, `@tanstack/react-query@5`, `@tanstack/react-table@8`
- Install Shadcn/ui: `npx shadcn@latest init` → configure Tailwind, base components
- Install: `react-markdown`, `react-syntax-highlighter` (for runbook + kubectl commands rendering)
- `VITE_API_URL` env var in `.env.development` (default `http://localhost:8080`), `.env.production` (relative `/`)

**Task 4.1.2** — TypeScript types (~3h)
- File: `src/types/index.ts`
```typescript
interface ProblemCase {
  id: string; namespace: string; name: string; kind: string;
  spec: { affectedResource: {...}; detector: string; severity: 'critical'|'high'|'medium'|'low'; symptoms: string[] };
  status: { state: 'open'|'investigating'|'resolved'; firstSeen: string; lastSeen: string; resolvedAt?: string; latestDiagnosticRunRef?: string; latestAnalysisResultRef?: string };
}
interface DiagnosticRun { id: string; spec: {...}; status: { state: string; evidenceRef?: string; collectionErrors?: string[]; ... } }
interface AnalysisResult { id: string; problemCaseId: string; status: string; likelyRootCause?: string; confidence?: number; confidenceRationale?: string; evidenceMapping?: EvidenceMapping[]; recommendedRunbook?: string; kubectlCommands?: string[]; safetyNotes?: string[]; styledMessage?: string; consistencyCheckStatus?: string; createdAt: string }
interface Settings { persona: { enabled: boolean; idleChatter: boolean; idleIntervalSecs: number }; bedrock: { modelId: string; region: string; thinkingBudget: number }; evidence: { retentionDays: number }; analysis: { retentionDays: number } }
// WS event types matching full-plan.md §4c
type WSEvent = ProblemCaseCreatedEvent | ProblemCaseUpdatedEvent | ProblemCaseResolvedEvent | DiagnosticRunStatusChangedEvent | AnalysisResultCompletedEvent | AnalysisResultFailedEvent
```

**Task 4.1.3** — API client (~2h)
- File: `src/api/client.ts`
- `apiFetch<T>(path: string, options?: RequestInit): Promise<T>` — prepends `VITE_API_URL`, handles non-2xx as thrown errors with status code

**Task 4.1.4** — TanStack Query definitions (~2h)
- File: `src/api/queries.ts`
- Query key factories: `problemCaseKeys`, `diagnosticRunKeys`, `analysisResultKeys`, `settingsKeys`
- Query functions for all GET endpoints
- Mutation for `POST /analyze` + `PUT /settings`
- `QueryClient` configured with `staleTime: 30_000`, `retry: 2`

**Task 4.1.5** — `useWebSocket` hook (~3h)
- File: `src/hooks/useWebSocket.ts`
- Connects on mount to `${WS_URL}/ws` (derived from `VITE_API_URL`)
- Reconnects with exponential backoff: initial 1s, doubles each attempt, max 30s cap
- On message: parse JSON → call `queryClient.invalidateQueries(keysForEvent(event))`
- `keysForEvent`: maps event type to query keys to invalidate (e.g. `ProblemCase.Created` → invalidate `problemCaseKeys.lists()`)
- Exposes `connectionStatus: 'connecting' | 'connected' | 'disconnected'`
- Cleans up on unmount

**Task 4.1.6** — Router + layout (~1h)
- File: `src/App.tsx`
- Routes: `/` → Overview, `/problems` → Inbox, `/problems/:id` → Detail, `/settings` → Settings
- Shared layout: sidebar nav (Overview, Problems, Settings links) + `connectionStatus` indicator badge

---

### [4.2] Overview screen — `/` (~3h)
- File: `src/pages/Overview.tsx`

**ClusterHealthBanner**
- Derives overall health from open ProblemCase count + max severity
- States: `healthy` (0 open), `degraded` (open but no critical), `critical` (any critical open)
- Color-coded: green / amber / red

**SeverityBreakdown**
- 4 count cards: critical / high / medium / low
- Counts from `GET /api/v1/problemcases?status=open` grouped client-side

**TopProblems**
- 5 most severe open ProblemCases
- Columns: severity badge, namespace/kind/name, detector, firstSeen
- Clickable → navigates to Problem Detail

**WS wiring**
- Subscribes to `ProblemCase.*` events → `queryClient.invalidateQueries(problemCaseKeys.lists())`

---

### [4.3] Problem Inbox screen — `/problems` (~4h)
- File: `src/pages/ProblemInbox.tsx`

**ProblemTable**
- TanStack Table v8
- Columns: severity badge, namespace, kind/name, detector, firstSeen, lastSeen, analysisStatus (derived from latestAnalysisResultRef presence)
- Server-side pagination via `continue` token (pass to next `GET /api/v1/problemcases` call)
- Client-side sort on fetched page (no server-side sort in CRD API)

**Filters**
- Severity multi-select, status select, namespace select
- All filter state stored in URL query params (`?severity=high,critical&status=open&namespace=default`)
- Filters wired to query params passed to `GET /api/v1/problemcases`

**WS wiring**
- `ProblemCase.Created/Updated/Resolved` → `queryClient.invalidateQueries(problemCaseKeys.list(currentFilters))`

---

### [4.4] Problem Detail screen — `/problems/:id` (~4h + 3h = 7h, split into two sub-tasks)

**Task 4.4.1** — Primary panels (~4h)
- File: `src/pages/ProblemDetail.tsx`

`ProblemSummaryCard`: affectedResource (kind/namespace/name), detector, severity badge, firstSeen/lastSeen, state badge

`AnalysisPanel`:
- `likelyRootCause` text block
- Confidence gauge: horizontal bar 0–100%, color: red <0.4, amber <0.7, green ≥0.7
- `confidenceRationale` text (small, muted)
- `evidenceMapping` list: each item shows evidenceType + observation + relevance badge

`RunbookPanel`:
- Render `recommendedRunbook` as Markdown (`react-markdown`)
- Collapsible

`KubectlCommandsPanel`:
- Each command in a code block with copy button
- `safetyNotes` shown as amber warning callouts below commands

**Task 4.4.2** — Secondary panels + WS wiring (~3h)

`RawEvidencePanel`:
- Collapsible JSON viewer (syntax-highlighted via `react-syntax-highlighter`)
- Fetches from `GET /api/v1/problemcases/:id/evidence` — lazy load (only fetch when panel expanded)

`AnalysisHistoryList`:
- `GET /api/v1/problemcases/:id/evidence` returns latest; history from repeated `GET /api/v1/analysisresults/:id` calls per `latestAnalysisResultRef` — list all results ordered by `created_at`
- Each row: created_at, model, status, confidence (if completed)

`PersonaMessageCard`:
- Only shown when `settings.persona.enabled = true` AND `styledMessage` present in latest analysis
- Speech bubble styling; `consistencyCheckStatus = warning` shown as a small amber flag

`ReAnalyzeButton`:
- `POST /api/v1/problemcases/:id/analyze` mutation
- Disabled when DiagnosticRun in progress (`status.state = running | pending`)
- Loading spinner during mutation

WS event wiring:
- `DiagnosticRun.StatusChanged` → invalidate DiagnosticRun query → update ReAnalyzeButton state
- `AnalysisResult.Completed/Failed` → invalidate AnalysisResult query → refresh AnalysisPanel

---

### [4.5] Settings screen — `/settings` (~2h)
- File: `src/pages/Settings.tsx`

`PersonaToggle`: switch bound to `settings.persona.enabled`; on change → `PUT /api/v1/settings { "persona.enabled": true/false }`

`IdleChatterToggle`: same pattern for `settings.persona.idle_chatter`

`BedrockConfig` (display-only): model ID, region — read from settings; note: "Configured via Helm values"

`ThinkingBudgetDisplay`: numeric display of `bedrock.thinking_budget`; note "0 = fast mode (/no_think)"

All toggles use optimistic updates via TanStack Query mutation + `onError` rollback.

---

### [4.6] Persona speech bubble + idle chatter (~4h)
- File: `src/components/PersonaBubble.tsx`

**Render conditions**: only when `settings.persona.enabled = true`

**Analysis mode**:
- Shows `styledMessage` from highest-severity open ProblemCase that has a completed analysis
- Fixed position: bottom-right, `z-index: 50`, above main content
- Severity badge in bubble header; click → navigates to Problem Detail

**Idle mode**:
- Only when `persona.idle_chatter = true` AND no critical/high severity open ProblemCase active
- Client-side 5-minute timer (resets on each idle message fetch)
- On timer fire: `GET /api/v1/persona/idle-message` → render in bubble
- Dismissable per session: `sessionStorage.setItem('bubbleDismissed', 'true')`; reappears after next timer interval

**Both modes**:
- Speech bubble CSS: rounded, with a tail pointer pointing up-right
- Close button (×): dismisses for current display cycle

---

## Integration test entry point
With full backend stack running (`make dev-up`):
1. Deploy broken workload; ProblemCase appears on Overview within 30s (post-debounce) + WS push
2. Problem Inbox: filter by `severity=critical` → shows the problem
3. Problem Detail: trigger re-analysis → spinner shows → analysis result appears after Bedrock responds
4. Settings: toggle persona on → PersonaBubble appears in Problem Detail with styledMessage
5. Idle mode: set `idleIntervalSecs` to 10 via direct DB edit for testing → GET idle-message fires → bubble renders idle message
