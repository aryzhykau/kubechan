# Testing and verification

Validation is layered. Unit tests prove deterministic behavior; production builds prove language integration; Tilt proves image, chart, cluster, and ingress integration.

## Canonical commands

Run from the repository root unless a command changes directories.

```bash
# All Go packages
go test ./...

# CI-equivalent Go race and coverage run
go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...

# LLM gateway with configured coverage threshold
cd services/llm-gateway && uv run pytest -v

# LLM gateway type checking
cd services/llm-gateway && uv run pyright .

# Frontend TypeScript and production bundle
cd services/frontend-ui && npm ci && npm run build

# Helm
helm lint helm/kubechan --values helm/kubechan/values-dev.yaml

# Non-interactive local deployment and readiness check
tilt ci
```

Convenience targets:

```bash
make test       # Go + Python tests
make build      # Go builds + uv sync + frontend build
make lint       # golangci-lint + pyright
make e2e        # kind context check + tilt ci
```

The frontend currently has no unit-test runner or lint script. Do not document or invoke `npm test` or `npm run lint` until those scripts exist.

## Scope matrix

| Change | Minimum iteration check | Required before handoff |
|---|---|---|
| Go helper or detector | Target package `go test` | `go test ./...` |
| Reconciler or CRD | Target controller tests | `make generate`, `go test ./...`, Helm lint |
| Source catalog | collector tests | diagnostics-worker tests plus fresh-run E2E |
| GraphEngine | graphengine tests | diagnostics-worker tests, frontend build, fresh-run E2E |
| Prompt/model schema | focused pytest | full gateway pytest and fresh-run E2E |
| Gateway validation | route tests | full gateway pytest |
| Frontend | `npm run build` | production build and browser verification |
| Helm/Tilt | Helm lint | `tilt ci` |
| Cross-service contract | relevant unit suites | all affected builds plus fresh-run E2E |

## Test organization

### Go

- Standard `testing` package; tests are co-located.
- controller-runtime fake client is used for Kubernetes behavior that does not require a live API server.
- backend database tests use isolated temporary/in-memory SQLite state.
- GraphEngine tests assert deterministic IDs, ordering, missing references, candidate ranking, and source attachment.

CI requires global Go coverage and runs the race detector. Thresholds are configured in `.github/go-coverage.yml`.

### Python

- pytest tests live in `services/llm-gateway/tests/`.
- FastAPI `TestClient`, `unittest.mock`, and async mocks isolate provider calls.
- `pyproject.toml` enforces branch coverage and a 70% minimum.
- Route tests must cover invalid citations, invalid causal paths, model variants, and provider failures.

### Frontend

The current automated gate is `tsc -b && vite build`. Browser behavior is verified against the Tilt deployment. When a frontend test framework is introduced, add scripts to `package.json`, CI, Makefile, and this document together.

## Runtime E2E procedure

`tilt ci` proves that local images build, load into kind, deploy through the Helm-rendered manifests, and reach readiness. It does not create or inspect an incident automatically.

For evidence/analysis changes:

1. Ensure the active context starts with `kind-`.
2. Run `tilt ci` or keep `tilt up` running.
3. Apply the relevant fixture, such as `hack/demo-manual-incident.yaml`.
4. Trigger a new analysis. Do not reuse an old DiagnosticRun for a new schema.
5. Verify evidence `schemaVersion`, source paths, graph nodes/edges/candidates, and citation validation.
6. Open `/incidents/:incidentId/assessments/:runId`.
7. Select the broken relationship and candidate relationship.
8. Confirm the inspector shows the exact metadata/log/event source and that proposed topology is not presented as observed topology.

## Generated-code check

CI runs controller-gen and fails if it changes tracked output. After touching CRD types:

```bash
make generate
git diff -- api/v1alpha1/zz_generated.deepcopy.go helm/kubechan/crds/
```

## Known gaps

- No automated browser E2E suite.
- No comprehensive envtest suite for every controller.
- No frontend component/unit test framework.
- Tilt E2E validates readiness, while scenario assertions remain manual.
