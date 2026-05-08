# Contributing to KubeChan

Thank you for your interest in contributing! KubeChan is a read-only Kubernetes AI troubleshooting assistant, and contributions of all kinds are welcome — bug fixes, new features, documentation, and testing improvements.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Making Changes](#making-changes)
- [Commit Style](#commit-style)
- [Pull Request Process](#pull-request-process)
- [Architecture Notes](#architecture-notes)

---

## Code of Conduct

Be respectful and constructive. Harassment of any kind is not tolerated.

---

## Getting Started

1. Fork the repository and clone your fork.
2. Create a feature branch from `main`:
   ```bash
   git checkout -b feature/your-topic
   ```
3. Make your changes, write tests, and open a pull request.

---

## Development Setup

### Prerequisites

| Tool | Version |
|------|---------|
| Go | ≥ 1.26 |
| Node.js | ≥ 20 |
| Docker / Podman | any recent |
| [Tilt](https://tilt.dev) | ≥ 0.33 |
| [kind](https://kind.sigs.k8s.io) or a real cluster | — |
| `kubectl` | ≥ 1.28 |
| `helm` | ≥ 3.14 |

### Local dev cluster (Tilt)

```bash
kind create cluster --name kubechan
tilt up
```

Tilt watches all services and live-reloads them on save.

### Run tests

```bash
# Go unit + controller envtest
go test ./...

# Frontend
cd services/frontend-ui && npm ci && npm run test

# LLM gateway
cd services/llm-gateway && pip install -e ".[dev]" && pytest
```

### Regenerate CRDs and deep-copy code

After modifying any type under `api/`, run:

```bash
make generate manifests
```

This updates `zz_generated.deepcopy.go` and the CRD YAML files under `helm/kubechan/crds/`.

---

## Project Structure

```
api/v1alpha1/          # CRD types (kubebuilder)
services/
  cluster-watcher/     # controller-runtime reconcilers
  diagnostics-worker/  # diagnostic job runner
  backend-api/         # REST + WebSocket API (Go)
  llm-gateway/         # LLM proxy (Python / FastAPI)
  frontend-ui/         # React + Vite SPA
helm/kubechan/         # Helm chart (CRDs + templates)
hack/                  # Demo manifests and test fixtures
```

---

## Making Changes

### API types (`api/`)

- Follow existing `+kubebuilder:` marker conventions.
- All new fields must have JSON tags and be nullable (`*Type`) unless they are required.
- Run `make generate manifests` after every change.
- Update the corresponding `helm/kubechan/crds/` YAML by committing the regenerated output.

### Controllers (`services/cluster-watcher/`, `services/diagnostics-worker/`)

- Keep `Reconcile()` idempotent — re-running it on the same object must be safe.
- Prefer `client.MergeFrom` patches over full `Update` calls.
- Status changes must go through the status subresource (`status().Patch()` / `status().Update()`).
- Use `ctrl.Result{RequeueAfter: d}` instead of `time.Sleep`.
- Add or update `envtest` tests for any new reconciler logic.

### Backend API (`services/backend-api/`)

- New endpoints belong in `handler/`; keep handler functions thin — business logic in helpers.
- Validate all user input at the handler boundary.
- Do not expose internal Kubernetes object details that are not needed by the UI.

### LLM Gateway (`services/llm-gateway/`)

- All prompt templates live in `app/prompts/`. Keep them versioned if the output schema changes.
- New model integrations should implement the existing provider interface.
- Add `pytest` tests for any new provider or prompt logic.

### Frontend (`services/frontend-ui/`)

- Components live in `src/`; keep them small and focused.
- Use the existing API client utilities in `src/api/` rather than raw `fetch`.
- Run `npm run lint` before committing.

---

## Commit Style

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short summary>

[optional body]
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`.

Examples:
```
feat(cluster-watcher): add PVC pressure detector
fix(backend-api): prevent nil dereference in incident handler
docs: update CONTRIBUTING with envtest setup
```

---

## Pull Request Process

1. Ensure `go test ./...` and `npm run test` pass locally.
2. Run `make generate manifests` if you touched API types, and commit the result.
3. Keep PRs focused — one logical change per PR.
4. Fill in the PR template (what changed, why, how to test).
5. A maintainer will review and merge; please address all review comments before the next ping.

---

## Architecture Notes

- **KubeChan never modifies cluster state.** All reconcilers are read-only; no patch/update is issued against user workloads.
- The `cluster-watcher` detects broken workloads and creates `Incident` CRs. The `diagnostics-worker` picks those up and populates `DiagnosticRun` CRs with evidence. The `backend-api` reads from both Kubernetes and the Postgres DB; the `llm-gateway` is called by the backend to produce analysis.
- Exclusion rules (`KubeChanExclusionRule` CRD) are evaluated in the watcher before an `Incident` is created.

---

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
