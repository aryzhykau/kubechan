# Phase 2A — diagnostics-worker

## Prerequisites (from other services)
- `api/v1alpha1` CRD types available (Phase 0.2)
- `POST /internal/evidence` request schema agreed with backend-api team before task 2A.4 can be tested end-to-end
- backend-api `POST /internal/evidence` endpoint live (Phase 2B.6) for full integration

---

## Tasks (ordered)

### [2A.1] DiagnosticRun watcher (~2h)
- File: `services/diagnostics-worker/controllers/diagnosticrun_reconciler.go`
- controller-runtime `Reconciler` for `v1alpha1.DiagnosticRun`
- Predicate: only process DiagnosticRuns where `status.state == "pending"`
  ```go
  WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
      dr := obj.(*v1alpha1.DiagnosticRun)
      return dr.Status.State == "pending"
  }))
  ```
- On reconcile entry: immediately patch `status.state = running` (prevents double-processing on restart)
- After collection: patch `status.state = completed` or `status.state = failed`
- `main.go`: `ctrl.NewManager` + register DiagnosticRunReconciler; in-cluster config

---

### [2A.2] Collector interface (~1h)
- File: `services/diagnostics-worker/collector/interface.go`
```go
type ProblemCaseRef struct {
    Namespace string
    Name      string
    Kind      string
    AffectedResource struct {
        Namespace string
        Kind      string
        Name      string
    }
}

type Evidence struct {
    CollectorName string
    Data          interface{} // typed per collector, marshalled to JSON
    Error         string      // empty if successful
}

type Collector interface {
    Name() string
    Collect(ctx context.Context, ref ProblemCaseRef) (Evidence, error)
}
```
- Collection errors are non-fatal: collector returns partial Evidence with Error field populated

---

### [2A.3] All collectors

**Task 2A.3.1** — `PodCollector` (~2h)
- File: `services/diagnostics-worker/collector/pod.go`
- Fetch pod by namespace/name
- Collect: `spec` (containers, initContainers, volumes, nodeName, tolerations), `status` (phase, conditions, containerStatuses, initContainerStatuses — including restartCount, lastState, state)

**Task 2A.3.2** — `EventsCollector` (~2h)
- File: `services/diagnostics-worker/collector/events.go`
- List events where `involvedObject.name == podName` + `involvedObject.namespace == namespace`
- Return last 50 events sorted by `lastTimestamp` descending
- Collect: reason, message, type (Normal/Warning), count, firstTimestamp, lastTimestamp

**Task 2A.3.3** — `LogsCollector` (~3h)
- File: `services/diagnostics-worker/collector/logs.go`
- Current logs: `client.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{TailLines: &tailLines})` — default 500 lines
- Previous logs: same with `Previous: true` — default 200 lines
- Collect for each container in pod (skip if container never ran)
- Populate `logTruncationInfo`: compare actual byte length vs. limit; set `truncated: true` if hit limit
- On `previous` log failure (container has no previous run): populate `collectionErrors`, do not fail whole collection

**Task 2A.3.4** — `OwnerCollector` (~3h)
- File: `services/diagnostics-worker/collector/owner.go`
- Walk `pod.OwnerReferences`: Pod → ReplicaSet → Deployment/StatefulSet/DaemonSet
- For each owner: fetch spec + status
- Collect Deployment: `spec.replicas`, `spec.strategy`, `spec.selector`, `status.readyReplicas`, `status.unavailableReplicas`, `status.conditions`
- Stop at the root owner (no further ownerReferences)

**Task 2A.3.5** — `ServiceCollector` (~2h)
- File: `services/diagnostics-worker/collector/service.go`
- List Services in same namespace where `spec.selector` is a subset of pod labels
- For each matching Service: collect `spec` (clusterIP, type, ports, selector) + `status`
- List EndpointSlices with label `kubernetes.io/service-name=<svcName>`: collect `endpoints` (addresses, conditions.ready, targetRef)

**Task 2A.3.6** — `IngressCollector` (~2h)
- File: `services/diagnostics-worker/collector/ingress.go`
- List Ingresses in namespace
- Filter: any `spec.rules[*].http.paths[*].backend.service.name` matches a service collected in 2A.3.5
- Collect: `spec.rules`, `spec.tls` (hostnames only, no certs), `status.loadBalancer`

**Task 2A.3.7** — `ConfigMapCollector` (~2h)
- File: `services/diagnostics-worker/collector/configmap.go`
- Source: ConfigMap names referenced in pod spec (volumes, envFrom, env.valueFrom.configMapKeyRef)
- Collect: name, namespace, list of keys (`.data` keys only — no values)
- Reason: config key presence is diagnostically useful; values may contain sensitive data

**Task 2A.3.8** — `SecretCollector` (~2h)
- File: `services/diagnostics-worker/collector/secret.go`
- Source: Secret names referenced in pod spec (volumes, envFrom, imagePullSecrets, env.valueFrom.secretKeyRef)
- Collect: name, namespace, list of keys, type
- **`.data` and `.stringData` are explicitly excluded** — struct must not contain these fields
- Unit test must assert no secret data values appear in output (see 2A.5.2)

**Task 2A.3.9** — `NodeCollector` (~1h)
- File: `services/diagnostics-worker/collector/node.go`
- Fetch node named in `pod.Spec.NodeName`
- Collect: `status.conditions` (Ready, MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable), `status.allocatable`, `status.capacity`

---

### [2A.4] Redaction pipeline (~4h)
- File: `services/diagnostics-worker/redact/pipeline.go`
- Input: marshalled evidence JSON string; output: redacted JSON string + `redactionSummary`
- Regex patterns to redact (replace with `[REDACTED]`):
  - Bearer tokens: `Bearer [A-Za-z0-9\-._~+/]+=*`
  - Basic auth: `Basic [A-Za-z0-9+/]+=*`
  - Password params: `(?i)(password|passwd|secret|api_key|apikey|token)=[^\s&"']+`
  - AWS access keys: `AKIA[0-9A-Z]{16}`
  - AWS secret keys (common pattern): `(?i)aws_secret[_\w]*\s*[:=]\s*[^\s"']+`
  - PEM blocks: `-----BEGIN [A-Z ]+-----[\s\S]*?-----END [A-Z ]+-----`
- Walk all string-typed JSON fields recursively; apply each pattern
- Populate `redactionSummary.patternsApplied` (count) + `redactionSummary.redactedFields` (JSON path list)
- Runs AFTER collection, BEFORE `POST /internal/evidence`

---

### [2A.5] Evidence dispatch + tests

**Task 2A.5.1** — POST evidence to backend-api (~2h)
- File: `services/diagnostics-worker/dispatch/evidence.go`
- After collection + redaction: `POST http://backend-api.kubechan.svc.cluster.local/internal/evidence`
- Request body: full evidence payload per agreed contract (see db-architecture-design.md)
- On HTTP failure: log error; still mark DiagnosticRun `state = completed` (partial evidence is better than no evidence)
- Populate `collectionErrors` in DiagnosticRun status for any collector that failed

**Task 2A.5.2** — Unit test: no secret data leakage (~2h)
- File: `services/diagnostics-worker/collector/secret_test.go`
- Create a fake Secret with `.data` values; run `SecretCollector.Collect`; assert output struct contains no `.data` or `.stringData` content
- This test blocks merge if it fails (CI gate)

**Task 2A.5.3** — Unit test: redaction pipeline (~2h)
- File: `services/diagnostics-worker/redact/pipeline_test.go`
- Table-driven: each pattern gets a test case; verify `[REDACTED]` replacement and `redactedFields` populated

---

## Integration test entry point
With Phase 1 running (ProblemCase CRDs being created) and backend-api Phase 2B live:
1. A ProblemCase exists in the cluster
2. POST to `backend-api /api/v1/problemcases/:id/analyze` → creates DiagnosticRun (state=pending)
3. diagnostics-worker picks it up, runs all collectors, posts to `/internal/evidence`
4. `kubectl get diagnosticrun <id>` → state=completed, evidenceRef populated
5. `GET /api/v1/problemcases/:id/evidence` → returns evidence payload from SQLite
