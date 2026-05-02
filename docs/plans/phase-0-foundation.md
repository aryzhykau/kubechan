# Phase 0 — Foundation

## Prerequisites
None. This phase must complete before all others.

## External dependencies
None.

---

## Tasks (ordered)

### [0.1] Monorepo layout — ~1h
Create the top-level directory structure:
```
kubechan/
  services/
    cluster-watcher/
    diagnostics-worker/
    backend-api/
    llm-gateway/
    frontend-ui/
  api/
    v1alpha1/
  helm/kubechan/
  docs/
  Makefile
  .github/workflows/
```
- Establishes import paths and build boundaries for all downstream work.
- Agree on and set Go module path: `github.com/org/kubechan/api/v1alpha1` — used by cluster-watcher, diagnostics-worker, and backend-api.

---

### [0.2] CRD Go types + controller-gen

**Task 0.2.1** — Define `ProblemCase` Go types (~2h)
- File: `api/v1alpha1/problemcase_types.go`
- `ProblemCaseSpec`: affectedResource (namespace/kind/name), detector string, severity (critical|high|medium|low), symptoms []string, relatedResources
- `ProblemCaseStatus`: state (open|investigating|resolved), firstSeen, lastSeen, resolvedAt (optional), latestDiagnosticRunRef string, latestAnalysisResultRef string
- Labels for dedup: `kubechan.io/affected-resource`, `kubechan.io/detector`
- Markers: `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`
- Printer columns: `+kubebuilder:printcolumn` for severity + state + firstSeen

**Task 0.2.2** — Define `DiagnosticRun` Go types (~2h)
- File: `api/v1alpha1/diagnosticrun_types.go`
- `DiagnosticRunSpec`: problemCaseRef string, requestedAt metav1.Time
- `DiagnosticRunStatus`: state (pending|running|completed|failed), collectedAt metav1.Time, collectorVersion string, evidenceRef string (SQLite ID), collectionErrors []string, redactionSummary struct, logTruncationInfo struct
- Markers: `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`

**Task 0.2.3** — Scheme registration (~30min)
- File: `api/v1alpha1/groupversion_info.go`
- `GroupVersion`, `SchemeBuilder`, `AddToScheme` — standard controller-runtime pattern

**Task 0.2.4** — Run controller-gen object (~30min)
```
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
```
- Generates `api/v1alpha1/zz_generated.deepcopy.go`
- Check generated file into repo (NOT gitignored)

**Task 0.2.5** — Run controller-gen CRD (~30min)
```
controller-gen crd paths="./api/..." output:crd:artifacts:config=helm/kubechan/crds/
```
- Outputs: `helm/kubechan/crds/problemcases.kubechan.io.yaml`, `diagnosticruns.kubechan.io.yaml`
- Check generated YAMLs into repo

---

### [0.3] Helm chart skeleton

**Task 0.3.1** — `Chart.yaml` + `values.yaml` (~2h)
- `Chart.yaml`: name, version, appVersion, description
- `values.yaml`: all service toggles (clusterWatcher, diagnosticsWorker, backendApi, llmGateway, frontendUi, persona) per full-plan.md §7

**Task 0.3.2** — `values-dev.yaml` (~1h)
- Smaller resource limits, single replicas
- NodePort services for backend-api + frontend-ui (for Docker Desktop access)
- `debounceWindowSecs: 5` for faster dev feedback

**Task 0.3.3** — Placeholder templates (~2h)
- `helm/kubechan/templates/<service>/deployment.yaml` for all 5 services
- `helm/kubechan/templates/<service>/serviceaccount.yaml` for all 5 services
- `helm/kubechan/templates/_helpers.tpl`: common labels helper (`kubechan.io/component`, `app.kubernetes.io/name`)

---

### [0.4] Local dev environment

**Task 0.4.1** — Tiltfile (~2h)
```python
docker_build('kubechan/cluster-watcher', 'services/cluster-watcher')
docker_build('kubechan/diagnostics-worker', 'services/diagnostics-worker')
docker_build('kubechan/backend-api', 'services/backend-api')
docker_build('kubechan/llm-gateway', 'services/llm-gateway')
docker_build('kubechan/frontend-ui', 'services/frontend-ui')
k8s_yaml(helm('helm/kubechan', values=['values-dev.yaml']))
```

**Task 0.4.2** — Makefile targets (~1h)
- `make generate` — runs controller-gen object + crd
- `make dev-up` — `kubectl apply -f helm/kubechan/crds/` + `tilt up`
- `make dev-down` — `tilt down`
- `make lint` — golangci-lint (Go services) + pyright (llm-gateway)
- `make test` — `go test ./...` + `pytest`

---

### [0.5] CI pipeline

**Task 0.5.1** — Dockerfiles (~5h total, 1h each)
- `services/cluster-watcher/Dockerfile`: builder (Go 1.24 + module cache), runner (gcr.io/distroless/static)
- `services/diagnostics-worker/Dockerfile`: same Go pattern
- `services/backend-api/Dockerfile`: same Go pattern
- `services/llm-gateway/Dockerfile`: builder (pip install --no-cache-dir), runner (python:3.13-slim)
- `services/frontend-ui/Dockerfile`: builder (Node + `vite build`), runner (nginx:alpine + static files)

**Task 0.5.2** — GitHub Actions workflow (~2h)
- File: `.github/workflows/ci.yml`
- Trigger: `on: pull_request`
- Jobs (parallel): lint-go, lint-python, build-go, build-python, build-frontend, test-go, test-python
- Extra step in lint-go: run `make generate` + `git diff --exit-code` to assert controller-gen output is up-to-date

---

## Integration contracts this phase
- [ ] Agree on Go module path for `api/v1alpha1` — must be set before cluster-watcher, diagnostics-worker, and backend-api import it

## Integration test entry point
Run `make dev-up` against Docker Desktop Kubernetes (`kubectl config use-context docker-desktop`). Verify:
- CRDs installed: `kubectl get crds | grep kubechan.io` shows 2 entries
- All 5 placeholder Deployments reach `Pending` state
- Tilt UI shows 5 resources with no build errors
