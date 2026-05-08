# Codebase Structure

## Core Sections (Required)

### 1) Top-Level Map

| Path | Purpose | Evidence |
|------|---------|----------|
| `api/v1alpha1/` | Shared Kubernetes CRD type definitions and generated DeepCopy code. Imported by all three Go services. | `api/v1alpha1/incident_types.go`, `go.mod` module path |
| `services/backend-api/` | Central REST API service: chi router, SQLite DB, Kubernetes informer cache, WebSocket hub, LLM dispatch goroutine. | `services/backend-api/main.go` |
| `services/cluster-watcher/` | Kubernetes controller watching Pods, Deployments, Services, Nodes, Events. Writes ProblemCase and Incident CRDs. | `services/cluster-watcher/main.go` |
| `services/diagnostics-worker/` | Kubernetes controller reconciling DiagnosticRun CRDs. Collects evidence and POSTs to backend-api. | `services/diagnostics-worker/main.go` |
| `services/frontend-ui/` | React/Vite SPA. TypeScript source in `src/`, nginx config for production serving. | `services/frontend-ui/src/App.tsx`, `services/frontend-ui/nginx.conf` |
| `services/llm-gateway/` | FastAPI app. Receives `POST /analyze`, calls AWS Bedrock or GitHub Copilot, returns structured JSON analysis. | `services/llm-gateway/main.py`, `app/routes.py` |
| `helm/kubechan/` | Single Helm chart that deploys all five services plus CRDs. | `helm/kubechan/Chart.yaml`, `helm/kubechan/templates/` |
| `docs/` | Project description, phase plans, and this codebase knowledge directory. | `docs/project-description.md`, `docs/plans/` |
| `hack/` | Developer scripts: boilerplate header, demo YAML manifests, test-crasher workloads. | `hack/` |
| `.github/` | CI workflows (`ci.yml`, `release.yml`), coverage config, custom agent definitions, reusable skills. | `.github/workflows/`, `.github/skills/` |
| `Tiltfile` | Local dev orchestration: builds Docker images, applies Helm chart via Tilt on Docker Desktop k8s. | `Tiltfile` |
| `Makefile` | Convenience targets: generate, dev-up/down, lint, test, build. | `Makefile` |

---

### 2) Entry Points

| Service | Entry point | How invoked |
|---------|-------------|-------------|
| backend-api | `services/backend-api/main.go` | `go build ./services/backend-api/` → `/backend-api` binary |
| cluster-watcher | `services/cluster-watcher/main.go` | `go build ./services/cluster-watcher/` → `/cluster-watcher` binary |
| diagnostics-worker | `services/diagnostics-worker/main.go` | `go build ./services/diagnostics-worker/` → `/diagnostics-worker` binary |
| llm-gateway | `services/llm-gateway/main.py` → `app` FastAPI instance | `uvicorn main:app` (see Dockerfile `CMD`) |
| frontend-ui | `services/frontend-ui/src/main.tsx` → React root | `vite build` → static files served by nginx |

All Dockerfiles use a two-stage build (builder + minimal runtime). Go services run on `distroless/static:nonroot` or `distroless/base:nonroot`. Python uses `python:3.13-slim`.

---

### 3) Module Boundaries

All three Go services share a **single `go.mod`** at the repo root (`module github.com/org/kubechan`). The `api/v1alpha1` package is the only cross-service import.

| Boundary | What belongs here | What must not be here |
|----------|-------------------|------------------------|
| `api/v1alpha1/` | CRD struct definitions, `+kubebuilder` markers, generated `DeepCopy*` functions, scheme registration | Business logic, HTTP handlers, DB queries |
| `services/backend-api/handler/` | HTTP handler structs and their methods; routing is in `main.go` | Direct k8s writes beyond CRD patch/status, DB migrations |
| `services/backend-api/db/` | `sql.DB` open + migrations; query helpers | HTTP layer, Kubernetes client |
| `services/backend-api/ws/` | WebSocket Hub and Client; `Broadcast` | Business logic, CRD operations |
| `services/backend-api/k8s/` | `client.Client` wrapper, informer cache Watcher, MoodSyncer | HTTP handlers, DB queries |
| `services/backend-api/startup/` | One-time startup actions (admin bootstrap, pending-request recovery) | Ongoing request handling |
| `services/cluster-watcher/controllers/` | controller-runtime reconcilers | Evidence collection, LLM calls |
| `services/cluster-watcher/detector/` | Stateless detector logic implementing `Detector` interface | Reconciler state, DB, HTTP |
| `services/cluster-watcher/debounce/` | Per-resource debounce timer | Detection logic |
| `services/cluster-watcher/exclusion/` | `IsExcluded()` — matches a resource+detector against enabled `KubechanExclusionRule` CRDs; evaluates time windows | Detection logic, CRD writes |
| `services/cluster-watcher/controllers/exclusionrule_reconciler.go` | Watches `KubechanExclusionRule` events; on create/enable auto-resolves open Incidents + ProblemCases that match the rule | Exclusion matching logic |
| `services/backend-api/handler/exclusion_rules.go` | REST CRUD for `KubechanExclusionRule` CRDs (list, create, patch enabled, delete) | K8s reconciliation, DB |
| `services/cluster-watcher/problemcase/` | ProblemCase lifecycle and lookup helpers | Detection logic |
| `services/diagnostics-worker/collector/` | Evidence type definitions | DiagnosticRun reconciler |
| `services/diagnostics-worker/controllers/` | `DiagnosticRunReconciler` | Evidence type definitions |
| `services/llm-gateway/app/` | FastAPI routes, Pydantic models, prompt builder, provider adapters | Kubernetes API access |
| `services/frontend-ui/src/` | React components, custom hooks, API client (`api.ts`); notable: `ExclusionRulesPage.tsx`, `ExclusionRuleModal.tsx` (LLM proposal → user confirm → CRD create flow), `AugmentIncidentModal.tsx`, `ManualIncidentModal.tsx` | Server logic |

---

### 4) Naming and Organization Rules

**Go services:**
- Package names: `snake_case` single word (e.g., `handler`, `detector`, `debounce`, `watcherconfig`)
- File names: `snake_case.go` (e.g., `correlation_reconciler.go`, `admin_bootstrap.go`)
- Type names: `PascalCase` (e.g., `CorrelationReconciler`, `Incidents`, `MoodSyncer`)
- Test files: `*_test.go` co-located with source file
- Constants: `PascalCase` (e.g., `LabelIncident`, `IncidentStateOpen`)

**Python (llm-gateway):**
- Package/module names: `snake_case` (e.g., `models.py`, `routes.py`, `base.py`)
- Class names: `PascalCase` (e.g., `AnalyzeRequest`, `BedrockProvider`)
- Directory: flat `app/` package with `providers/` sub-package

**TypeScript (frontend-ui):**
- Component files: `PascalCase.tsx` (e.g., `IncidentList.tsx`, `KubeChanSidebar.tsx`)
- Hook files: `camelCase.ts` (e.g., `useWebSocket.ts`)
- Utility files: `camelCase.ts` (e.g., `api.ts`, `chatter.ts`)
- No barrel (`index.ts`) exports observed; components imported directly by path

**Organization pattern:** services are organized by technical layer within each service (e.g., `handler/`, `db/`, `ws/`, `k8s/` in backend-api), not by feature.

---

### 5) Evidence

- `go.mod` — module name and Go version
- `services/backend-api/main.go` — entry point, package boundaries, imports
- `services/cluster-watcher/main.go` — entry point, controller registration
- `services/llm-gateway/main.py` — FastAPI app creation
- `services/frontend-ui/src/App.tsx` — React root, component imports
- `api/v1alpha1/incident_types.go` — CRD type definition example
- `api/v1alpha1/exclusionrule_types.go` — `KubechanExclusionRule` spec (TargetResources, Selector, TimeWindow)
- `services/cluster-watcher/controllers/exclusionrule_reconciler.go` — ExclusionRuleReconciler
- `services/backend-api/handler/exclusion_rules.go` — REST handler
- `services/frontend-ui/src/ExclusionRuleModal.tsx` — LLM proposal → confirm UI
- `.github/workflows/ci.yml` — how each service is built/tested
- `Tiltfile` — local dev build context boundaries per service
