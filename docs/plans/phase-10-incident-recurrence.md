# Phase 10 — Recurring Incidents and Occurrence-Scoped Diagnostics

## Goal

Represent repeated failures of the same workload as one long-lived Incident instead of creating
an unrelated Incident after every recovery or for every detector. Keep detector findings as
ProblemCases inside the Incident and bind every DiagnosticRun to the ProblemCase that triggered
it while analyzing all active symptoms of the current occurrence.

The user-facing result is one Incident such as:

```text
api-gateway unavailable
CrashLoopBackOff · DeploymentUnavailable · ServiceNoEndpoints
Repeated 7 times · last occurrence 4 minutes ago
```

This phase deliberately improves the existing five detectors without adding new detectors.

## Decisions

### Incident identity

An automatically detected Incident is identified by the following fingerprint inputs:

```text
affected cluster + resource namespace + root kind + root name
```

For the current single-cluster product, the cluster component is implicit and is not persisted.
The effective v1 fingerprint is therefore:

```text
namespace + root kind + root name
```

The fingerprint is stored as a deterministic hash in the label
`kubechan.io/incident-fingerprint`. Human-readable root-kind and root-name labels remain available
for inspection. Detector is intentionally excluded: different detector findings can describe the
cause and consequences of the same workload outage. Hashing avoids Kubernetes label length and
character restrictions.

Manual Incidents are not deduplicated or reopened by this phase.

### Recurrence window

When a new ProblemCase is correlated:

1. Reuse an open auto Incident with the same fingerprint.
2. Otherwise, reopen the most recently resolved auto Incident with the same fingerprint when its
   `resolvedAt` is no more than 24 hours old.
3. Otherwise, create a new Incident.

The 24-hour value is a cluster-watcher setting with a default of `24h`, so a later chart setting
does not require changing the CRD.

### Meaning of an occurrence

A new occurrence starts when an Incident is created or transitions from `resolved` back to
`open`. Additional ProblemCases correlated while that Incident is already open belong to the same
occurrence and must not increment the occurrence counter.

ProblemCase remains the concrete detected signal and the anchor for occurrence-scoped evidence.
No new Occurrence CRD is introduced. An occurrence can contain more than one ProblemCase when
several Pods or detectors report symptoms of the same root workload during the same open period.

For example, these belong to one occurrence:

```text
Incident: Deployment/api-gateway
├── CrashLoopBackOff          (probable cause)
├── DeploymentUnavailable    (workload impact)
└── ServiceNoEndpoints       (service impact)
```

The correlation layer does not permanently classify cause versus consequence. It groups
unambiguous resource relationships; diagnostics explains the likely causal order.

### Resolving Service to a root workload

Service has no owner reference to a Deployment or StatefulSet. For a Service ProblemCase, the
correlator resolves candidate workload roots through its selector:

1. List Pods matching `Service.spec.selector` and walk each Pod owner chain to its root workload.
2. Also compare the selector with workload Pod-template labels. This supports the important case
   where no Pod currently exists or is Ready.
3. Deduplicate the candidate roots.
4. If exactly one root is found, use that workload as the Incident root.
5. If multiple roots are found (for example canary or blue/green), keep Service as the Incident
   root and attach the candidate workloads as related resources for diagnostics.
6. If no root is found, keep Service as the Incident root. Diagnostics can then report a selector
   mismatch or a missing workload without guessing ownership.

Empty selectors, `ExternalName` Services, and ambiguous matches must never be assigned to an
arbitrary workload. Headless Service behavior remains governed by the detector and is not expanded
in this phase.

### Diagnostic scope

DiagnosticRun keeps both references:

```yaml
spec:
  incidentRef: incident-deployment-api-gateway-abc12
  problemCaseRef: crashloopbackoff-pod-api-gateway-xyz89
```

`incidentRef` supplies the long-lived history and root workload. `problemCaseRef` records which
active signal triggered the run. A run with `problemCaseRef` collects that trigger plus every
currently active ProblemCase in the same Incident, but must not collect old resolved ProblemCases.

Existing DiagnosticRuns that only have `incidentRef` remain supported and retain their current
incident-wide collection behavior. Manual Incident runs also remain incident-scoped because they
do not have a detector-created ProblemCase.

## CRD changes

### Incident status

Add:

```go
// OccurrenceCount is incremented when the incident is first opened or reopened after resolution.
OccurrenceCount int `json:"occurrenceCount,omitempty"`

// LastOccurrenceAt is the time the current/latest occurrence began.
LastOccurrenceAt *metav1.Time `json:"lastOccurrenceAt,omitempty"`
```

`OpenedAt` remains the time this Incident was originally created and is not overwritten on reopen.
`ResolvedAt` is cleared when the Incident is reopened and set again when all current ProblemCases
are resolved.

Do not add an unbounded occurrence history array to CRD status in this phase. The member
ProblemCases and DiagnosticRuns are the detailed history.

### DiagnosticRun spec

No schema addition is required: `incidentRef` and `problemCaseRef` already exist. This phase makes
their combined semantics explicit and ensures detector-driven runs populate both fields.

## Reconciliation behavior

### Correlating a new ProblemCase

The correlation reconciler must:

1. Resolve the ProblemCase owner chain to the root workload.
2. Build a fingerprint from the root. For Service findings, first apply the safe selector-based
   root resolution rules above.
3. Look up matching auto Incidents by the fingerprint label.
4. Prefer the open Incident.
5. Otherwise reopen the newest resolved Incident inside the recurrence window.
6. Otherwise create and initialize a new Incident.
7. Add the ProblemCase to the selected Incident and label it with the Incident name.

Reopening is one status patch:

```text
state             = open
resolvedAt        = nil
occurrenceCount   = occurrenceCount + 1
lastOccurrenceAt  = now
```

Creation initializes `occurrenceCount = 1`, `openedAt = now`, and `lastOccurrenceAt = now`.

### Resolving an Incident

Resolution continues to occur when all member ProblemCases are resolved. Historical
ProblemCases remain listed on the Incident, but only non-resolved members contribute to
`activeProblemCases`.

When a reopened Incident receives new ProblemCases, old resolved members must not influence the
current lifecycle.

### Concurrency

Two ProblemCases for the same fingerprint may reconcile concurrently. Incident selection and
creation must be idempotent. Tests must cover concurrent-looking create conflicts so that the
system does not leave two open Incidents for one fingerprint.

## Diagnostic behavior

### Starting analysis from an auto Incident

`POST /api/v1/incidents/{id}/analyze` selects a triggering ProblemCase as follows:

1. Consider non-resolved ProblemCases belonging to the Incident.
2. Prefer the one with the latest `status.lastSeen`.
3. Store its name in `DiagnosticRun.spec.problemCaseRef`.
4. Always store the Incident name in `DiagnosticRun.spec.incidentRef`.

If an auto Incident has no active ProblemCase, analysis remains allowed as a historical,
incident-wide run with no `problemCaseRef`.

### Evidence collection

When `problemCaseRef` is present, diagnostics-worker collects:

- the referenced triggering ProblemCase;
- all other non-resolved ProblemCases currently belonging to the Incident;
- affected resources for those active ProblemCases;
- the Incident root workload;
- current root workload Pods, events, logs, dependencies, and related resources;
- the manual/user context already attached to the Incident, if any.

It does not append resolved historical ProblemCases from `Incident.spec.problemCases`.

When `problemCaseRef` is absent, the existing incident-wide behavior is retained for backward
compatibility and manual incidents.

## API and UI changes

### API types

Expose the new Incident status fields and `DiagnosticRun.spec.problemCaseRef` in frontend types.
Diagnostic run summaries must include `problemCaseRef` so the UI can associate a run with the
corresponding manifestation.

### Incident list

For recurring auto Incidents show:

```text
<root name>
<active detector summary>
Repeated <occurrenceCount> times
Last occurrence <relative time>
```

For `occurrenceCount == 1`, do not show a redundant “Repeated 1 time” label.

### Incident detail/history

The first UI version shows:

- occurrence count;
- original `openedAt`;
- `lastOccurrenceAt`;
- current active ProblemCases;
- resolved ProblemCases as previous manifestations;
- DiagnosticRuns grouped by `problemCaseRef`;
- incident-wide/manual runs in a separate “General diagnostics” group.

This phase does not add a graphical timeline.

## Ordered implementation tasks

1. Extend `IncidentStatus`, regenerate DeepCopy code, and regenerate the Incident Helm CRD.
2. Add root-only fingerprint construction and labels to cluster-watcher.
3. Add safe Service selector → Pod/workload root resolution.
4. Implement the 24-hour reopen rule for a matching root fingerprint.
5. Initialize and update occurrence status fields during create/reopen/resolve transitions.
6. Update analysis creation to select and store the latest active `problemCaseRef`.
7. Make diagnostics-worker collect current active ProblemCases while preserving legacy
   incident-wide runs.
8. Include `problemCaseRef` in backend diagnostic summaries and frontend API types.
9. Update Incident list and detail/history presentation.
10. Add unit, reconciliation, handler, collector, and frontend tests.
11. Run Go tests, frontend tests/build, Helm lint/template validation, and a kind smoke test.

## Required tests

### cluster-watcher

- Same root and same detector while open joins the existing Incident.
- Same root with a different detector joins the existing Incident.
- A Service selecting Pods from exactly one workload uses that workload root.
- A Service with no current Pods but exactly one matching workload template uses that root.
- A Service selecting multiple workload roots remains a Service-rooted Incident.
- A Service with no matching Pods or workload templates remains a Service-rooted Incident.
- Same fingerprint inside 24 hours reopens the resolved Incident.
- Same fingerprint outside 24 hours creates a new Incident.
- Reopen increments `occurrenceCount` exactly once and clears `resolvedAt`.
- Additional ProblemCases while open do not increment `occurrenceCount`.
- Manual Incidents are never selected for automatic recurrence.
- Existing Incidents without a fingerprint remain readable and are not mutated accidentally.
- Create conflicts do not produce duplicate open Incidents.

### backend-api

- Analyze on an open auto Incident stores the latest active `problemCaseRef`.
- Analyze on a resolved Incident without an active ProblemCase remains incident-scoped.
- Manual Incident creation remains incident-scoped.
- Diagnostic summaries return `problemCaseRef`.

### diagnostics-worker

- A run with `problemCaseRef` collects the trigger, all active sibling ProblemCases, and current
  root context.
- A run with an unknown ProblemCase records a collection error instead of silently analyzing all
  historical ProblemCases.
- Resolved historical ProblemCases are excluded from occurrence-scoped evidence.
- A legacy run without `problemCaseRef` keeps the existing behavior.

### frontend

- Recurrence count is hidden for a first occurrence and shown for repeated incidents.
- Last occurrence uses `lastOccurrenceAt`.
- Runs are grouped under the matching ProblemCase.
- Runs without `problemCaseRef` appear under general diagnostics.

## Migration and compatibility

- CRD fields are additive; existing objects remain valid.
- Existing Incidents have no fingerprint and `occurrenceCount == 0`. They are displayed as one
  occurrence but are not retroactively merged.
- New correlation behavior applies only to ProblemCases observed after deployment.
- Existing DiagnosticRuns without `problemCaseRef` remain accessible and collect evidence using
  the legacy path.
- No database migration is required unless backend summary persistence lacks a place for
  `problemCaseRef`; prefer deriving it from the DiagnosticRun CRD first.

## Non-goals

- New detectors.
- Flapping detection or severity escalation.
- Comparing evidence signatures between occurrences.
- Reusing an old LLM answer automatically.
- Automatic remediation.
- Multi-cluster identity.
- A new Occurrence CRD.
- Retroactively merging existing Incident objects.
- Guessing a workload when a Service selector resolves to zero or multiple candidates.

## Acceptance criteria

1. Repeating `CrashLoopBackOff` for the same root workload within 24 hours reopens the same
   Incident and increments its occurrence count.
2. Different detector findings for the same unambiguous root workload appear in that Incident as
   separate active ProblemCases.
3. A diagnostic run started for the active recurrence stores both Incident and ProblemCase refs.
4. Its evidence contains the triggering ProblemCase, active sibling symptoms, and fresh workload
   context, but not old resolved ProblemCases.
5. Existing manual and historical diagnostics continue to work.
6. The UI clearly shows recurrence count, last occurrence, manifestation history, and which
   diagnostic belongs to which ProblemCase.
7. A Service is merged into a workload Incident only when selector-based ownership resolves to
   exactly one workload root.
