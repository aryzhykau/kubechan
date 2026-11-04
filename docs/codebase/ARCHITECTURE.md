# Architecture

This document describes the current runtime architecture. Historical implementation proposals live under `docs/plans/`.

## Architectural style

KubeChan is an event-driven Kubernetes application with five deployable services. Kubernetes CRDs carry operational state between controllers; backend-api provides persistence and browser-facing APIs; llm-gateway is an isolated model boundary.

Core constraints:

1. KubeChan never mutates user workloads. Remediation is advisory.
2. KubeChan-owned CRDs may be created and updated by controllers and backend-api.
3. Evidence and graph topology are deterministic. The model may interpret them but cannot extend them.
4. backend-api uses SQLite and is a single-replica, single-writer service.
5. A DiagnosticRun is an immutable evidence and analysis snapshot.

## Runtime topology

```text
Kubernetes API
  │
  ├── cluster-watcher
  │     ├── detectors
  │     ├── ProblemCase CRDs
  │     └── Incident CRDs
  │
  ├── diagnostics-worker
  │     ├── watches DiagnosticRun CRDs
  │     ├── collects evidence
  │     ├── builds atomic source catalog
  │     └── builds deterministic investigation graph
  │              │
  │              └── POST /internal/evidence
  │                         │
  └────────────────── backend-api ───── POST /analyze ───── llm-gateway
                         │                                      │
                         ├── SQLite                              ├── prompt builder
                         ├── REST API                            ├── LiteLLM SDK / Copilot adapter
                         ├── WebSocket hub                       └── assessment validation
                         │
                         └── frontend-ui
                               ├── incident operations
                               ├── diagnostics
                               └── assessment workspace
```

## Incident and analysis flow

### Automatic incident

1. cluster-watcher evaluates Pods, Deployments, Services, Nodes, and related events.
2. A detector creates or updates a ProblemCase after the debounce threshold.
3. CorrelationReconciler follows ownership to the workload root and creates or reuses an Incident.
4. A user requests analysis; backend-api creates a DiagnosticRun.
5. diagnostics-worker collects an immutable evidence snapshot.
6. `collector.BuildSourceCatalog` normalizes fields, log lines, and events into citation-safe sources.
7. `graphengine.Build` derives observed topology, missing references, and ranked candidates from that snapshot.
8. diagnostics-worker POSTs the snapshot to backend-api.
9. backend-api stores evidence and dispatches the analysis request to llm-gateway.
10. llm-gateway builds the prompt, invokes the selected provider, parses Assessment v2, validates citations, and validates the causal path against the supplied graph.
11. backend-api stores the result and broadcasts `Analysis.Completed`.
12. frontend-ui opens the dedicated Assessment route with graph, source inspector, evidence ledger, and remediation runbook.

The diagnostic-run list is refreshed as soon as a start request succeeds. The backend records `pending`, `in_progress`, `completed`, or `failed` on the analysis request and broadcasts `Analysis.InProgress`, `Analysis.Completed`, or `Analysis.Failed` so the browser refreshes the run list at each transition. For older request rows with stale status, the list uses the DiagnosticRun analysis state. The assistant panel also polls the list to recover from missed WebSocket events. Components waiting for one run poll its DiagnosticRun status as a fallback when the completion event is missed.

OpenAI, Anthropic, and Bedrock requests use LiteLLM's async Python SDK. Provider keys and AWS credentials are passed as per-call parameters, not process-wide environment variables. GitHub Copilot remains on its dedicated SDK adapter. Account-subscription OAuth flows are outside the current integration.

Successful analysis responses include provider-reported input/output token usage when available. The gateway estimates missing counts with tiktoken and marks those fields as estimates. The backend stores this metadata in the analysis response payload, which is shown with the model in the incident analysis UI.

### Manual incident

The user selects a root resource, describes the issue, and optionally adds related resources. backend-api creates an Incident, ownership record, analysis request, and DiagnosticRun. Collection and analysis then follow the same path as automatic incidents.

### More-information loop

When a valid response requests more information and names suggested resource kinds, the user may add resources through the augment flow. backend-api updates the Incident and creates a new DiagnosticRun. Prior snapshots remain unchanged.

### Exclusion rules

`KubechanExclusionRule` CRDs suppress matching detectors and can resolve matching open Incidents when enabled. Rules may target exact resources, selectors, detectors, and recurring time windows. The LLM may propose a rule; only the user can confirm its creation.

## Service ownership

| Module | Owns | Must not own |
|---|---|---|
| `api/v1alpha1/` | CRD schemas and generated Kubernetes code | HTTP, persistence, analysis logic |
| `cluster-watcher/detector/` | Stateless anomaly evaluation | Evidence collection or writes |
| `cluster-watcher/controllers/` | Reconciliation, correlation, and KubeChan CRD writes | LLM calls |
| `cluster-watcher/exclusion/` | Rule matching | Detector-specific logic |
| `diagnostics-worker/collector/` | Evidence types and atomic source catalog | Model interpretation |
| `diagnostics-worker/graphengine/` | Deterministic topology and candidate ranking | Kubernetes reads or model calls |
| `diagnostics-worker/controllers/` | Kubernetes collection and DiagnosticRun lifecycle | Analysis conclusions |
| `backend-api/handler/` | REST boundary, orchestration, persistence calls | Prompt construction |
| `backend-api/db/` | SQLite lifecycle, migrations, retention | Kubernetes reconciliation |
| `backend-api/ws/` | Authenticated event fan-out | Business logic |
| `llm-gateway/app/prompt.py` | Prompt construction and context budgeting | Kubernetes access |
| `llm-gateway/app/providers/litellm_provider.py` | LiteLLM-backed OpenAI, Anthropic, and Bedrock calls | Prompt construction or evidence validation |
| `llm-gateway/app/routes.py` | Provider dispatch and trust-boundary validation | Graph construction |
| `frontend-ui/src/store/` | Shared browser state, RTK Query, WebSocket events | Server-side policy |
| `frontend-ui/src/pages/` | Route-level presentation and interaction | Authoritative evidence generation |

## Investigation trust boundary

The contract is detailed in `docs/contracts/investigation-analysis.md`.

- Source IDs are produced before the model call.
- Node, edge, and candidate IDs are produced before the model call.
- Claims may cite only supplied source IDs.
- A causal path may reference only supplied graph IDs.
- Invalid citations are removed and reflected in citation validation.
- Candidate topology is proposed, not observed, and must be visually distinct.

## Persistence and recovery

backend-api stores users, settings, evidence, requests, prompts, and analysis results in SQLite using embedded migrations. Analysis dispatch is asynchronous. Pending requests are recovered on backend startup, but there is no durable external job queue.

## Primary patterns

- Kubernetes controller/reconciler for level-triggered state handling.
- Strategy interfaces for detectors and LLM providers.
- Deterministic builders for source catalog and graph topology.
- Validation boundary around non-deterministic model output.
- RTK Query for browser server state and WebSocket invalidation/update events.
- Embedded SQL migrations and startup recovery.

## Known risks

- SQLite prevents safe horizontal scaling of backend-api.
- `/internal/evidence` relies on cluster network isolation rather than application authentication.
- LLM dispatch has startup recovery but no durable queue or exactly-once guarantee.
- WebSocket JWT query parameters can be exposed by proxy access logs.
- Full prompts and raw model responses are currently logged and stored; production retention and redaction need explicit policy.
- There is no automated browser E2E suite; Tilt validation is currently an operator-driven runtime check.

See `docs/codebase/CONCERNS.md` for the broader risk register.
