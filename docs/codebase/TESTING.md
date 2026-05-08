# Testing Patterns

## Core Sections (Required)

### 1) Test Stack and Commands

**Go:**
- Primary test framework: `go test` (standard library)
- Assertion library: standard `testing.T` (no third-party assertion library found)
- Mocking: controller-runtime `sigs.k8s.io/controller-runtime/pkg/client/fake` (fake Kubernetes client)
- Coverage: `vladopajic/go-test-coverage` action with thresholds from `.github/go-coverage.yml`

**Python (llm-gateway):**
- Primary test framework: `pytest` ≥8.0
- Coverage: `pytest-cov` ≥7.1.0 with `--cov-fail-under=70`
- Type checking (dev): `pyright` ≥1.1

```bash
# Run all Go tests
make test-go        # → go test ./...

# Run Go tests with coverage
go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...

# Run Python tests
make test-python    # → cd services/llm-gateway && python -m pytest -v

# Run Python tests with coverage (default — pyproject.toml addopts)
cd services/llm-gateway && uv run pytest -v

# Run all tests
make test
```

---

### 2) Test Layout

**Go:**
- Test files are **co-located** with source files: `foo.go` + `foo_test.go` in the same directory
- Naming convention: `*_test.go`; test functions `TestXxx(t *testing.T)`
- Some packages have multiple test files grouped by concern (e.g., `handler/handler_test.go`, `handler/handler2_test.go`, `handler/auth2_test.go`)

**Python:**
- Test files in `services/llm-gateway/tests/` directory
- Naming convention: `test_*.py` (pytest discovery default)

**No setup files** (e.g., `TestMain`) observed — each test is self-contained.

---

### 3) Test Scope Matrix

| Scope | Covered? | Typical target | Notes |
|-------|----------|----------------|-------|
| Unit (Go) | Yes | `detector/`, `debounce/`, `exclusion/`, `problemcase/`, `handler/`, `db/`, `ws/`, `watcherconfig/` | Most packages have `*_test.go`; fake k8s client used for CRD operations |
| Unit (Python) | Yes | `llm-gateway/app/` modules | `tests/` directory; coverage ≥70% enforced |
| Integration | Partial | `backend-api/k8s/k8s_test.go`, `startup/startup_test.go` | Uses fake client; no real k8s cluster in CI |
| E2E | No | — | No e2e framework configured; `make e2e` target exists in Makefile but is [TODO] |

---

### 4) Mocking and Isolation Strategy

**Go:**
- Kubernetes API interactions mocked via `sigs.k8s.io/controller-runtime/pkg/client/fake.NewClientBuilder().Build()` — no real cluster required
- `*sql.DB` used directly in tests with an in-memory SQLite database (`:memory:`) [TODO — not explicitly verified, likely pattern given `db_test.go` exists]
- No test fixtures directory; setup is inline per test function
- Common failure mode: tests that rely on `KUBECONFIG` (e.g., pod log collection) tolerate failures with a log message (`"tolerate pod-log errors in collect tests (no kubeconfig in unit env)"` — from CI commit history)

**Python:**
- Mock strategy: [TODO — not verified from test source files]
- FastAPI `TestClient` likely used (standard pattern for FastAPI apps) — [TODO]

---

### 5) Coverage and Quality Signals

**Go:**
- Coverage tool: `vladopajic/go-test-coverage` with profile `coverage.out`
- Thresholds (`.github/go-coverage.yml`):
  - Global: ≥65%
  - `cluster-watcher/detector`: ≥80%
- Generated code excluded: `api/v1alpha1/zz_generated.*`
- `main.go` files excluded (nothing testable in entry points)
- Badge: `.badges/main/coverage.svg` auto-updated on `main` branch pushes
- Race detector: enabled (`-race` flag in CI)

**Python:**
- Coverage tool: `pytest-cov` with branch coverage (`branch = true`)
- Threshold: ≥70% (enforced in CI — `--cov-fail-under=70` in `pyproject.toml` addopts)
- Coverage report: `services/llm-gateway/coverage.xml` (uploaded as CI artifact)
- Badge: `.badges/main/coverage.svg` (shared with Go badge)

**Known gaps:**
- E2E tests do not exist
- No envtest-based integration tests for controllers (noted in `.github/go-coverage.yml` comment: "Target: raise this to 50% once backend-api and cluster-watcher controllers have envtest-based tests")
- `handler/` package tests were primarily added to reach coverage thresholds (commit history shows series of coverage-improving commits)
- Python mock strategy details [TODO]

---

### 6) Evidence

- `.github/go-coverage.yml` — Go coverage thresholds and exclusions
- `.github/workflows/ci.yml` — Go `-race` flag, `vladopajic/go-test-coverage` action, pytest invocation
- `services/llm-gateway/pyproject.toml` — `[tool.pytest.ini_options]` with `--cov-fail-under=70`
- `services/backend-api/db/db_test.go` — Go DB test example
- `services/cluster-watcher/detector/crashloopbackoff_test.go` — detector unit test example
- `services/backend-api/handler/handler_test.go` — handler unit test example
- `Makefile` — `test-go`, `test-python`, `e2e` targets
