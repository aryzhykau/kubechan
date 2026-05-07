# Phase 7 — Exclusion Rules & Smart Evidence Collection

## Summary

Two related features driven by a real observed case: a KEDA-managed deployment scaled to zero
during off-hours triggered a `ServiceNoEndpoints` incident. KubeChan correctly diagnosed it as
expected behaviour but had no mechanism to (a) suppress the same false positive next time, or
(b) fetch the `ScaledObject` CRD the LLM asked for.

**Feature A — KubechanExclusionRule CRD**
Operators (or KubeChan on LLM suggestion) can declare rules that suppress specific detectors for
specific resources, optionally scoped to a recurring time window.

**Feature B — Dynamic Evidence Collection + Smart Resource Picker**
Replace hardcoded kind lists and namespace-scoped resource listings with a discovery-backed,
cluster-aware resource picker. Attach per-resource evidence slice selection (spec/status/events/logs)
so the user controls exactly what gets sent to the LLM.

---

## Prerequisites

- All Phase 0–4 services implemented and integrated (MVP running).
- `controller-runtime` dynamic client available in `diagnostics-worker`.
- `client-go` discovery API available in `backend-api`.

---

## Critical Path

```
7.1 (CRD) → 7.2 (cluster-watcher enforcement) → 7.3 (backend exclusion API)
7.4 (ResourceRef.evidenceSlices + apiGroup) → 7.5 (backend discovery endpoint) → 7.6 (dynamic collector)
7.7 (LLM schema) → 7.8 (frontend) — can start 7.8 in parallel with 7.6/7.7
```

---

## Tasks (ordered)

---

### [7.1] KubechanExclusionRule CRD (~3h)

**Task 7.1.1** — Go type `api/v1alpha1/exclusionrule_types.go` (~2h)

New file alongside existing CRD types.

```go
type ExclusionRuleSpec struct {
    // Human-readable explanation of why this rule exists.
    Description string `json:"description,omitempty"`

    // Enabled controls whether this rule is actively enforced.
    // Defaults to true.
    // +kubebuilder:default=true
    Enabled bool `json:"enabled"`

    // Namespace optionally scopes the entire rule to a single namespace.
    // When set, the rule only fires if the affected resource is in this namespace.
    // Takes precedence over any namespace specified inside TargetResources or Selector.
    // +optional
    Namespace string `json:"namespace,omitempty"`

    // Detectors lists the detector names to suppress.
    // Empty slice means suppress ALL detectors for matched resources.
    // +optional
    Detectors []string `json:"detectors,omitempty"`

    // TargetResources is a list of exact resource references to match.
    // Can be used together with Selector — a resource matching EITHER is suppressed.
    // +optional
    TargetResources []ResourceRef `json:"targetResources,omitempty"`

    // Selector matches resources by label selector, optionally filtered by namespace and kinds.
    // Can be used together with TargetResources — a resource matching EITHER is suppressed.
    // +optional
    Selector *ExclusionSelector `json:"selector,omitempty"`

    // TimeWindow restricts the rule to specific recurring time periods.
    // Absent = rule applies 24/7.
    // +optional
    TimeWindow *ExclusionTimeWindow `json:"timeWindow,omitempty"`
}

type ExclusionSelector struct {
    // Namespace further narrows the selector to a specific namespace.
    // If ExclusionRuleSpec.Namespace is also set, both must match.
    // +optional
    Namespace   string            `json:"namespace,omitempty"`
    Kinds       []string          `json:"kinds,omitempty"`
    MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

type ExclusionTimeWindow struct {
    // Timezone is an IANA timezone name, e.g. "Europe/Berlin". Defaults to UTC.
    // +kubebuilder:default=UTC
    Timezone string            `json:"timezone"`
    Periods  []ExclusionPeriod `json:"periods"`
}

type ExclusionPeriod struct {
    // Start and End are "HH:MM" in 24h format.
    Start string   `json:"start"`
    End   string   `json:"end"`
    // Days: Mon, Tue, Wed, Thu, Fri, Sat, Sun
    Days  []string `json:"days"`
}

type ExclusionRuleStatus struct {
    // LastMatchedAt is when this rule last suppressed a ProblemCase creation.
    // +optional
    LastMatchedAt *metav1.Time `json:"lastMatchedAt,omitempty"`

    // SuppressedCount is the lifetime count of suppressed ProblemCase creations.
    // +optional
    SuppressedCount int `json:"suppressedCount,omitempty"`
}
```

Markers:
```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=".spec.enabled"
// +kubebuilder:printcolumn:name="Detectors",type=string,JSONPath=".spec.detectors"
// +kubebuilder:printcolumn:name="Description",type=string,JSONPath=".spec.description"
```

Register in `groupversion_info.go` / `doc.go` and add to `SchemeBuilder`.

**Task 7.1.2** — Regenerate DeepCopy (~0.5h)
- Run `make generate` (or `controller-gen object:headerFile=hack/boilerplate.go.txt ./api/...`)
- Verify `zz_generated.deepcopy.go` has `DeepCopyObject` for `KubechanExclusionRule` and `KubechanExclusionRuleList`

**Task 7.1.3** — Helm CRD YAML (~0.5h)
- File: `helm/kubechan/crds/kubechan.io_kubechanexclusionrules.yaml`
- Generate via `controller-gen crd` or hand-write from the Go type
- Add to `helm/kubechan/templates/backend-api/clusterrole.yaml` (get/list/watch/create/patch/delete for `kubechanexclusionrules`)
- Add to `helm/kubechan/templates/cluster-watcher/clusterrole.yaml` (get/list/watch + patch status for `kubechanexclusionrules`)

---

### [7.2] cluster-watcher — Exclusion enforcement (~3h)

**Task 7.2.1** — `exclusion/matcher.go` (~2h)
- File: `services/cluster-watcher/exclusion/matcher.go`
- Function: `func IsExcluded(ctx context.Context, reader client.Reader, controlNS string, obj client.Object, detectorName string) (bool, string, error)`
  - Lists all `KubechanExclusionRule` in `controlNS`
  - For each rule where `Enabled == true`:
    - **Namespace scope check** (fast pre-filter): if `rule.Spec.Namespace != ""`, skip rule unless `obj.GetNamespace() == rule.Spec.Namespace`
    - **Resource match** (OR logic — passes if EITHER condition is true):
      - `TargetResources`: any entry with exact `namespace/kind/name` match against `obj`
      - `Selector`: `obj.GetNamespace()` matches `selector.Namespace` (if set) AND `obj.GetObjectKind().GroupVersionKind().Kind` is in `selector.Kinds` (if set) AND all `selector.MatchLabels` are present in `obj.GetLabels()`
    - Both `TargetResources` and `Selector` can be populated simultaneously; rule fires if either matches
    - **Detector check**: `rule.Spec.Detectors` empty = match all detectors; else `detectorName` must appear in the list
    - **TimeWindow check** if set: parse IANA timezone, check current time vs periods
  - Returns `(true, ruleName, nil)` on first matching rule
  - Returns `(false, "", nil)` if no rule matches
- Time window logic: if `end < start` in "HH:MM" terms (spans midnight), handle wrap-around correctly

**Task 7.2.2** — Hook into ProblemCase creation (~1h)
- Files: all reconcilers that call `problemcase.EnsureOpen(...)` or equivalent
  - `services/cluster-watcher/controllers/service_reconciler.go`
  - `services/cluster-watcher/controllers/deployment_reconciler.go`
  - `services/cluster-watcher/controllers/pod_reconciler.go` (if applicable)
- After detector fires and before creating/updating ProblemCase, call `exclusion.IsExcluded()`
- If excluded:
  - Skip `ProblemCase` creation
  - Emit a Kubernetes `Event` on the affected object: `Reason: ExclusionRuleSuppressed`, `Message: "Suppressed by KubechanExclusionRule <name>"`
  - Patch `rule.Status.LastMatchedAt = now`, increment `rule.Status.SuppressedCount`
  - Log at `Info` level: `"exclusion rule matched, skipping ProblemCase"` with fields `rule`, `resource`, `detector`

---

### [7.3] backend-api — Exclusion Rules REST API (~3h)

**Task 7.3.1** — Handler `handler/exclusion_rules.go` (~2h)

New handler struct:
```go
type ExclusionRules struct {
    K8s              client.Client
    DefaultNamespace string
}
```

Endpoints:
- `GET /api/v1/exclusion-rules` — list all `KubechanExclusionRule` in `DefaultNamespace`; return array of full CRD specs
- `POST /api/v1/exclusion-rules` — create a new rule from JSON body; validates `description` non-empty, at least one of `targetResources`/`selector` set
- `PATCH /api/v1/exclusion-rules/{name}` — accepts `{ "enabled": true/false }`; patches only `spec.enabled` using `MergePatch`
- `DELETE /api/v1/exclusion-rules/{name}` — deletes the CRD; returns `204`

Response shape for list/get items:
```json
{
  "name": "keda-offhours-myapp",
  "spec": { "enabled": true, "description": "...", "detectors": [...], "targetResources": [...], "timeWindow": {...} },
  "status": { "lastMatchedAt": "...", "suppressedCount": 5 }
}
```

**Task 7.3.2** — Register routes in `main.go` (~0.5h)
```go
r.Get("/api/v1/exclusion-rules", exclusionHandler.List)
r.Post("/api/v1/exclusion-rules", exclusionHandler.Create)
r.Patch("/api/v1/exclusion-rules/{name}", exclusionHandler.SetEnabled)
r.Delete("/api/v1/exclusion-rules/{name}", exclusionHandler.Delete)
```

**Task 7.3.3** — RBAC: add `kubechanexclusionrules` to backend-api ClusterRole verbs (~0.5h)

---

### [7.4] ResourceRef — evidenceSlices + apiGroup (~1.5h)

**Task 7.4.1** — Extend `ResourceRef` in `api/v1alpha1/problemcase_types.go` (~0.5h)
```go
type ResourceRef struct {
    Namespace string `json:"namespace,omitempty"`
    Kind      string `json:"kind"`
    Name      string `json:"name"`

    // APIGroup is the Kubernetes API group for this resource kind, e.g. "apps", "keda.sh".
    // Empty means the core API group. Used by the dynamic client in diagnostics-worker.
    // +optional
    APIGroup string `json:"apiGroup,omitempty"`

    // EvidenceSlices controls which parts of the resource the diagnostics-worker collects.
    // Valid values:
    //   spec        — resource spec field
    //   status      — full status object
    //   conditions  — status.conditions array, extracted separately for clarity
    //   events      — Kubernetes Events for this resource
    //   logs        — container logs (Deployment, StatefulSet, DaemonSet, Pod, Job only)
    //   metrics     — current CPU/memory from metrics-server (same container-having kinds)
    //   labels      — metadata.labels
    //   annotations — metadata.annotations (sensitive values redacted)
    //   ownerRefs   — metadata.ownerReferences chain, resolved recursively up to the root
    // Empty = collect all slices applicable to the kind (backward-compatible default).
    // Default ON when non-empty: spec, status, conditions, events, labels.
    // Default OFF: logs, metrics, annotations, ownerRefs.
    // +optional
    EvidenceSlices []string `json:"evidenceSlices,omitempty"`
}
```

**Task 7.4.2** — Regenerate DeepCopy (~0.5h)
- Run `make generate` again; confirm `ResourceRef.DeepCopyInto` copies the new slice fields

**Task 7.4.3** — Update `relatedResourceIn` in `handler/manual_incident.go` and `handler/augment.go` (~0.5h)
```go
type relatedResourceIn struct {
    Kind           string   `json:"kind"`
    APIGroup       string   `json:"apiGroup,omitempty"`
    Name           string   `json:"name"`
    Namespace      string   `json:"namespace"`
    EvidenceSlices []string `json:"evidenceSlices,omitempty"`
}
```
Pass `APIGroup` and `EvidenceSlices` through into the `ResourceRef` when building the CRD spec.

---

### [7.5] backend-api — Discovery-backed resource listing (~4h)

**Task 7.5.1** — `handler/resources.go`: `ListKinds` endpoint (~2h)

New endpoint:
```
GET /api/v1/namespaces/{ns}/kinds?q=Scaled
```
- Uses `discovery.NewDiscoveryClientForConfig(cfg)` (inject `*rest.Config` into the `Resources` handler)
- Calls `discoveryClient.ServerPreferredNamespacedResources()` — returns all namespaced resource types known to the API server
- Filters to resources with `list` and `get` verbs (readable resources only)
- Optional `q` param: case-insensitive prefix filter on `kind`
- Response: `[{ "kind": "ScaledObject", "apiGroup": "keda.sh" }]` — sorted alphabetically, no duplicates

**Task 7.5.2** — `handler/resources.go`: extend `ListResources` for dynamic kinds (~2h)

Current `GET /api/v1/namespaces/{ns}/resources?kind=X` only handles a hardcoded switch.

Extend the handler:
- Accept optional `apiGroup` query param: `?kind=ScaledObject&apiGroup=keda.sh`
- When `apiGroup` is present (or kind is not in the hardcoded switch): use `dynamic.NewForConfig(cfg)` to list resources via `schema.GroupVersionResource` resolved from the discovery cache
- Return same `[{ name, namespace }]` shape as today
- Hardcoded-switch path stays for known core/apps kinds (no regression)
- Inject `*rest.Config` into `Resources` handler (already available in `main.go`)

**Task 7.5.3** — Register new route in `main.go` (~0.5h)
```go
r.Get("/api/v1/namespaces/{ns}/kinds", resourcesHandler.ListKinds)
```

---

### [7.6] diagnostics-worker — Dynamic unstructured collector (~4h)

**Task 7.6.1** — `collector/dynamic.go` (~3h)

New collector function:
```go
func CollectUnstructured(
    ctx context.Context,
    cfg *rest.Config,
    ref v1alpha1.ResourceRef,
    slices []string,  // nil/empty = all
) (*UnstructuredEvidence, error)
```

Logic:
1. Build `DiscoveryClient` from `cfg`; resolve `(Kind, APIGroup) → GVR` using `restmapper.GetAPIGroupResources`
2. Build `dynamic.Interface` client; call `resource.Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})`
3. For each requested slice:
   - `spec`: extract `obj.Object["spec"]`; run existing redaction pass
   - `status`: extract `obj.Object["status"]`
   - `conditions`: extract `obj.Object["status"].("conditions")` array; fallback to empty array if absent
   - `events`: call `coreV1.Events(ref.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=<name>"})`
   - `logs`: only for container-having kinds (Deployment/StatefulSet/DaemonSet/Pod/Job); resolve pod selector → collect container + init container logs via existing log collector
   - `metrics`: call `metricsClient.MetricsV1beta1().PodMetricses(ref.Namespace).List(...)` filtered by pod selector; attach CPU/memory per container
   - `labels`: extract `obj.GetLabels()`
   - `annotations`: extract `obj.GetAnnotations()`; redact values whose keys contain `token`, `secret`, `password`, `key`, `credential` (case-insensitive)
   - `ownerRefs`: walk `obj.GetOwnerReferences()` recursively (max 5 hops); return chain as `[{kind, name, uid}]`
4. Return `UnstructuredEvidence{ Kind, APIGroup, Name, Namespace, Slices map[string]json.RawMessage }`

**Task 7.6.2** — Hook into DiagnosticRun collection (~1h)
- File: `services/diagnostics-worker/collector/collector.go` (or equivalent orchestrator)
- For each `ref` in `incident.Spec.RelatedResources`:
  - If `ref.APIGroup != ""` OR kind is not in the hardcoded known-kinds set: use `CollectUnstructured`
  - Otherwise: use existing typed collectors (no regression)
  - Pass `ref.EvidenceSlices` as the `slices` argument

---

### [7.7] llm-gateway — suggestExclusionRule in response schema (~2h)

**Task 7.7.1** — Extend Pydantic response model (`app/models.py` or equivalent) (~1h)
```python
class ExclusionRuleProposal(BaseModel):
    reason: str
    detectors: list[str]
    target_resources: list[dict]  # [{namespace, kind, name}]
    time_window: dict | None = None  # {timezone, periods:[{start,end,days}]}

class AnalysisResult(BaseModel):
    ...existing fields...
    suggest_exclusion_rule: ExclusionRuleProposal | None = None
```

**Task 7.7.2** — Extend system prompt (~1h)
- Add a section instructing the LLM when to emit `suggest_exclusion_rule`:
  - Only when the LLM has **high confidence** the behaviour is intentional and operator-configured
    (e.g. KEDA scale-to-zero, maintenance drain, intentional PDB disruption)
  - Never suggest for transient problems or genuine failures
  - The `reason` field must explain in plain language why this is expected
  - Include evidence: reference the specific resource (e.g. `ScaledObject`) that confirms intent

---

### [7.8] backend-api — propagate suggestExclusionRule (~1h)

**Task 7.8.1** — `handler/analysis.go` (or wherever LLM response is stored)
- Add `suggest_exclusion_rule` JSON column to `analysis_results` table via a new DB migration
- Store the raw JSON blob from the LLM response
- Include it in the `GET /api/v1/diagnosticruns/{id}/analysisresult` response

---

### [7.9] frontend-ui (~10h total)

**Task 7.9.1** — `api.ts` additions (~0.5h)
```typescript
export interface KindItem { kind: string; apiGroup: string }
export interface ExclusionRuleProposal {
  reason: string
  detectors: string[]
  targetResources: Array<{ namespace: string; kind: string; name: string }>
  timeWindow?: { timezone: string; periods: Array<{ start: string; end: string; days: string[] }> }
}
export interface ExclusionRule {
  name: string
  spec: {
    enabled: boolean; description: string; detectors: string[]
    targetResources?: ResourceRef[]; selector?: object; timeWindow?: object
  }
  status: { lastMatchedAt?: string; suppressedCount?: number }
}

// New API calls:
api.listKinds(namespace: string, q?: string): Promise<KindItem[]>
api.listResources(namespace: string, kind: string, apiGroup?: string): Promise<ResourceItem[]>
api.listExclusionRules(): Promise<ExclusionRule[]>
api.createExclusionRule(spec: object): Promise<ExclusionRule>
api.setExclusionRuleEnabled(name: string, enabled: boolean): Promise<void>
api.deleteExclusionRule(name: string): Promise<void>
```

**Task 7.9.2** — `ResourcePicker` shared component (~3h)
- File: `services/frontend-ui/src/ResourcePicker.tsx`
- Props: `value: ResourceEntry | null`, `onChange: (entry: ResourceEntry | null) => void`
- Where `ResourceEntry = { namespace: string; kind: string; apiGroup: string; name: string; evidenceSlices: string[] }`
- UI layout (three fields in a row, then slices):
  1. **Namespace** — `Select` from `GET /api/v1/namespaces`; unchanged from today
  2. **Kind** — `Autocomplete` (MUI); on namespace select: fetches `GET /api/v1/namespaces/{ns}/kinds`; user types to filter; shows `kind (apiGroup)` in dropdown; resets name when kind changes
  3. **Name** — `Autocomplete` (MUI); on kind select: fetches `GET /api/v1/namespaces/{ns}/resources?kind=X&apiGroup=Y`; shows name options
  4. **Evidence slices** — rendered only after name is chosen; nine `Chip` toggles grouped into two rows:
     - Row 1 (default ON):  `spec`  `status`  `conditions`  `events`  `labels`
     - Row 2 (default OFF): `logs`  `metrics`  `annotations`  `ownerRefs`
     - `logs` and `metrics` chips are only rendered for container-having kinds (`Deployment`, `StatefulSet`, `DaemonSet`, `Pod`, `Job`)
     - `metrics` chip is visually marked `(metrics-server)` with a tooltip warning that it requires metrics-server to be installed
- Emits full `ResourceEntry` with selected slices on every change

**Task 7.9.3** — Update `ManualIncidentModal.tsx` to use `ResourcePicker` (~1.5h)
- Remove hardcoded `ROOT_KINDS` and `RELATED_KINDS` arrays entirely
- Root resource section: same namespace → kind → name layout, but kind is now `Autocomplete` backed by `ListKinds` (full discovery, same as related resources); name backed by `ListResources`; no evidence slices for root resource (always collect everything for the incident root)
- Related resources section: replace kind toggle + namespace/name selects with `ResourcePicker` component per row (includes slice selection)
- Submit: include `apiGroup` and `evidenceSlices` in `relatedResources` payload; root resource `apiGroup` also included

**Task 7.9.4** — Update `AugmentIncidentModal.tsx` to use `ResourcePicker` (~1h)
- Same replacement: swap kind toggle + name select for `ResourcePicker` rows
- Include `apiGroup` and `evidenceSlices` in augment payload

**Task 7.9.5** — `ExclusionRuleModal.tsx` (~2h)
- Opened from two places: "Review & Create Rule" button in `KubeChanSidebar` (LLM suggestion), and "Add Rule" button on the Exclusion Rules page
- When opened from LLM suggestion: pre-fills form with `suggestExclusionRule` proposal data (read-only preview mode + editable description)
- Fields:
  - Description (required, text field)
  - Namespace scope (optional text field with namespace autocomplete; if set, restricts the whole rule to that namespace)
  - Detectors (multi-select chips from the known detector list + free text for custom detector names; empty = suppress all)
  - Target Resources (one or more namespace→kind→name rows using the same namespace/kind/name selects as `ResourcePicker` but without slice toggles)
  - Selector (optional toggle section; if enabled: namespace field + kinds multi-select + label key=value pairs)
  - Note: Target Resources and Selector are both optional individually but at least one must be non-empty on submit
  - Time window (optional toggle; if enabled: timezone autocomplete from IANA list + period rows with start/end time inputs + day checkboxes)
- Submit → `POST /api/v1/exclusion-rules`; on success close modal + refresh rules list

**Task 7.9.6** — `ExclusionRulesPage.tsx` (~2h)
- New top-level page/route accessible from the Settings nav (new "Exclusion Rules" tab)
- Table columns: Name | Description | Detectors | Resources matched | Time window | Enabled | Actions
- Enabled column: `Switch` component; on toggle → `PATCH /api/v1/exclusion-rules/{name}` with debounce
- Actions: Delete button with inline confirm (`"Remove rule '...'?" → [Cancel] [Remove]`)
- Row expansion or side panel: shows full CRD spec in a styled code block
- Empty state message: KubeChan chatter line in the style of existing `chatter.ts` reactions
- "Add Rule" button top-right: opens `ExclusionRuleModal` in blank mode

**Task 7.9.7** — `KubeChanSidebar.tsx` — suggest exclusion rule banner (~1h)
- When `analysisResult.suggestExclusionRule` is present, render a distinct amber banner below the main analysis (separate from the blue `needsMoreInfo` banner):
  ```
  ⚠ KubeChan suggests suppressing this detector
  "This is managed by KEDA and expected to scale to zero..."
  [Review & Create Rule]
  ```
- "Review & Create Rule" opens `ExclusionRuleModal` pre-filled with proposal
- Banner disappears once a rule with matching detectors+resources exists (check against rules list)

**Task 7.9.8** — `chatter.ts` additions (~0.5h)
- Add reaction lines for exclusion rule events:
  - `exclusionRuleCreated`: `["Fine, I'll look the other way. But only because you said so.", "Rule noted. I'll pretend that never happened.", "Suppressed. Don't make this a habit."]`
  - `exclusionRulesEmpty`: `["Nothing's off the record. I'm watching everything.", "No exclusions. Everything is fair game."]`
- Wire `exclusionRuleCreated` reaction in `ExclusionRuleModal` after successful POST

---

### [7.10] Helm chart updates (~1.5h)

**Task 7.10.1** — ClusterRole updates
- `cluster-watcher` ClusterRole: add `kubechanexclusionrules` get/list/watch + status patch
- `backend-api` ClusterRole: add `kubechanexclusionrules` get/list/watch/create/patch/delete
- `diagnostics-worker` ClusterRole: no change needed (uses in-cluster config for dynamic client, existing ClusterRole already has broad read)

**Task 7.10.2** — CRD file added to `helm/kubechan/crds/`
- Helm installs CRDs from `crds/` on first install and upgrades — no additional template needed

---

## Integration Contracts

| Contract | Between | Detail |
|---|---|---|
| `ResourceRef.apiGroup + evidenceSlices` | All services | Must be in the CRD type before any service implementation starts |
| `KubechanExclusionRule` CRD installed | cluster-watcher ↔ backend-api | cluster-watcher lists it; backend-api creates/patches it |
| `suggestExclusionRule` JSON field | llm-gateway → backend-api → frontend | Field name and shape agreed before 7.7 starts |
| `GET /api/v1/namespaces/{ns}/kinds` | backend-api → frontend | Response shape `[{kind, apiGroup}]` agreed before 7.9.2 starts |

---

## Risk Assessment

**Highest-risk task: 7.6.1 (dynamic unstructured collector)**
The dynamic client + REST mapper resolution adds a new dependency path in diagnostics-worker. If the
cluster's API server is slow or the discovery cache is stale, evidence collection for user-added
resources may fail silently. Mitigation: wrap in a timeout context, surface errors in
`DiagnosticRunStatus.CollectionErrors` (already supported), and fall back gracefully — a missing
dynamic evidence block must never fail the entire DiagnosticRun.

**Second-highest risk: 7.5.1 (discovery endpoint)**
`ServerPreferredNamespacedResources()` can return errors for aggregated APIs that are temporarily
unavailable (e.g. metrics-server down). The handler must tolerate partial results and not 500 the
entire response — log per-group errors, return whatever was successfully discovered.

**Critical path summary:**
Tasks 7.1 → 7.4 must complete first (CRD types + ResourceRef shape) as every other task depends on
them. Tasks 7.2 and 7.3 can then proceed in parallel. Tasks 7.5 and 7.6 can run in parallel after
7.4. Task 7.7 is independent of all Go work. Task 7.9 (frontend) can begin on stubs/mocks after
7.9.1 is done and unblock from real endpoints as 7.3/7.5 land.
