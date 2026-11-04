# Contributing to KubeChan

KubeChan is a read-only Kubernetes troubleshooting system. Read [`AGENTS.md`](AGENTS.md) before making changes and use [`docs/README.md`](docs/README.md) to find the current architecture and contracts.

## Prerequisites

| Tool | Version or requirement |
|---|---|
| Go | 1.26.x (`go.mod` is authoritative) |
| Python | 3.13+ |
| uv | Current stable |
| Node.js | 22 |
| Docker | Current stable |
| kind | A reachable cluster and active `kind-*` kubectl context |
| kubectl | 1.28+ |
| Helm | 3.14+ |
| Tilt | 0.33+ |
| ingress-nginx | Installed in the development cluster |

## Local setup

```bash
git clone <your-fork>
cd kubechan
git checkout -b feature/your-topic

kubectl config use-context kind-<cluster-name>
make dev-up
```

The application is available at `http://kubechan.local`, with a direct frontend fallback at `http://localhost:8080`. Tilt runs at `http://localhost:10350`.

To build and wait for readiness without leaving Tilt running:

```bash
make e2e
```

## Repository map

```text
api/v1alpha1/                    CRD types and generated code
services/cluster-watcher/        detection and incident correlation
services/diagnostics-worker/     collection, source catalog, GraphEngine
services/backend-api/            REST/WebSocket API and SQLite
services/llm-gateway/            prompt, providers, output validation
services/frontend-ui/            React operational UI
helm/kubechan/                   Helm chart and development values
docs/codebase/                   current architecture documentation
docs/contracts/                  cross-service contracts
docs/plans/                      historical plans
```

## Making changes

### CRDs

- Follow existing kubebuilder markers and JSON conventions.
- Use pointers only when absence must be distinct from a zero value.
- Run `make generate` after changing `api/v1alpha1/`.
- Commit generated DeepCopy and CRD changes with the source change.

### Controllers

- Keep reconciliation idempotent and level-triggered.
- Use status subresources for status changes.
- Never mutate user workloads.
- Do not use blocking sleeps in reconciler code.
- Add deterministic unit tests or fake-client tests for new behavior.

### Evidence and GraphEngine

- Keep sources atomic and citation-safe.
- Build graph topology only from resources in the evidence snapshot.
- Represent missing targets explicitly.
- Keep proposed candidate topology separate from observed topology.
- Update [`docs/contracts/investigation-analysis.md`](docs/contracts/investigation-analysis.md) when changing the contract.

### Backend API

- Add routes and dependency wiring in `main.go`; endpoint implementations belong in `handler/`.
- Validate input at the HTTP boundary.
- Add a new ordered SQLite migration for schema changes; never modify an applied migration.
- Preserve auth and manual-incident ownership checks.

### LLM gateway

- Prompt construction lives in `app/prompt.py`.
- Provider integrations implement the adapter in `app/providers/base.py`.
- Treat model output as untrusted: validate citations and graph IDs in `app/routes.py`.
- Add pytest coverage for prompt, parser, provider, or schema behavior.

### Frontend

- Shared server state belongs in RTK Query APIs under `src/store/api/`.
- Route-level UI belongs under `src/pages/`.
- Preserve accessibility, semantic status colors, responsive behavior, and the enterprise operational design language in `AGENTS.md`.
- The current frontend gate is `npm run build`; there is no test or lint script yet.

## Verification

Use the matrix in [`docs/codebase/TESTING.md`](docs/codebase/TESTING.md). The standard full checks are:

```bash
go test ./...

cd services/llm-gateway
uv run pyright .
uv run pytest -v
cd ../..

cd services/frontend-ui
npm ci
npm run build
cd ../..

helm lint helm/kubechan --values helm/kubechan/values-dev.yaml
```

For cross-service behavior, run `tilt ci` and validate a fresh DiagnosticRun. A completed run is an immutable snapshot and will not acquire fields introduced by newer code.

## Commits and pull requests

Use focused Conventional Commits:

```text
feat(graph): add HTTPRoute relationships
fix(analysis): reject unknown causal-path edges
docs: update investigation contract
```

Before opening a pull request:

1. Rebase or merge the current `main` according to project policy.
2. Run the checks required for the changed areas.
3. Run `make generate` if CRD types changed.
4. Update current documentation and contracts.
5. Confirm `git status` contains no unrelated generated or local files.
6. Explain the behavior change, trust-boundary impact, and verification performed.

## License

Contributions are licensed under the [Apache License 2.0](LICENSE).
