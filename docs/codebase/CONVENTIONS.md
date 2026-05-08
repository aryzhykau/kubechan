# Coding Conventions

## Core Sections (Required)

### 1) Naming Rules

#### Go (backend-api, cluster-watcher, diagnostics-worker)

| Item | Rule | Example | Evidence |
|------|------|---------|----------|
| Files | `snake_case.go`; test files `snake_case_test.go` | `correlation_reconciler.go`, `admin_bootstrap.go` | `services/cluster-watcher/controllers/` |
| Functions/methods | `PascalCase` for exported, `camelCase` for unexported | `ReceiveEvidence`, `namespacedName`, `jwtSecret` | `services/backend-api/handler/` |
| Types/structs/interfaces | `PascalCase` | `CorrelationReconciler`, `Detector`, `MoodSyncer` | `services/cluster-watcher/detector/interface.go` |
| Constants | `PascalCase` for exported, `camelCase` for unexported | `LabelIncident`, `adminUsername` | `services/cluster-watcher/controllers/correlation_reconciler.go` |
| Packages | Single lowercase word (`snake_case` permitted for compound) | `handler`, `detector`, `watcherconfig`, `debounce` | `services/cluster-watcher/` |
| Env vars | `UPPER_SNAKE_CASE` | `JWT_SECRET`, `DB_PATH`, `LOG_TAIL_LINES` | `services/backend-api/main.go` |

#### Python (llm-gateway)

| Item | Rule | Example | Evidence |
|------|------|---------|----------|
| Files/modules | `snake_case.py` | `models.py`, `routes.py`, `base.py` | `services/llm-gateway/app/` |
| Classes | `PascalCase` | `AnalyzeRequest`, `BedrockProvider`, `LLMProvider` | `services/llm-gateway/app/models.py`, `app/providers/base.py` |
| Functions/methods | `snake_case` | `build_prompt`, `make_provider`, `parse_llm_json` | `services/llm-gateway/app/` |
| Constants | `UPPER_SNAKE_CASE` | `EVIDENCE_TOKEN_BUDGET` | `services/llm-gateway/app/config.py` |

#### TypeScript (frontend-ui)

| Item | Rule | Example | Evidence |
|------|------|---------|----------|
| Component files | `PascalCase.tsx` | `IncidentList.tsx`, `KubeChanSidebar.tsx` | `services/frontend-ui/src/` |
| Hook files | `camelCase.ts` | `useWebSocket.ts` | `services/frontend-ui/src/useWebSocket.ts` |
| Utility files | `camelCase.ts` | `api.ts`, `chatter.ts` | `services/frontend-ui/src/` |
| Exported types/interfaces | `PascalCase` | `WSEvent`, `CurrentUser`, `Incident` | `services/frontend-ui/src/api.ts` |
| Functions/variables | `camelCase` | `getToken`, `clearToken`, `moodLevel` | `services/frontend-ui/src/App.tsx` |

---

### 2) Formatting and Linting

**Go:**
- Formatter: `gofmt` (implicit via `golangci-lint`); no explicit config file found in repo root
- Linter: `golangci-lint` (version pinned to `latest` in CI)
- Enforced rules include: `errcheck` (verified: all returned errors must be handled), `staticcheck`, `vet`; lint errors blocked PRs
- Run: `make lint-go` → `golangci-lint run ./...`

**Python:**
- Formatter: none explicitly configured (no `ruff`, `black`, or `.flake8` found in `pyproject.toml`)
- Type checker: `pyright` (configured as dev dependency)
- Run: `make lint-python` → `cd services/llm-gateway && pyright .`
- [TODO] No formatter enforced for Python — code style consistency is type-checker only

**TypeScript:**
- Formatter: none explicitly configured (no `.prettierrc`, `.eslintrc` found)
- Compiler: `tsc -b` as part of Vite build
- [TODO] No ESLint or Prettier config found; TypeScript strictness settings not verified beyond default Vite template

---

### 3) Import and Module Conventions

**Go:**
- Standard Go import grouping (stdlib, external, internal) — enforced implicitly by `goimports` (part of golangci-lint)
- Module path: `github.com/org/kubechan`; internal packages imported as `github.com/org/kubechan/services/...`
- The `api/v1alpha1` package is imported by all three Go services as `v1alpha1 "github.com/org/kubechan/api/v1alpha1"`
- No barrel exports; each package exports only what is used by other packages

**Python:**
- `from __future__ import annotations` used in all app files (deferred evaluation for type hints)
- Relative imports avoided; all imports use absolute `from app.xxx import yyy`
- Provider imports inside `make_provider()` function body to avoid circular imports (noted in comment)

**TypeScript:**
- No path aliases (`@/`) found; all imports use relative paths (e.g., `./api`, `./IncidentList`)
- No barrel (`index.ts`) exports observed; components imported directly

---

### 4) Error and Logging Conventions

**Go error handling:**
- Errors are returned, not panicked, throughout all layers
- Error wrapping uses `fmt.Errorf("context: %w", err)` consistently
- HTTP handlers call `writeError(w, http.StatusXxx, err.Error())` for user-facing errors
- Kubernetes `client.IgnoreNotFound(err)` used in reconcilers to handle deleted objects gracefully
- At startup, fatal errors call `logger.Error(...)` then `os.Exit(1)`

**Logging (Go):**
- backend-api and diagnostics-worker use `log/slog` with `slog.NewJSONHandler(os.Stdout, nil)` — structured JSON logs to stdout
- cluster-watcher uses `controller-runtime`'s `ctrl.SetLogger(zap.New(...))` — zap-backed structured logs
- Log messages use `slog.Error("context", "error", err)` or key-value pairs, not format strings
- No sensitive data redaction in logs is explicitly enforced at the logging layer; documented as post-MVP concern

**Python logging:**
- `structlog` is a production dependency but `logging.basicConfig` is used in `main.py`; structlog integration depth is [TODO]
- Log messages use `logger.info("msg | key=%s", value)` style in `routes.py`

**Sensitive data:**
- Secret values are never collected by diagnostics-worker (only metadata checked)
- ConfigMap values for known-sensitive keys are redacted (per `project-description.md`); exact redaction implementation is [TODO] (not verified in collector source)

---

### 5) Testing Conventions

- **Go:** test files named `*_test.go` and co-located with source files (e.g., `correlation_reconciler.go` alongside `controllers_test.go`)
- **Python:** test files in `services/llm-gateway/tests/` directory
- **Mocking in Go:** controller-runtime `fake.Client` used to mock Kubernetes API; no external mock framework found
- **Mocking in Python:** [TODO] — mock strategy not verified from test files
- **Coverage expectations:**
  - Go: 65% total minimum (`.github/go-coverage.yml`); `cluster-watcher/detector` must be ≥80%
  - Python: 70% minimum enforced by `pytest-cov --cov-fail-under=70` (`pyproject.toml`)

---

### 6) Evidence

- `services/backend-api/handler/helpers.go` — `writeError`, `writeJSON`, `namespacedName`
- `services/backend-api/handler/auth.go` — error return conventions, `jwtSecret`, `ctxKeyUserID`
- `services/cluster-watcher/controllers/correlation_reconciler.go` — `client.IgnoreNotFound`, error wrapping
- `services/backend-api/main.go` — `log/slog` JSON logger initialization
- `services/cluster-watcher/main.go` — `ctrl.SetLogger(zap.New(...))`
- `services/llm-gateway/app/models.py` — Pydantic models, naming rules
- `services/frontend-ui/src/api.ts` — TypeScript naming, relative imports
- `.github/go-coverage.yml` — coverage thresholds
- `services/llm-gateway/pyproject.toml` — pytest-cov config
- `Makefile` — lint commands
