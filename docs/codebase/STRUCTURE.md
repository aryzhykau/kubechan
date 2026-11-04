# Codebase structure

## Top-level map

| Path | Responsibility |
|---|---|
| `AGENTS.md` | Repository-wide agent instructions and verification rules |
| `api/v1alpha1/` | Namespaced CRD type definitions, scheme registration, and generated DeepCopy code |
| `services/cluster-watcher/` | Detection, correlation, exclusion matching, and KubeChan CRD reconciliation |
| `services/diagnostics-worker/` | Evidence collection, source normalization, and investigation graph construction |
| `services/backend-api/` | REST/WebSocket API, auth, Kubernetes cache, SQLite, and analysis orchestration |
| `services/llm-gateway/` | Prompt construction, LLM provider adapters, parsing, and validation |
| `services/frontend-ui/` | React application, Redux store, operational pages, and nginx proxy |
| `helm/kubechan/` | Helm chart, CRDs, RBAC, probes, ingress, and development values |
| `docs/codebase/` | Current architecture, conventions, integrations, testing, and concerns |
| `docs/contracts/` | Current cross-service payload and behavior contracts |
| `docs/plans/` | Historical implementation plans; not current sources of truth |
| `hack/` | Demo manifests, developer hooks, and test fixtures |
| `Tiltfile` | kind-only local build and deployment orchestration |
| `Makefile` | Code generation, build, test, lint, and Tilt convenience targets |

## Service entry points

| Service | Entry point | Runtime |
|---|---|---|
| backend-api | `services/backend-api/main.go` | `/backend-api` on port 8080 |
| cluster-watcher | `services/cluster-watcher/main.go` | controller-runtime manager |
| diagnostics-worker | `services/diagnostics-worker/main.go` | controller-runtime manager |
| llm-gateway | `services/llm-gateway/main.py` | Uvicorn/FastAPI on port 8000 |
| frontend-ui | `services/frontend-ui/src/main.tsx` | Static Vite bundle served by nginx on port 8080 |

All Go services share the root `go.mod`. Python dependencies are locked by `uv.lock`. Frontend dependencies are locked by `package-lock.json`.

## Important module boundaries

### diagnostics-worker

```text
collector/
  types.go       evidence, source, node, edge, and candidate contracts
  catalog.go     deterministic atomic source catalog

graphengine/
  engine.go      pure deterministic graph builder

controllers/
  diagnosticrun_reconciler.go
                 Kubernetes reads, snapshot assembly, persistence handoff
```

`graphengine` must remain independent of Kubernetes clients and LLM behavior. It receives collector types and returns a deterministic snapshot.

### backend-api

```text
handler/         REST endpoints and request-level orchestration
db/              SQLite initialization, migrations, retention
k8s/             client/cache wrappers and mood synchronization
startup/         admin bootstrap and request recovery
ws/              authenticated WebSocket clients and broadcast hub
```

Routing and dependency wiring live in `main.go`. Database schema changes require a new ordered migration; existing migrations are immutable.

### llm-gateway

```text
app/models.py    Pydantic request and response contracts
app/prompt.py    evidence formatting, budgets, and prompt instructions
app/parser.py    model response extraction
app/routes.py    provider call and trust-boundary validation
app/providers/   Bedrock and GitHub Copilot adapters
tests/           FastAPI, prompt, parser, and provider tests
```

### frontend-ui

```text
src/store/api/          RTK Query API slices
src/store/slices/       local application state
src/store/middleware/   WebSocket integration
src/pages/incidents/    incident workspace and Assessment page
src/pages/diagnostics/  DiagnosticRun inventory and detail
src/pages/admin/        users, settings, and exclusion rules
src/components/shared/  reusable operational UI primitives
src/api/index.ts        shared transport types and legacy helpers
```

Route declarations and application shell composition live in `src/App.tsx`.

## Generated and persistent artifacts

| Artifact | Owner | Rule |
|---|---|---|
| `api/v1alpha1/zz_generated.deepcopy.go` | controller-gen | Run `make generate`; do not edit manually |
| `helm/kubechan/crds/*.yaml` | controller-gen | Run `make generate`; do not edit manually |
| `services/frontend-ui/dist/` | Vite | Build output; not a source file |
| `services/frontend-ui/tsconfig.tsbuildinfo` | TypeScript | Local build cache; do not include opportunistically |
| backend SQLite PVC | backend-api | Runtime state, not repository state |

## Naming

- Go files use `snake_case.go`; exported identifiers use PascalCase; tests are co-located as `*_test.go`.
- Python modules/functions use snake_case and classes use PascalCase.
- React components use PascalCase filenames; hooks and utilities use camelCase.
- API JSON fields use lower camelCase unless constrained by Kubernetes conventions.
- Deterministic graph/source IDs are opaque contracts. Do not parse them in presentation code when a typed field exists.
