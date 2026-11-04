# KubeChan agent guide

This file is the operational source of truth for coding agents working in this repository. Read it before changing code. Use `README.md` for the product overview and `docs/README.md` to find deeper documentation.

## Product invariants

- KubeChan is advisory. Never mutate user workloads as part of diagnosis or remediation.
- Controllers may create and update KubeChan-owned CRDs and internal state.
- Evidence comes from Kubernetes state, events, and logs. The LLM does not create evidence or topology.
- `graphengine` builds the investigation graph deterministically from the exact evidence snapshot sent to the model.
- The model may return claims, citations, remediation, and a causal-path overlay. Every referenced source, node, edge, and candidate must resolve against that snapshot.
- Missing resources remain explicit reference nodes. Proposed topology must never look like observed Kubernetes topology.
- Existing DiagnosticRuns are immutable snapshots. Contract changes must preserve rendering of older runs or provide an explicit compatibility path.

## Repository map

| Area | Responsibility |
|---|---|
| `api/v1alpha1/` | Kubernetes CRD types and generated artifacts |
| `services/cluster-watcher/` | Detects anomalies and manages ProblemCase/Incident CRDs |
| `services/diagnostics-worker/collector/` | Evidence schema and atomic source catalog |
| `services/diagnostics-worker/graphengine/` | Deterministic graph construction and candidate ranking |
| `services/diagnostics-worker/controllers/` | DiagnosticRun orchestration and evidence collection |
| `services/backend-api/` | REST/WebSocket API, SQLite persistence, analysis dispatch |
| `services/llm-gateway/` | Prompt construction, providers, assessment and citation validation |
| `services/frontend-ui/src/store/` | Redux state, RTK Query APIs, WebSocket middleware |
| `services/frontend-ui/src/pages/` | Route-level operational UI |
| `helm/kubechan/` | Production chart and kind/Tilt development values |

## Sources of truth

Read in this order:

1. `AGENTS.md` — constraints and required verification.
2. `README.md` — current user-visible capabilities and local quick start.
3. `docs/codebase/ARCHITECTURE.md` — current service flow and ownership.
4. `docs/contracts/investigation-analysis.md` — evidence, graph, and assessment contract.
5. `docs/codebase/STRUCTURE.md` — module boundaries and entry points.
6. `docs/codebase/TESTING.md` — validation matrix.

Files under `docs/plans/` are historical design records. They are context, not current requirements. When a plan conflicts with code or the documents above, follow the current contract and implementation.

## Codex skills

- Use `$update-landing-page` for any task that audits, updates, or improves the GitHub Pages landing site or the `gh-pages` branch.

## Change rules

### Evidence and graph

- Add evidence fields in `collector/types.go`, populate them in the reconciler, and expose matching TypeScript types.
- Increment `Evidence.schemaVersion` for incompatible payload changes.
- Sources must be atomic and addressable. Prefer an exact JSON pointer or one log/event record over a whole object.
- IDs must be deterministic and stable within a snapshot.
- Graph edges describe observed relationships. Candidate relationships describe proposed corrections and must be visually distinct.
- Add deterministic unit tests for every new node, edge, missing-reference, or candidate rule.

### LLM gateway

- The canonical prompt builder is `services/llm-gateway/app/prompt.py`.
- Pydantic models in `app/models.py` define the model-facing response contract.
- Never accept arbitrary model citations or graph IDs. Validate against the request payload in `app/routes.py`.
- A model failure must not silently weaken citation integrity.

### Frontend

- Prefer RTK Query endpoints in `src/store/api/` for shared server state. A route-local paired fetch is acceptable when the data is consumed only by that route.
- Preserve the operational visual language: dense, calm, evidence-first, and suitable for enterprise incident response.
- Avoid generic AI dashboard styling, decorative gradients, excessive pills, glassmorphism, and card grids.
- Use CSS variables/tokens, visible focus states, semantic colors, and keyboard-accessible graph interactions.
- Broken topology is red; proposed topology is amber and dashed; healthy/observed topology stays neutral or green.

### Generated files

- After changing `api/v1alpha1/`, run `make generate` and commit generated DeepCopy and CRD changes.
- Do not hand-edit `zz_generated.deepcopy.go` or generated CRDs.

## Local development

The active Kubernetes context must be kind and must already contain ingress-nginx.

```bash
kubectl config use-context kind-<cluster-name>
make dev-up
```

Primary endpoints:

- Application: `http://kubechan.local`
- Direct frontend fallback: `http://localhost:8080`
- Tilt: `http://localhost:10350`

Use `tilt ci` or `make e2e` for a non-interactive build, deployment, and readiness check.

## Required verification

Run the smallest relevant set while iterating, then the full applicable set before handoff.

```bash
# Go
go test ./...

# LLM gateway
cd services/llm-gateway && uv run pytest -q

# Frontend typecheck and production bundle
cd services/frontend-ui && npm run build

# Chart
helm lint helm/kubechan --values helm/kubechan/values-dev.yaml

# Cross-service or runtime behavior
tilt ci
```

For an analysis-contract change, create a fresh DiagnosticRun after deployment. Old runs do not contain new evidence fields. Verify the API payload and the Assessment page, including selection of the affected node/edge and its exact source.

## Working-tree safety

- The worktree may contain user changes. Inspect `git status` before editing and stage only files belonging to the task.
- Do not discard, reset, reformat, or include unrelated changes.
- Use feature branches prefixed with `codex/` unless the user requests another convention.
- Do not push, tag, release, or modify remote state unless the user asks.

## Documentation maintenance

Update documentation in the same change when modifying:

- service ownership or data flow;
- a public endpoint or CRD;
- evidence, graph, or assessment contracts;
- local setup or required tooling;
- validation commands.

Keep `README.md` concise. Put implementation contracts in `docs/contracts/`, operational instructions in `docs/runbooks/`, current architecture in `docs/codebase/`, and historical proposals in `docs/plans/`.
