import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'

const source = readFileSync(new URL('../src/analysis/investigation.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } })
const helpers = {}
runInNewContext(outputText, { exports: helpers })
const graph = { entryResourceId:'entry', nodes:[{id:'entry'},{id:'neighbor'},{id:'hidden'}], edges:[{id:'edge',from:'entry',to:'neighbor'},{id:'hidden-edge',from:'entry',to:'hidden'}], candidates:[] }
const draft = (nodeIds=[], edgeIds=[]) => ({ version:1, revision:1, graph:{nodeIds,edgeIds,candidateIds:[]}, assessment:{nodeInsights:[]} })

test('historical review-only drafts still render',()=>{
  assert.equal(helpers.investigationGraph(graph,draft()).nodes.length,0)
  const selected=helpers.investigationGraph(graph,draft(['entry'],['edge']))
  assert.equal(selected.nodes.length,1)
  assert.equal(selected.edges.length,0)
})

test('live scope starts at the root and restores reads independently of reviews', () => {
  const resource = { kind: 'Pod', namespace: 'demo', name: 'root' }
  const preview = helpers.rootPreviewGraph(resource)
  assert.equal(preview.nodes[0].resource, resource)
  assert.equal(preview.nodes[0].state, 'collecting')
  const scope = {nodeIds:['entry','neighbor'],edgeIds:['edge'],candidateIds:[],sourceIds:['field:absence']}
  const latest = helpers.latestInvestigationScope({attemptId:'current',events:[
    {attemptId:'current',investigationGraph:scope}, {attemptId:'old',investigationGraph:{nodeIds:['hidden']}},
  ]})
  assert.equal(latest, scope)
  const canonical = {...graph, nodes: [{id:'entry',attributes:{hidden:true}}, {id:'neighbor',type:'reference'}],
    edges:[{id:'edge',from:'entry',to:'neighbor',origin:'observed',state:'broken',sourceIds:['field:absence','field:unread']}]}
  const projected = helpers.investigationGraph(canonical, latest)
  assert.equal(projected.nodes.length, 2)
  assert.equal(projected.nodes[0].attributes, undefined)
  assert.equal(projected.nodes[1].sourceIds.join(','), 'field:absence')
})

test('only canonical reviewed nodes and delivered connecting edges can render',()=>{
  const selected=helpers.investigationGraph(graph,draft(['entry','neighbor','invented'],['edge','hidden-edge','invented']))
  assert.equal(selected.nodes.length,2)
  assert.equal(selected.edges.length,1)
  assert.equal(selected.edges[0].id,'edge')
  assert.equal(graph.nodes.length,3)
})

test('failed-run drafts restore the latest revision of the current attempt only',()=>{
  const old={...draft(['hidden']),revision:99}
  const current={...draft(['entry']),revision:2}
  const restored=helpers.latestDraft({attemptId:'current',status:'failed',events:[
    {attemptId:'old',draft:old},{attemptId:'current',draft:current},{attemptId:'current',draft:{...draft(),revision:1}},
  ]})
  assert.equal(restored,current)
})

test('all analysis entry points use encoded assessment routes',()=>{
  assert.equal(helpers.assessmentPath('ns/incident','run/1'),'/incidents/ns%2Fincident/assessments/run%2F1')
})
