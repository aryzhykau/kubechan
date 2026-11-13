import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'

// Exercise the production pure helpers without adding a browser test framework.
const source = readFileSync(new URL('../src/analysis/progress.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } })
const helpers = {}
runInNewContext(outputText, { exports: helpers })
const target = { nodeId: 'pod', resource: { kind: 'Pod', name: 'gateway-123', namespace: 'demo' } }
const event = (changes = {}) => ({ seq: 1, stage: 'get_logs', phase: 'started', target, ...changes })
const progress = (changes = {}) => ({ available: true, status: 'running', stage: 'get_logs', events: [event()], ...changes })

test('live activity identifies the actual resource and current operation', () => {
  assert.match(helpers.analysisProgressLabel(progress()), /reading logs for Pod\/gateway-123 in demo/)
  assert.match(helpers.analysisEventLabel(event({ stage: 'get_graph_neighbors' })), /looking for dependencies of Pod\/gateway-123/)
  assert.match(helpers.analysisProgressLabel(progress({ stage: 'model_request', events: [event({ phase: 'completed' }), event({ stage: 'model_request', target: undefined })] })), /reviewing evidence from Pod\/gateway-123/)
})

test('uncollected previous logs do not imply health or absence of the resource', () => {
  const label = helpers.analysisEventLabel(event({ phase: 'completed', previousLogs: true, availability: 'not_collected', returnedCount: 0 }))
  assert.match(label, /no collected log lines from the previous container/)
  assert.doesNotMatch(label, /healthy|resource is missing/)
})

test('terminal reason and rejected response remain independently visible', () => {
  const failed = progress({ status: 'failed', stage: 'failed', reasonCode: 'budget_exhausted', events: [event({ stage: 'validating', phase: 'failed', reasonCode: 'output_schema_invalid', validationFields: [{ path: 'summary', kind: 'missing' }] })] })
  assert.match(helpers.analysisProgressReason(failed), /Execution limits/)
  assert.match(helpers.analysisValidationReason(failed), /summary \(missing\)/)
  failed.reasonCode = 'gateway_error'
  assert.match(helpers.analysisProgressReason(failed), /connection failed/)
  assert.match(helpers.analysisValidationReason(failed), /summary/)
})

test('the journal shows the gateway reason for a rejected draft write', () => {
  const detail = 'A cited source ID was not delivered.'
  const rejected = event({ stage: 'validating', phase: 'failed', reasonCode: 'draft_change_deferred', validationDetail: detail })
  assert.equal(helpers.analysisEventLabel(rejected), detail)
  assert.match(helpers.analysisEventLabel({ ...rejected, validationDetail: undefined }), /draft entry/)
})

test('execution failures identify the actual budget and numeric usage', () => {
  const failed = progress({ status: 'failed', stage: 'failed', reasonCode: 'budget_exhausted', events: [event({ stage: 'execution_limit', executionLimit: { kind: 'requests', used: 8, maximum: 8 } })] })
  assert.match(helpers.analysisProgressReason(failed), /Model request limit reached · 8 used \/ 8 allowed/)
  failed.events[0].executionLimit = { kind: 'input_tokens', used: 97000, maximum: 96000 }
  assert.match(helpers.analysisProgressReason(failed), /Cumulative input token limit reached/)
})

test('old runs show the recorded reason without inventing missing detail', () => {
  assert.match(helpers.analysisProgressReason(progress({ status: 'failed', reasonCode: 'validation_failed', events: [] })), /valid evidence-backed assessment/)
  assert.match(helpers.analysisProgressReason(progress({ status: 'failed', reasonCode: '', events: [] })), /without a recorded detailed reason/)
  assert.match(helpers.analysisProgressReason(progress({ status: 'timed_out', reasonCode: 'timeout' })), /time limit/)
  assert.match(helpers.analysisProgressReason(progress({ status: 'interrupted', reasonCode: 'backend_restarted' })), /backend restarted/)
})

test('repaired success does not present an earlier rejection as a failed run', () => {
  const rejected = event({ stage: 'validating', phase: 'failed', reasonCode: 'missing_citations' })
  assert.match(helpers.analysisProgressLabel(progress({ events: [rejected, event({ stage: 'model_request' })] })), /correcting the response/)
  assert.equal(helpers.analysisProgressReason(progress({ status: 'completed', reasonCode: 'validation_failed', events: [rejected] })), '')
})

test('history consolidates completed calls but retains every validation rejection', () => {
  const events = [event({ toolCallId: 'call-1' }), event({ seq: 2, toolCallId: 'call-1', phase: 'completed' }), event({ seq: 3, stage: 'validating', phase: 'failed' })]
  const history = helpers.analysisActivityEvents(progress({ events }))
  assert.equal(history.length, 2)
  assert.equal(history[0].seq, 2)
  assert.equal(history[1].seq, 3)
})
