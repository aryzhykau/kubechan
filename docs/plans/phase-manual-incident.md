# Manual Incident Creation — Implementation Plan

## Business goal

KubeChan currently only knows about problems it discovers via cluster-watcher. This feature lets a
user report a problem themselves: pick a root resource, optionally tag related resources, describe
what they are seeing in plain text, and submit. KubeChan collects evidence and runs LLM analysis
exactly like an auto-detected incident. The result appears in the same diagnostics UI with a
"Manual" badge and the user's description as the symptom text.

## Architecture decision

Create a real `Incident` CRD. The existing pipeline (Incident → DiagnosticRun → diagnostics-worker
→ /internal/evidence → LLM dispatch) is reused unchanged. Three new spec fields carry the
manual-specific context. No shared `pkg/collector` extraction is needed.

---

## Implementation steps

### Step 1 — Extend the Incident CRD

**File:** `api/v1alpha1/incident_types.go`

Add to `IncidentSpec`:

```go
// Source indicates who created the incident.
// +kubebuilder:validation:Enum=auto;manual
// +kubebuilder:default=auto
// +optional
Source string `json:"source,omitempty"`

// UserMessage is the plain-text description provided by the user for manual incidents.
// +optional
UserMessage string `json:"userMessage,omitempty"`

// RelatedResources is a list of additional Kubernetes resources the user tagged as relevant.
// Cross-namespace is allowed. Only used for manual incidents.
// +optional
RelatedResources []ResourceRef `json:"relatedResources,omitempty"`
```

Add to `IncidentStatus` (for display in kubectl):

```go
// Source is copied from spec for use as a print column.
// +optional
Source string `json:"source,omitempty"`
```

Update `+kubebuilder:printcolumn` markers to include Source:
```
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=".spec.source"
```

After editing run:
```
controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/...
```

This regenerates `zz_generated.deepcopy.go`. The `RelatedResources []ResourceRef` field needs a
loop in `DeepCopyInto` — controller-gen handles that automatically.

**Also update Helm CRD YAML:**  
`helm/kubechan/crds/kubechan.io_incidents.yaml` — regenerate with:
```
controller-gen crd paths=./api/... output:crd:artifacts:config=helm/kubechan/crds
```

---

### Step 2 — DB migration

**File:** `services/backend-api/db/migrations/003_manual_incidents.sql`

```sql
-- Migration 003: source + user_message for manual incidents

ALTER TABLE evidence          ADD COLUMN source       TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE evidence          ADD COLUMN user_message TEXT;
ALTER TABLE analysis_results  ADD COLUMN source       TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE analysis_requests ADD COLUMN source       TEXT NOT NULL DEFAULT 'auto';
```

The `source` column on all three tables lets the diagnostics page filter / badge correctly without
re-reading the CRD.

---

### Step 3 — Backend-api: K8s resource listing endpoints

**File:** `services/backend-api/handler/resources.go` (new)

New handler struct:

```go
type Resources struct {
    K8s client.Client
}
```

Endpoints:

```
GET /api/v1/namespaces
    → lists all Namespaces, filters out kube-system, kube-public, kube-node-lease
    → returns []string of namespace names

GET /api/v1/namespaces/{ns}/resources?kind=Deployment
    → lists resources of given kind in namespace
    → returns []struct{ Name string `json:"name"` }
    → supported kinds: Deployment, StatefulSet, DaemonSet, Job, CronJob,
                       Pod, Service, PersistentVolumeClaim, ConfigMap
```

Register in `main.go`:

```go
resources := &handler.Resources{K8s: k8s}

r.Route("/api/v1", func(r chi.Router) {
    // ... existing routes ...
    r.Get("/namespaces", resources.ListNamespaces)
    r.Get("/namespaces/{ns}/resources", resources.ListResources)
    r.Post("/incidents/manual", manualIncident.Create)  // see step 4
})
```

**RBAC** — add to `helm/kubechan/templates/backend-api/clusterrole.yaml` (or create if missing):

```yaml
- apiGroups: [""]
  resources: [namespaces, pods, persistentvolumeclaims, configmaps, services, events]
  verbs: [get, list, watch]
- apiGroups: [apps]
  resources: [deployments, statefulsets, daemonsets, replicasets]
  verbs: [get, list, watch]
- apiGroups: [batch]
  resources: [jobs, cronjobs]
  verbs: [get, list, watch]
```

---

### Step 4 — Backend-api: POST /api/v1/incidents/manual

**File:** `services/backend-api/handler/manual_incident.go` (new)

Request body:

```json
{
  "namespace":       "production",
  "resourceKind":    "Deployment",
  "resourceName":    "ml-inference",
  "userMessage":     "pods restarting since 14:00 deploy, memory climbing",
  "relatedResources": [
    {"kind": "PersistentVolumeClaim", "name": "ml-model-cache", "namespace": "production"},
    {"kind": "ConfigMap",             "name": "ml-config",      "namespace": "production"}
  ]
}
```

Handler flow:

1. Validate: `namespace`, `resourceKind`, `resourceName` required; `userMessage` min 10 chars.
2. Build `Incident` CRD with:
   - `Spec.Source = "manual"`
   - `Spec.UserMessage = req.UserMessage`
   - `Spec.RootResource = ResourceRef{Kind, Name, Namespace}`
   - `Spec.RelatedResources = req.RelatedResources`
   - `Spec.ProblemCases = []` (empty — diagnostics-worker handles empty list)
   - `Status.OpenedAt = now`
   - `GenerateName = "manual-"`
   - `Namespace = req.Namespace`
3. `h.K8s.Create(ctx, inc)` — CRD creation triggers the existing pipeline automatically.
4. Return `{ "incidentId": "<namespace>/<name>" }`.

The rest (DiagnosticRun creation, evidence collection, LLM dispatch) happens via the existing
watcher + reconciler flow — no changes to diagnostics-worker needed for this minimal version.

Handler struct:

```go
type ManualIncident struct {
    K8s              client.Client
    DefaultNamespace string
}
```

---

### Step 5 — diagnostics-worker: RelatedResources evidence collection

**File:** `services/diagnostics-worker/controllers/diagnosticrun_reconciler.go`

In `collect()`, after existing root resource + problemcases collection, add:

```go
// Collect events + pod logs for any RelatedResources from the incident spec.
for _, rr := range inc.Spec.RelatedResources {
    rrNS := rr.Namespace
    if rrNS == "" {
        rrNS = ns
    }
    events, _ := r.collectEvents(ctx, rrNS, rr.Kind, rr.Name)
    // append to ev.RelatedResourceEvidence (new field — see collector/types.go)
}
```

**File:** `services/diagnostics-worker/collector/types.go`

Add to `Evidence`:

```go
// RelatedResourceEvidence holds events (and optionally logs) for user-tagged related resources.
// Only populated for manual incidents.
RelatedResourceEvidence []RelatedResourceEvidence `json:"relatedResourceEvidence,omitempty"`

// UserMessage is the plain-text description provided by the user for manual incidents.
UserMessage string `json:"userMessage,omitempty"`
```

New type:

```go
type RelatedResourceEvidence struct {
    Resource ResourceRef `json:"resource"`
    Events   []K8sEvent  `json:"events,omitempty"`
    Logs     string      `json:"logs,omitempty"`
}
```

**File:** `services/backend-api/handler/internal.go`

`ReceiveEvidence` handler already stores the full payload. For manual incidents, also:
- copy `source` from `analysis_requests` row into `evidence` row
- pass `userMessage` to `dispatchAnalysis` (step 6)

---

### Step 6 — llm-gateway: userMessage injection

**File:** `services/llm-gateway/main.py`

Add to `AnalyzeRequest`:

```python
userMessage: str = ""
```

In `_build_prompt`, when `user_message` is non-empty, prepend a framing block before the evidence
section:

```
USER REPORTED: "pods restarting since 14:00 deploy, memory climbing"
Treat this as your primary diagnostic framing. Interpret all evidence below in light of
what the user described. The user has direct knowledge of the symptom timeline.
```

Pass through from `analyze()`:

```python
user_message = req.userMessage
prompt = _build_prompt(payload, ..., user_message=user_message)
```

No new endpoint — `userMessage` is optional, so the auto-flow is unchanged.

---

### Step 7 — Frontend: api.ts additions

**File:** `services/frontend-ui/src/api.ts`

```typescript
export interface ResourceItem { name: string; namespace: string }

export const listNamespaces = (): Promise<string[]> =>
  apiFetch('/api/v1/namespaces')

export const listResources = (ns: string, kind: string): Promise<ResourceItem[]> =>
  apiFetch(`/api/v1/namespaces/${ns}/resources?kind=${kind}`)

export const createManualIncident = (body: {
  namespace: string
  resourceKind: string
  resourceName: string
  userMessage: string
  relatedResources: { kind: string; name: string; namespace: string }[]
}): Promise<{ incidentId: string }> =>
  apiFetch('/api/v1/incidents/manual', { method: 'POST', body: JSON.stringify(body) })
```

---

### Step 8 — Frontend: ManualIncidentModal component

**File:** `services/frontend-ui/src/ManualIncidentModal.tsx` (new)

A modal with 3 sections:

**Section 1 — Root resource (required)**
- Namespace dropdown (loads `listNamespaces()` on open)
- Kind selector: pill buttons — Deployment / StatefulSet / DaemonSet / Pod / Job
- Resource name dropdown (loads `listResources(ns, kind)` when ns+kind are set, or freeform input
  as fallback if listing fails)

**Section 2 — Related resources (optional)**
- "Add related resource" button → appends a row with namespace+kind+name dropdowns
- Up to 5 rows; each row has a remove (×) button

**Section 3 — Describe the problem (required)**
- `<textarea placeholder="What are you seeing?">`
- Min 10 chars enforced on submit

Submit button → `createManualIncident(...)` → calls `onCreated(incidentId)` prop on success.

Loading state on submit (spinner + disabled button). Error message on API failure.

---

### Step 9 — Frontend: wire into IncidentList + App.tsx

**IncidentList (wherever it lives):**
- Add "Report an issue" button in the header/toolbar
- Clicking it sets `showManualModal = true`
- Render `<ManualIncidentModal onClose={...} onCreated={handleManualCreated} />`

**App.tsx `handleManualCreated(incidentId)`:**
- Sets KubeChan sidebar to `thinking` pose
- Stores `incidentId` in state
- Polls `GET /api/v1/incidents/{id}` every 2s until the incident has a completed DiagnosticRun
  (check via `GET /api/v1/diagnosticruns?incidentId=...` or WS event `incident-analysis-done`)
- When done: set sidebar to `speaking`, display analysis result — same path as existing analyze flow

**Incident list item display:**
- Show "Manual" badge when `spec.source === "manual"`
- Show `spec.userMessage` as the symptom text (truncated to 1 line) instead of ProblemCase symptoms

---

## File change summary

| File | Change |
|---|---|
| `api/v1alpha1/incident_types.go` | Add `Source`, `UserMessage`, `RelatedResources` to `IncidentSpec` |
| `api/v1alpha1/zz_generated.deepcopy.go` | Regenerate |
| `helm/kubechan/crds/kubechan.io_incidents.yaml` | Regenerate |
| `helm/kubechan/templates/backend-api/clusterrole.yaml` | Add list rules for core+apps+batch resources |
| `services/backend-api/db/migrations/003_manual_incidents.sql` | New — source + user_message columns |
| `services/backend-api/handler/resources.go` | New — namespace + resource listing |
| `services/backend-api/handler/manual_incident.go` | New — POST /api/v1/incidents/manual |
| `services/backend-api/main.go` | Register new routes + handler structs |
| `services/diagnostics-worker/collector/types.go` | Add `RelatedResourceEvidence`, `UserMessage` to `Evidence` |
| `services/diagnostics-worker/controllers/diagnosticrun_reconciler.go` | Collect RelatedResources evidence |
| `services/llm-gateway/main.py` | Add `userMessage` to request + prompt injection |
| `services/frontend-ui/src/api.ts` | `listNamespaces`, `listResources`, `createManualIncident` |
| `services/frontend-ui/src/ManualIncidentModal.tsx` | New component |
| `services/frontend-ui/src/App.tsx` | Wire modal + `handleManualCreated` |
| Incident list component | Manual badge + userMessage symptom text |

## Implementation order

1. CRD types + regenerate deepcopy + Helm CRD YAML (Step 1)
2. DB migration (Step 2)
3. Backend resource listing + manual incident handler + route registration (Steps 3–4)
4. diagnostics-worker RelatedResources collection (Step 5)
5. llm-gateway userMessage (Step 6)
6. Frontend api.ts (Step 7)
7. Frontend ManualIncidentModal (Step 8)
8. Wire modal into app + incident list badge (Step 9)
