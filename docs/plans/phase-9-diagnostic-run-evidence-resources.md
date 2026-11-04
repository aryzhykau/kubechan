# Phase 9 — Diagnostic Run Evidence: Unified Resource View

## Summary

The diagnostics detail page currently renders collected evidence through four hardcoded tabs —
*Logs*, *Events*, *Config*, *PVCs*. This layout was designed for the original narrow collector
that only targeted a fixed set of resource kinds. Two things have changed:

1. **The diagnostics worker now collects any K8s resource.** The `RelatedResourceEvidence`
   type already carries a generic `Spec map[string]any` that can hold any fields extracted
   from any resource kind. The hardcoded tabs cannot render this.

2. **KubeChan can request additional context on follow-up runs.** When the LLM responds with
   `needsMoreInfo: true` and a `suggestedResources` list, the next diagnostic run collects
   those extra resources and stores them in `relatedResourceEvidence`. These resources are
   currently **invisible** on the diagnostic run detail page.

**Solution:** Replace the four fixed tabs with a single, resource-centric list. Each collected
resource is one expandable card. The card renders whatever fields are present — logs, events,
phase, storage class, arbitrary spec keys — without knowing the kind in advance. All four
evidence sources (`rootResource`, `workloadPodLogs`, `pvcInfos`, `relatedResourceEvidence`)
are merged client-side into one unified list.

---

## Prerequisites

- Phase 7 complete (dynamic evidence collection, `RelatedResourceEvidence` in worker).
- Phase 8 complete (analysis result moved to incident card; DR detail page simplified).
- `relatedResourceEvidence` is populated in the evidence DB payload by the worker.

---

## Design decisions

### Normalization happens client-side

The backend `/api/v1/diagnosticruns/{id}/evidence` endpoint returns the raw `Evidence`
payload as stored. No backend changes are needed. The frontend merges the four evidence
arrays into a single `ResourceCard[]` list.

Normalization mapping:

| Payload field | `ResourceCard` kind label | Notes |
|---|---|---|
| `rootResource` + `rootResourceEvents` | `rootResource.kind` | events come from `rootResourceEvents` |
| `workloadPodLogs[]` | `Pod` | logs, prev logs, events, configMaps, secrets |
| `pvcInfos[]` | `PersistentVolumeClaim` | phase, storageClass, requestedStorage, events |
| `relatedResourceEvidence[]` | `resource.kind` | events, logs, generic `spec` key-value pairs |

### Generic field rendering

Each `ResourceCard` carries a `fields: FieldEntry[]` list assembled at normalization time.
A `FieldEntry` is one of:

```ts
type FieldEntry =
  | { type: 'text';   key: string; value: string }
  | { type: 'logs';   key: string; value: string; truncated?: boolean }
  | { type: 'events'; key: string; value: K8sEvent[] }
  | { type: 'kv';     key: string; value: Record<string, string> }  // ConfigMap data, flat maps
  | { type: 'chips';  key: string; value: string[] }                // secret names, mount paths
  | { type: 'json';   key: string; value: unknown }                 // arbitrary nested objects
```

The renderer iterates `fields` and picks a sub-component per `type`. The `json` type renders
a syntax-highlighted, read-only JSON pre-block — it is not a silent fallback but a first-class
type used intentionally for nested structures. This means adding a new evidence field in the
worker requires zero frontend changes.

### CRD resources (e.g. ScaledObject from KEDA)

For resource kinds the worker does not have a typed handler for (everything outside the
`switch` cases in `collectResourceSpec`), it falls to `collectDynamicSpec` which uses the
dynamic Kubernetes client. It returns the raw object as:

```json
{ "spec": { ...full CRD spec... }, "status": { ...full CRD status... } }
```

So `rre.Spec` for a `ScaledObject` looks like:

```json
{
  "spec": {
    "scaleTargetRef": { "name": "my-deployment" },
    "minReplicaCount": 0,
    "maxReplicaCount": 10,
    "triggers": [ { "type": "cron", "metadata": { "timezone": "Europe/London" } } ]
  },
  "status": {
    "conditions": [ { "type": "Ready", "status": "True" } ]
  }
}
```

Neither `spec` nor `status` are flat `Record<string, string>` maps or primitives, so the
normalization function emits `{ type: 'json', key: 'spec', value: {...} }` and
`{ type: 'json', key: 'status', value: {...} }` — two collapsible JSON blocks on the card.
This is the correct and expected rendering for any CRD fetched via the dynamic client.

### Expansion state

All resource cards are collapsed by default. The first card in the list (root resource) is
expanded by default. Expansion state is local component state — no Redux involvement.

### Empty state

If a card has no fields other than its identifier, it renders a single "No data collected"
note inside the expanded view.

---

## Tasks (ordered)

---

### [9.1] Frontend types — add `relatedResourceEvidence` and `FieldEntry` (~1h)

**Task 9.1.1** — Extend `EvidencePayload` in `services/frontend-ui/src/api/index.ts`

Add the missing `relatedResourceEvidence` field that currently exists in the Go
`Evidence` struct but is absent from the TypeScript type:

```ts
export interface RelatedResourceEvidence {
  resource: { kind: string; name: string; namespace?: string; apiGroup?: string }
  events?: K8sEvent[]
  logs?: string
  spec?: Record<string, unknown>
}

// Add to EvidencePayload:
relatedResourceEvidence?: RelatedResourceEvidence[]
```

**Task 9.1.2** — Define `ResourceCard` and `FieldEntry` types

New types used only by the DR detail page (can live in the component file or a local
`types.ts` alongside it):

```ts
type FieldEntry =
  | { type: 'text';   key: string; value: string }
  | { type: 'logs';   key: string; value: string; truncated?: boolean }
  | { type: 'events'; key: string; value: K8sEvent[] }
  | { type: 'kv';     key: string; value: Record<string, string> }
  | { type: 'chips';  key: string; value: string[] }

interface ResourceCard {
  id: string           // unique key for React: `${kind}/${namespace}/${name}`
  kind: string
  name: string
  namespace?: string
  fields: FieldEntry[]
}
```

---

### [9.2] Normalization function (~1.5h)

**Task 9.2.1** — Write `buildResourceCards(payload: EvidencePayload): ResourceCard[]`

Pure function, easily unit-testable. Lives in a sibling file
`services/frontend-ui/src/pages/diagnostics/buildResourceCards.ts`.

Steps:
1. Root resource card — `kind`/`name` from `payload.rootResource`, field `events` from
   `payload.rootResourceEvents`.
2. Pod cards — one per entry in `payload.workloadPodLogs`. Fields:
   - `text` for `phase`
   - `logs` for `logs` and `prevLogs` (key `"Previous container logs"`)
   - `events` for `events`
   - `kv` for each ConfigMap's `data`
   - `chips` for secret names
3. PVC cards — one per entry in `payload.pvcInfos`. Fields:
   - `text` for `phase`, `storageClass`, `requestedStorage`
   - `events` for `events`
4. Related resource cards — one per entry in `payload.relatedResourceEvidence`. Fields:
   - `events` if `events` non-empty
   - `logs` if `logs` non-empty
   - For each top-level key in `spec`:
     - `text` if the value is a primitive (string/number/bool)
     - `kv` if the value is a flat `Record<string, string>` (all values are strings, no nesting)
     - `json` if the value is an object or array — this is the expected path for resources
       collected via the dynamic client (CRDs like `ScaledObject`, `HPA`, etc.) where the
       entire `spec` and `status` sub-trees arrive as nested arbitrary objects

Cards with no fields still appear (name-only, with "No data collected" in expand view).

**Task 9.2.2** — Write unit tests for `buildResourceCards`

File: `services/frontend-ui/src/pages/diagnostics/buildResourceCards.test.ts`

Cover:
- Empty payload returns empty array.
- Root resource with no events produces a card with zero fields.
- Pod with logs, events, and both configMap data and secrets.
- PVC with all fields present.
- `relatedResourceEvidence` with `spec` containing string values, a `Record<string, string>`
  map, and a nested object — verify `text`, `kv`, `json` dispatch respectively.
- `relatedResourceEvidence` simulating a KEDA `ScaledObject` (dynamic client path):
  `spec = { spec: { minReplicaCount: 0, triggers: [...] }, status: { conditions: [...] } }` —
  verify both top-level keys produce `json` entries, not `kv` or `text`.
- Mixed payload with all four sources — verify ordering (root first, then pods, then
  PVCs, then related).

---

### [9.3] Field renderer sub-components (~1.5h)

**Task 9.3.1** — `FieldRenderer` component in `DiagnosticRunDetail.tsx`

Replaces `LogsTab`, `EventsTab`, `ConfigTab`, `PVCsTab` with a single renderer.

```tsx
function FieldRenderer({ field }: { field: FieldEntry }) {
  switch (field.type) {
    case 'text':   return <TextRow key={field.key} label={field.key} value={field.value} />
    case 'logs':   return <LogBlock key={field.key} label={field.key} value={field.value} truncated={field.truncated} />
    case 'events': return <EventsSection key={field.key} label={field.key} events={field.value} />
    case 'kv':     return <KVBlock key={field.key} label={field.key} data={field.value} />
    case 'chips':  return <ChipList key={field.key} label={field.key} items={field.value} />
    case 'json':   return <JsonBlock key={field.key} label={field.key} value={field.value} />
  }
}
```

Sub-components (`TextRow`, `LogBlock`, `EventsSection`, `KVBlock`, `ChipList`) are
extracted directly from the existing tab implementations — same styles, same logic.
`EventsSection` wraps the existing `EventsTable`. `JsonBlock` is a new component: a
label + `<Paper component="pre">` with `JSON.stringify(value, null, 2)`, same dark styling
as the existing log blocks — no external JSON viewer library needed.

---

### [9.4] Resource list layout (~1.5h)

**Task 9.4.1** — `ResourceCardList` component

Replaces the `Tabs` + four tab panels in `DiagnosticRunDetail.tsx`.

- Uses MUI `Accordion` (or a custom expand/collapse) — one per `ResourceCard`.
- Accordion summary: `<kind>/<name>` in monospace, optional namespace as secondary text,
  field-count badge (e.g. `3 fields`).
- Accordion details: iterates `card.fields` and renders `<FieldRenderer field={f} />` for
  each.
- First card expanded by default via `defaultExpanded`.
- Empty list (no evidence collected yet): renders a single informational note.

**Task 9.4.2** — Replace tab block in `DiagnosticRunDetail.tsx`

Remove the `TABS` constant, `activeTab` state, and the four `Tab*` function components.
Wire in `<ResourceCardList cards={buildResourceCards(evidence?.payload ?? {})} />`.
Keep all other existing UI (back button, run ID, analysis result section, collection errors
banner, redaction/truncation notices) unchanged.

---

### [9.5] Smoke test (~0.5h)

**Task 9.5.1** — Manual verification against a live cluster

- Open a DR that has only workload pods → all pod cards visible, root card visible.
- Open a DR that was a follow-up run (has `relatedResourceEvidence`) → related resource
  cards appear after PVC cards, `spec` fields rendered as key-value rows.
- Open a DR with no evidence (`evidenceRef` absent) → empty state shown, no crash.

---

## Files changed

| File | Change |
|---|---|
| `services/frontend-ui/src/api/index.ts` | Add `RelatedResourceEvidence`, extend `EvidencePayload` |
| `services/frontend-ui/src/pages/diagnostics/buildResourceCards.ts` | New — normalization function |
| `services/frontend-ui/src/pages/diagnostics/buildResourceCards.test.ts` | New — unit tests |
| `services/frontend-ui/src/pages/diagnostics/DiagnosticRunDetail.tsx` | Replace tabs with `ResourceCardList` + `FieldRenderer` |

No backend changes. No CRD changes. No Redux changes.

---

## Out of scope

- Sorting or filtering the resource list by kind or name.
- Diffing evidence between two diagnostic runs.
- Rendering CRD-specific icons per resource kind.
- Showing which `suggestedResources` from the LLM correspond to which collected resource.
