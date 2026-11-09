# KubeChan

<p align="center">
  <img src="services/frontend-ui/public/kubechan-idle-1.png" alt="KubeChan" width="160" />
</p>

A read-only Kubernetes AI troubleshooting assistant. KubeChan watches your cluster, detects broken workloads, collects diagnostic evidence, and calls an LLM to produce a root-cause analysis — surfaced through a web UI with an opinionated tsundere character who will absolutely judge your configuration choices.

The opt-in [snapshot agent](docs/contracts/snapshot-agent.md) investigates saved evidence and streams progress and cited findings to the Assessment page.

> KubeChan never modifies cluster state. It is advisory only.

---

## Architecture overview

```
┌──────────────────────────────────────────────────────────────────────┐
│  Kubernetes cluster                                                  │
│                                                                      │
│  ┌────────────────────┐  ProblemCase/Incident  ┌──────────────────┐ │
│  │  cluster-watcher   │──────── CRDs ─────────▶│diagnostics-worker│ │
│  │  (Go controller)   │                         │  (Go controller) │ │
│  └────────────────────┘                         └────────┬─────────┘ │
│           │                                              │            │
│    watches Pods, Deployments,                   POST /internal/      │
│    Services, Ingresses, Nodes                   evidence             │
│                                                          ▼            │
│                                              ┌───────────────────┐  │
│  User (manual incident) ──────────────────▶  │  backend-api (Go) │  │
│                                              │  chi + SQLite     │  │
│                                              └──────┬────────────┘  │
│                                                     │               │
│                              POST /analyze          │  WebSocket     │
│                              ┌──────────────────────┘  events       │
│                              ▼                          │             │
│                   ┌──────────────────────┐              │             │
│                   │   llm-gateway        │              │             │
│                   │   (Python/FastAPI)   │              │             │
│                   │ LiteLLM SDK +        │              │             │
│                   │ Copilot adapter      │              │             │
│                   └──────────────────────┘              │             │
│                                                         ▼             │
│                                          ┌──────────────────────┐   │
│                                          │   frontend-ui        │   │
│                                          │  (React + Vite)      │   │
│                                          └──────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Incident pipelines

KubeChan supports two parallel ways incidents can be created and analyzed.

### Auto pipeline — cluster-watcher detects a problem

```
cluster-watcher detects anomaly (CrashLoopBackOff, ServiceNoEndpoints, …)
     │
     ▼
Creates ProblemCase CRD (with affected resource, detector, severity, symptoms)
     │
     ▼
CorrelationReconciler groups ProblemCase into an Incident CRD
(reuses an existing open Incident for the same workload root if one exists)
     │
     ▼
User opens UI → sees open Incident → clicks "Ask KubeChan to help"
     │
     ▼  POST /api/v1/incidents/{id}/analyze
backend-api creates DiagnosticRun CRD
     │
     ▼
diagnostics-worker reconciles DiagnosticRun:
  ├── collects pod logs (current + previous crash logs)
  ├── collects K8s events on the pod
  ├── collects PVC status and events
  ├── collects ConfigMap contents + mount paths
  ├── auto-discovers all Ingresses in the namespace
  ├── normalizes observations into immutable, addressable evidence sources
  ├── builds a deterministic resource graph from the same evidence snapshot
  └── POSTs structured evidence to backend-api /internal/evidence
     │
     ▼
backend-api stores evidence, dispatches async goroutine
     │
     ▼  POST /analyze
llm-gateway:
  ├── builds prompt from all collected evidence
  ├── calls OpenAI, Anthropic, or Bedrock through LiteLLM; Copilot keeps its SDK adapter
  ├── parses and validates an evidence-backed assessment
  ├── accepts only citations and graph IDs present in the evidence snapshot
  └── if LLM confidence < 0.65 → sets needsMoreInfo + suggestedResources
     │
     ▼
backend-api stores analysis result, broadcasts WebSocket event
     │
     ▼  WebSocket "Analysis.Completed"
frontend-ui → dedicated Assessment page renders the diagnosis, graph,
              evidence chain, source inspector, and remediation runbook
```

### Manual pipeline — user reports an issue

```
User clicks "+ Report an issue" in the UI
     │
     ▼
ManualIncidentModal:
  ├── picks root resource (kind + name, namespace-scoped, includes Ingresses)
  ├── writes a description of the problem
  └── optionally tags related resources (Service, Deployment, ConfigMap, Ingress, PVC, …)
     │
     ▼  POST /api/v1/incidents/manual
backend-api:
  ├── creates Incident CRD (source: manual, userMessage, relatedResources)
  ├── creates DiagnosticRun CRD
  └── creates analysis_request record
     │
     ▼
diagnostics-worker + llm-gateway pipeline runs identically to auto pipeline
(user message is included verbatim in the LLM prompt)
     │
     ▼
User can manually resolve the incident at any time via the "Resolve" button
```

### Needs-more-info loop

When the LLM returns `needsMoreInfo: true` (confidence < 0.65), the frontend shows a banner listing the suggested resource kinds (with LLM-generated reasons). The user can click **Add context & Re-analyze** to open the `AugmentIncidentModal`, select the suggested resources, and trigger a new DiagnosticRun that merges the new evidence with the original.

---

## Services

### `cluster-watcher` — Go, controller-runtime

Watches all Pods, Deployments, Services, and Nodes. Runs a set of detectors on every reconcile event, creates/updates **ProblemCase** and **Incident** CRDs when anomalies are detected. A debounce window (default 30 s, 5 s in dev) prevents noise during rollouts.

**Detectors:**

| Detector | Trigger |
|---|---|
| `CrashLoopBackOffDetector` | Pod in CrashLoopBackOff |
| `ImagePullBackOffDetector` | Pod failing to pull image |
| `PendingTooLongDetector` | Pod Pending > threshold (default 5 min) |
| `DeploymentUnavailableDetector` | Deployment has unavailable replicas |
| `ServiceNoEndpointsDetector` | Service with no ready endpoints |

**Key env vars:**

| Var | Default | Description |
|---|---|---|
| `DEBOUNCE_WINDOW_SECS` | `30` | Seconds before re-evaluating a workload |
| `PENDING_THRESHOLD_SECS` | `300` | Seconds before a Pending pod is flagged |
| `CONTROL_NAMESPACE` | `kubechan` | Namespace where CRDs are written |

---

### `diagnostics-worker` — Go, controller-runtime

Reconciles **DiagnosticRun** CRDs. Collects structured diagnostic evidence for the referenced incident workload and POSTs it to backend-api.

**Evidence collected:**

- Pod status, current logs, previous container logs (crash evidence)
- Kubernetes events on the pod and root resource
- ConfigMap contents: keys, file mount paths, env var names (values of sensitive keys redacted)
- Secret existence check (values never collected)
- PVC phase and events
- All Ingresses in the workload namespace — auto-discovered regardless of whether the user tagged them

**Investigation graph:**

- Built by `graphengine` from the exact resources included in the evidence snapshot; the LLM cannot add nodes or relationships
- Represents ownership, Service selectors, Ingress backends, pod configuration dependencies, Secrets, and PVC mounts
- Keeps unresolved references as explicit `missing` nodes instead of silently dropping them
- Ranks replacement candidates using deterministic signals such as `renamed-from`, selector matches, and compatible ports
- Separates observed and proposed topology: for example, `Ingress → missing Service` is `broken`, while `Ingress → candidate Service` is `should route to`
- Attaches stable source IDs to nodes and edges so every UI claim can open the exact metadata, event, or log record behind it

**Key env vars:**

| Var | Default | Description |
|---|---|---|
| `BACKEND_API_URL` | `http://kubechan-backend-api:8080` | Where to POST evidence |
| `LOG_TAIL_LINES` | `200` | Lines of current logs |
| `PREV_LOG_TAIL_LINES` | `100` | Lines of previous (crashed) container logs |

---

### `backend-api` — Go, chi + SQLite

Central API server and orchestration hub. Serves the frontend REST + WebSocket API, stores all persistent state in a local SQLite database (auto-migrated on startup), and dispatches async analysis requests to llm-gateway.

**API surface:**

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/incidents` | List incidents (filterable by state, namespace) |
| `GET` | `/api/v1/incidents/{id}` | Get a single incident |
| `POST` | `/api/v1/incidents/manual` | Create a manual incident + kick off analysis |
| `POST` | `/api/v1/incidents/{id}/analyze` | Trigger a new DiagnosticRun |
| `POST` | `/api/v1/incidents/{id}/augment` | Add more related resources + re-analyze |
| `POST` | `/api/v1/incidents/{id}/resolve` | Manually resolve an incident (patches CRD status) |
| `GET` | `/api/v1/incidents/{id}/evidence` | Fetch latest collected evidence |
| `GET` | `/api/v1/diagnosticruns` | List DiagnosticRun summaries with analysis results |
| `DELETE` | `/api/v1/diagnosticruns` | Bulk-delete DiagnosticRun records |
| `GET` | `/api/v1/diagnosticruns/{id}` | Get a single DiagnosticRun |
| `DELETE` | `/api/v1/diagnosticruns/{id}` | Delete a DiagnosticRun |
| `GET` | `/api/v1/diagnosticruns/{id}/evidence` | Get evidence for a run |
| `GET` | `/api/v1/diagnosticruns/{id}/analysisresult` | Get analysis for a run |
| `GET` | `/api/v1/analysisresults/{id}` | Fetch an analysis result by ID |
| `POST` | `/api/v1/analysisresults/{id}/rate` | Submit thumbs-up/down feedback |
| `GET` | `/api/v1/problemcases` | List ProblemCases |
| `GET` | `/api/v1/problemcases/{id}` | Get a ProblemCase |
| `GET` | `/api/v1/namespaces` | List namespaces |
| `GET` | `/api/v1/namespaces/{ns}/resources` | List resources by kind in a namespace |
| `GET` | `/api/v1/settings` | Get user settings |
| `PUT` | `/api/v1/settings` | Update user settings |
| `GET` | `/api/v1/kubechan/state` | Get KubeChan mood state |
| `POST` | `/api/v1/kubechan/poke` | Poke KubeChan |
| `POST` | `/internal/evidence` | Receive evidence from diagnostics-worker |
| `GET` | `/ws` | WebSocket for push events |

**WebSocket events pushed to frontend:**

| Event | Trigger |
|---|---|
| `Incident.Created` | New Incident CRD appears |
| `Incident.Updated` | Incident CRD changes |
| `Incident.Resolved` | Incident status → resolved |
| `ProblemCase.Created` | New ProblemCase CRD |
| `ProblemCase.Updated` | ProblemCase state change |
| `ProblemCase.Resolved` | ProblemCase resolved |
| `DiagnosticRun.Updated` | DiagnosticRun status change |
| `Analysis.Completed` | LLM analysis stored (includes `analysisId`, `incidentId`, `needsMoreInfo`, `suggestedResources`) |
| `KubeChanState.Updated` | Mood level changed |

**Key env vars:**

| Var | Default | Description |
|---|---|---|
| `DB_PATH` | `/data/kubechan.db` | SQLite database file |
| `LLM_GATEWAY_URL` | `http://kubechan-llm-gateway:8080` | llm-gateway base URL |
| `DEFAULT_NAMESPACE` | `kubechan` | Namespace for CRD operations |

---

### `llm-gateway` — Python + FastAPI

Receives a structured evidence payload, builds a detailed prompt, calls OpenAI, Anthropic, or AWS Bedrock through the LiteLLM SDK (GitHub Copilot currently uses its SDK adapter), and returns a structured evidence-first assessment. The model may annotate the deterministic graph with a causal path, but it cannot create topology or cite evidence outside the immutable source catalog.

**Provider and model:** selected through per-user LLM settings. OpenAI and Anthropic API keys are passed per request; Bedrock supports bearer API keys and AWS IAM credentials. The model selector lists KubeChan's curated model choices, which may not include every model enabled for a given account.

**Analysis output:**

| Field | Content |
|---|---|
| `openingRant` | KubeChan's scathing, humiliating opening remark |
| `likelyRootCause` | Exact technical root cause, one sentence |
| `evidenceChain` | Proof from specific log lines / events / config values |
| `recommendation` | Numbered kubectl steps to fix the problem |
| `closingInsult` | Final parting shot |
| `confidence` | 0.0–1.0 score |
| `needsMoreInfo` | `true` if confidence < 0.65 and more evidence could help |
| `suggestedResources` | List of `{kind, reason}` the LLM wants to see |
| `prompt` | Full prompt sent to the model (stored for debugging) |
| `assessment` | Versioned claims, citations, remediation steps, and causal-path annotations |
| `citationValidation` | Count and IDs of citations rejected by the gateway |

**LLM credentials and model parameters** are selected per user in Settings. OpenAI and Anthropic API keys and explicit Bedrock credentials are passed per request. Bedrock can also use the standard AWS credential chain supplied to the gateway deployment.

---

### `frontend-ui` — TypeScript, React 19 + Vite + MUI + nginx

Single-page application served by nginx. Shows an incident list and the KubeChan sidebar character, and connects via WebSocket for live updates.

**Incident operations features:**

- Open and resolved incidents, sorted by `openedAt`
- Manual badge on user-reported incidents
- Expandable **Details** panel per incident showing:
  - Root resource pill (kind / name / namespace)
  - User description (manual incidents)
  - Related resources tagged by user (manual incidents)
  - Problem cases (auto incidents)
  - KubeChan's suggested resources (when `needsMoreInfo` was returned)
- "Ask KubeChan to help" / "Ask KubeChan again" analyze button
- **Needs-more-info banner** when LLM requested more evidence — lists suggestion chips with reasons, opens AugmentIncidentModal
- **Resolve button** on open manual incidents — inline confirm → patches Incident CRD → KubeChan reacts immediately
- Dedicated Assessment route for every DiagnosticRun
- Interactive resource graph with broken relationships, proposed targets, zoom controls, and keyboard-accessible selection
- Evidence inspector that resolves graph entities and claims to atomic JSON pointers, events, or individual log lines
- Evidence-chain ledger and ordered remediation runbook

**KubeChan sidebar:**

- Pose transitions: `idle` → `thinking` → `speaking` → `chatter`
- `speaking`: compact completion state with a link to the dedicated Assessment page; technical analysis is not duplicated in the sidebar
- `chatter`: persona reaction line for events (new incident, resolved, poke, silence, analysis rating)
- Mood system: open incidents raise mood level (0 = calm, 1 = irritated, 2 = rage); mood affects idle and reaction lines
- Poke escalation: poke 3× → annoyed, 5× → rage

---

## CRDs

| CRD | Purpose |
|---|---|
| `Incident` (`kubechan.io`) | Groups related ProblemCases by workload root. Can be auto-created by cluster-watcher or manually created by the user. Carries `source`, `userMessage`, `relatedResources`. |
| `ProblemCase` (`kubechan.io`) | A specific detected anomaly on a resource (e.g. CrashLoopBackOff on pod `foo`) |
| `DiagnosticRun` (`kubechan.io`) | A request to collect evidence; lifecycle: `pending → running → completed/failed` |
| `KubeChanState` (`kubechan.io`) | Singleton persona/mood state shared by the services and UI |
| `KubechanExclusionRule` (`kubechan.io`) | Detector suppression rule scoped by namespace, resources, labels, and optional time windows |

The control-plane CRs live in the `kubechan` namespace. `Incident`, `ProblemCase`, and `DiagnosticRun` expose status used by their reconcilers and the UI.

---

## Running locally

### Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Docker | latest | Runtime used by kind and Tilt image builds |
| kind | ≥ 0.22 | A reachable kind cluster must be the active kubectl context |
| ingress-nginx | current | Required in the kind cluster for `kubechan.local` |
| Tilt | ≥ 0.33 | `brew install tilt` |
| kubectl | ≥ 1.28 | Configured for the target kind cluster |
| Go | 1.26.2+ | for local builds / codegen |
| Python | 3.13+ | for llm-gateway local development |
| uv | current | Python dependency and command runner |
| Node.js + npm | current LTS | frontend build and local development |
| LLM access | — | OpenAI/Anthropic API keys, Bedrock credentials, or GitHub Copilot credentials configured in the UI |

---

### 1. Configure AWS credentials

```bash
kubectl create namespace kubechan --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret generic kubechan-bedrock-api-key \
  --namespace kubechan \
  --from-literal=AWS_ACCESS_KEY_ID=<your-key-id> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<your-secret-key>
```

---

### 2. Select the kind cluster

```bash
kubectl config use-context kind-<cluster-name>
kubectl cluster-info
```

Tilt refuses to start against a non-kind context. Development data is stored in the cluster's default StorageClass, so no host data directory is required.

---

### 3. Start the stack

```bash
make dev-up
# or equivalently:
tilt up
```

Run these commands from the repository root. Tilt builds all five images from the local checkout, loads them directly into kind, renders the Helm chart with `values-dev.yaml`, and waits for every workload to become healthy.

For a non-interactive build suitable for a final verification run:

```bash
tilt ci
```

---

### 4. Open the UI

| Service | URL |
|---|---|
| Frontend UI | http://kubechan.local |
| Frontend fallback | http://localhost:8080 |
| Tilt dashboard | http://localhost:10350 |

---

### 5. Demo: trigger a broken Ingress incident

`hack/demo-manual-incident.yaml` deploys the `kubechan-demo` namespace with:
- `Deployment/nginx` + `Service/nginx-svc`
- `Ingress/nginx` with a deliberately wrong backend service name (`nginx` instead of `nginx-svc`)

```bash
kubectl apply -f hack/demo-manual-incident.yaml
```

Then in the UI: **Report an issue** → select `Deployment/nginx` → describe the issue → add `Ingress/nginx` as a related resource → submit. Open the completed assessment to inspect the broken `Ingress → Service/nginx` relationship, the proposed `Ingress → Service/nginx-svc` route, and the exact backend metadata used as evidence.

---

### 6. Tear down

```bash
make dev-down
# or:
tilt down
```

---

## Development tasks

```bash
# Regenerate CRD YAMLs and DeepCopy after changing api/v1alpha1/
make generate

# Build all Go services locally
make build-go

# Run Go unit tests
make test-go

# Lint Go
make lint-go

# Lint Python (llm-gateway)
make lint-python
```

---

## Project structure

```
api/v1alpha1/          — CRD type definitions (Incident, ProblemCase, DiagnosticRun)
helm/kubechan/         — Helm chart (CRDs, templates, dev + prod values)
services/
  cluster-watcher/     — Detects cluster anomalies, creates ProblemCase + Incident CRDs
  diagnostics-worker/  — Collects evidence and builds deterministic investigation graphs
  backend-api/         — REST + WebSocket API, SQLite, orchestration, mood syncer
  llm-gateway/         — Builds prompts, validates citations/causal paths, calls the configured LLM
  frontend-ui/         — React SPA with incident operations and assessment workspaces
hack/                  — Demo manifests and test fixtures
docs/                  — Architecture design docs and phase plans
Tiltfile               — Local dev orchestration
Makefile               — Common dev tasks
```


