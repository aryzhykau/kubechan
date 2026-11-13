import type { AnalysisProgress, AnalysisProgressEvent } from '../api/index'

const STAGES: Record<string, string> = {
  queued: 'Waiting to start analysis',
  indexing: 'Preparing the evidence snapshot',
  model_request: 'Analyzing collected evidence',
  get_graph_neighbors: 'Tracing resource dependencies',
  get_resource: 'Checking resource state',
  get_events: 'Checking collected events',
  get_logs: 'Reading collected logs',
  read_sources: 'Checking exact evidence sources',
  validating: 'Validating the assessment and citations',
  finalizing: 'Writing the assessment from inspected evidence',
  execution_limit: 'An execution limit was reached',
  context_delivered: 'Evidence delivered to the agent',
  draft_updated: 'Investigation draft updated',
  memory_updated: 'Investigation notes saved',
  draft_write: 'Checking a draft update',
  completed: 'Analysis completed',
  partial: 'Analysis stopped with a partial assessment',
  failed: 'Analysis failed',
  cancelled: 'Analysis cancelled',
  timed_out: 'Analysis timed out',
  interrupted: 'Analysis interrupted by a backend restart',
}

export function analysisProgressTitle(progress?: AnalysisProgress): string {
  return progress?.available ? STAGES[progress.stage ?? ''] ?? 'Analysis in progress' : 'Preparing analysis'
}

const TOOL_ACTIONS: Record<string, [string, string, string]> = {
  get_graph_neighbors: ['I’m looking for dependencies of', 'I checked dependencies of', 'relationships'],
  get_resource: ['I’m checking the collected state of', 'I checked the collected state of', 'fields'],
  get_events: ['I’m checking events for', 'I checked events for', 'events'],
  get_logs: ['I’m reading logs for', 'I read logs for', 'log lines'],
  read_sources: ['I’m reading exact evidence sources', 'I read exact evidence sources', 'sources'],
}

function targetLabel(event?: AnalysisProgressEvent): string {
  const ref = event?.target?.resource
  return ref ? `${ref.kind}/${ref.name}${ref.namespace ? ` in ${ref.namespace}` : ''}` : ''
}

export function analysisEventLabel(event: AnalysisProgressEvent): string {
  if (event.stage === 'draft_updated' && event.draft && event.change) {
    const { kind, id } = event.change
    const assessment = event.draft.assessment
    if (kind === 'review') {
      const review = assessment.nodeInsights?.find(item => item.nodeId === id)
      if (review) return `I reviewed ${targetLabel(event) || 'the resource'} · ${review.relevance === 'context' ? 'relevance not determined' : review.relevance.replace('_', ' ')} · ${review.comment}`
    }
    if (kind === 'graph') return 'I updated the investigation graph and resource cards.'
    if (kind === 'claim' || kind === 'hypothesis') {
      const claim = (kind === 'claim' ? assessment.claims : assessment.alternativeHypotheses).find(item => item.id === id)
      if (claim) return `${kind === 'claim' ? 'Finding' : 'Hypothesis'} · ${claim.statement} (${claim.verdict.replace('_', ' ')})`
    }
    if (kind === 'conclusion') return `Conclusion · ${assessment.summary}`
    if (kind === 'gap') return 'I recorded or updated an evidence gap.'
    if (kind === 'remediation') return `Recommendation · ${assessment.remediation.find(item => String(item.order) === id)?.title ?? ''}`
    if (kind === 'retracted') return 'I withdrew a draft record after updating the investigation.'
    return 'I updated the supported causal path.'
  }
  if (event.stage === 'memory_updated') {
    if (event.phase === 'failed') return 'A notes update was rejected; previously saved observations are retained.'
    const count = event.returnedCount ?? 0
    return count ? `I saved ${count} ${count === 1 ? 'observation' : 'observations'} for the next checks.` : 'I updated the investigation notes.'
  }
  if (event.stage === 'draft_write' && event.phase === 'failed') return 'A draft update was rejected; previously accepted findings are retained.'
  const action = TOOL_ACTIONS[event.stage]
  if (action) {
    const target = targetLabel(event)
    const subject = target || (event.stage === 'read_sources' ? '' : 'the selected resource')
    if (event.reasonCode === 'snapshot_read_repeated') return `I already checked this page${target ? ` for ${target}` : ''}.`
    if (event.phase === 'failed') return `I couldn’t read the requested snapshot data${target ? ` for ${target}` : ''}.`
    const previous = event.previousLogs ? ' from the previous container' : ''
    if (event.phase === 'completed' && event.availability === 'not_collected') return `The snapshot has no collected ${action[2]}${previous}${target ? ` for ${target}` : ''}.`
    return `${event.phase === 'completed' ? action[1] : action[0]}${subject ? ` ${subject}` : ''}${previous}${event.phase === 'completed' ? ` · ${event.returnedCount ?? 0} ${action[2]} returned` : ''}`
  }
  if (event.stage === 'model_request') return `I’m analyzing the evidence and choosing the next check${event.turn ? ` · model request ${event.turn}` : ''}`
  if (event.stage === 'context_delivered' && event.turnTokens) return `Model request ${event.turn} · ${event.turnTokens.input.toLocaleString()} input / ${event.turnTokens.output.toLocaleString()} output tokens`
  if (event.stage === 'finalizing') return 'I’m writing the assessment from the evidence I inspected.'
  if (event.stage === 'indexing') return 'I’m preparing the saved evidence snapshot.'
  if (event.stage === 'validating') return event.phase === 'failed' ? validationEventReason(event) : event.phase === 'completed' ? event.reasonCode === 'snapshot_quotes' ? 'The assessment passed validation; evidence excerpts come directly from the snapshot.' : 'The assessment and citations passed validation.' : 'I’m validating the assessment and its citations.'
  if (event.stage === 'execution_limit' && event.executionLimit) {
    const names = { requests: 'Model request limit', input_tokens: 'Cumulative input token limit', output_tokens: 'Cumulative output token limit', tool_calls: 'Tool-call limit' }
    const limit = event.executionLimit
    return `${names[limit.kind]} reached · ${limit.used.toLocaleString()} used / ${limit.maximum.toLocaleString()} allowed.`
  }
  return STAGES[event.stage] ?? 'Analysis in progress'
}

export function analysisProgressLabel(progress?: AnalysisProgress): string {
  if (progress?.status !== 'running') return analysisProgressTitle(progress)
  const event = progress.events[progress.events.length - 1]
  if (!event) return analysisProgressTitle(progress)
  if (event.stage === 'model_request' || event.stage === 'context_delivered') {
    const prior = progress.events.slice(0, -1).reverse().find(item => item.stage === 'draft_updated' || item.stage === 'validating' || item.stage === 'finalizing' || TOOL_ACTIONS[item.stage])
    if (prior?.stage === 'draft_updated') return 'I’m checking the next step against the findings recorded so far.'
    if (prior?.stage === 'validating' && prior.phase === 'failed') return 'I’m correcting the response so its evidence and structure pass validation.'
    if (prior?.stage === 'finalizing') return analysisEventLabel(prior)
    const target = targetLabel(prior)
    if (target) return `I’m reviewing evidence from ${target} before the next check.`
  }
  return analysisEventLabel(event)
}

export function analysisActivityEvents(progress: AnalysisProgress): AnalysisProgressEvent[] {
  // Replace a tool's transient start with its recorded outcome; retain model turns
  // and validation failures so a correction is visible after reconnect.
  const settled = new Set(progress.events.filter(event => event.toolCallId && event.phase !== 'started').map(event => event.toolCallId))
  return progress.events.filter(event => !(event.phase === 'started' && event.toolCallId && settled.has(event.toolCallId)))
}

const REASONS: Record<string, string> = {
  unsupported_request: 'This mode requires a Kubernetes-only snapshot and a supported provider.',
  validation_failed: 'The model did not return a valid evidence-backed assessment.',
  budget_exhausted: 'Execution limits were reached before a valid assessment was available.',
  provider_error: 'The model request failed.',
  gateway_error: 'The analysis connection failed before completion.',
  result_storage_failed: 'The result could not be validated or saved.',
  timeout: 'The analysis exceeded its time limit before a valid assessment was available.',
  backend_restarted: 'The backend restarted while this analysis was running. This attempt was not replayed.',
  cancelled: 'The analysis was cancelled.',
}

const VALIDATION_REASONS: Record<string, string> = {
  output_json_invalid: 'The model returned malformed JSON.',
  output_schema_invalid: 'The model response did not match the assessment schema.',
  unread_citation: 'The model cited evidence it had not read.',
  citation_quote_mismatch: 'A model quote did not match its evidence source.',
  missing_citations: 'A supported claim was missing evidence citations.',
  invalid_confidence: 'The model returned an invalid confidence value.',
  duplicate_claim_ids: 'The model reused a claim identifier.',
  invalid_node_insight: 'A resource annotation could not be validated against inspected evidence.',
  invalid_causal_path: 'The causal path could not be validated against the inspected graph.',
  invalid_assessment: 'The assessment failed evidence validation.',
  unusable_model_response: 'The model did not produce a usable structured assessment.',
  unavailable_tool: 'The model requested a tool that was unavailable at this stage.',
  draft_not_finalized: 'The model returned text before finalizing its recorded assessment.',
  draft_change_deferred: 'One draft entry needs more evidence or input. Saved findings are retained.',
  tool_arguments_invalid: 'Some tool arguments need correction. Valid updates can still be saved.',
}

function validationEventReason(event: AnalysisProgressEvent): string {
  const reason = event.validationDetail ?? VALIDATION_REASONS[event.reasonCode ?? ''] ?? 'The response needs correction before it can be accepted.'
  const fields = event.validationFields?.map(field => `${field.path} (${field.kind.replace(/_/g, ' ')})`).join(', ')
  return fields ? `${reason} Fields: ${fields}.` : reason
}

export function analysisValidationReason(progress?: AnalysisProgress): string {
  const rejected = progress?.events.slice().reverse().find(event => event.stage === 'validating' && event.phase === 'failed')
  return rejected ? validationEventReason(rejected) : ''
}

export function analysisProgressReason(progress?: AnalysisProgress): string {
  if (progress?.status === 'running' || progress?.status === 'completed' || progress?.status === 'partial') return ''
  if (progress?.reasonCode === 'validation_failed') return analysisValidationReason(progress) || REASONS.validation_failed
  if (progress?.reasonCode === 'budget_exhausted') {
    const limit = progress.events.slice().reverse().find(event => event.stage === 'execution_limit' && event.executionLimit)
    if (limit) return analysisEventLabel(limit)
  }
  return REASONS[progress?.reasonCode ?? ''] ?? (progress?.available ? 'This run stopped without a recorded detailed reason.' : '')
}
