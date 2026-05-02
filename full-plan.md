# KubeChan: Full Planning Document

## 1. Business Requirements

| ID   | Requirement |
|------|------------|
| BR-1 | Deploy into any standard Kubernetes cluster via a single Helm chart command |
| BR-2 | Detect cluster problems automatically without user configuration beyond installation |
| BR-3 | Present diagnosed problems, root causes, and remediation guidance through a web UI |
| BR-4 | Never modify user workloads in any way |
| BR-5 | Support external LLM endpoints (AWS Bedrock) in MVP |
| BR-6 | Enable/disable tsundere persona layer without affecting technical content |
| BR-7 | Support notifications (MVP: within backend-api) |
| BR-8 | Protect sensitive data: secret values must never leave the cluster to LLMs |
| BR-9 | Operable by a single Kubernetes operator with read-only intent |

---

## 2. Functional Requirements

### Detection
- FR-D1: cluster-watcher watches Pods, Deployments, ReplicaSets, StatefulSets, DaemonSets, Services, EndpointSlices, Ingresses, Nodes, Events
- FR-D2: Implement MVP detectors: CrashLoopBackOff, ImagePullBackOff/ErrImagePull, PendingTooLong, DeploymentUnavailable, ServiceNoEndpoints
- FR-D3: Debounce transient states before creating ProblemCases
- FR-D4: Deduplicate related symptoms into a single ProblemCase
- FR-D5: Auto-resolve ProblemCases when symptoms disappear
- FR-D6: Create/update ProblemCase CRDs

### Diagnostics
- FR-DW1: Collect: pod status, pod events, current logs, previous logs, owner manifests, deployment/statefulset/daemonset status, service selectors, endpoints/endpointslices, ingress backend info, configmap metadata, secret metadata (no values), node conditions
- FR-DW2: Redact sensitive patterns from logs
- FR-DW3: Truncate logs to configurable size limit
- FR-DW4: Store collected evidence reference in DiagnosticRun CRD; full payload in SQLite
- FR-DW5: Never perform write actions

### LLM Analysis
- FR-L1: llm-gateway builds structured prompts from diagnostic context using priority-tier evidence budget
- FR-L2: Calls AWS Bedrock via Converse API (default model: Qwen3 32B)
- FR-L3: Validates structured LLM output via Bedrock tool use + Pydantic
- FR-L4: Produces: likely root cause, confidence, evidence mapping, runbook, kubectl commands, safety notes
- FR-L5: Produces persona-styled message if persona mode enabled
- FR-L6: Consistency check between raw and styled output
- FR-L7: Stores result in SQLite; patches AnalysisResult ref on ProblemCase CRD

### API
- FR-A1: backend-api serves REST API for the UI
- FR-A2: Exposes CRUD on ProblemCases, DiagnosticRuns, AnalysisResults
- FR-A3: Triggers DiagnosticRun creation
- FR-A4: Triggers LLM analysis (durable via analysis_requests table)
- FR-A5: Stores user settings (persona on/off, idle chatter, Bedrock config)
- FR-A6: Exposes health/status endpoints
- FR-A7: Pushes real-time events to connected UI clients via WebSocket

### UI
- FR-UI1: Overview: cluster health summary, open count, most severe, assistant bubble
- FR-UI2: Problem Inbox: list with filters by severity/status/namespace
- FR-UI3: Problem Detail: RCA, evidence, confidence, runbook, kubectl commands, raw data, analysis history, persona message
- FR-UI4: Settings: persona on/off, idle chatter on/off, Bedrock config display
- FR-UI5: Manual re-analysis trigger
- FR-UI6: Real-time updates via WebSocket (push event → REST fetch pattern)
- FR-UI7: Persona speech bubble in idle state when persona mode enabled and no critical problem active
- FR-UI8: Persona idle chatter rate-limited (5-minute client-side timer)

---

## 3. Non-Functional Requirements

### Security
- NFR-S1: Product strictly read-only for user workloads
- NFR-S2: Secret values never transmitted to any LLM or external service
- NFR-S3: RBAC least-privilege per service
- NFR-S4: LLM gateway has no Kubernetes RBAC permissions
- NFR-S5: Logs redacted before storage or transmission
- NFR-S6: Persona output must not introduce new technical facts

### Reliability
- NFR-R1: cluster-watcher resilient to API server interruptions (controller-runtime handles reconnect)
- NFR-R2: diagnostics-worker handles partial collection failures gracefully (collectionErrors field)
- NFR-R3: LLM calls have configurable timeouts and retry with exponential backoff (max 3 retries)
- NFR-R4: Analysis intent durable via analysis_requests table — survives backend-api restarts

### Scalability
- NFR-SC1: Function in clusters up to 100 nodes in MVP
- NFR-SC2: SQLite evidence/results prunable via configurable TTL (default 7 days)

### Observability
- NFR-O1: All services emit structured JSON logs (Go: log/slog; Python: structlog)
- NFR-O2: All services expose Prometheus metrics endpoint (/metrics)
- NFR-O3: All services expose /healthz (liveness) and /readyz (readiness)

### Performance
- NFR-P1: UI loads Problem Inbox in <2s for up to 500 open problems
- NFR-P2: Diagnostics collection completes within 60s for typical problems
- NFR-P3: LLM analysis completes within 120s (Bedrock dependent)

### Deployability
- NFR-D1: Full product deployable via single `helm install`
- NFR-D2: Helm chart supports configurable resource requests/limits per service
- NFR-D3: Bedrock model + region + IRSA role configurable via Helm values
- NFR-D4: Local dev via Docker Desktop Kubernetes + Tilt (no extra cluster setup)

---

## 4. MVP Milestone Plan

### M0 – Foundation (Weeks 1–2)
- Monorepo structure setup
- CRD Go types + controller-gen (ProblemCase, DiagnosticRun, AnalysisResult)
- Helm chart skeleton with `crds/` directory (ArgoCD pattern)
- Local dev: Docker Desktop Kubernetes + Tilt
- CI pipeline: Dockerfiles + lint + build per service

### M1 – Detection (Weeks 3–4)
- cluster-watcher: controller-runtime Manager + reconcilers for all 10 resource types
- 5 MVP detectors
- ProblemCase CRD lifecycle (create, update, resolve)
- Debounce (30s) and deduplication logic

### M2 – Diagnostics + API core (Weeks 5–6)
- diagnostics-worker: all collectors, redaction pipeline, log truncation
- backend-api: chi HTTP server + SQLite schema + migrations
- Internal contracts: POST /internal/evidence + POST /internal/analysis-result
- WebSocket hub + CRD watch → broadcast loop

### M3 – LLM Integration (Weeks 7–8)
- llm-gateway: system prompt template + Bedrock Converse adapter + tool use
- Evidence priority-tier budget builder (4 tiers, 120K token default)
- Structured output validation + consistency checker
- AnalysisResult write-back flow + analysis_requests durable tracking

### M4 – UI (Weeks 9–10)
- Vite + React 19 + Shadcn/ui scaffold
- useWebSocket hook (push → invalidate → REST fetch pattern)
- Overview, Problem Inbox, Problem Detail, Settings screens
- Persona speech bubble (analysis mode + idle mode)

### M5 – Hardening (Weeks 11–12)
- RBAC finalization and security review
- Observability: structured logs, metrics, health endpoints all services
- E2E tests against Docker Desktop cluster
- Helm chart documentation, NOTES.txt, README

---

## 4b. Implementation Order & Dependency Graph

Dependencies flow strictly downward. Parallel work is safe within the same phase.

```
Phase 0 ──────────────────────────────────────────────────────────────
  [0.1] Monorepo layout
  [0.2] CRD Go types + controller-gen → output to helm/kubechan/crds/
  [0.3] Helm chart skeleton (crds/ directory, placeholder Deployments)
  [0.4] Local dev: Docker Desktop + Tiltfile + values-dev.yaml
  [0.5] CI: Dockerfiles + lint + build per service

Phase 1 ──────────────────────────────────────────────────────────────  (depends on 0.2)
  [1.1] cluster-watcher: Manager setup + reconciler registration
  [1.2] 5 MVP detectors
  [1.3] debounce + dedup + ProblemCase lifecycle

Phase 2 ──────────────────────────────────────────────────────────────  (depends on 0.2; 1.x parallel)
  [2A] diagnostics-worker: collectors + redaction + POST /internal/evidence
  [2B] backend-api: HTTP server + SQLite + WebSocket hub + CRD watch loop
       Integration point: POST /internal/evidence contract (agree before starting)

Phase 3 ──────────────────────────────────────────────────────────────  (depends on 2B)
  [3A] llm-gateway: prompt builder + Bedrock adapter + tool use + persona
  [3B] backend-api: POST /internal/analysis-result + WS broadcast
       Integration point: POST /internal/analysis-result contract (agree before starting)

Phase 4 ──────────────────────────────────────────────────────────────  (depends on 2B REST + 3B WS)
  [4.1] frontend-ui: scaffold + useWebSocket hook + API client
  [4.2] Overview screen      ┐
  [4.3] Problem Inbox screen │ parallel once 4.1 done
  [4.4] Problem Detail screen│
  [4.5] Settings screen      │
  [4.6] Persona bubble        ┘

Phase 5 ──────────────────────────────────────────────────────────────  (depends on all)
  [5.1] RBAC finalization + security review
  [5.2] Observability: metrics + logs + health endpoints
  [5.3] E2E tests (Docker Desktop)
  [5.4] Helm docs + NOTES.txt + README
```

---

## 4c. WebSocket Design

Pattern: **notification-push + REST pull**. WebSocket carries lightweight typed events only. Frontend fetches full data via REST on event receipt. REST is the single source of truth.

### Endpoint
`GET /ws` on backend-api — upgrades to WebSocket (gorilla/websocket or nhooyr/websocket).

### Event schema
```json
{ "type": "string", "id": "string", ...typeSpecificFields }
```

| Event type | Fields |
|-----------|--------|
| `ProblemCase.Created` | id, namespace, name, kind, severity, detector |
| `ProblemCase.Updated` | id, severity, state |
| `ProblemCase.Resolved` | id |
| `DiagnosticRun.StatusChanged` | id, problemCaseId, status |
| `AnalysisResult.Completed` | id, problemCaseId, confidence |
| `AnalysisResult.Failed` | id, problemCaseId, error |

### Hub (Go)
- `Hub` struct: `register chan *Client`, `unregister chan *Client`, `broadcast chan []byte`
- Read pump + write pump goroutines per client
- Ping every 25s, write deadline 10s, pong timeout 30s
- controller-runtime informer on all 3 CRD types → `OnAdd/OnUpdate/OnDelete` → serialize event → broadcast

### Frontend hook (`useWebSocket`)
- Connects on mount; reconnects with exponential backoff (max 30s)
- On message: `queryClient.invalidateQueries(keysForEvent(event))` — does NOT store data
- Exposes `connectionStatus: 'connecting' | 'connected' | 'disconnected'`

---

## 5. Service-by-Service Task Breakdown

### Phase 0 — Foundation

**[0.1] Monorepo layout**
```
kubechan/
  services/
    cluster-watcher/
    diagnostics-worker/
    backend-api/
    llm-gateway/
    frontend-ui/
  api/
    v1alpha1/         ← Go CRD types (shared by cluster-watcher, diagnostics-worker, backend-api)
  helm/kubechan/
  docs/
  Makefile
  .github/workflows/
```

**[0.2] CRD types + controller-gen**
- Define Go structs for ProblemCase, DiagnosticRun, AnalysisResult in `api/v1alpha1/`
- Run `controller-gen` to produce CRD YAML + DeepCopy methods
- Use controller-runtime `client.Client` directly
- Output CRD YAMLs to `helm/kubechan/crds/` — Helm `crds/` directory (NOT `templates/crds/`)

**[0.3] Helm skeleton**
- Chart.yaml, values.yaml with all toggles
- `crds/` directory at chart root — Helm installs CRDs before all other resources, never deletes on uninstall (ArgoCD pattern)
- Placeholder Deployments for all 5 services
- ServiceAccounts per service

**[0.4] Local dev**
- Docker Desktop Kubernetes — enable in Docker Desktop settings; `kubectl config use-context docker-desktop`
- No kind required
- Tiltfile: `docker_build` per service + `k8s_yaml(helm('helm/kubechan', values=['values-dev.yaml']))`
- `make dev-up` = CRDs applied + `tilt up`; `make dev-down` = `tilt down`
- `values-dev.yaml`: smaller resource limits, single replicas, NodePort for backend-api + frontend-ui

**[0.5] CI**
- Multi-stage Dockerfile per service
- GitHub Actions: lint + build + unit test on PR
- `controller-gen` output checked into repo (not gitignored)

---

### Phase 1 — cluster-watcher / controller (Go)

The cluster-watcher IS a proper Kubernetes controller running a controller-runtime `Manager` with multiple `Reconciler` implementations — exactly how ArgoCD, cert-manager, and Flux are built.

**[1.1] Manager setup + reconciler registration**
- `main.go`: `ctrl.NewManager(cfg, ctrl.Options{})` with in-cluster config; `mgr.Start(ctx)` blocks
- Manager owns shared informer cache — all reconcilers share one API server connection
- `api/v1alpha1/` types registered with Manager's scheme

Controller structure — one Reconciler per resource type:
```
controllers/
  pod_reconciler.go          ← watches Pods
  deployment_reconciler.go   ← watches Deployments
  service_reconciler.go      ← watches Services + EndpointSlices
  node_reconciler.go         ← watches Nodes
  event_reconciler.go        ← watches Events
  problemcase_reconciler.go  ← watches own ProblemCase CRDs (resolve logic)
```

Each reconciler:
```go
type PodReconciler struct {
    client.Client
    Scheme    *runtime.Scheme
    Detectors []detector.Detector
    Debouncer *debounce.Debouncer
}
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
```
- `ctrl.NewControllerManagedBy(mgr).For(&corev1.Pod{}).Complete(r)` — hooks into Manager cache
- API server pushes events via watch; reconciler called reactively, not polled
- Reconcile is idempotent

**[1.2] Detector interface + 5 MVP detectors**
- `detector/interface.go`: `Detector` interface — `Name() string` + `Evaluate(ctx, obj client.Object, reader client.Reader) ([]Symptom, error)`
  - `client.Reader` backed by Manager cache — no extra API server calls for cross-resource lookups
- `detector/crashloopbackoff.go` — containerStatuses[*].state.waiting.reason == "CrashLoopBackOff"
- `detector/imagepullbackoff.go` — reason in ["ImagePullBackOff", "ErrImagePull"]
- `detector/pendingtoolong.go` — phase == Pending + time.Since(creationTimestamp) > threshold (default 5m)
- `detector/deploymentunavailable.go` — unavailableReplicas > 0 && readyReplicas < spec.replicas
- `detector/servicenoendpoints.go` — no ready endpoints for non-headless ClusterIP service (reads EndpointSlices via client.Reader)

**[1.3] Debounce + dedup + ProblemCase lifecycle**
- `debounce/debouncer.go`: per-resource-key timer (`sync.Map[string]*time.Timer`); resets on each reconcile; fires after window (default 30s)
- Dedup: `client.List` ProblemCases with label selector `kubechan.io/affected-resource=<ns.kind.name>` + `kubechan.io/detector=<name>`; if open one exists → patch `status.lastSeen` + `status.symptoms` only
- Lifecycle:
  - Create → `client.Create` ProblemCase; set labels for dedup; `state = open`, `firstSeen = now`
  - Update → `client.Status().Patch` lastSeen + symptoms
  - Resolve → `problemcase_reconciler.go` re-evaluates affected resource; if no symptoms → patch `state = resolved`, `resolvedAt = now`

---

### Phase 2A — diagnostics-worker (Go)

**[2A.1] DiagnosticRun watcher**
- controller-runtime controller on DiagnosticRun CRDs; predicate: only `state == pending`
- On reconcile: set `running` → run collectors → set `completed` or `failed`

**[2A.2] Collectors** (`Collector` interface: `Collect(ctx, ref) (Evidence, error)`)
- `collector/pod.go` — pod spec + status (phase, conditions, containerStatuses, initContainerStatuses)
- `collector/events.go` — events: involvedObject.name == podName, last 50
- `collector/logs.go` — current logs tail 500 lines + previous logs tail 200 lines; capture truncation info
- `collector/owner.go` — walk ownerReferences: ReplicaSet → Deployment/StatefulSet/DaemonSet spec + status
- `collector/service.go` — services whose selector matches pod labels + their EndpointSlices
- `collector/ingress.go` — ingresses whose backend service matches; collect backend config
- `collector/configmap.go` — configmap metadata (name, namespace, keys list) from pod spec; no .data values
- `collector/secret.go` — secret metadata only (name, namespace, keys list, type); `.data` and `.stringData` explicitly excluded at collection time
- `collector/node.go` — node conditions for the scheduled node

**[2A.3] Redaction pipeline**
- `redact/pipeline.go`: regex patterns over all string fields in evidence payload
- Patterns: Bearer tokens, Basic auth, password=, passwd=, secret=, api_key=, apikey=, token=, AKIA... (AWS keys), PEM headers
- Replace with `[REDACTED]`; populate `redactionSummary.redactedFields`

**[2A.4] POST evidence to backend-api**
- `POST http://backend-api/internal/evidence` after collection + redaction
- On failure: partial evidence with `collectionErrors` populated; still mark state = completed

---

### Phase 2B — backend-api core (Go)

**[2B.1] HTTP server + SQLite**
- `chi` router + `modernc.org/sqlite` driver (CGO-free, pure Go)
- Embedded SQL migration files applied at startup
- Full schema: see Section 10 (Database Schema)

**[2B.2] REST endpoints**
- `GET  /api/v1/problemcases` — list from CRD API; ?severity=, ?status=, ?namespace=; paginate via continue token
- `GET  /api/v1/problemcases/:id`
- `GET  /api/v1/problemcases/:id/evidence` — from SQLite by problem_case_id
- `GET  /api/v1/diagnosticruns/:id`
- `GET  /api/v1/analysisresults/:id` — from SQLite
- `POST /api/v1/problemcases/:id/analyze` — create DiagnosticRun CRD (state=pending) + insert analysis_requests row
- `GET  /api/v1/settings` / `PUT /api/v1/settings`
- `GET  /api/v1/persona/idle-message` — triggers lightweight Bedrock call for idle chatter
- `GET  /healthz` + `GET /readyz`
- `POST /internal/evidence` — store in SQLite; update DiagnosticRun CRD evidenceRef; dispatch to llm-gateway if analysis_requests row exists
- `POST /internal/analysis-result` — store in SQLite; update analysis_requests; patch ProblemCase CRD latestAnalysisResultRef; push WS event

**[2B.3] WebSocket hub**
- `ws/hub.go`: Hub with register/unregister/broadcast channels; goroutine-safe
- `ws/client.go`: read pump + write pump; ping every 25s, write deadline 10s
- `ws/events.go`: typed event structs → JSON
- `GET /ws` upgrades HTTP → WebSocket

**[2B.4] CRD watch → WS broadcast**
- controller-runtime informer in backend-api on ProblemCase, DiagnosticRun, AnalysisResult CRDs
- OnAdd/OnUpdate/OnDelete → serialize WS event → hub.broadcast

**[2B.5] Startup recovery**
- On startup: query `analysis_requests WHERE status = 'pending'` → re-dispatch to llm-gateway
- Prevents lost analysis intent across restarts

---

### Phase 3A — llm-gateway (Python + FastAPI)

**[3A.1] FastAPI scaffold**
- `main.py`: lifespan for boto3 client init
- `config.py`: Pydantic BaseSettings — BEDROCK_REGION, BEDROCK_MODEL_ID (default: qwen3-32b), THINKING_BUDGET (int, default 0), BACKEND_API_URL
- `POST /analyze` → 202 Accepted; background task runs analysis
- `GET /healthz`

**[3A.2] System prompt template** (first-class versioned artifact)
- File: `prompts/system_prompt.md` — Jinja2 template, never inline in code
- Sections:
  1. **Role** — "You are a Kubernetes diagnostic assistant..."
  2. **Constraints** — evidence-only; no invented facts; no destructive defaults; no claim of performed actions
  3. **Evidence glossary** — pod phase definitions, exit codes, event reason codes, condition types
  4. **Output contract** — "You MUST call the `submit_analysis` tool with a JSON object matching this schema exactly."
  5. **Confidence calibration** — degrade confidence when evidence missing/truncated; state reason explicitly
  6. **Persona block** (Jinja2 `{% if persona_enabled %}`) — "Also populate `styledMessage`. Same technical meaning. No new facts, no new warnings."
- `prompts/tool_schema.json` — Bedrock tool use definition for `submit_analysis`

**[3A.3] Evidence priority-tier builder**
- `prompt_builder.py`: `build_evidence_block(evidence, token_budget=120000) -> str`
- Fill order until budget exhausted:
  - Tier 1 (always): detector result, pod phase, container states, exit codes, latest 10 events
  - Tier 2: current logs tail 300 lines, previous logs tail 100 lines
  - Tier 3: owner manifest spec + status, deployment/statefulset status
  - Tier 4: service selectors, endpoint state, node conditions, configmap/secret metadata
- Append "NOTE: evidence truncated at tier N" when budget exhausted

**[3A.4] Bedrock Converse adapter**
- `bedrock_client.py`: boto3 bedrock-runtime; IRSA injects creds automatically; static env var creds as fallback
- Converse call: system prompt, user message (evidence block), toolConfig with toolChoice forced to `submit_analysis`
- THINKING_BUDGET > 0: `additionalModelRequestFields: { thinking: { type: "enabled", budget_tokens: N } }`
- THINKING_BUDGET == 0: prepend `/no_think` to system prompt
- Validation failure: retry once with stricter prompt; exponential backoff on throttling; max 3 retries total

**[3A.5] Consistency checker**
- `consistency.py`: if styledMessage present, scan for fact-introducing phrases absent from likelyRootCause + recommendedRunbook
- Deny-list: "actually", "also note that", "warning:", "critical:", "you must", "immediately"
- On match: set consistencyCheckStatus = warning; log; do NOT block delivery

**[3A.6] Result write-back**
- Success: `POST backend-api/internal/analysis-result`
- Failure after retries: POST with status = failed + error_message

---

### Phase 3B — backend-api analysis completion (Go)

- `POST /internal/analysis-result`: validate → upsert analysis_results in SQLite → mark analysis_requests completed → patch ProblemCase CRD latestAnalysisResultRef → push AnalysisResult.Completed WS event
- `POST /api/v1/problemcases/:id/analyze` full flow: create DiagnosticRun CRD + insert analysis_requests(pending) → on /internal/evidence arrival → POST llm-gateway/analyze (fire-and-forget with 130s timeout)

---

### Phase 4 — frontend-ui (TypeScript + React)

**[4.1] Scaffold + infrastructure**
- Vite + React 19 + TypeScript
- react-router v7 for routing
- TanStack Query v5 for data fetching
- Shadcn/ui (Radix primitives + Tailwind) for components
- `src/api/client.ts` — typed fetch wrapper; base URL from VITE_API_URL
- `src/hooks/useWebSocket.ts` — connects, reconnects with exponential backoff (max 30s), calls `queryClient.invalidateQueries(keysForEvent(event))` on message; exposes connectionStatus
- `src/api/queries.ts` — all TanStack Query definitions
- `src/types/` — TypeScript interfaces mirroring all API response shapes

**[4.2] Overview screen** (`/`)
- ClusterHealthBanner — derived from open count + max severity
- SeverityBreakdown — count cards: critical / high / medium / low
- TopProblems — 5 most severe open ProblemCases
- Subscribes to ProblemCase.* WS events → invalidate overview queries

**[4.3] Problem Inbox screen** (`/problems`)
- ProblemTable — paginated, sortable (TanStack Table)
- Filters: severity multi-select, status select, namespace select — all via URL query params
- Columns: severity badge, namespace, kind/name, detector, firstSeen, lastSeen, analysisStatus
- WS events ProblemCase.Created/Updated/Resolved → invalidate list query

**[4.4] Problem Detail screen** (`/problems/:id`)
- ProblemSummaryCard — affectedResource, detector, severity, firstSeen/lastSeen
- AnalysisPanel — likelyRootCause, confidence gauge, evidenceMapping list
- RunbookPanel — markdown-rendered runbook
- KubectlCommandsPanel — copyable command list
- RawEvidencePanel — collapsible JSON viewer
- AnalysisHistoryList — past AnalysisResults ordered by created_at
- PersonaMessageCard — styledMessage when persona enabled
- ReAnalyzeButton — POST /api/v1/problemcases/:id/analyze; disabled during in-progress DiagnosticRun
- WS events DiagnosticRun.StatusChanged, AnalysisResult.Completed/Failed → invalidate + inline status

**[4.5] Settings screen** (`/settings`)
- PersonaToggle — PUT /api/v1/settings persona.enabled
- IdleChatterToggle — PUT /api/v1/settings persona.idle_chatter
- BedrockConfig — display-only: model ID + region (set via Helm)
- ThinkingBudgetDisplay — read from settings bedrock.thinking_budget

**[4.6] Persona speech bubble + idle chatter**
- PersonaBubble — floating component, bottom-right, z-index above main content
- Analysis mode: shows styledMessage from highest-severity open problem with completed analysis
- Idle mode: client-side 5-minute timer → GET /api/v1/persona/idle-message → render
  - Only fires when: persona.enabled AND persona.idle_chatter AND no critical/high problem active
  - Visually distinct: no severity badge, no notification, dismissable per session

---

### Phase 5 — Hardening

**[5.1] RBAC finalization**
- Audit all ClusterRole manifests: minimum required verbs only
- Unit test: assert diagnostics-worker collected evidence never contains secret .data keys
- Network Policy manifests — optional, off by default, documented in values.yaml

**[5.2] Observability — all services**
- Go: log/slog structured JSON; prometheus/client_golang /metrics; instrument HTTP handlers + CRD ops + detector fire rate counter
- Python: structlog + prometheus_fastapi_instrumentator; counters for Bedrock calls, retries, consistency warnings, thinking_budget usage
- /healthz liveness (process alive) + /readyz readiness (CRD client connected + SQLite reachable) on all services

**[5.3] E2E tests**
- Docker Desktop Kubernetes + helm install; deploy misbehaving workloads
- Scenarios: ImagePullBackOff ProblemCase creation, CrashLoopBackOff, auto-resolve on fix, analysis trigger → AnalysisResult stored, WS event received by client
- `make e2e` target orchestrates install + test scenarios + assertions on CRD states

**[5.4] Helm + docs**
- values.yaml fully commented (every field)
- NOTES.txt: post-install instructions (UI URL, IRSA setup steps, LLM config)
- README: prerequisites, install, IRSA setup, CRD upgrade procedure, uninstall
- values-dev.yaml for Docker Desktop local dev

---

## 6. CRD Schema Proposal

### ProblemCase (kubechan.io/v1alpha1)

Labels (for dedup selector):
- `kubechan.io/affected-resource: <namespace.kind.name>`
- `kubechan.io/detector: <detectorName>`

spec:
- affectedResource: { namespace, kind, name }
- detector: string
- severity: critical | high | medium | low
- symptoms: []string
- relatedResources: [{ namespace, kind, name }]

status:
- state: open | investigating | resolved
- firstSeen: datetime
- lastSeen: datetime
- resolvedAt: datetime (optional)
- latestDiagnosticRunRef: string (DiagnosticRun name)
- latestAnalysisResultRef: string (SQLite analysis_results.id)

### DiagnosticRun (kubechan.io/v1alpha1)

spec:
- problemCaseRef: string
- requestedAt: datetime

status:
- state: pending | running | completed | failed
- collectedAt: datetime
- collectorVersion: string
- evidenceRef: string (SQLite evidence.id)
- collectionErrors: []string
- redactionSummary: { patternsApplied: int, redactedFields: []string }
- logTruncationInfo: { truncated: bool, originalBytes: int, truncatedBytes: int }

### AnalysisResult CRD
Not used. AnalysisResult data lives entirely in SQLite.
ProblemCase.status.latestAnalysisResultRef points to SQLite analysis_results.id.

---

## 7. Helm Chart Structure

ArgoCD-style: CRDs live in `helm/kubechan/crds/` — installed first, never deleted on uninstall.

```
helm/kubechan/
  Chart.yaml
  values.yaml
  values-dev.yaml              ← Docker Desktop overrides
  crds/
    problemcase.yaml           ← generated by controller-gen
    diagnosticrun.yaml
  templates/
    cluster-watcher/
      deployment.yaml
      serviceaccount.yaml
      clusterrole.yaml
      clusterrolebinding.yaml
      configmap.yaml           ← debounce windows, detector thresholds
    diagnostics-worker/
      deployment.yaml
      serviceaccount.yaml
      clusterrole.yaml
      clusterrolebinding.yaml
    backend-api/
      deployment.yaml
      service.yaml
      serviceaccount.yaml
      clusterrole.yaml
      clusterrolebinding.yaml
      pvc.yaml                 ← SQLite PVC
    llm-gateway/
      deployment.yaml
      service.yaml
      serviceaccount.yaml      ← annotated with IRSA role ARN if llmGateway.irsa.roleArn set
    frontend-ui/
      deployment.yaml
      service.yaml
      ingress.yaml             ← conditional on frontendUi.ingress.enabled
    _helpers.tpl
    NOTES.txt
```

Key values.yaml toggles:
```yaml
clusterWatcher:
  enabled: true
  debounceWindowSecs: 30
  pendingTooLongThresholdSecs: 300

diagnosticsWorker:
  enabled: true
  logTailLines: 500
  prevLogTailLines: 200

backendApi:
  enabled: true
  storage:
    pvc:
      size: 5Gi
      storageClass: ""
  retention:
    evidenceDays: 7
    analysisResultsDays: 30

llmGateway:
  enabled: true
  bedrock:
    region: us-east-1
    modelId: qwen3-32b
    thinkingBudget: 0
    evidenceTokenBudget: 120000
  irsa:
    roleArn: ""                 # set for EKS IRSA
  credentials:
    secretName: ""              # fallback: k8s secret with AWS_ACCESS_KEY_ID etc.

frontendUi:
  enabled: true
  ingress:
    enabled: false

persona:
  enabled: false
  idleChatter: false
  idleIntervalSecs: 300
```

**CRD upgrade note:** `helm upgrade` does NOT update CRDs in `crds/`. For CRD schema changes, manually apply updated CRDs before running `helm upgrade` — same pattern as ArgoCD. Document in upgrade guide.

---

## 8. Security / RBAC Plan

### kubechan:cluster-watcher ClusterRole
```yaml
rules:
- apiGroups: [""]
  resources: [pods, nodes, services, endpoints, events, configmaps]
  verbs: [get, list, watch]
- apiGroups: [apps]
  resources: [deployments, replicasets, statefulsets, daemonsets]
  verbs: [get, list, watch]
- apiGroups: [networking.k8s.io]
  resources: [ingresses]
  verbs: [get, list, watch]
- apiGroups: [discovery.k8s.io]
  resources: [endpointslices]
  verbs: [get, list, watch]
- apiGroups: [kubechan.io]
  resources: [problemcases, problemcases/status]
  verbs: [get, list, watch, create, update, patch]
```

### kubechan:diagnostics-worker ClusterRole
```yaml
rules:
- apiGroups: [""]
  resources: [pods, pods/log, nodes, services, endpoints, configmaps]
  verbs: [get, list, watch]
- apiGroups: [""]
  resources: [secrets]
  verbs: [get, list]             # implementation MUST strip .data and .stringData
- apiGroups: [apps]
  resources: [deployments, replicasets, statefulsets, daemonsets]
  verbs: [get, list, watch]
- apiGroups: [networking.k8s.io]
  resources: [ingresses]
  verbs: [get, list, watch]
- apiGroups: [discovery.k8s.io]
  resources: [endpointslices]
  verbs: [get, list, watch]
- apiGroups: [kubechan.io]
  resources: [diagnosticruns, diagnosticruns/status]
  verbs: [get, list, watch, update, patch]
```

### kubechan:backend-api ClusterRole
```yaml
rules:
- apiGroups: [kubechan.io]
  resources: [problemcases, problemcases/status, diagnosticruns, diagnosticruns/status]
  verbs: [get, list, watch, create, update, patch, delete]
- apiGroups: [""]
  resources: [configmaps]
  verbs: [get, list, create, update]
```

### llm-gateway
- **No ClusterRole. Zero Kubernetes API access.**
- Receives pre-collected, pre-redacted evidence via HTTP from backend-api only
- Bedrock credentials: IRSA (preferred for EKS) or static creds Secret (fallback)
- Not reachable externally; ClusterIP service only

### Additional controls
- Inter-service comms: in-cluster DNS only (`http://backend-api.kubechan.svc.cluster.local`)
- Secret `.data`/`.stringData` excluded at diagnostics-worker collection time (code + unit test)
- Redaction pipeline runs before any POST to backend-api
- Network Policy (optional, off by default): restrict llm-gateway egress to Bedrock endpoint only
- frontend-ui: HTTPS via Ingress TLS or LoadBalancer TLS in production
- backend-api: write paths restricted to kubechan.io CRDs only (no workload modification)

---

## 9. Open Questions and Risks

### Open Questions

| ID | Question | Implication |
|----|----------|-------------|
| OQ-3 | UI auth: anonymous single-user or multi-user? | Determines whether backend-api needs auth middleware |
| OQ-4 | backend-api exposure: NodePort, LoadBalancer, or Ingress? | Affects default Helm chart and security posture |
| OQ-7 | DiagnosticRun trigger: automatic on ProblemCase creation, manual only, or both? | Both implied; confirm for UX |
| OQ-9 | Multi-tenancy / namespace isolation in MVP? | Not in description; assume single-tenant |

### Risks

| ID | Risk | Mitigation |
|----|------|-----------|
| R-2 | **LLM latency** — Bedrock slow or rate-limited | Async analysis with WS status updates; 130s timeout + 3 retries with backoff |
| R-3 | **Structured output reliability** — model may not produce valid tool call JSON | Pydantic validation; retry once with stricter prompt; fallback to raw text |
| R-4 | **Secret value leakage** — diagnostics-worker has get/list on secrets | Strip .data at collection time; unit test asserts no secret data in evidence payload |
| R-5 | **Persona correctness** — persona layer changes technical meaning | Consistency checker; raw analysis always canonical and always visible in UI |
| R-6 | **CRD proliferation** — many transient failures on large clusters | Deduplication + debounce in controller; TTL pruning (7-day default) |
| R-7 | **RBAC scope creep** | ClusterRole manifests reviewed in PRs; CI lint on RBAC rules |
| R-8 | **No UI auth in MVP** | Document in-cluster-only intent; recommend Ingress OAuth2-proxy as Helm option |

---

## 10. Database Schema (SQLite — backend-api PVC)

```sql
-- Migration 001

CREATE TABLE evidence (
    id                  TEXT PRIMARY KEY,
    diagnostic_run_id   TEXT NOT NULL,
    problem_case_id     TEXT NOT NULL,
    collected_at        DATETIME NOT NULL,
    collector_version   TEXT NOT NULL,
    payload             TEXT NOT NULL,              -- full evidence JSON blob
    payload_bytes       INTEGER NOT NULL,           -- for retention pruning + auditing
    redaction_summary   TEXT,                       -- JSON: { patternsApplied, redactedFields[] }
    log_truncation_info TEXT,                       -- JSON: { truncated, originalBytes, truncatedBytes }
    collection_errors   TEXT,                       -- JSON array of error strings
    created_at          DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_evidence_problem_case   ON evidence(problem_case_id);
CREATE INDEX idx_evidence_diagnostic_run ON evidence(diagnostic_run_id);
CREATE INDEX idx_evidence_created_at     ON evidence(created_at);

CREATE TABLE analysis_results (
    id                       TEXT PRIMARY KEY,
    problem_case_id          TEXT NOT NULL,
    diagnostic_run_id        TEXT NOT NULL,
    model                    TEXT NOT NULL,
    model_runtime            TEXT NOT NULL DEFAULT 'external',
    status                   TEXT NOT NULL,              -- 'completed' | 'failed'
    likely_root_cause        TEXT,                       -- extracted for list display
    confidence               REAL,                       -- 0.0–1.0
    consistency_check_status TEXT,                       -- 'passed' | 'warning' | 'failed'
    has_styled_message       INTEGER NOT NULL DEFAULT 0, -- 1 if persona message present
    thinking_budget_used     INTEGER,                    -- tokens used (0 = /no_think)
    error_message            TEXT,                       -- populated when status = 'failed'
    payload                  TEXT NOT NULL,              -- full analysis JSON blob
    created_at               DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_ar_problem_case ON analysis_results(problem_case_id);
CREATE INDEX idx_ar_created_at   ON analysis_results(created_at);
CREATE INDEX idx_ar_status       ON analysis_results(status);

-- Durable bridge between DiagnosticRun completion and llm-gateway dispatch
-- Survives backend-api restarts; re-dispatched on startup if status = 'pending'
CREATE TABLE analysis_requests (
    id                TEXT PRIMARY KEY,
    problem_case_id   TEXT NOT NULL,
    diagnostic_run_id TEXT NOT NULL,
    requested_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    status            TEXT NOT NULL DEFAULT 'pending', -- 'pending'|'dispatched'|'completed'|'failed'
    dispatched_at     DATETIME,
    completed_at      DATETIME
);
CREATE INDEX idx_areq_status       ON analysis_requests(status);
CREATE INDEX idx_areq_problem_case ON analysis_requests(problem_case_id);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,           -- JSON-encoded: bool → "true", int → "42", string → '"value"'
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO settings(key, value) VALUES
    ('persona.enabled',            'false'),
    ('persona.idle_chatter',       'false'),
    ('persona.idle_interval_secs', '300'),
    ('bedrock.model_id',           '"qwen3-32b"'),
    ('bedrock.region',             '"us-east-1"'),
    ('bedrock.thinking_budget',    '0'),
    ('evidence.retention_days',    '7'),
    ('analysis.retention_days',    '30');
```

### Table relationships

| Relationship | Description |
|-------------|-------------|
| `evidence.diagnostic_run_id` → `DiagnosticRun.name` (CRD) | Logical FK across store boundary |
| `evidence.problem_case_id` → `ProblemCase.name` (CRD) | Logical FK across store boundary |
| `analysis_results.problem_case_id` → `ProblemCase.name` (CRD) | Logical FK |
| `analysis_results.diagnostic_run_id` → `DiagnosticRun.name` (CRD) | Logical FK |
| `analysis_requests.diagnostic_run_id` → `DiagnosticRun.name` (CRD) | Drives dispatch; one per DiagnosticRun |
| `analysis_requests` → `analysis_results` | One analysis_request fulfilled by one analysis_result |

### Retention pruning
Background goroutine in backend-api, runs every 6 hours:
```sql
DELETE FROM evidence         WHERE created_at < datetime('now', '-' || ? || ' days');
DELETE FROM analysis_results WHERE created_at < datetime('now', '-' || ? || ' days');
DELETE FROM analysis_requests WHERE status IN ('completed','failed')
  AND completed_at < datetime('now', '-30 days');
```

---

## 11. Locked Decisions

| Decision | Choice | Notes |
|----------|--------|-------|
| Evidence storage | SQLite on PVC inside backend-api (MVP) | Zero new deps; swap to Postgres post-MVP |
| LLM provider | AWS Bedrock | Converse API — model-agnostic; swap via Helm value |
| Default model | Qwen3 32B | Upgrade: Qwen3 235B or Claude Sonnet 4.6 via one values.yaml change |
| Auth to Bedrock | IRSA (preferred) | Static credentials Secret as non-EKS fallback |
| Structured output | Bedrock tool use (function calling) | Reliable JSON; Pydantic validation in llm-gateway |
| Thinking mode | Configurable token budget | 0 = /no_think, N = budget_tokens |
| System prompt | Versioned `prompts/system_prompt.md` | Not inline code; independently reviewable |
| backend-api replicas | Single replica (MVP) | SQLite single-writer constraint |
| Evidence in CRDs | Reference ID only | Payload in SQLite; avoids etcd 1.5MB limit |
| Evidence token budget | 120K tokens default | 4-tier fill: pod → logs → manifests → infra |
| Local dev environment | Docker Desktop Kubernetes | No kind needed |
| Helm CRD pattern | `crds/` directory (ArgoCD-style) | Install-first, never deleted on uninstall |
| Controller pattern | controller-runtime Manager + Reconcilers | Shared cache, push-based, idempotent |
| WS pattern | Push event → REST fetch | WS = notifications only; REST = data |
