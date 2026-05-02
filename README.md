# KubeChan

A read-only Kubernetes AI troubleshooting assistant. KubeChan watches your cluster, detects broken workloads, collects diagnostic evidence, and calls an LLM to produce a root-cause analysis — surfaced through a web UI with an opinionated tsundere character who will absolutely judge your configuration choices.

> KubeChan never modifies cluster state. It is advisory only.

---

## Architecture overview

```
┌──────────────────────────────────────────────────────────────────────┐
│  Kubernetes cluster                                                  │
│                                                                      │
│  ┌────────────────────┐   ProblemCase/   ┌──────────────────────┐   │
│  │  cluster-watcher   │──DiagnosticRun──▶│ diagnostics-worker   │   │
│  │  (Go controller)   │     CRDs         │  (Go controller)     │   │
│  └────────────────────┘                  └──────────┬───────────┘   │
│           │                                         │ POST /evidence │
│           │ watches Pods,                           │                │
│           │ Deployments, Services                   ▼                │
│           │                              ┌──────────────────────┐   │
│           └──────────────────────────────▶  backend-api (Go)    │   │
│                                          │  chi + SQLite        │   │
│                                          └──────┬───────────────┘   │
│                                                 │                   │
│                              POST /analyze      │    WebSocket       │
│                              ┌──────────────────┘    events         │
│                              ▼                        │              │
│                   ┌──────────────────────┐            │              │
│                   │   llm-gateway        │            │              │
│                   │   (Python/FastAPI)   │            │              │
│                   │   AWS Bedrock        │            │              │
│                   └──────────────────────┘            │              │
│                                                       ▼              │
│                                          ┌──────────────────────┐   │
│                                          │   frontend-ui        │   │
│                                          │  (React + Vite)      │   │
│                                          └──────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Flow: end-to-end analysis sequence

```
User opens UI
     │
     ▼
frontend-ui  ─── GET /api/v1/incidents ──────────────────▶  backend-api
                                                                  │
                                                    reads Incident CRDs
                                                         from K8s cache
     │
     ├── User clicks an incident → "Ask KubeChan"
     │
     ▼
frontend-ui  ─── POST /api/v1/incidents/{id}/analyze ────▶  backend-api
                                                                  │
                                                    creates DiagnosticRun CRD
                                                         in K8s API server
                                                                  │
                                                                  ▼
                                                    diagnostics-worker
                                                    (reconciles DiagnosticRun)
                                                         │
                                                         ├── collect pod logs
                                                         ├── collect K8s events
                                                         ├── collect PVC status
                                                         ├── collect ConfigMap
                                                         │   contents + mount paths
                                                         └── POST /internal/evidence
                                                              to backend-api
                                                                  │
                                                                  ▼
                                                    backend-api stores evidence,
                                                    dispatches async goroutine
                                                                  │
                                                                  ▼
                                                    backend-api  ─── POST /analyze ──▶  llm-gateway
                                                                                              │
                                                                                    builds prompt with
                                                                                    all collected evidence
                                                                                              │
                                                                                    calls AWS Bedrock
                                                                                    (Qwen3-32B)
                                                                                              │
                                                                                    parses structured
                                                                                    JSON response
                                                                                              │
                                                                                    returns 5-section
                                                                                    analysis ◀────────┘
                                                                  │
                                                    backend-api stores analysis result,
                                                    broadcasts WebSocket event
                                                                  │
                                                                  ▼
frontend-ui  ◀── WebSocket "Analysis.Completed" ─────────────────┘
     │
     ▼
KubeChan sidebar renders:
  1. Opening rant       (scathing mockery)
  2. Root cause         (exact technical fact)
  3. Evidence chain     (proof from logs/events/config)
  4. Recommendations    (numbered kubectl steps)
  5. Closing insult     (parting shot)
```

---

## Services

### `cluster-watcher` — Go, controller-runtime

Watches all Pods, Deployments, and Services in the cluster. Runs a set of detectors on every reconcile event and creates or updates **ProblemCase** and **Incident** CRDs when anomalies are detected.

| Detector | Trigger |
|---|---|
| `CrashLoopBackOffDetector` | Pod in CrashLoopBackOff |
| `ImagePullBackOffDetector` | Pod failing to pull image |
| `PendingTooLongDetector` | Pod Pending > threshold (default 5 min) |
| `DeploymentUnavailableDetector` | Deployment has unavailable replicas |
| `ServiceNoEndpointsDetector` | Service with no ready endpoints |

A debounce window (default 30 s, 5 s in dev) prevents noisy re-detection during rollouts.

**Key env vars:**

| Var | Default | Description |
|---|---|---|
| `DEBOUNCE_WINDOW_SECS` | `30` | Seconds to wait before re-evaluating a workload |
| `PENDING_THRESHOLD_SECS` | `300` | Seconds before a Pending pod is flagged |
| `CONTROL_NAMESPACE` | `kubechan` | Namespace where CRDs are written |

---

### `diagnostics-worker` — Go, controller-runtime

Reconciles **DiagnosticRun** CRDs. When a new run appears, it collects evidence for the referenced incident workload and POSTs it to backend-api.

Evidence collected per workload pod:
- Last N lines of current logs + previous container logs (crash evidence)
- Kubernetes events on the pod
- ConfigMap contents: keys + file paths (volume mounts) or env var names (env injection)
- Secret existence (values are never collected)
- PVC status and events

**Key env vars:**

| Var | Default | Description |
|---|---|---|
| `BACKEND_API_URL` | `http://kubechan-backend-api:8080` | Where to POST collected evidence |
| `LOG_TAIL_LINES` | `200` | Lines of current logs to collect |
| `PREV_LOG_TAIL_LINES` | `100` | Lines of previous (crashed) container logs |

---

### `backend-api` — Go, chi + SQLite

Central API server and orchestration hub. Serves the frontend REST + WebSocket API, stores all persistent state in a local SQLite database, and dispatches async analysis requests to llm-gateway.

**API surface:**

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/incidents` | List all detected incidents |
| `POST` | `/api/v1/incidents/{id}/analyze` | Trigger a new DiagnosticRun for an incident |
| `GET` | `/api/v1/analysisresults/{id}` | Fetch a completed analysis result |
| `GET` | `/api/v1/incidents/{id}/evidence` | Fetch latest collected evidence |
| `POST` | `/internal/evidence` | Receive evidence from diagnostics-worker |
| `GET` | `/ws` | WebSocket for push events (`Analysis.Completed`) |
| `GET` | `/healthz` / `/readyz` | Health probes |

**Key env vars:**

| Var | Default | Description |
|---|---|---|
| `DB_PATH` | `/data/kubechan.db` | SQLite database file |
| `LLM_GATEWAY_URL` | `http://kubechan-llm-gateway:8080` | llm-gateway base URL |

---

### `llm-gateway` — Python, FastAPI + boto3

Receives a structured evidence payload, builds a detailed prompt, calls AWS Bedrock via the Converse API, and returns a 5-section JSON analysis.

**Model:** `qwen.qwen3-32b-v1:0` (aliased as `qwen3-32b`). Also supports `qwen3-235b`.

**Analysis output sections:**

| Field | Content |
|---|---|
| `openingRant` | KubeChan's scathing, humiliating opening remark |
| `likelyRootCause` | Exact technical root cause, one sentence |
| `evidenceChain` | Proof from specific log lines / events / config values |
| `recommendation` | Numbered kubectl steps to fix the problem |
| `closingInsult` | Final parting shot |
| `confidence` | 0.0–1.0 confidence score |

**Key env vars:**

| Var | Default | Description |
|---|---|---|
| `BEDROCK_REGION` | `us-east-1` | AWS region for Bedrock |
| `BEDROCK_MODEL_ID` | `qwen3-32b` | Model alias or full Bedrock model ID |
| `TEMPERATURE` | `0.3` | LLM sampling temperature |
| `MAX_TOKENS` | `4096` | Maximum response tokens |
| `AWS_ACCESS_KEY_ID` | — | AWS credentials (via secret) |
| `AWS_SECRET_ACCESS_KEY` | — | AWS credentials (via secret) |

---

### `frontend-ui` — TypeScript, React + Vite + nginx

Single-page application served by nginx. Shows an incident list on the left, the KubeChan sidebar character on the right, and connects via WebSocket for live analysis updates.

The KubeChan character displays analysis results as a speech bubble with 5 styled sections. Pose transitions: `idle` → `thinking` (spinner) → `speaking` (results).

---

## CRDs

| CRD | Purpose |
|---|---|
| `ProblemCase` (`kubechan.io`) | A specific detected anomaly on a resource (e.g. CrashLoopBackOff on pod `foo`) |
| `DiagnosticRun` (`kubechan.io`) | A request to collect evidence for an incident; lifecycle: `pending → running → completed/failed` |

---

## Running locally

### Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Docker Desktop | latest | Kubernetes must be enabled in settings |
| Tilt | ≥ 0.33 | `brew install tilt` |
| kubectl | ≥ 1.28 | bundled with Docker Desktop |
| Go | 1.25+ | for local builds / codegen |
| Python | 3.12+ | for llm-gateway local dev |
| AWS account | — | with Bedrock access to `qwen.qwen3-32b-v1:0` in `us-east-1` |

---

### 1. Configure AWS credentials

The llm-gateway reads credentials from a Kubernetes Secret named `kubechan-bedrock-api-key`.

```bash
kubectl create namespace kubechan --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret generic kubechan-bedrock-api-key \
  --namespace kubechan \
  --from-literal=AWS_ACCESS_KEY_ID=<your-key-id> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<your-secret-key>
```

---

### 2. Create the data directory

The backend-api SQLite database is persisted to a host path so it survives pod restarts.

```bash
mkdir -p .data
chmod 777 .data
```

> If you are on macOS the path defaults to `/Users/<you>/kubechan/.data`. Update `helm/kubechan/values-dev.yaml` → `backendApi.storage.hostPath` if your workspace is elsewhere.

---

### 3. Start the stack

```bash
make dev-up
# or equivalently:
kubectl config use-context docker-desktop
kubectl create namespace kubechan --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f helm/kubechan/crds/ --server-side
tilt up
```

Tilt will build all five images, deploy the Helm chart with dev overrides, and set up live-reload for every service.

---

### 4. Open the UI

| Service | URL |
|---|---|
| Frontend UI | http://localhost:30081 |
| Backend API | http://localhost:30080 |
| Tilt dashboard | http://localhost:10350 |

---

### 5. Tear down

```bash
make dev-down
# or:
tilt down
```

---

## Development tasks

```bash
# Regenerate CRD YAMLs and DeepCopy methods after changing api/v1alpha1/
make generate

# Build all Go services locally
make build-go

# Run all Go unit tests
make test-go

# Lint Go
make lint-go

# Lint Python (llm-gateway)
make lint-python
```

---

## Project structure

```
api/v1alpha1/          — CRD type definitions (Go)
helm/kubechan/         — Helm chart (CRDs, templates, dev + prod values)
services/
  cluster-watcher/     — Detects cluster anomalies, creates CRDs
  diagnostics-worker/  — Collects evidence for DiagnosticRuns
  backend-api/         — REST + WebSocket API, SQLite, orchestration
  llm-gateway/         — Builds prompts, calls AWS Bedrock
  frontend-ui/         — React SPA (Vite dev, nginx prod)
Tiltfile               — Local dev orchestration
Makefile               — Common dev tasks
```
