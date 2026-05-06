Project: Kubernetes AI Troubleshooting Assistant

Goal:
Build a Kubernetes-native read-only AI assistant that detects suspicious cluster states, helps diagnose problems, generates evidence-based RCA, and presents results through a web UI with an optional tsundere persona layer.

Core product idea:
The system continuously observes a Kubernetes cluster, identifies potential problems, collects diagnostic context, asks an LLM to analyze the evidence, and shows the user what is wrong, why it likely happened, and what to do next.

The assistant must never modify cluster state. It is advisory only.

Non-goals:
- No automatic remediation.
- No write actions against user workloads.
- No LLM direct access to Kubernetes API.
- No secret value exposure.
- Persona must not affect technical correctness.

---

Implementation status (as of May 2026):
MVP is fully implemented and running. All services are deployed and integrated.

---

Primary user flows:

Auto flow:
1. cluster-watcher detects an anomaly (CrashLoopBackOff, ServiceNoEndpoints, etc.).
2. Creates ProblemCase CRD with detector, severity, and symptoms.
3. CorrelationReconciler groups it into an Incident CRD (reuses open Incident for same workload root).
4. User opens UI, sees open Incident, clicks "Ask KubeChan to help".
5. backend-api creates DiagnosticRun CRD.
6. diagnostics-worker collects evidence (logs, events, Ingresses, PVCs, ConfigMaps).
7. backend-api sends evidence + user message to llm-gateway.
8. LLM returns 5-section analysis + confidence + optional needsMoreInfo + suggestedResources.
9. Analysis broadcast via WebSocket to frontend.
10. KubeChan sidebar renders analysis in speech bubble.

Manual flow:
1. User clicks "+ Report an issue".
2. ManualIncidentModal: picks root resource, writes description, optionally tags related resources.
3. backend-api creates Incident CRD (source: manual), DiagnosticRun, and analysis_request.
4. Same evidence collection and analysis pipeline runs.
5. User can manually resolve the incident at any time.

Needs-more-info flow:
1. LLM returns needsMoreInfo: true (confidence < 0.65) + suggestedResources list.
2. Frontend shows needs-more-info banner with suggestion chips and reasons.
3. User opens AugmentIncidentModal, selects suggested resources.
4. POST /incidents/{id}/augment merges new resources into Incident CRD, creates new DiagnosticRun.
5. Full analysis pipeline re-runs with augmented evidence.

---

Architecture:
- frontend-ui (React 19 + Vite + MUI)
- backend-api (Go + chi + SQLite)
- cluster-watcher (Go + controller-runtime)
- diagnostics-worker (Go + controller-runtime)
- llm-gateway (Python + FastAPI + boto3/Bedrock)
- CRDs: Incident, ProblemCase, DiagnosticRun

Services:

1. frontend-ui
Purpose:
User-facing web interface.

Implemented:
- Incident list with open/resolved sections, live WebSocket updates.
- Manual incident badge, source label.
- Expandable incident details panel:
  - Root resource pill (kind/name/namespace)
  - User description (manual)
  - Related resources chips (manual)
  - Problem cases chips (auto)
  - KubeChan suggested resources (when needsMoreInfo)
- "Ask KubeChan to help" / "Ask KubeChan again" analyze buttons.
- Needs-more-info banner with suggestion chips + reason tooltips.
- AugmentIncidentModal: select additional resources to re-analyze.
- Resolve button on open manual incidents (inline confirm flow).
- Diagnostics page: list/delete DiagnosticRuns, expand analysis results.
- KubeChan sidebar with pose transitions: idle → thinking → speaking → chatter.
- Speaking pose: 5-section analysis, confidence badge, thumbs-up/down rating.
- Chatter pose: persona reaction lines for events (new incident, resolved, poke, silence, rating).
- Mood system: mood level (0–2) affects idle and reaction lines.
- Poke escalation: 3 pokes = annoyed, 5 pokes = rage.
- ManualIncidentModal: namespace picker, root resource kind selector, related resource kind toggles with name autocomplete. Supports Ingress as a resource kind.
- Settings page.

Stack:
TypeScript + React 19 + Vite 6 + MUI v9 + nginx.

2. backend-api
Purpose:
Central API and orchestration layer.

Implemented:
- chi router + SQLite (modernc/sqlite, embedded migrations).
- 5 DB migrations: analysis_requests, evidence, analysis_results (with prompt column), diagnosticruns view, needs_more_info + suggested_resources columns.
- controller-runtime informer cache for K8s CRDs (read-only).
- REST API:
  - GET/POST incidents, manual incident creation, analyze, augment, resolve.
  - GET/DELETE diagnosticruns (list + bulk delete).
  - GET analysisresults + rate endpoint.
  - GET evidence.
  - GET/POST settings.
  - GET/POST kubechan/state, kubechan/poke.
  - GET namespaces + resources by kind.
- WebSocket hub: broadcasts Incident, ProblemCase, DiagnosticRun, Analysis.Completed, KubeChanState.Updated events.
- Async LLM dispatch goroutine after evidence received.
- MoodSyncer: maintains KubeChanState singleton CRD based on open incident count.
- Augment endpoint: merges new relatedResources into Incident CRD (deduplicated), creates new DiagnosticRun + analysis_request.
- Resolve endpoint: patches Incident.status.state = resolved via status subresource.
- Startup recovery: re-dispatches pending analysis requests after restart.
- Background pruner for stale records.

Stack:
Go 1.25 + chi + controller-runtime + modernc/sqlite.

3. cluster-watcher
Purpose:
Read-only Kubernetes watcher/controller.

Implemented:
- Reconcilers for: Pod, Deployment, Service, Node, K8s Events.
- Detectors: CrashLoopBackOff, ImagePullBackOff, PendingTooLong, DeploymentUnavailable, ServiceNoEndpoints.
- Debounce window per resource (default 30s, 5s in dev) to suppress transient states.
- CorrelationReconciler: groups ProblemCases into Incidents by walking owner-reference chain to workload root.
- maybeResolveIncident: resolves Incident when all member ProblemCases are resolved.
- ProblemCaseReconciler: manages ProblemCase lifecycle (open → investigating → resolved).

Stack:
Go + controller-runtime.

4. diagnostics-worker
Purpose:
Collect structured evidence for a DiagnosticRun.

Implemented:
- Reconciles DiagnosticRun CRDs.
- Evidence collected per workload:
  - Pod status, current logs (last N lines), previous container logs.
  - K8s events on pod and root resource.
  - ConfigMap contents + mount paths / env var injection.
  - Secret existence check (values never collected).
  - PVC phase and events.
  - All Ingresses in the workload namespace — auto-discovered regardless of user tagging. Ingress spec includes rules, paths, backend service + port, and annotations.
- Deduplication of related resources using kind/ns/name map.
- Evidence structured and POSTed to backend-api /internal/evidence.

Stack:
Go + controller-runtime.

5. llm-gateway
Purpose:
Stable interface between backend-api and AWS Bedrock.

Implemented:
- FastAPI with single POST /analyze endpoint.
- Builds detailed prompt including: root resource spec, related resource specs (all evidence types), user message (verbatim), userMessage-based framing.
- Calls AWS Bedrock via boto3 Converse API.
- Model: qwen.qwen3-32b-v1:0 (also supports qwen3-235b).
- Temperature: 0.3, max tokens: 4096.
- Structured JSON response parsing with Pydantic v2.
- Response fields: openingRant, likelyRootCause, evidenceChain, recommendation, closingInsult, confidence, needsMoreInfo, suggestedResources (list of {kind, reason}), prompt.
- Prompt stored in response for backend-api to persist for debugging.
- needsMoreInfo set to true when confidence < 0.65.

Stack:
Python 3.12 + FastAPI + boto3 + Pydantic v2.

---

CRDs:

1. Incident
Groups related ProblemCases sharing a common workload root. Also created directly for manual incidents.

Fields:
- rootResource (kind, name, namespace)
- problemCases (list of ProblemCase names)
- source: auto | manual
- userMessage (manual incidents only)
- relatedResources (list of ResourceRef, manual incidents)
- status.state: open | resolved
- status.openedAt
- status.resolvedAt
- status.activeProblemCases (count)

2. ProblemCase
Represents a detected cluster anomaly.

Fields:
- affectedResource (kind, name, namespace)
- detector
- severity: critical | high | medium | low
- symptoms (list of strings)
- relatedResources
- latestDiagnosticRunRef
- status.state: open | investigating | resolved
- status.firstSeen
- status.lastSeen
- status.resolvedAt

3. DiagnosticRun
Represents one evidence collection run.

Fields:
- incidentRef (name of parent Incident)
- requestedAt
- status.state: pending | running | completed | failed
- status.evidenceRef
- status.collectionErrors

---

Security:
- Product is read-only for user workloads.
- No automatic remediation.
- LLM has no direct Kubernetes API access.
- Secret values are never collected or sent to LLM.
- ConfigMap values for known-sensitive keys are redacted.
- Log lines are collected as-is; redaction of secrets in logs is a post-MVP concern.
- RBAC is least-privilege per service.

---

RBAC model (implemented):
- cluster-watcher: get/list/watch Pods, Deployments, ReplicaSets, StatefulSets, DaemonSets, Services, Nodes, Events; create/update/patch ProblemCase, Incident CRDs.
- diagnostics-worker: get/list/watch Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Services, Endpoints, Ingresses, ConfigMaps, Secrets (metadata only), PVCs, Events; get pod logs; create/update/patch DiagnosticRun CRDs.
- backend-api: get/list/watch/create/update/patch Incident, ProblemCase, DiagnosticRun, KubeChanState CRDs; get/list Ingresses.
- llm-gateway: no Kubernetes RBAC.

---

Persona:
- Always-on tsundere character named KubeChan.
- Pose: idle / thinking / speaking / chatter.
- Chatter lines organized by event type with mood-level overrides (0=calm, 1=irritated, 2=rage).
- Events with reactions: idle, nav-incidents, nav-diagnostics, open-run, delete-run, no-incidents, many-incidents, poke, poke-annoyed, poke-rage, new-incident, incident-resolved, silence-hint, silence-paranoid, rating-up, rating-up-flustered, rating-down-high-conf, rating-down-low-conf, dismissed-analysis.
- Silence awareness: sends silence-hint after 5 min idle, silence-paranoid after 10 min.
- Poke escalation resets after 8s of no poking.
- Incident resolve: analysis bubble is dismissed immediately when user confirms resolve; KubeChan reacts with incident-resolved line.
- Mood syncer updates mood level from open incident count; persisted in KubeChanState CRD.

---

Post-MVP (not yet implemented):
- Bundled in-cluster model runtime (Gemma).
- Custom ProblemRule CRD.
- DiagnosticProfile CRD.
- Notification channels (Slack, Discord, Telegram, webhook).
- Postgres storage.
- Multi-cluster support.
- Multi-user auth.
- Historical incident analytics.
- Log redaction for common sensitive patterns.
- IngressBackendInvalid and NodeNotReady detectors.


Goal:
Build a Kubernetes-native read-only AI assistant that detects suspicious cluster states, helps diagnose problems, generates evidence-based RCA and runbooks, and presents results through a web UI with an optional tsundere persona layer.

Core product idea:
The system continuously observes a Kubernetes cluster, identifies potential problems, collects diagnostic context, asks an LLM to analyze the evidence, and shows the user what is wrong, why it likely happened, and what to do next.

The assistant must never modify the cluster. It is advisory only.

Non-goals:
- No automatic remediation in the first version.
- No write actions against user workloads.
- No LLM direct access to Kubernetes API.
- No secret value exposure.
- Persona must not affect technical correctness.

Primary user flow:
1. User installs the product via Helm chart.
2. Product deploys into the cluster with read-only permissions.
3. User opens the UI.
4. UI shows cluster health and current detected problems.
5. User selects a problem.
6. System gathers diagnostic evidence if needed.
7. LLM generates:
   - likely root cause
   - evidence
   - confidence
   - recommended runbook
   - useful kubectl commands
8. UI shows the strict technical result.
9. If persona mode is enabled, assistant speech bubble displays a stylized message derived from the technical result.
10. User can disable persona mode.

Architecture:
- frontend-ui
- backend-api
- cluster-watcher
- diagnostics-worker
- llm-gateway
- optional model-runtime
- optional notification-service
- CRDs for state storage

Services:

1. frontend-ui
Purpose:
User-facing web interface.

Responsibilities:
- Display cluster health summary.
- Display Problem Inbox.
- Display ProblemCase details.
- Display Evidence, RCA, Confidence, Runbook, Raw Data.
- Display optional assistant/persona speech bubble.
- Allow persona mode on/off.
- Allow manual re-analysis.
- Allow quiet mode for persona idle chatter.
- Show notifications for new or updated problems.

Suggested stack:
TypeScript + React.

2. backend-api
Purpose:
Central API and orchestration layer.

Responsibilities:
- Serve UI API.
- Read/write product CRDs.
- Trigger DiagnosticRun.
- Trigger LLM analysis.
- Store user settings.
- Store persona settings/state.
- Expose health/status endpoints.
- Enforce product-level permissions.
- Coordinate problem lifecycle.

Suggested stack:
Go.

3. cluster-watcher
Purpose:
Read-only Kubernetes watcher/controller.

Responsibilities:
- Watch Kubernetes resources:
  - Pods
  - Deployments
  - ReplicaSets
  - StatefulSets
  - DaemonSets
  - Services
  - EndpointSlices
  - Ingresses
  - Nodes
  - Events
- Run built-in detectors.
- Create/update ProblemCase CRDs.
- Mark ProblemCase as resolved when symptoms disappear.
- Debounce transient states.
- Deduplicate related symptoms.
- Avoid calling LLM directly.

Initial built-in detectors:
- CrashLoopBackOff
- ImagePullBackOff / ErrImagePull
- CreateContainerConfigError
- PendingTooLong
- DeploymentUnavailable
- ServiceNoEndpoints
- IngressBackendInvalid
- NodeNotReady
- WarningEvents

Suggested stack:
Go.

4. diagnostics-worker
Purpose:
Collect structured evidence for a ProblemCase.

Responsibilities:
- Receive or watch DiagnosticRun requests.
- Collect only read-only diagnostic data.
- Gather:
  - pod status
  - pod events
  - current logs
  - previous logs
  - owner manifest
  - deployment/statefulset/daemonset status
  - service selector
  - endpoints/endpointslices
  - ingress backend info
  - related configmap metadata
  - related secret metadata only, not values
  - node conditions
- Normalize data into structured diagnostic context.
- Redact sensitive values.
- Limit log size.
- Store collected evidence in DiagnosticRun or referenced storage.
- Never perform write/remediation actions.

Suggested stack:
Go.

5. llm-gateway
Purpose:
Stable interface between product and LLM/model runtime.

Responsibilities:
- Receive structured diagnostic context.
- Build prompts for analysis.
- Call configured LLM endpoint.
- Validate structured output.
- Produce raw technical analysis.
- Produce persona-styled message if enabled.
- Run consistency checks between raw and styled output.
- Return AnalysisResult.
- Never access Kubernetes API directly.

Suggested stack:
Python.

6. model-runtime
Purpose:
Optional in-cluster LLM runtime.

Responsibilities:
- Serve model inference endpoint.
- Run Gemma or other configured model.
- Support GPU configuration where available.
- Be optional in Helm chart.

Default mode:
External LLM endpoint.

Optional mode:
Bundled in-cluster model runtime.

7. notification-service
Purpose:
Optional notification delivery.

Responsibilities:
- Notify about new or changed ProblemCases.
- Support rate limiting and deduplication.
- Support channels later:
  - Slack
  - Discord
  - Telegram
  - Email
  - Webhook

MVP:
Can be part of backend-api.

CRDs:

1. ProblemCase
Represents a detected cluster problem.

Fields:
- id
- affectedResource
- namespace
- kind
- name
- detector
- severity
- status: open | investigating | resolved
- symptoms
- evidenceRefs
- firstSeen
- lastSeen
- resolvedAt
- relatedResources
- latestDiagnosticRunRef
- latestAnalysisResultRef

2. DiagnosticRun
Represents one evidence collection run.

Fields:
- id
- problemCaseRef
- status: pending | running | completed | failed
- collectedAt
- collectorVersion
- evidence
- collectionErrors
- redactionSummary
- logTruncationInfo

3. AnalysisResult
Represents one LLM analysis.

Fields:
- id
- problemCaseRef
- diagnosticRunRef
- model
- modelRuntime
- createdAt
- rawAnalysis
- styledMessage
- likelyRootCause
- confidence
- evidenceMapping
- recommendedRunbook
- suggestedKubectlCommands
- safetyNotes
- consistencyCheckStatus

Security requirements:
- Product is read-only for user workloads.
- No automatic remediation.
- LLM has no direct Kubernetes API access.
- Secrets values must never be sent to LLM.
- Secret metadata may be used:
  - exists/missing
  - referenced/not referenced
  - key names only if necessary
- Logs must be redacted for common sensitive patterns.
- RBAC must be least privilege.
- Persona output must not introduce new technical facts.

RBAC model:
- cluster-watcher:
  - read watched resources
  - create/update ProblemCase
- diagnostics-worker:
  - read diagnostic resources and pod logs
  - create/update DiagnosticRun
- backend-api:
  - read/write product CRDs
  - no direct workload modification
- llm-gateway:
  - no Kubernetes RBAC

UI concept:
Primary UI is a Problem Inbox, not a metrics dashboard.

Main screens:
1. Overview
- cluster health status
- count of open problems
- most severe problems
- assistant status bubble

2. Problems
- list of ProblemCases
- filters by severity/status/namespace
- affected resource
- symptom
- firstSeen/lastSeen
- analysis status

3. Problem Detail
- summary
- likely root cause
- evidence
- confidence
- runbook
- suggested kubectl commands
- raw diagnostic data
- analysis history
- persona message if enabled

4. Settings
- persona on/off
- idle chatter on/off
- LLM endpoint config
- bundled model config
- notification config later

Persona requirements:
- Persona is optional and can be disabled.
- Persona is a presentation layer only.
- Persona must not change technical meaning.
- Raw technical analysis is canonical.
- Styled persona message is derived from raw analysis.
- Persona idle chatter may appear in the speech bubble when:
  - persona mode is enabled
  - no critical problem is being shown
  - user is idle or on overview
  - rate limit allows it
- Idle chatter must not:
  - imply false cluster problems
  - look like alerts
  - interrupt workflow
  - trigger notifications
  - require action

LLM behavior:
Input:
- detector result
- structured evidence
- relevant logs/events/manifests
- resource relationships

Output:
- likely root cause
- confidence
- evidence references
- recommended runbook
- suggested kubectl commands
- safety notes
- optional persona-styled message

LLM must:
- distinguish evidence from hypothesis
- cite evidence from collected context
- avoid unsupported claims
- ask for more evidence only when needed
- not suggest destructive actions as default
- not claim actions were performed

MVP scope:
Implement:
- Helm chart
- CRDs:
  - ProblemCase
  - DiagnosticRun
  - AnalysisResult
- frontend-ui
- backend-api
- cluster-watcher
- diagnostics-worker
- llm-gateway
- external LLM endpoint support
- detectors:
  - CrashLoopBackOff
  - ImagePullBackOff / ErrImagePull
  - PendingTooLong
  - DeploymentUnavailable
  - ServiceNoEndpoints
- UI:
  - Overview
  - Problem Inbox
  - Problem Detail
  - Settings
- Persona:
  - enable/disable
  - styled speech bubble
  - idle chatter with rate limit
- No bundled model runtime in MVP unless explicitly prioritized.

Post-MVP:
- Bundled Gemma runtime
- Custom ProblemRule CRD
- DiagnosticProfile CRD
- Notification channels
- Postgres storage
- Multi-cluster support
- Multi-user auth
- Team/org settings
- Historical incident analytics
- Fine-tuning pipeline
- More detectors
- RAG over internal runbooks

Important architectural constraints:
- Controller/watcher detects suspicious states.
- Diagnostics worker collects facts.
- LLM explains facts and creates runbook.
- Backend orchestrates.
- UI presents.
- Persona decorates.
- Model runtime is replaceable.
- Kubernetes write actions are out of scope.