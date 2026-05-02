Project: Kubernetes AI Troubleshooting Assistant

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