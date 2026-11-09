# Investigation analysis contract

This document defines the cross-service contract from collected Kubernetes evidence to the Assessment UI.

## Ownership

| Stage | Owner | May create | Must validate |
|---|---|---|---|
| Collection | diagnostics-worker reconciler | Kubernetes observations | API errors, missing resources, redaction |
| Source catalog | `collector.BuildSourceCatalog` | Atomic source records and deterministic source IDs | Stable resource identity and addressable paths |
| Topology | `graphengine.BuildWithPolicy` | Nodes, observed and inferred edges, missing references, candidates | All graph entities are derived deterministically from the evidence snapshot |
| Assessment | llm-gateway/provider | Claims, remediation, causal-path and node-insight annotations | Output schema |
| Trust boundary | llm-gateway route | Canonical citations, causal path, and node insights | Every returned ID exists in the request payload |
| Presentation | Assessment page | Layout and selection state | Backwards compatibility and exact source lookup |

The LLM never creates graph topology and never supplies authoritative source content.

The opt-in [snapshot agent](snapshot-agent.md) reads this same immutable evidence
through bounded tools. Its citations and graph annotations must also resolve
against the journal of context actually delivered to the model. Single-shot
analysis and historical payloads retain their existing behavior.

The agent's live drawing is a selection of canonical snapshot IDs. The root and
requested resource nodes are visible during investigation. The model chooses
relationships and writes source-backed resource cards. An observed broken
relationship can reveal its missing endpoint after the exact source is delivered.
Working notes and the most recent tool exchange replace older raw model context.

## Evidence snapshot

`collector.Evidence` is stored as an immutable DiagnosticRun snapshot. Its `schemaVersion` describes the payload contract. A new run is required to observe collector or GraphEngine changes; previously completed runs are not rewritten.

Important sections:

- `rootResource`: workload entry point selected by correlation or the user.
- `problemCases`: detector observations associated with the incident.
- `workloadPodLogs`: pod state, labels, logs, events, and configuration dependencies.
- `pvcInfos`: PVC state and events.
- `relatedResourceEvidence`: explicitly selected and automatically discovered resources.
- `sources`: normalized citation-safe observations.
- `graph`: deterministic topology derived from this same payload.

## Atomic sources

Each source has a deterministic ID and identifies one observation:

- one Kubernetes field with an exact JSON pointer;
- one detector field or symptom for an active ProblemCase;
- one log line;
- one event;
- one collection error when applicable.

Discovered workload Pods include individual label sources under `/labels/` and
referenced claim names under `/pvcNames/`. These sources allow a Service selector
comparison and a Pod-to-PVC scheduling diagnosis from the same snapshot.

Example:

```json
{
  "id": "field:e06fed0d1b55ee0f",
  "type": "resource_field",
  "resource": {
    "apiGroup": "networking.k8s.io",
    "kind": "Ingress",
    "namespace": "kubechan-demo",
    "name": "nginx"
  },
  "path": "/rules/0/paths/0/backendService",
  "value": "nginx"
}
```

Secret contents are never sources. Only safe metadata such as existence may be recorded.

## Investigation graph

Graph schema v2 contains:

- `entryResourceId`: the Kubernetes resource at which the investigation started;
- `policy`: the traversal, size, candidate, and inference bounds used for this snapshot;
- `truncated`: whether policy bounds excluded graph entities;
- `nodes`: present resources and unresolved reference nodes;
- `edges`: deterministic observed or inferred relationships;
- `candidates`: deterministic proposed corrections.

Discovery starts from the root resource and can traverse Ingresses, Services, workloads, Pods, HPAs, EndpointSlices, ConfigMaps, Secrets, PVCs, and StorageClasses without requiring application annotations. Explicit resources attached by a user are retained as investigation context. Automatically discovered resources are included only within the recorded policy bounds.

### Node rules

- `resource` nodes represent resources present in the collected snapshot.
- `reference` nodes represent a target named by configuration but not present in the snapshot.
- Node IDs encode canonical identity: node type, API group, kind, namespace, and name.
- Node `sourceIds` resolve only to sources in the same snapshot.

### Observed edge rules

Observed edges describe current Kubernetes configuration or ownership:

- `manages`;
- `selects`;
- `routes_to`;
- `configured_by`;
- `uses_secret`;
- `mounts`;
- `scales`;
- `has_endpoints`;
- `uses_storage_class`.

`resolved` means both endpoints exist. `broken` means the configuration names an unresolved reference. These edges are facts, not model conclusions.

### Inferred edge rules

Inference is used only to bridge an observed gap, such as a Service with no exact Pod match. It is based on deterministic partial-selector scoring and produces an edge with `origin=inferred`, `score`, `strength`, reasons, and source IDs. Explicitly conflicting application identity labels exclude a candidate. An inferred edge is an investigation hint, never observed Kubernetes topology and never sufficient by itself to prove a root cause.

### Candidate rules

A candidate is a proposed replacement for a missing reference. It includes:

- `sourceNodeId`: resource whose configuration would change;
- `referenceNodeId`: unresolved target currently named by that configuration;
- `candidateNodeId`: present resource proposed as the replacement;
- `score`, `strength`, and evidence-backed reasons.

For a broken Ingress backend, the UI therefore renders:

```text
Ingress/nginx ── routes to (broken) ──> Service/nginx [missing]
       └────── should route to ──────> Service/nginx-svc [candidate]
```

There is no Kubernetes relationship between the missing and candidate Services. The candidate retains `referenceNodeId` only to explain what would be replaced.

Strong candidates use the label `should route to`; weaker candidates use `possible target`. Candidate edges are amber and dashed so they cannot be confused with observed topology.

## Assessment v2

The model returns:

- `summary` and bounded `confidence`;
- `claims` with verdicts and citations;
- `alternativeHypotheses`;
- `missingEvidence`;
- ordered `remediation`;
- optional `causalPath` annotations over the supplied graph;
- `nodeInsights`, containing one relevance classification and short hover comment per graph node.

The gateway accepts a citation only if its `sourceId` exists in `payload.sources`. It accepts a causal path only if the entry resource and every node, edge, and candidate ID exist in `payload.graph`. A node insight is retained only when its `nodeId`, `claimIds`, and `sourceIds` resolve against the same immutable request snapshot; duplicate or unknown node annotations are removed. Invalid topology annotations are removed.

In `snapshot-agent-v1`, citation IDs must additionally have delivered source values.
The audited `snapshot-excerpt-v1` quote policy takes display excerpts directly from
those records after reference validation; model-authored quote text is not used.
Excerpts are bounded exact prefixes and link to the complete immutable source.
`agentExecution.citationQuotes` identifies this policy. Other analysis modes and
historical results keep their existing quoting behavior. See the [agent contract](snapshot-agent.md).

For Assessment v2 responses, the validated assessment confidence is also the top-level analysis confidence. The API uses the validated assessment confidence when listing or returning historical results whose stored top-level confidence differs.

Each successful analysis result also records `tokenUsage` in its stored response payload: `inputTokens`, `outputTokens`, and `totalTokens`. LiteLLM provider usage is used when available. Missing counts are filled with a `tiktoken` estimate and marked with `inputSource` or `outputSource: "tokenizer_estimate"`; counts reported by the provider use `"provider"`. The estimate uses the model's known tiktoken encoding when available and `cl100k_base` otherwise. Estimates for non-OpenAI tokenizers are approximate. Historical results without `tokenUsage` remain valid and display no count.

Node relevance is `relevant`, `context`, or `not_relevant`. Persona-disabled runs use neutral engineering comments. Persona-enabled runs use a concise tsundere voice while preserving the same evidence constraints. Persona selection is part of the immutable analysis result; changing it requires a new analysis.
An omitted node `comment` is normalized to an empty string; the UI shows its existing
no-comment fallback. This display-only default does not relax node, claim or source
validation and does not generate a model annotation.

Graph schema v2 records the effective bounded-discovery policy and distinguishes `observed` edges from `inferred` edges. Observed edges come only from literal Kubernetes references or exact selectors. Inferred edges are deterministic, scored, source-backed discovery hints; they are amber and dashed and never become observed topology. The default snapshot is bounded to four observed hops, one inferred hop, 50 nodes, 150 edges, and five candidates per gap.

If a valid causal-path step identifies a deterministic graph relationship but the model mangles its citation ID, the gateway may recover citations only from source IDs already attached to that validated entity or relationship. It never manufactures a new source.

## Frontend behavior

The Assessment route is `/incidents/:incidentId/assessments/:runId`.

- Old runs without a graph show an explicit compatibility message.
- Selecting a node, edge, candidate, or claim opens its exact evidence.
- Hovering or focusing a node shows its validated model insight and relevance; older runs fall back to deterministic technical metadata.
- Node insights render in a viewport-clamped HTML overlay above the graph, with a relevance-specific KubeChan reaction image and technical metadata. Long content wraps inside the overlay and scrolls when it exceeds the browser height.
- Relationship labels remain hidden until the corresponding edge is hovered or keyboard-focused.
- The resource/relationship inspector is below the graph so the graph keeps the full workspace width.
- Broken topology, proposed topology, and healthy topology use different semantic styling.
- Graph selection is keyboard accessible.
- The evidence ledger displays only citations that survived gateway validation.

## Changing the contract

When changing this contract:

1. Update Go evidence/graph types and collection logic.
2. Add deterministic collector or GraphEngine tests.
3. Update Pydantic request/response validation where model behavior changes.
4. Update TypeScript API types and backwards-compatible rendering.
5. Update this document.
6. Run Go tests, gateway tests, frontend build, and Helm lint.
7. Deploy with Tilt and create a fresh DiagnosticRun for E2E validation.
