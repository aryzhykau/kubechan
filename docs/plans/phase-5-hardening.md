# Phase 5 — Hardening

## Prerequisites (from other services)
- All Phase 0–4 services implemented and running end-to-end
- Docker Desktop Kubernetes cluster available for E2E tests

---

## Tasks (ordered)

### [5.1] RBAC finalization (~2h)
- File: `helm/kubechan/templates/cluster-watcher/clusterrole.yaml`
  - Audit implemented reconcilers; verify verbs match exactly what is used (get/list/watch for all resource types; get/list/watch/create/update/patch for `kubechan.io/problemcases`)
  - Remove any broad `*` verbs; each resource group explicitly listed
- File: `helm/kubechan/templates/diagnostics-worker/clusterrole.yaml`
  - Confirm `secrets` has only `get, list` — NOT `watch` (reduces event volume)
  - No `pods/exec`, no `pods/portforward`
- File: `helm/kubechan/templates/backend-api/clusterrole.yaml`
  - Only `kubechan.io` resources + configmaps; no access to user workload resources
- Verify `llm-gateway` has NO ClusterRole or ClusterRoleBinding at all

**Task 5.1.2** — Network Policy manifests (~2h)
- Off by default: `networkPolicy.enabled: false` in `values.yaml`
- When enabled:
  - `cluster-watcher`: egress to Kubernetes API server only
  - `diagnostics-worker`: egress to Kubernetes API server + backend-api
  - `backend-api`: egress to Kubernetes API server + llm-gateway; ingress from frontend-ui + diagnostics-worker + llm-gateway
  - `llm-gateway`: egress to Bedrock endpoint (HTTPS 443) + backend-api; ingress from backend-api only
  - `frontend-ui`: ingress from Ingress controller only

---

### [5.2] Observability — Go services (~3h each, 3 services = ~9h)
Apply to: `cluster-watcher`, `diagnostics-worker`, `backend-api`

**Structured logging** (per service):
- Replace any `fmt.Printf`/`log.Printf` with `slog.InfoContext(ctx, "msg", "key", value)`
- JSON format: `slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))`
- Standard fields on all log lines: `service`, `version`, `trace_id` (from chi `middleware.RequestID`)

**Prometheus metrics** (per service):
- File: `<service>/metrics/metrics.go`
- Register with `prometheus.DefaultRegisterer`
- cluster-watcher:
  - `kubechan_detector_evaluations_total{detector,result}` counter
  - `kubechan_problemcase_created_total{detector,severity}` counter
  - `kubechan_debounce_active_timers` gauge
- diagnostics-worker:
  - `kubechan_collections_total{status}` counter
  - `kubechan_collection_duration_seconds{collector}` histogram
  - `kubechan_redacted_fields_total` counter
- backend-api:
  - `kubechan_http_requests_total{method,path,status}` counter
  - `kubechan_ws_connected_clients` gauge
  - `kubechan_analysis_requests_total{status}` counter
  - `kubechan_sqlite_query_duration_seconds{query}` histogram
- `GET /metrics` route: `promhttp.Handler()`

**Health endpoints** (per service):
- `/healthz`: always `200 {"status":"ok"}` — just signals process is alive
- `/readyz` for cluster-watcher: CRD list call succeeds
- `/readyz` for diagnostics-worker: CRD list call succeeds
- `/readyz` for backend-api: CRD list succeeds AND `SELECT 1` on SQLite succeeds

---

### [5.3] Observability — llm-gateway (Python) (~2h)
- `structlog.configure(processors=[structlog.processors.JSONRenderer()])` at startup
- Standard fields: `service="llm-gateway"`, `version`, bound in `structlog.contextvars`
- `prometheus_fastapi_instrumentator`: auto-instrument all FastAPI endpoints
- Custom counters:
  - `kubechan_bedrock_calls_total{status}` (success/throttled/failed)
  - `kubechan_bedrock_retries_total`
  - `kubechan_consistency_warnings_total`
  - `kubechan_thinking_budget_tokens_used_total`
- `GET /metrics` route added (FastAPI route returning prometheus exposition format)
- `/readyz`: checks boto3 client initialized; returns `503` if not

---

### [5.4] E2E test: ImagePullBackOff scenario (~3h)
- File: `e2e/imagepullbackoff_test.go` (or shell script in `e2e/`)
- Setup: `helm install kubechan helm/kubechan -f values-dev.yaml`
- Deploy: `kubectl run pulltest --image=kubechan-test/nonexistent:fake -n default`
- Assert (with retry up to 60s):
  - `kubectl get problemcases -l kubechan.io/detector=ImagePullBackOff` returns 1 result
  - ProblemCase `status.state == open`
  - ProblemCase `spec.severity == high`
- Teardown: `kubectl delete pod pulltest`
- Assert (with retry up to 90s): ProblemCase `status.state == resolved`

---

### [5.5] E2E test: CrashLoopBackOff scenario (~2h)
- Deploy: Pod with command `["sh", "-c", "exit 1"]`
- Assert: ProblemCase created with `detector=CrashLoopBackOff`, `severity=critical`, within debounce window
- Teardown + assert resolve

---

### [5.6] E2E test: full analysis pipeline (~3h)
- Prerequisites: valid Bedrock credentials available in CI (via IRSA or env vars)
- Trigger: `POST /api/v1/problemcases/:id/analyze`
- Assert (with retry up to 150s):
  - DiagnosticRun CRD transitions: pending → running → completed
  - `GET /api/v1/problemcases/:id/evidence` returns non-empty payload
  - `GET /api/v1/analysisresults/<latestAnalysisResultRef>` returns `status=completed`, `confidence > 0`
  - ProblemCase CRD `status.latestAnalysisResultRef` set
- Assert WebSocket: connect to `/ws` before triggering; verify `AnalysisResult.Completed` event received

---

### [5.7] `make e2e` target (~2h)
- File: `Makefile`
- Steps:
  1. `helm install kubechan helm/kubechan -f values-dev.yaml --wait --timeout 120s`
  2. Run E2E test binary / scripts
  3. `helm uninstall kubechan` (always, even on failure — `trap` or `defer`)
- `make e2e-clean`: force uninstall + delete CRDs for fresh state

---

### [5.8] Helm documentation (~2h)
- `helm/kubechan/values.yaml`: comment every field with type, default, and one-line description
- `helm/kubechan/templates/NOTES.txt`:
  - Post-install: how to get the UI URL (NodePort/LoadBalancer/Ingress)
  - IRSA setup steps summary (link to README for full steps)
  - First-time check: `kubectl get problemcases`
- `README.md` at repo root:
  - Prerequisites (Docker Desktop / EKS, Helm 3, kubectl)
  - Quick install (`helm install`)
  - IRSA setup for Bedrock
  - CRD upgrade procedure: "apply CRD YAMLs manually before `helm upgrade`"
  - Uninstall (note: CRDs not deleted by Helm — manual cleanup)
  - `values-dev.yaml` dev workflow

---

## Integration test entry point
`make e2e` runs the full suite against Docker Desktop. All 3 E2E scenarios pass. `kubectl get all -n kubechan` shows all services healthy. `helm uninstall kubechan` completes cleanly (CRDs remain as expected).
