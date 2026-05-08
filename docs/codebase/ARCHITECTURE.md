# Architecture

## Core Sections (Required)

### 1) Architectural Style

- **Primary style:** Event-driven microservices pipeline with Kubernetes CRDs as the event bus
- **Why this classification:** Services do not call each other directly (except diagnostics-worker → backend-api POST, and backend-api → llm-gateway POST). State transitions are communicated by writing or watching Kubernetes CRDs (`ProblemCase`, `Incident`, `DiagnosticRun`). Each service reconciles CRD state changes via controller-runtime.
- **Primary constraints:**
  1. Read-only toward user workloads — no service may modify Pods, Deployments, or other user-owned resources.
  2. CRDs are the only shared state between cluster-watcher, diagnostics-worker, and backend-api. No direct service-to-service calls for events.
  3. SQLite single-writer in backend-api — no horizontal scaling of that service without replacing the data store.

---

### 2) System Flow

#### Auto incident flow

```
cluster-watcher reconciler
  → detects anomaly (CrashLoopBackOff / ImagePullBackOff / PendingTooLong / etc.)
  → creates ProblemCase CRD

CorrelationReconciler (cluster-watcher)
  → watches ProblemCase events
  → walks owner-reference chain to find workload root
  → creates or reuses an Incident CRD

diagnostics-worker DiagnosticRunReconciler
  → watches DiagnosticRun CRDs (created by backend-api on user "Analyze" action)
  → collects evidence (logs, events, ConfigMaps, Secrets metadata, PVCs, Ingresses)
  → POST /internal/evidence to backend-api

backend-api Internal.ReceiveEvidence
  → stores evidence in SQLite
  → creates analysis_request record
  → dispatches goroutine: POST /analyze to llm-gateway

llm-gateway /analyze
  → builds Jinja2 prompt from evidence payload
  → calls AWS Bedrock or GitHub Copilot LLM
  → parses structured JSON response
  → returns AnalyzeResponse to backend-api

backend-api
  → stores analysis_result in SQLite
  → broadcasts Analysis.Completed event via WebSocket Hub

frontend-ui useWebSocket hook
  → receives WS event
  → renders 5-section analysis in KubeChanSidebar
```

#### Manual incident flow

```
User fills ManualIncidentModal
  → POST /api/v1/incidents/manual
  → backend-api creates Incident CRD (source=manual) + DiagnosticRun CRD + analysis_request
  → same evidence collection and LLM pipeline runs
```

#### Needs-more-info flow

```
LLM returns needsMoreInfo=true (confidence < 0.65) + suggestedResources
  → frontend renders AugmentIncidentModal
  → User selects additional resources
  → POST /api/v1/incidents/{id}/augment
  → backend-api merges resources into Incident CRD, creates new DiagnosticRun
  → full pipeline re-runs with augmented evidence
```

#### Exclusion rule flow

```
LLM returns suggestExclusionRule (ExclusionRuleProposal) in AnalyzeResponse
  → frontend renders ExclusionRuleModal with pre-filled proposal
  → User confirms (optionally edits detectors, target resources, time window)
  → POST /api/v1/exclusion-rules
  → backend-api creates KubechanExclusionRule CRD in control namespace

ExclusionRuleReconciler (cluster-watcher) watches KubechanExclusionRule events
  → on create/update of an enabled rule: scans all open Incidents
  → auto-resolves Incidents whose root resource + detector match the rule

exclusion.IsExcluded() called inside every detector reconciler
  → if a matching enabled rule exists → ProblemCase is not created / is suppressed
  → time window respected (rule only active during configured recurrence periods)

User can also manage rules manually:
  GET  /api/v1/exclusion-rules        → list all rules (ExclusionRulesPage)
  POST /api/v1/exclusion-rules        → create a new rule
  PATCH /api/v1/exclusion-rules/{name} → enable/disable a rule
  DELETE /api/v1/exclusion-rules/{name} → delete a rule
```

---

### 3) Layer/Module Responsibilities

| Layer or module | Owns | Must not own | Evidence |
|-----------------|------|--------------|----------|
| `api/v1alpha1/` | CRD type structs, scheme registration, generated DeepCopy | HTTP, DB, business logic | `api/v1alpha1/incident_types.go` |
| `cluster-watcher/detector/` | Stateless anomaly detection from k8s object state | ProblemCase writes, debounce state | `services/cluster-watcher/detector/interface.go` |
| `cluster-watcher/controllers/` | Reconcile loop, debounce management, ProblemCase/Incident CRD writes | Detection logic, evidence collection | `services/cluster-watcher/controllers/correlation_reconciler.go` |
| `cluster-watcher/exclusion/` | Rule evaluation — `IsExcluded()` checks `KubechanExclusionRule` CRDs (by resource, detector name, time window) | Detection logic, CRD writes | `services/cluster-watcher/exclusion/matcher.go` |
| `cluster-watcher/controllers/exclusionrule_reconciler.go` | Watches `KubechanExclusionRule` events; on enable auto-resolves matching open Incidents + ProblemCases | Exclusion matching logic | `services/cluster-watcher/controllers/exclusionrule_reconciler.go` |
| `backend-api/handler/exclusion_rules.go` | REST CRUD for `KubechanExclusionRule` CRDs (list, create, patch enabled, delete) | Kubernetes watch/reconcile | `services/backend-api/handler/exclusion_rules.go` |
| `diagnostics-worker/controllers/` | DiagnosticRun reconciliation, evidence collection orchestration, POST to backend-api | Detection logic, LLM calls | `services/diagnostics-worker/main.go` |
| `backend-api/handler/` | HTTP request handling, input validation, CRD reads/writes, DB queries, LLM dispatch goroutine | Business logic shared across handlers | `services/backend-api/handler/internal.go` |
| `backend-api/db/` | SQLite open, schema migrations, pruner | HTTP layer, Kubernetes client | `services/backend-api/db/db.go` |
| `backend-api/ws/` | WebSocket Hub (broadcast to all clients), per-client goroutines | Business logic | `services/backend-api/ws/hub.go` |
| `backend-api/k8s/` | Kubernetes client wrapper, informer cache Watcher, MoodSyncer | HTTP handlers | `services/backend-api/k8s/` |
| `backend-api/startup/` | Admin user bootstrap, pending analysis_request recovery on restart | Request handling | `services/backend-api/startup/admin_bootstrap.go` |
| `llm-gateway/app/` | Prompt building, provider selection, LLM call, response parsing | Kubernetes API access | `services/llm-gateway/app/routes.py` |
| `frontend-ui/src/` | React UI, state management, WebSocket client, API client | Server logic | `services/frontend-ui/src/App.tsx` |

---

### 4) Reused Patterns

| Pattern | Where found | Why it exists |
|---------|-------------|---------------|
| **Strategy (interface)** | `cluster-watcher/detector/interface.go` — `Detector` interface with `Name()` + `Evaluate()` | Allows each anomaly type to be independently implemented and tested without changing reconciler logic |
| **Controller/Reconciler** | All controllers in cluster-watcher and diagnostics-worker via controller-runtime | Standard Kubernetes controller pattern; level-triggered reconciliation with cache-backed reads |
| **Hub-and-spoke WebSocket broadcast** | `backend-api/ws/hub.go` | Fan-out to all connected frontend clients; slow-consumer protection via non-blocking channel send |
| **Embedded SQL migrations** | `backend-api/db/db.go` with `//go:embed migrations/*.sql` | Schema migrations applied at startup from embedded FS; tracked in `schema_migrations` table |
| **Startup recovery** | `backend-api/startup/recovery.go` — re-dispatches pending analysis requests | Compensates for lack of a durable job queue; handles crashes mid-pipeline |
| **Provider adapter** | `llm-gateway/app/providers/base.py` `LLMProvider` ABC with `BedrockProvider` and `CopilotProvider` | Decouples LLM backend selection from prompt building and response parsing |
| **Debounce** | `cluster-watcher/debounce/debouncer.go` | Suppresses transient cluster states (e.g., rolling-update restarts) to avoid false ProblemCases |
| **Singleton CRD** | `KubeChanState` — one per namespace, managed by `MoodSyncer` | Persists mood level across backend-api restarts without a separate DB field |

---

### 5) Known Architectural Risks

- **SQLite single-writer:** `backend-api` uses WAL-mode SQLite on a volume. Multiple replicas are not supported. Evidence writes during burst analysis can create write contention. See CONCERNS.md.
- **No durable job queue:** LLM dispatch uses an in-memory goroutine launched by `ReceiveEvidence`. On crash, `startup.RecoverPendingRequests` replays pending requests, but any in-flight work is lost. If the backend-api crashes after evidence is stored but before dispatch, recovery relies on correct DB state.
- **Internal endpoint unauthenticated:** `POST /internal/evidence` has no auth check. It relies on network-level isolation (cluster-internal DNS). If cluster network policy is not enforced, any pod can inject evidence.
- **WebSocket token in URL:** Token is passed as `?token=` query parameter. This is logged by most HTTP servers/proxies in access logs. See CONCERNS.md.

---

### 6) Evidence

- `services/backend-api/main.go` — full wiring of handlers, informer cache, WebSocket hub, MoodSyncer
- `services/cluster-watcher/controllers/correlation_reconciler.go` — ProblemCase → Incident correlation
- `services/cluster-watcher/detector/interface.go` — `Detector` strategy interface
- `services/backend-api/handler/internal.go` — `ReceiveEvidence` → LLM dispatch goroutine
- `services/backend-api/ws/hub.go` — broadcast hub implementation
- `services/llm-gateway/app/providers/base.py` — provider factory pattern
- `services/backend-api/startup/recovery.go` — startup recovery pattern
- `services/cluster-watcher/controllers/exclusionrule_reconciler.go` — ExclusionRuleReconciler auto-resolves Incidents on rule creation
- `services/cluster-watcher/exclusion/matcher.go` — `IsExcluded()` with time-window evaluation
- `api/v1alpha1/exclusionrule_types.go` — `KubechanExclusionRule` CRD spec (TargetResources, Selector, TimeWindow)
- `services/llm-gateway/app/models.py` — `ExclusionRuleProposal` in `AnalyzeResponse`
- `docs/project-description.md` — end-to-end flow description
