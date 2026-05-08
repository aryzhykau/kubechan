# Technology Stack

## Core Sections (Required)

### 1) Runtime Summary

This is a **polyglot monorepo** with five services across three languages. Each service has its own runtime summary.

#### Go services (cluster-watcher, diagnostics-worker, backend-api)

| Area | Value | Evidence |
|------|-------|----------|
| Primary language | Go | `go.mod` |
| Runtime + version | Go 1.26.2 | `go.mod` line 3, `services/backend-api/Dockerfile` `FROM golang:1.26-alpine` |
| Package manager | Go modules (`go mod`) | `go.mod`, `go.sum` |
| Module/build system | `go build`, `make build-go` | `Makefile` |

#### Python service (llm-gateway)

| Area | Value | Evidence |
|------|-------|----------|
| Primary language | Python | `services/llm-gateway/pyproject.toml` |
| Runtime + version | Python ≥ 3.13 | `pyproject.toml` `requires-python = ">=3.13"`, `Dockerfile` `FROM python:3.13-slim` |
| Package manager | `uv` (lockfile: `uv.lock`) | `services/llm-gateway/Dockerfile` builder stage `FROM ghcr.io/astral-sh/uv:python3.13-bookworm-slim`, CI `astral-sh/setup-uv@v5` |
| Module/build system | `uv sync --frozen` | `.github/workflows/ci.yml` |

#### TypeScript service (frontend-ui)

| Area | Value | Evidence |
|------|-------|----------|
| Primary language | TypeScript 5.7 | `services/frontend-ui/package.json` `"typescript": "~5.7.2"` |
| Runtime + version | Node.js (version unspecified) + nginx for serving | `services/frontend-ui/Dockerfile` (nginx), `package.json` |
| Package manager | npm | `services/frontend-ui/package.json` |
| Module/build system | Vite 6 (`vite build`) | `services/frontend-ui/vite.config.ts`, `package.json` |

---

### 2) Production Frameworks and Dependencies

#### Go (all three Go services share one `go.mod`)

| Dependency | Version | Role in system | Evidence |
|------------|---------|----------------|----------|
| `github.com/go-chi/chi/v5` | v5.2.5 | HTTP router for backend-api REST API | `go.mod` |
| `sigs.k8s.io/controller-runtime` | v0.20.4 | Controller/reconciler framework for cluster-watcher and diagnostics-worker; also used for informer cache in backend-api | `go.mod` |
| `k8s.io/client-go` | v0.32.3 | Kubernetes API client | `go.mod` |
| `k8s.io/api` / `k8s.io/apimachinery` | v0.32.3 | Kubernetes API types | `go.mod` |
| `modernc.org/sqlite` | v1.50.0 | Pure-Go SQLite driver for backend-api persistent store | `go.mod` |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | JWT signing/verification for auth | `go.mod` |
| `github.com/google/uuid` | v1.6.0 | UUID generation for DB primary keys | `go.mod` |
| `github.com/gorilla/websocket` | v1.5.3 | WebSocket server in backend-api | `go.mod` |
| `golang.org/x/crypto` | v0.50.0 | bcrypt for password hashing | `go.mod` |
| `go.uber.org/zap` | v1.27.0 (indirect) | Structured logging for controller-runtime (cluster-watcher) | `go.mod` |

#### Python (llm-gateway)

| Dependency | Version | Role in system | Evidence |
|------------|---------|----------------|----------|
| `fastapi` | 0.115.6 | HTTP API framework | `pyproject.toml` |
| `uvicorn[standard]` | 0.34.0 | ASGI server | `pyproject.toml` |
| `boto3` | ≥1.38.0 | AWS Bedrock LLM calls | `pyproject.toml` |
| `github-copilot-sdk` | ≥0.3.0 | GitHub Copilot LLM provider | `pyproject.toml` |
| `pydantic` | 2.10.6 | Request/response model validation | `pyproject.toml` |
| `pydantic-settings` | 2.7.1 | Config from env vars | `pyproject.toml` |
| `structlog` | 25.1.0 | Structured logging | `pyproject.toml` |
| `jinja2` | 3.1.5 | Prompt templating | `pyproject.toml` |
| `httpx` | 0.28.1 | Async HTTP client | `pyproject.toml` |

#### TypeScript / frontend-ui

| Dependency | Version | Role in system | Evidence |
|------------|---------|----------------|----------|
| `react` | ^19.0.0 | UI framework | `package.json` |
| `react-dom` | ^19.0.0 | DOM rendering | `package.json` |
| `@mui/material` | ^9.0.0 | Component library | `package.json` |
| `@mui/icons-material` | ^9.0.0 | Icons | `package.json` |
| `@emotion/react` / `@emotion/styled` | ^11.14.x | MUI CSS-in-JS runtime | `package.json` |

---

### 3) Development Toolchain

| Tool | Purpose | Evidence |
|------|---------|----------|
| `golangci-lint` | Go linting (errcheck, vet, staticcheck, etc.) | `Makefile` `lint-go` target, `.github/workflows/ci.yml` |
| `controller-gen` v0.21.0 | Generate DeepCopy methods and CRD YAMLs from Go types | `Makefile` `generate` target |
| `pyright` | Python static type checking for llm-gateway | `Makefile` `lint-python`, `pyproject.toml` `[dependency-groups] dev` |
| `pytest` + `pytest-cov` | Python unit testing and coverage | `pyproject.toml` `[dependency-groups] dev` |
| `vladopajic/go-test-coverage` | Enforce Go coverage thresholds in CI | `.github/workflows/ci.yml`, `.github/go-coverage.yml` |
| `Tilt` | Local dev hot-reload orchestration (Docker Desktop k8s) | `Tiltfile` |
| `Helm` | Kubernetes manifest templating | `helm/kubechan/` |

---

### 4) Key Commands

```bash
# Install Go dependencies
go mod download

# Install Python dependencies (llm-gateway)
cd services/llm-gateway && uv sync --frozen --group dev

# Install frontend dependencies
cd services/frontend-ui && npm install

# Build all Go services
make build-go

# Build frontend
make build-frontend

# Run all tests
make test

# Run Go tests only
make test-go

# Run Python tests only
make test-python

# Lint Go
make lint-go

# Lint Python
make lint-python

# Generate CRD YAMLs and DeepCopy code
make generate

# Start local dev cluster (requires Docker Desktop)
make dev-up
```

---

### 5) Environment and Config

- Config sources: environment variables injected at runtime; no `.env.example` file exists in the repo
- Required env vars (discovered from source code):

| Service | Var | Default | Purpose |
|---------|-----|---------|---------|
| backend-api | `DB_PATH` | `/data/kubechan.db` | SQLite database path |
| backend-api | `JWT_SECRET` | *(required — startup fails if empty)* | JWT signing key |
| backend-api | `JWT_TTL_HOURS` | `24` | Token TTL |
| backend-api | `LLM_GATEWAY_URL` | *(empty — LLM calls skipped)* | URL of llm-gateway |
| backend-api | `DEFAULT_NAMESPACE` | `kubechan` | Kubernetes namespace for CRDs |
| backend-api | `PORT` | `8080` | HTTP server port |
| cluster-watcher | `DEBOUNCE_WINDOW_SECS` | `30` | Debounce before creating ProblemCase |
| cluster-watcher | `PENDING_THRESHOLD_SECS` | `300` | Threshold for PendingTooLong detector |
| cluster-watcher | `UNAVAILABLE_THRESHOLD_SECS` | `300` | Threshold for DeploymentUnavailable |
| cluster-watcher | `BACKEND_API_URL` | *(empty)* | URL for live-reload config from backend-api |
| cluster-watcher | `CONTROL_NAMESPACE` | `kubechan` | Namespace for CRDs |
| cluster-watcher | `DEV_MODE` | `false` | Enable dev-mode verbose logging |
| diagnostics-worker | `BACKEND_API_URL` | `http://kubechan-backend-api:8080` | backend-api internal URL |
| diagnostics-worker | `LOG_TAIL_LINES` | `200` | Lines of pod logs to collect |
| diagnostics-worker | `PREV_LOG_TAIL_LINES` | `100` | Lines of previous container logs |
| llm-gateway | `EVIDENCE_TOKEN_BUDGET` | `120000` | Prompt token budget cap |
| llm-gateway | AWS credentials | *(standard boto3 env or IRSA)* | Bedrock access |

- Deployment constraint: backend-api requires a writable volume for SQLite (`DB_PATH`). All Go services run as UID 65532 (distroless nonroot).

---

### 6) Evidence

- `go.mod` — Go module, version, all production dependencies
- `services/llm-gateway/pyproject.toml` — Python version, all dependencies, test config
- `services/frontend-ui/package.json` — TypeScript, React, MUI versions
- `services/backend-api/Dockerfile` — Go 1.26-alpine builder, distroless/base:nonroot runner
- `services/llm-gateway/Dockerfile` — uv builder, python:3.13-slim runner
- `Makefile` — build, test, lint commands
- `.github/workflows/ci.yml` — CI pipeline, Go + Python CI tools
- `services/backend-api/main.go` — env var reading (`envOr`)
- `services/cluster-watcher/main.go` — env var reading
- `services/llm-gateway/app/config.py` — `EVIDENCE_TOKEN_BUDGET` env var
