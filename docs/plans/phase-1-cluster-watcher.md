# Phase 1 — cluster-watcher

## Prerequisites (from other services)
- `api/v1alpha1` CRD Go types + DeepCopy generated (Phase 0.2)
- Go module path agreed (Phase 0.1 integration contract)
- CRDs installed in cluster (Phase 0.2.5 + `make dev-up`)

---

## Tasks (ordered)

### [1.1] Manager setup + reconciler registration

**Task 1.1.1** — `main.go` (~2h)
- `ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme})` — in-cluster config via `rest.InClusterConfig()`; `ctrl.GetConfigOrDie()` handles both in-cluster and kubeconfig (for dev)
- Register `api/v1alpha1` types with Manager's scheme: `utilruntime.Must(v1alpha1.AddToScheme(scheme))`
- Register all reconcilers (stubs at this point) — fills out as [1.1.2]–[1.1.7] are completed
- `if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil { ... }` — blocks

**Task 1.1.2** — `PodReconciler` stub + registration (~2h)
- File: `services/cluster-watcher/controllers/pod_reconciler.go`
- Struct: embeds `client.Client`, `Scheme *runtime.Scheme`, `Detectors []detector.Detector`, `Debouncer *debounce.Debouncer`
- `Reconcile(ctx, req ctrl.Request) (ctrl.Result, error)` — stub returning `ctrl.Result{}, nil`
- Manager registration: `ctrl.NewControllerManagedBy(mgr).For(&corev1.Pod{}).Complete(r)`

**Task 1.1.3** — `DeploymentReconciler` stub + registration (~2h)
- Same pattern as PodReconciler
- `For(&appsv1.Deployment{})`
- Also watches ReplicaSets, StatefulSets, DaemonSets via `Watches()` on same reconciler

**Task 1.1.4** — `ServiceReconciler` stub + registration (~2h)
- Watches `corev1.Service` + `discoveryv1.EndpointSlice`
- `For(&corev1.Service{}).Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(...))`

**Task 1.1.5** — `NodeReconciler` stub + registration (~1h)
- `For(&corev1.Node{})`

**Task 1.1.6** — `EventReconciler` stub + registration (~1h)
- `For(&corev1.Event{})`
- Note: Events are high-volume; add `WithEventFilter(predicate.NewPredicateFuncs(...))` to filter by involvedObject kind

**Task 1.1.7** — `ProblemCaseReconciler` stub + registration (~2h)
- Watches own `v1alpha1.ProblemCase` CRDs
- Responsible for resolve logic (Phase 1.3) — stub for now
- `For(&v1alpha1.ProblemCase{})`

---

### [1.2] Detector interface + 5 MVP detectors

**Task 1.2.1** — `Detector` interface (~1h)
- File: `services/cluster-watcher/detector/interface.go`
```go
type Symptom struct {
    Message  string
    Severity string // critical|high|medium|low
}

type Detector interface {
    Name() string
    Evaluate(ctx context.Context, obj client.Object, reader client.Reader) ([]Symptom, error)
}
```
- `client.Reader` is backed by Manager's informer cache — no extra API server calls

**Task 1.2.2** — `CrashLoopBackOffDetector` (~2h)
- File: `services/cluster-watcher/detector/crashloopbackoff.go`
- Check: `pod.Status.ContainerStatuses[*].State.Waiting.Reason == "CrashLoopBackOff"`
- Also check `InitContainerStatuses`
- Return Symptom with severity: `critical`

**Task 1.2.3** — `ImagePullBackOffDetector` (~2h)
- File: `services/cluster-watcher/detector/imagepullbackoff.go`
- Check: reason in `["ImagePullBackOff", "ErrImagePull"]` in containerStatuses + initContainerStatuses
- Severity: `high`

**Task 1.2.4** — `PendingTooLongDetector` (~2h)
- File: `services/cluster-watcher/detector/pendingtoolong.go`
- Check: `pod.Status.Phase == corev1.PodPending && time.Since(pod.CreationTimestamp.Time) > threshold`
- Threshold default: 5 minutes; configurable via struct field set from Helm values (ConfigMap mount)
- Severity: `medium`

**Task 1.2.5** — `DeploymentUnavailableDetector` (~2h)
- File: `services/cluster-watcher/detector/deploymentunavailable.go`
- Check: `deploy.Status.UnavailableReplicas > 0 && deploy.Status.ReadyReplicas < *deploy.Spec.Replicas`
- Also: `deploy.Status.AvailableReplicas == 0 && *deploy.Spec.Replicas > 0`
- Severity: `high`

**Task 1.2.6** — `ServiceNoEndpointsDetector` (~3h)
- File: `services/cluster-watcher/detector/servicenoendpoints.go`
- Guard: only run for non-headless ClusterIP services (`spec.clusterIP != "None"` and `spec.type == ClusterIP`)
- Use `reader.List` to fetch EndpointSlices with label `kubernetes.io/service-name=<svcName>` in same namespace
- Check: no EndpointSlice has any `endpoints[*].conditions.ready == true`
- Severity: `high`

**Task 1.2.7** — Wire detectors into reconcilers (~1h)
- `PodReconciler` runs: CrashLoopBackOff, ImagePullBackOff, PendingTooLong
- `DeploymentReconciler` runs: DeploymentUnavailable
- `ServiceReconciler` runs: ServiceNoEndpoints

**Task 1.2.8** — Unit tests for all 5 detectors (~3h)
- Use `sigs.k8s.io/controller-runtime/pkg/client/fake.NewClientBuilder()` to build test reader
- Table-driven tests: happy path (symptom returned), clean path (no symptom), edge cases (nil specs)

---

### [1.3] Debounce + dedup + ProblemCase lifecycle

**Task 1.3.1** — `Debouncer` (~3h)
- File: `services/cluster-watcher/debounce/debouncer.go`
```go
type Debouncer struct {
    timers sync.Map // key: string → *time.Timer
    window time.Duration
}
func (d *Debouncer) Debounce(key string, fn func()) // resets timer on each call
func (d *Debouncer) Cancel(key string)
```
- On each reconcile with symptoms: call `d.Debounce(resourceKey, func() { createOrUpdateProblemCase(...) })`
- Window default: 30s (configurable via Helm values ConfigMap)

**Task 1.3.2** — Deduplication lookup (~2h)
- File: `services/cluster-watcher/problemcase/lookup.go`
- `client.List` ProblemCases filtered by labels:
  - `kubechan.io/affected-resource=<namespace.kind.name>`
  - `kubechan.io/detector=<detectorName>`
- Return open (state != resolved) ProblemCase if found, nil otherwise

**Task 1.3.3** — ProblemCase create (~2h)
- File: `services/cluster-watcher/problemcase/lifecycle.go`
- If no existing open ProblemCase: `client.Create` with:
  - Labels set for dedup
  - `spec` filled from detector + resource ref
  - `status.state = open`, `status.firstSeen = now`, `status.lastSeen = now`
- Handle conflict errors (another reconciler beat us to it): re-list and patch instead

**Task 1.3.4** — ProblemCase update (lastSeen + symptoms) (~1h)
- If existing open ProblemCase found: `client.Status().Patch` with `lastSeen = now`, updated `symptoms`
- Use `client.MergeFrom` patch type

**Task 1.3.5** — Auto-resolve via `ProblemCaseReconciler` (~3h)
- File: `services/cluster-watcher/controllers/problemcase_reconciler.go`
- On reconcile of a ProblemCase with `state = open`:
  1. Fetch the affected resource (pod/deployment/service/node) from cache
  2. Re-run the detector named in `spec.detector`
  3. If no symptoms returned: `client.Status().Patch` → `state = resolved`, `resolvedAt = now`
  4. If resource no longer exists: also resolve (resource was deleted, problem gone)
- Requeue every 60s: `ctrl.Result{RequeueAfter: 60 * time.Second}`

**Task 1.3.6** — Integration test (~2h)
- Spin up envtest (`sigs.k8s.io/controller-runtime/pkg/envtest`)
- Create a Pod with CrashLoopBackOff state → assert ProblemCase created within debounce window
- Update Pod to Running → trigger ProblemCaseReconciler → assert `state = resolved`

---

## Integration test entry point
With `make dev-up` running against Docker Desktop:
1. Deploy a broken pod: `kubectl run crasher --image=nonexistent:tag`
2. Wait 30s (debounce window)
3. `kubectl get problemcases` — should show one open ProblemCase with detector=ImagePullBackOff
4. Fix: `kubectl delete pod crasher`
5. Within ~60s: `kubectl get problemcases` — state should flip to `resolved`
