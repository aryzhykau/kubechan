# Snapshot agent v1

`snapshot-agent-v1` is an opt-in PydanticAI analysis mode for Kubernetes and data
pipeline investigations. Evidence v3, GraphEngine, graph v2/v3 and Assessment v2
remain unchanged. The canonical full graph is built before analysis. The agent
receives a compact map and reads exact evidence through bounded snapshot tools.

## Enablement and compatibility

Set Helm `backendApi.analysisMode=snapshot-agent-v1` (backend environment
`ANALYSIS_MODE=snapshot-agent-v1`). The production default is `single-shot`;
`values-dev.yaml` enables the agent for Tilt/kind development. This first version
supports Bedrock through PydanticAI's native Converse adapter. Credentials and
client retry settings are local to each request; no process environment is changed.
The existing LiteLLM SDK/Copilot single-shot path remains available. Other agent
providers fail explicitly. The gateway selects `data-pipeline-v1` for graphs with
DataSources or external entities and `kubernetes-v1` otherwise. New results record
the selection as `agentExecution.investigator`.

Provider/model support for ordinary completion does not establish tool-calling
compatibility. The configured Bedrock model and region require a real tool-call
round-trip check before operational use. Offline tests use PydanticAI FunctionModel
and do not invoke an external model or read a cluster.

## Ownership and storage

Worker collects the bounded evidence and GraphEngine constructs topology from
that evidence. Backend stores the original JSON in SQLite `evidence.payload` on
its PVC; DiagnosticRun stores `evidenceRef`. Gateway indexes a private copy in
memory. Tools read only that copy. Resource objects, events and log lines are
never fetched live by the agent. A new observation requires a new DiagnosticRun.

Every attempt pins evidence ID, a SHA-256 of canonical JSON, and its request/run
IDs. Snapshot ID catalogs reject duplicates and dangling edge endpoints. Secret
fields other than `/exists` cannot be returned even if supplied in a malformed
source catalog. There are no shell, filesystem, network, or built-in provider tools.

## Initial context and tools

The initial model message contains the report as data, the root ID, up to eight
short detector sources, collection metadata and a complete compact projection of
the captured graph. Nodes carry canonical IDs, names, kinds, scopes and states.
Edges and candidates carry IDs, endpoints and relationship metadata. Resource
attributes, source values, events and logs remain behind read tools. The startup
map shares the 128,000-byte snapshot context budget; oversized maps fail
explicitly, and collector truncation is surfaced separately.

| Tool | Model action |
|---|---|
| `inspect(resource_id)` | Read one resource's fields, events, log excerpts, collection errors and nearby relationships |
| `read_more(cursor)` | Continue a particular section by its returned cursor or reread an exact delivered source by ID |
| `record_finding(statement, source_ids, finding_id?, verdict?)` | Save one finding, revise it by ID, or retract it by passing only the ID |
| `verify(summary?, gaps?, remediation?)` | Check citations and the proposed conclusion. It may return one review of already-read evidence. |
| `finish()` | Save the verified Assessment v2 |

Each overview has bounded previews and gives separate cursors for fields, events,
current logs, previous logs and collection errors. Field groups share the preview
so a large section cannot crowd out every other field. The cursor is issued by
the runner for the exact resource, section and offset in the current attempt.
Counts, availability, omissions and truncation are reported for each section.
Source IDs on graph metadata only identify references. A citation requires
delivery of the source value. Tool reads are bounded by 25 records and 12,000
UTF-8 bytes per result and share a cumulative 128,000-byte delivery budget with
the startup map. Oversized atomic records remain identified as omitted.

The data pipeline investigator reads external nodes using their canonical source
attachments. The originating DataSource owns each source and its JSON pointer.
The agent sees external scopes and edge bases and links a finding to inspected
external nodes that own the cited attachments. Legacy graphs with only a
DataSource root still expose the sources collected for that root. The pipeline
profile distinguishes configuration, task state, broker positions and evidence
of delivery or replay. It uses the same tools, delivery checks and budgets.

Missing references are inspectable using collected evidence on the referring
resource. A reference source keeps its original owner and pointer. Observed,
inferred and proposed relationships retain their canonical graph identities and
origins. The model chooses which leads to check and records independent blockers
as separate findings. Confirming one blocker leaves other evidence-backed leads
open when they could still prevent recovery.

## Incremental assessment and investigation workspace

After the first request, the runner sends the compact startup map, a bounded
projection of recently delivered exact evidence, section receipts, accepted
findings and the latest complete tool exchange. This keeps the matching assistant
calls and results together for Bedrock. Evidence shares the context budget across
resources to preserve comparisons. Section receipts point to unread pages, and
the runner lists unchecked neighbors with failing states first. Three consecutive
repeated pages or finding edits without new citations close exploration. One model
response may request up to three reads. An identical finding edit keeps its
existing revision.

The runner builds the live graph from inspected nodes and their canonical
relationships. A broken observed edge may reveal its missing endpoint when
its reference evidence has been read. Resource cards link to accepted findings
that cite the resource. The current tool protocol emits no causal-path overlay.
Historical Assessment v2 results and persisted progress retain their original
annotations and graph scopes.

`indexing/completed` publishes the root and an empty draft. Read progress uses
the existing stage names for resource, event, log and exact source reads.
Started reads identify their resource. Completed reads can expand the graph
scope and publish available source IDs. `context_delivered` acknowledges
delivered evidence and reports provider tokens. `draft_updated` persists the
accepted Assessment v2 draft and revision. Rejected finding edits preserve the
accepted draft. The backend settles the run only after an explicit successful
`finish` and restores unfinished drafts after failures or reconnects.

`agentExecution.toolProtocol=snapshot-verify-v1` identifies the verification protocol and
`historyPolicy=snapshot-reads-v2` identifies automatic context retention. The
result and progress formats continue to use `assessmentBuilder=incremental-v1`
and draft version 1. Each draft is bounded to 48,000 UTF-8 bytes, 24 claims and
12 hypotheses. Finding writes reserve room for a default conclusion. Verification
accepts up to 16 gaps and 12 advisory steps. Each gap should identify an open
diagnostic question, the evidence needed, and how it could change the diagnosis.
Unavailable background data and prerequisites for remediation do not belong in
diagnostic gaps. When only gaps are available, the runner prepares an inconclusive
summary.

Verification closes general exploration and can return one review of delivered
evidence for broken or inferred relationships whose sources are not covered by
a single finding. If budget permits, the review can also request inspection of
up to two unread nodes directly linked to the entry or an inspected node by a
broken or inferred relationship, or linked nodes in a failure state. Only
those inspections are available during review. The review allows up to four
responses when it requests inspections and up to two otherwise.
If it closes with inspections still outstanding, the result is partial.
The runner enters verification automatically at its exploration limits and
completes a review when its window closes. Once verified, only `finish()` is
available. It persists the frozen draft with validated citations. Progress reason
codes distinguish `verify_requested`, `verify_review_required`, `draft_verified`,
`finish_requested` and `finish_completed`. An interrupted run retains its
previously accepted draft until the backend receives a successful result.

## Evidence boundary and result

Gateway stages a delivery manifest for each bootstrap/tool result. It commits
the manifest only after a provider request containing it returns. Generated but
unsent tool results cannot support a final answer. The journal records result
digests, byte counts, source IDs with delivered values, and graph entity IDs.
An interrupted attempt has no completed assessment; no message replay is implied.

PydanticAI output validation and final gateway validation require:

- Exact canonical source IDs from the current snapshot **and** delivered values.
- Exact quotes when a quote is supplied; supported claims require citations.
- Unique primary claim IDs and unique node insights with resolved references.
- Seen graph entities and connected adjacent causal edges; a root cause requires
  a supported claim citing delivered evidence for that entity.
- Confidence in `[0,1]`. Inconclusive/healthy evidence may use `causalPath=null`.

Agent citations use the explicit `snapshot-excerpt-v1` display policy. The model
selects exact delivered `sourceId` values and should omit `quote`; model-authored
quote strings are not used as evidence. Gateway copies the assessment, clears its
display quotes, and validates all source, claim and graph references before reading
any citation content. It then fills quotes with an exact prefix of at most 512
characters from the delivered atomic value, log line or event text. Non-string
values use the same canonical JSON representation as the final quote validator.
Null/absent text may produce an empty quote. Full source records remain unchanged.
The resulting assessment passes strict quote and reference validation again.

`agentExecution.citationQuotes` records the policy, `maxChars`, citation count and
`ignoredModelQuotes` count. Raw discarded quote text is not stored or logged. The
UI identifies excerpts as coming directly from the saved evidence and links to
the complete source. This projection never changes source IDs, claim verdicts,
confidence or graph annotations, and does not establish semantic entailment.
Legacy single-shot quoting and historical agent results retain their old policy.

No suffix/handle guessing or recovery from unread graph sources is performed in
agent mode. Invalid output may be retried at most twice within the shared request/token
budget, then fails explicitly. It never becomes Assessment v1. These checks prove
reference integrity, not semantic entailment of a claim by its citations.
An unknown or already closed tool call may also be corrected at most twice within
the same 40-request ceiling. Such a call never executes a snapshot read; it
produces `unavailable_tool` progress without recording its name or arguments.
Registered argument-schema corrections are bounded. The runner rejects an
unknown cursor and a supported or partially supported finding without exact
delivered citations. Findings marked unsupported are stored as hypotheses.
Invalid edits leave the accepted draft intact.

Validation rejections produce durable `validating/failed` progress with an
allowlisted reason code distinguishing malformed JSON, schema errors, unread
citations, quote mismatches, unsupported node annotations and invalid causal
paths. Logs contain the code and known schema field paths/error types, never raw
model output, source contents or Pydantic input values. The terminal transport
code remains `validation_failed`; UI resolves the last persisted rejection to a
fixed explanation, including bounded `validationFields` paths and error kinds
when available. Budget failures remain `budget_exhausted`; an `execution_limit`
event identifies the request, cumulative input/output token or tool-call ceiling
with numeric used/maximum counts. Timeout, cancellation, backend restart and
provider/transport failure keep distinct explanations. A terminal reason and its
last rejected response are displayed separately. Historical events without these
optional details remain readable; the UI never invents a missing historical reason.
Successful repaired results retain `validationIssues` in `agentExecution`.
The verified draft includes the audited quote projection. The frontend shows the read-driven graph and
provides a separate full-snapshot view. Legacy top-level text is derived from
the validated Assessment.

Successful results add `analysisMode` and `agentExecution` (version, request/attempt
IDs, snapshot hash, terminal status, stop reason, request/tool counts, exposed IDs
and context journal). Backend retains this audit metadata alongside Assessment v2.
Hidden model reasoning and raw model messages are not progress events or audit
content. Token usage sums PydanticAI's input/output usage across all model turns,
including successful output-repair turns. Provider failure billing may be unknown;
no token/cost estimate is fabricated for such an attempt.

## Bounds and terminal outcomes

Default limits: 40 model requests (including corrections), up to 50 snapshot read
calls and 49 finding write attempts, plus finalization within an SDK ceiling of 102
total tool calls; 400k cumulative input tokens, 75k cumulative output tokens, 8k maximum output
per request (or a lower user-configured `maxTokens`), five seconds per tool and
600 seconds per gateway run. Backend allows 610 seconds for dispatch/settlement
and accepts model turns through 40 and at most 384 gateway progress events.
SDK transport retries are disabled.
Provider-reported token limits are checked after responses;
the last response can cross a token bound. Byte limits and request/tool ceilings
also constrain execution independently of tokenizer accuracy.
These cumulative run totals do not enlarge the selected model's per-request
context window or output limit.

Before allowing more exploration, the gateway reserves up to three requests for
verification and finalization, leaving up to 37 exploratory requests by default.
The review deadline leaves two requests for `finish` when the budget permits.
The gateway also considers
cumulative provider-reported token usage: input forecasts allow 50% growth over
the last request and reserve exploration plus the finalization turns; output
reserves two per-request output caps plus a forecast of the next exploratory
response. This is an early-stop forecast, not a preflight token guarantee: a third
completion turn may not fit after large outputs. A large context jump can still
reach the hard provider-reported limit. The 40-request ceiling remains a hard maximum.
An already exhausted request limit does not emit a fictitious next model turn;
the stream returns `budget_exhausted`, which the backend persists as the reason.

When those reserves no longer fit, at the tool-call ceiling, or after three
consecutive repeated reads or finding edits without new citations, the incremental
runner closes reads and verifies the accepted draft. The legacy runner closes reads after two repeated
deliveries or errors without new facts.
If it returns a valid assessment after forced wrap-up, status is `partial`.
Exhaustion without a valid result is `failed`, not a fabricated partial assessment.
Missing evidence alone does not imply execution failure: a completed inconclusive
assessment may set `needsMoreInfo=true`.

Backend persists attempts/progress in `analysis_attempts` and `analysis_progress`.
Only a running attempt can commit a final result. Result, terminal event and SQL
request state are committed atomically before WebSocket fan-out. Detailed outcomes
are `completed`, `partial`, `failed`, `cancelled`, `timed_out`, or `interrupted`.
Existing CRD/list status uses `completed` for valid completed/partial artifacts and
`failed` for other terminal outcomes; detailed agent state remains available.

Cancellation settles the attempt and cancels the gateway HTTP stream; late results
are rejected. An already in-flight provider request may finish and be billed;
its result is discarded and no new tool/model calls are started. Backend restart
records `interrupted/backend_restarted` durably.
There is no automatic replay or paid retry after a restart. A user reanalysis uses
a new DiagnosticRun. Existing results and evidence are not rewritten.

## Progress transport and REST recovery

Backend sends correlated `analysisMode`, `analysisRequestId` and `attemptId` to
gateway `POST /analyze/stream`. NDJSON frames are `progress`, one validated `result`,
or a sanitized `error` code. Backend bounds the stream, validates identity/order
and allowlisted stage/phase fields, and saves each event before broadcasting
`Analysis.Progress`. There is no new gateway-to-backend callback endpoint.

Each persisted event has version, runId/diagnosticRunId, analysisRequestId,
attemptId, monotonic per-attempt `seq`, occurrence/recording UTC timestamps,
stage/phase, and optional tool call ID/count. Stages describe actual indexing,
model requests, tool calls, finalization, validation or an execution limit. Tool
events optionally include `target={nodeId,resource}`, `previousLogs`, and collected
data `availability`. Gateway resolves targets from its frozen graph; backend
rejects unknown node IDs and replaces resource metadata with the canonical request
snapshot values before persistence. Query strings, source contents and model prose
are not copied into progress. Validation details and target sizes are bounded.

UI renders first-person action messages from these actual events, for example
“I’m looking for dependencies of Deployment/gateway” and “I’m reading logs for
Pod/gateway-123 in demo”. These messages do not require an additional model call.
The current action appears in the assistant sidebar. The incident drawer,
DiagnosticRun detail and Assessment page show a persisted activity history with
times, resource checks, returned record counts and response corrections. Completed
tool calls replace their transient start rows. An uncollected log/event slice is
explicitly distinguished from an empty query result; neither proves health.
UI text comes from fixed templates; model reasoning and intermediate hypotheses
are not treated as progress messages. REST recovery restores the same history
after reload/reconnect. Failure status remains visible even without an assessment
or a graph payload.

- `GET /api/v1/diagnosticruns/{id}/analysisprogress` returns current attempt state,
  ordered events and lastSeq. Old runs return `available=false`.
- Optional `afterSeq` and `attemptId` select incremental events. An attempt change
  resets the cursor. State, events and cursor are read in one database transaction.
- `POST /api/v1/diagnosticruns/{id}/analysis/cancel` cancels a running agent attempt.

RTK Query loads the persisted progress after reconnect and polls active execution
as a fallback. WebSocket invalidates that query; it is not the durable event store.
Existing `Analysis.Completed`/`Analysis.Failed` events remain compatible.
