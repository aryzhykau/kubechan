"""Offline integration tests for the four-tool snapshot investigation."""
import asyncio
import json
from typing import Any, Protocol, cast

import boto3
import pytest
from botocore.stub import Stubber
from pydantic_ai.exceptions import UsageLimitExceeded
from pydantic_ai.messages import ModelRequest, ModelResponse, ToolCallPart, ToolReturnPart, UserPromptPart
from pydantic_ai.models.bedrock import BedrockConverseModel
from pydantic_ai.models.function import FunctionModel
from pydantic_ai.providers.bedrock import BedrockProvider
from pydantic_ai.usage import RequestUsage

from app.agent import AgentBudget
from app.investigation import run_incremental_agent
from app.models import AssessmentV2
from tests.test_snapshot_agent import dependency_payload as dependency_payload, payload as payload, request

INVESTIGATION_TOOLS = {"inspect", "read_more", "record_finding", "record_branch", "submit_plan", "verify"}
TOOLS = INVESTIGATION_TOOLS | {"finish", "add_branch", "close_branch"}


def inspect(node):
    return "inspect", {"resource_id": node}


def finding(statement, *sources, finding_id="", verdict="supported"):
    return "record_finding", {"statement": statement, "source_ids": list(sources), "finding_id": finding_id, "verdict": verdict}


def verify(summary="", **details):
    return "verify", {"summary": summary, **details}


def finish():
    return "finish", {}


def returned(messages, tool) -> dict[str, Any]:
    part = next(part for part in messages[-1].parts if isinstance(part, ToolReturnPart) and part.tool_name == tool)
    value = json.loads(part.content) if isinstance(part.content, str) else part.content
    assert isinstance(value, dict)
    return value



class SnapshotResult(Protocol):
    assessment: AssessmentV2
    agentExecution: dict[str, Any]
    prompt: str
    needsMoreInfo: bool

def run(payload, actions, budget=None):
    progress, calls = [], []

    async def model(messages, info):
        turn = len(calls)
        calls.append((messages, info))
        assert not info.output_tools
        assert {tool.name for tool in info.function_tools} <= TOOLS
        assert turn < len(actions), "unexpected extra model request"
        batch = actions[turn](messages) if callable(actions[turn]) else actions[turn]
        if asyncio.iscoroutine(batch):
            batch = await batch
        return ModelResponse(parts=[ToolCallPart(name, args, tool_call_id=f"{turn}-{i}") for i, (name, args) in enumerate(batch)],
                             usage=RequestUsage(input_tokens=100, output_tokens=100))

    async def sink(event):
        progress.append(event)

    result = asyncio.run(run_incremental_agent(request(payload), sink, model=FunctionModel(model), budget=budget))
    assert result.assessment is not None
    assert result.agentExecution is not None
    assert result.prompt is not None
    return cast(SnapshotResult, result), progress, calls


@pytest.mark.parametrize(("gaps", "expected_gaps", "confidence"), [
    ([], [], 0.8),
    ([" None ", "NO EVIDENCE GAPS.", "N/A"], [], 0.8),
    (["None", "None of the affected Pods has previous logs."],
     ["None of the affected Pods has previous logs."], 0.55),
])
def test_verification_and_finish_preserve_exact_citations_in_a_cascade(payload, gaps, expected_gaps, confidence):
    def service(messages):
        overview = returned(messages, "inspect")
        assert overview["related"][0] == {"type": "routes_to", "state": "resolved", "origin": "observed", "resourceState": "present",
                                          "resourceId": "service", "direction": "out"}
        return [inspect("service")]

    result, events, calls = run(payload, [
        [inspect("ingress")], service, [inspect("pod")],
        [finding("The Pod reports missing DB_HOST.", "log:crash"),
         verify("The Pod reports missing DB_HOST.", gaps=gaps, remediation=["Check the expected DB_HOST configuration."])],
        [finish()],
    ])
    assert {tool.name for tool in calls[0][1].function_tools} == INVESTIGATION_TOOLS
    assert result.agentExecution["toolProtocol"] == "snapshot-verify-v1"
    assert result.assessment.confidence == confidence
    assert [gap.description for gap in result.assessment.missingEvidence] == expected_gaps
    assert result.needsMoreInfo == bool(expected_gaps)
    assert result.assessment.claims[0].citations[0].quote == "required configuration DB_HOST is missing"
    assert result.assessment.remediation[0].title == "Check the expected DB_HOST configuration."
    scopes = [event["investigationGraph"] for event in events if event["phase"] == "completed" and event.get("investigationGraph")]
    assert [scope["nodeIds"] for scope in scopes] == [
        ["ingress"], ["ingress"], ["ingress", "service"], ["ingress", "pod", "service"],
    ]
    assert scopes[-1]["edgeIds"] == ["route", "select"]
    assert result.assessment.nodeInsights[0].nodeId == "pod"
    assert result.agentExecution["investigationGraph"] == scopes[-1]
    assert [event["turnTokens"] for event in events if event["stage"] == "context_delivered"] == [{"input": 100, "output": 100}] * 5
    assert {tool.name for tool in calls[-1][1].function_tools} == {"finish"}
    assert [event["reasonCode"] for event in events if event.get("reasonCode")] == [
        "verify_requested", "draft_verified", "finish_requested", "finish_completed",
    ]


def test_pipeline_agent_reads_scoped_task_evidence_and_links_its_card():
    root = {"apiGroup": "kubechan.io", "kind": "DataSource", "namespace": "lab", "name": "connect-a"}
    peer = {**root, "name": "connect-b"}
    task = {"apiGroup": "integrations.kubechan.io", "kind": "DebeziumTask", "name": "orders/0"}
    payload = {
        "graph": {"schemaVersion": 3, "entryResourceId": "root", "nodes": [
            {"id": "root", "type": "resource", "resource": root},
            {"id": "task-a", "type": "external", "resource": task, "external": {"scope": "connect-a"},
             "state": "degraded", "sourceIds": ["state-a", "cause-a"]},
            {"id": "task-b", "type": "external", "resource": task, "external": {"scope": "connect-b"},
             "sourceIds": ["state-b"]},
        ], "edges": [{"id": "reports-a", "from": "root", "to": "task-a", "origin": "observed", "basis": "reported"}]},
        "sources": [
            {"id": identity, "resource": resource, "type": "resource_field", "path": f"/externalSnapshot/observations/{i}/value", "value": value}
            for i, (identity, resource, value) in enumerate([
                ("state-a", root, "FAILED"), ("cause-a", root, "PostgresPublicationMissingAutocreateDisabled"),
                ("state-b", peer, "RUNNING"),
            ])
        ],
    }

    def inspect_result(messages):
        result = returned(messages, "inspect")
        assert {source["id"] for source in result["items"]} == {"state-a", "cause-a"}
        assert result["related"][0]["basis"] == "reported"
        return [finding("The task lacks its configured publication.", "state-a", "cause-a")]

    result, _, calls = run(payload, [[inspect("task-a")], inspect_result, [verify()], [finish()]])
    initial = json.loads(calls[0][0][0].parts[0].content)
    assert {node["scope"] for node in initial["graph"]["nodes"] if node["type"] == "external"} == {"connect-a", "connect-b"}
    assert result.agentExecution["investigator"] == "data-pipeline-v1"
    assert "You are the data pipeline investigator" in result.prompt
    assert result.assessment.nodeInsights[0].nodeId == "task-a"
    assert {citation.quote for citation in result.assessment.claims[0].citations} == {
        "FAILED", "PostgresPublicationMissingAutocreateDisabled",
    }


def test_preserves_independent_findings_after_checking_another_branch(dependency_payload):
    payload = dependency_payload
    service = {"kind": "Service", "namespace": "demo", "name": "gateway-public"}
    pod = next(node["resource"] for node in payload["graph"]["nodes"] if node["id"] == "pod")
    payload["rootResource"] = service
    payload["graph"]["entryResourceId"] = "service"
    payload["graph"]["nodes"].append({"id": "service", "type": "resource", "resource": service})
    payload["graph"]["edges"] = [edge for edge in payload["graph"]["edges"] if edge["to"] not in {"config", "secret"}]
    payload["graph"]["edges"] += [
        {"id": "edge:selector-lead", "from": "service", "to": "workload", "type": "possible_selector_target",
         "origin": "inferred", "state": "proposed"},
        {"id": "edge:mount", "from": "pod", "to": "pvc", "type": "mounts", "origin": "observed", "state": "resolved",
         "sourceIds": ["field:pod-claim"]},
    ]
    payload["sources"] += [
        {"id": identity, "type": "resource_field", "resource": resource, "path": path, "value": value}
        for identity, resource, path, value in (
            ("field:selector", service, "/selector", {"track": "canary"}),
            ("field:track", pod, "/labels/track", "stable"),
            ("field:pod-claim", pod, "/pvcNames/0", "uploads"),
        )
    ]

    def check_storage(messages):
        assert returned(messages, "inspect")["sections"]["events"]["sourceIds"] == ["event:pod"]
        initial = next(part for part in messages[0].parts if isinstance(part, UserPromptPart))
        assert isinstance(initial.content, str)
        state = json.loads(initial.content.split("Investigation state (data):\n")[1])
        assert any(item["id"] == "field:selector" and item["value"]["track"] == "canary"
                   for group in state["recentEvidence"] for item in group["items"])
        assert state["uninspectedNeighbors"][0]["resourceId"] == "pvc"
        return [finding("The Service selects canary and the Pod is stable.", "field:selector", "field:track", finding_id="selector"),
                inspect("pvc")]

    result, events, _ = run(payload, [
        [inspect("service")], [inspect("workload")], [inspect("pod")], check_storage,
        [finding("The missing StorageClass leaves uploads unbound and blocks scheduling.", "event:pod",
                 "field:pod-claim", "field:pvc-phase", "field:pvc-class", "field:class-exists", "event:pvc", finding_id="storage"),
         verify("A selector mismatch and an unbound PVC independently block recovery.")],
        [finish()],
    ])
    assert [claim.id for claim in result.assessment.claims] == ["selector", "storage"]
    assert all(claim.verdict == "supported" for claim in result.assessment.claims)
    assert result.assessment.confidence == 0.8
    assert not result.assessment.missingEvidence
    assert result.assessment.claims[1].citations[-1].quote.endswith("not found")
    scope = result.agentExecution["investigationGraph"]
    assert "class" in scope["nodeIds"]
    assert {"edge:class", "edge:mount", "edge:selector-lead"} <= set(scope["edgeIds"])
    assert result.assessment.causalPath is None
    assert all(event["phase"] != "failed" for event in events)


def test_verify_surfaces_read_but_uncited_relationship_evidence(dependency_payload):
    dependency_payload["graph"]["edges"] = [
        edge for edge in dependency_payload["graph"]["edges"] if edge["to"] not in {"config", "secret"}
    ]
    def review(messages):
        receipt = returned(messages, "verify")
        assert not receipt["verified"]
        relationship = next(item for item in receipt["review"] if item["type"] == "uses_storage_class")
        assert {item["id"]: item["value"] for item in relationship["evidence"]} == {
            "field:class-exists": False, "field:pvc-class": "missing-class",
        }
        return [finding("The configured StorageClass is missing.", "field:class-exists", "field:pvc-class"),
                verify("The Pod cannot schedule because its PVC references a missing StorageClass.")]

    result, events, calls = run(dependency_payload, [
        [inspect("pod")], [inspect("pvc")],
        [finding("The Pod could not schedule.", "event:pod", "field:pvc-class"), verify("The Pod could not schedule.")],
        review, [finish()],
    ])
    assert len(result.assessment.claims) == 2
    assert result.assessment.claims[1].citations[0].quote == "false"
    assert {tool.name for tool in calls[-2][1].function_tools} == {"record_finding", "record_branch", "verify"}
    assert {tool.name for tool in calls[-1][1].function_tools} == {"finish"}
    assert sum(event.get("reasonCode") == "verify_review_required" for event in events) == 1


def test_verify_requests_only_linked_unread_failure_leads(payload):
    payload["graph"]["edges"].append({
        "id": "edge:backend-lead", "from": "ingress", "to": "pod",
        "type": "possible_backend", "origin": "inferred", "state": "proposed",
    })
    next(node for node in payload["graph"]["nodes"] if node["id"] == "isolated")["state"] = "degraded"

    def review(messages):
        receipt = returned(messages, "verify")
        assert not receipt["verified"]
        assert receipt["inspectResources"] == ["pod"]
        return [inspect("isolated"), inspect("pod")]

    result, _, calls = run(payload, [
        [inspect("ingress")],
        [finding("Ingress routes to web.", "field:backend"), verify()],
        review,
        [finding("The application reports missing DB_HOST.", "log:crash"), verify()],
        [finish()],
    ])
    assert {tool.name for tool in calls[2][1].function_tools} == {"inspect", "record_finding", "record_branch", "verify"}
    assert len(result.assessment.claims) == 2
    assert "isolated" not in result.agentExecution["investigationGraph"]["nodeIds"]


def test_verify_requires_an_outcome_for_open_branch(payload):
    question = "Does the Ingress point at the intended Service?"

    def close_branch(messages):
        branch = returned(messages, "verify")["openBranches"][0]
        assert branch["question"] == question
        return [("record_branch", {
            "branch_id": branch["id"], "node_id": "ingress", "question": question,
            "status": "ruled_out", "outcome": "The backend is the intended Service.",
            "source_ids": ["field:backend"],
        }), verify()]

    result, events, _ = run(payload, [
        [inspect("ingress")],
        [("record_branch", {"node_id": "ingress", "question": question}), verify()],
        close_branch,
        [finish()],
    ])
    assert result.agentExecution["draft"]["branches"][0]["status"] == "ruled_out"
    assert any(event.get("change", {}).get("kind") == "branch" for event in events)


def test_submitted_plan_runs_ranked_branches_in_separate_sessions(payload):
    def close(status, outcome, source):
        def response(messages):
            assert len(messages) == 3
            prompt = next(part.content for part in messages[0].parts if isinstance(part, UserPromptPart))
            assert isinstance(prompt, str)
            state = json.loads(prompt.split("Investigation state (data):\n")[1])
            branch = next(branch for branch in state["branches"] if branch["status"] == "checking")
            assert branch["status"] == "checking"
            return [("close_branch", {"status": status, "outcome": outcome, "source_ids": [source],
                    "runbook": [{"action": "Set DB_HOST.", "verification": "Check that the pod becomes ready."}]
                    if status == "blocker" else []})]
        return response

    def start_branch(node):
        def response(messages):
            assert len(messages) == 1
            return [inspect(node)]
        return response

    result, events, calls = run(payload, [
        [inspect("ingress")],
        [("submit_plan", {"branches": [
            {"node_id": "pod", "question": "Does the pod start?", "priority": 2},
            {"node_id": "ingress", "question": "Does the backend resolve?", "priority": 1},
        ]})],
        start_branch("ingress"), close("ruled_out", "Backend matches.", "field:backend"),
        start_branch("pod"), close("blocker", "The pod lacks DB_HOST.", "log:crash"),
        [verify()], [finish()],
    ])
    assert {tool.name for tool in calls[2][1].function_tools} == {
        "inspect", "read_more", "add_branch", "close_branch",
    }
    assert [b["status"] for b in result.agentExecution["draft"]["branches"]] == ["ruled_out", "confirmed"]
    assert result.assessment.claims[0].statement == "The pod lacks DB_HOST."
    assert result.agentExecution["draft"]["branches"][1]["runbook"][0]["action"] == "Set DB_HOST."
    assert result.agentExecution["requests"] == 8
    assert len({event["branchId"] for event in events if event["stage"] == "get_resource" and "branchId" in event}) == 2


@pytest.mark.parametrize("extra_sources", [
    ["field:class-exists", "field:pvc-class", "field:config-exists"],
    ["field:pvc-class", "field:detector-symptom"],
])
def test_plan_covers_concrete_relationships_and_reuses_findings(dependency_payload, extra_sources):
    payload = dependency_payload
    graph = payload["graph"]
    workload = next(node for node in graph["nodes"] if node["id"] == "workload")
    service = {"kind": "Service", "name": "web", "namespace": "demo"}
    graph["entryResourceId"] = "service"
    graph["nodes"].append({"id": "service", "type": "resource", "state": "degraded", "resource": service})
    payload["sources"] += [
        {"id": "field:selector", "type": "resource_field", "resource": service, "path": "/selector", "value": {"version": "old"}},
        {"id": "field:labels", "type": "resource_field", "resource": workload["resource"], "path": "/labels", "value": {"version": "new"}},
        {"id": "field:detector-symptom", "type": "resource_field", "resource": service,
         "path": "/problemCases/endpoint/symptoms/0", "value": "No ready endpoints"},
    ]
    graph["edges"] += [
        {"id": "edge:selector", "from": "service", "to": "workload", "type": "possible_selector_target",
         "origin": "inferred", "state": "proposed", "sourceIds": ["field:selector", "field:labels"]},
        {"id": "edge:config-replica", "from": "pod", "to": "config", "type": "dependency",
         "origin": "observed", "state": "broken", "sourceIds": ["field:config-exists"]},
    ]

    def close_from_proof(outcome, *sources):
        def respond(messages):
            items = returned(messages, "inspect")["items"]
            assert set(sources) <= {item["id"] for item in items}
            return [("close_branch", {"status": "blocker", "outcome": outcome, "source_ids": list(sources)})]
        return respond

    def reuse_storage(messages):
        receipt = returned(messages, "close_branch")
        assert not receipt["accepted"]
        return [("close_branch", {"status": "blocker", "outcome": "The PVC class prevents traffic recovery.",
                                  "finding_id": receipt["existingFindingId"]})]

    result, events, _ = run(payload, [
        [inspect("service")],
        [("submit_plan", {"branches": [
            {"nodeId": "service", "edgeId": "edge:selector", "question": "Does the Service select the workload?", "priority": 1},
            {"node_id": "workload", "question": "Does the dependency relationship from Deployment/web to ConfigMap/web-config need to change for recovery?", "priority": 1},
            {"node_id": "service", "question": "Why is traffic unavailable?", "priority": 1},
        ]})],
        [inspect("service")],
        close_from_proof("The Service selector differs from the workload labels.", "field:selector", "field:labels"),
        [inspect("workload")], close_from_proof("The ConfigMap is absent.", "field:config-exists"),
        [inspect("workload")], close_from_proof("The Secret is absent.", "field:secret-exists"),
        [inspect("pvc")], close_from_proof("The PVC references an absent class.", "field:pvc-class", "field:class-exists"),
        [("close_branch", {"status": "blocker", "outcome": "The class referenced by the claim does not exist.",
                          "source_ids": extra_sources})],
        reuse_storage,
        [verify()], [finish()],
    ])
    branches = result.agentExecution["draft"]["branches"]
    assert [b.get("edgeId") for b in branches] == ["edge:selector", "edge:config", "edge:secret", "edge:class", None]
    assert all(b["status"] == "confirmed" for b in branches)
    assert len(result.assessment.claims) == 4
    assert branches[3]["findingId"] == branches[4]["findingId"]
    assert len({event["branchId"] for event in events if event["stage"] == "get_resource" and "branchId" in event}) == 4


def test_verify_cannot_skip_reserved_runtime_check(payload):
    pod = next(node for node in payload["graph"]["nodes"] if node["id"] == "pod")
    payload["sources"].append({"id": "event:warning", "resource": pod["resource"], "type": "event",
                               "event": {"type": "Warning", "reason": "BackOff", "message": "Restarting container"}})

    result, _, calls = run(payload, [
        [inspect("ingress")],
        [verify("Traffic routing explains the incident.")],
        [inspect("pod")],
        [("close_branch", {"status": "blocker", "outcome": "The application cannot start.",
                          "source_ids": ["log:crash", "event:warning"]})],
        [verify()], [finish()],
    ])
    assert {"record_branch", "record_finding"}.isdisjoint(tool.name for tool in calls[1][1].function_tools)
    assert result.agentExecution["status"] == "completed"
    assert result.agentExecution["draft"]["branches"][0]["nodeId"] == "pod"
    assert [claim.statement for claim in result.assessment.claims] == ["The application cannot start."]


def test_branch_rejection_is_journaled_and_valid_retry_can_close(payload):
    def retry(messages):
        failure = returned(messages, "close_branch")
        assert not failure["accepted"]
        assert "not delivered" in failure["error"]
        return [("add_branch", {"node_id": "pod", "question": "Does the Pod start?", "priority": 2}),
                ("close_branch", {"status": "ruled_out", "outcome": "The backend matches.",
                                  "source_ids": ["field:backend"]})]

    result, events, _ = run(payload, [
        [inspect("ingress")],
        [("submit_plan", {"branches": [
            {"node_id": "ingress", "question": "Is the backend wrong?", "priority": 1},
        ]})],
        [("close_branch", {"status": "invalid-status", "outcome": "The backend is wrong."})],
        [("close_branch", {"status": "blocker", "outcome": "The backend is wrong.",
                          "source_ids": ["unseen:source"]})],
        retry,
        [inspect("pod")],
        [("close_branch", {"status": "blocker", "outcome": "The P", "source_ids": ["log:crash"]})],
        [("close_branch", {"status": "blocker", "outcome": "The Pod lacks DB_HOST.",
                          "source_ids": ["log:crash"]})],
        [verify()], [finish()],
    ])
    first, second = result.agentExecution["draft"]["branches"]
    assert first["status"] == "ruled_out"
    assert second["status"] == "confirmed"
    assert len(result.assessment.claims) == 1
    rejected = [event for event in events if event.get("reasonCode") == "draft_change_deferred"]
    assert len(rejected) == 2
    assert rejected[1]["branchId"] == second["id"]
    assert "complete sentence" in rejected[1]["validationDetail"]
    assert rejected[0]["branchId"] == first["id"]
    assert "not delivered" in rejected[0]["validationDetail"]
    assert "unseen:source" not in rejected[0]["validationDetail"]
    invalid_args = next(event for event in events if event.get("reasonCode") == "tool_arguments_invalid")
    assert invalid_args["branchId"] == first["id"]
    assert invalid_args["validationFields"] == [{"path": "status", "kind": "literal_error"}]


def test_branch_budget_does_not_stop_the_next_branch(payload):
    def close_second(messages):
        prompt = next(part.content for part in messages[0].parts if isinstance(part, UserPromptPart))
        assert isinstance(prompt, str)
        state = json.loads(prompt.split("Investigation state (data):\n")[1])
        branch = next(branch for branch in state["branches"] if branch["status"] == "checking")
        assert branch["priority"] == 2
        return [("close_branch", {"status": "blocker", "outcome": "Ingress references this backend.", "source_ids": ["field:backend"]})]

    result, _, _ = run(payload, [
        [inspect("ingress")],
        [("submit_plan", {"branches": [
            {"node_id": "pod", "question": "Does the pod start?", "priority": 1},
            {"node_id": "ingress", "question": "Which backend is configured?", "priority": 2},
        ]})],
        [inspect("pod")],
        close_second,
    ], budget=AgentBudget(branch_requests=1))
    assert result.agentExecution["status"] == "partial"
    assert [branch["status"] for branch in result.agentExecution["draft"]["branches"]] == ["gap", "confirmed"]
    assert len(result.assessment.claims) == 1


def test_branch_timeout_keeps_findings_and_runs_remaining_checks(payload):
    async def stall(_):
        await asyncio.sleep(10)

    def close(outcome, source):
        return [("close_branch", {"status": "blocker", "outcome": outcome, "source_ids": [source]})]

    result, _, _ = run(payload, [
        [inspect("ingress")],
        [("submit_plan", {"branches": [
            {"node_id": "ingress", "question": "Is the backend wrong?", "priority": 1},
            {"node_id": "service", "question": "Does endpoint discovery work?", "priority": 2},
            {"node_id": "pod", "question": "Why does the pod fail?", "priority": 3},
        ]})],
        close("Ingress references the configured backend.", "field:backend"),
        stall,
        [inspect("pod")],
        close("The container reports missing DB_HOST.", "log:crash"),
    ], budget=AgentBudget(branch_seconds=0.2))

    assert result.agentExecution["status"] == "partial"
    assert [branch["status"] for branch in result.agentExecution["draft"]["branches"]] == ["confirmed", "gap", "confirmed"]
    assert len(result.assessment.claims) == 2
    assert [gap.description for gap in result.assessment.missingEvidence] == [
        "Branch timed out. Unresolved: Does endpoint discovery work?",
    ]


def test_partial_reports_checks_beyond_branch_and_run_budgets(payload):
    for number in range(10):
        ref = f"missing-{number}"
        payload["graph"]["nodes"].append({
            "id": ref, "type": "reference", "state": "missing",
            "resource": {"kind": "Service", "name": ref, "namespace": "demo"},
        })
        payload["graph"]["edges"].append({
            "id": f"check-{number}", "from": "ingress", "to": ref,
            "type": "routes_to", "origin": "observed", "state": "broken", "sourceIds": ["field:backend"],
        })

    result, _, _ = run(payload, [
        [inspect("ingress")],
        [("submit_plan", {"branches": [
            {"node_id": "ingress", "question": "How can traffic recover?", "priority": 1},
        ]})],
        [("close_branch", {"status": "blocker", "outcome": "The first backend reference is broken.",
                          "source_ids": ["field:backend"]})],
    ], budget=AgentBudget(requests=6))

    assert result.agentExecution["status"] == "partial"
    assert len(result.assessment.claims) == 1
    branches = result.agentExecution["draft"]["branches"]
    assert sum(branch["status"] == "confirmed" for branch in branches) == 1
    assert sum(branch["status"] == "gap" for branch in branches) == 4
    gaps = [gap.description for gap in result.assessment.missingEvidence]
    assert sum(gap.startswith("Unchecked before the run ended: ") for gap in gaps) == 4
    assert sum(gap.startswith("Unscheduled snapshot check: ") for gap in gaps) == 4
    assert "2 more snapshot relationship checks were not scheduled." in gaps


def test_branch_priority_and_checking_are_logged(payload, caplog):
    caplog.set_level("INFO", logger="llm-gateway")
    first = {"node_id": "ingress", "question": "Check the backend reference", "priority": 1}
    second = {"node_id": "ingress", "question": "Check the routing configuration", "priority": 2}

    def start_out_of_order(messages):
        receipts = [part.content for part in messages[-1].parts if isinstance(part, ToolReturnPart)]
        assert all(isinstance(receipt, dict) for receipt in receipts)
        first["branch_id"], second["branch_id"] = [receipt["branchId"] for receipt in cast(list[dict], receipts)]
        return [("record_branch", {**second, "status": "checking"})]

    def start_first(messages):
        assert not returned(messages, "record_branch")["accepted"]
        return [("record_branch", {**first, "status": "checking"})]

    result, _, _ = run(payload, [
        [inspect("ingress")],
        [("record_branch", first.copy()), ("record_branch", second.copy())],
        start_out_of_order,
        start_first,
        [("record_branch", {**first, "status": "ruled_out", "outcome": "Backend matches.",
                            "source_ids": ["field:backend"]}),
         ("record_branch", {**second, "status": "checking"})],
        [("record_branch", {**second, "status": "gap", "outcome": "Need the routing policy."}), verify()],
        [finish()],
    ])
    assert [branch["status"] for branch in result.agentExecution["draft"]["branches"]] == ["ruled_out", "gap"]
    assert "hypotheses=2" in caplog.text
    assert "priority=2 status=checking question='Check the routing configuration'" in caplog.text


def test_read_more_continues_a_section_and_can_reread_exact_sources(payload):
    pod = next(node["resource"] for node in payload["graph"]["nodes"] if node["id"] == "pod")
    payload["sources"] += [
        {"id": f"previous:{i}", "type": "log_line", "resource": pod, "path": f"/logs/main/previous/{i}",
         "line": {"number": i, "text": f"previous line {i}"}}
        for i in range(1, 5)
    ]

    def next_page(messages):
        overview = returned(messages, "inspect")
        assert overview["sections"]["previousLogs"]["sourceIds"] == ["previous:4", "previous:3"]
        assert overview["sections"]["currentLogs"]["sourceIds"] == ["log:crash"]
        return [("read_more", {"cursor": overview["sections"]["previousLogs"]["cursor"]})]

    def reread(messages):
        page = returned(messages, "read_more")
        assert page["sections"]["previousLogs"]["sourceIds"] == ["previous:2", "previous:1"]
        initial = next(part for part in messages[0].parts if isinstance(part, UserPromptPart))
        assert isinstance(initial.content, str)
        checks = json.loads(initial.content.split("Investigation state (data):\n")[1])["recentChecks"]
        assert all("cursor" not in check["sections"]["previousLogs"] for check in checks)
        return [("read_more", {"cursor": "previous:4"})]

    result, events, calls = run(payload, [
        [inspect("pod")], next_page, reread,
        [finding("Previous logs were captured.", "previous:4"), verify("Previous logs were captured.")],
        [finish()],
    ])
    assert result.assessment.claims[0].citations[0].quote == "previous line 4"
    assert any(event.get("previousLogs") and event["stage"] == "get_logs" for event in events)
    messages = calls[-2][0]
    assert len(messages) == 3 and isinstance(messages[1], ModelResponse) and isinstance(messages[2], ModelRequest)
    assert [part.tool_call_id for part in messages[1].parts if isinstance(part, ToolCallPart)] == [
        part.tool_call_id for part in messages[2].parts if isinstance(part, ToolReturnPart)
    ]


def test_unread_sources_are_rejected_and_claims_can_be_revised_or_retracted(payload):
    result, events, _ = run(payload, [
        [inspect("ingress"), finding("Premature finding.", "field:backend", finding_id="backend")],
        [finding("Backend is web.", "field:backend", finding_id="backend"),
         finding("Image failure is possible.", finding_id="hypothesis", verdict="unsupported")],
        [finding("Invalid replacement.", "field:hidden", finding_id="backend"),
         ("record_finding", {"finding_id": "hypothesis"})],
        [finding("Ingress routes to web.", "field:backend", finding_id="backend"),
         verify("Ingress routes to web.")],
        [finish()],
    ])
    assert [claim.statement for claim in result.assessment.claims] == ["Ingress routes to web."]
    assert not result.assessment.alternativeHypotheses
    assert [event["change"]["kind"] for event in events if event["stage"] == "draft_updated"] == [
        "claim", "hypothesis", "retracted", "claim", "conclusion",
    ]
    assert sum(event.get("reasonCode") == "draft_change_deferred" for event in events) == 2


def test_missing_evidence_and_secret_boundaries(payload):
    gap = ("Application authentication logs are needed to distinguish rejected "
           "credentials from a connection failure.")

    def check_secret(messages):
        overview = returned(messages, "inspect")
        assert overview["sections"]["fields"]["sourceIds"] == ["field:exists"]
        assert "DO-NOT-EXPOSE" not in json.dumps(overview)
        return [("read_more", {"cursor": "field:secret"}),
                verify("The Secret contains bad credentials.", gaps=[gap])]

    result, events, _ = run(payload, [
        [inspect("secret")], check_secret,
        [finish()],
    ])
    assert result.assessment.summary == "The snapshot does not establish a cause."
    assert result.needsMoreInfo and result.assessment.confidence == 0.3
    assert [item.description for item in result.assessment.missingEvidence] == [gap]
    assert not result.assessment.claims
    assert any(event.get("reasonCode") == "draft_verified" for event in events)
    assert "field:secret" not in result.agentExecution["exposed"]["sourceIds"]


@pytest.mark.parametrize("budget,read_count", [(AgentBudget(), 4), (AgentBudget(requests=3), 1)])
def test_read_limits_preserve_finalization(payload, budget, read_count):
    result, events, calls = run(payload, [[inspect("ingress")]] * read_count + [[
        finish(),
    ]], budget)
    assert {tool.name for tool in calls[-1][1].function_tools} == {"finish"}
    assert sum(event.get("reasonCode") == "snapshot_read_repeated" for event in events) == read_count - 1
    assert result.agentExecution["status"] == "partial"


def test_budget_exhaustion_keeps_the_last_accepted_draft(payload):
    calls, events = [], []

    async def model(_messages, info):
        calls.append(True)
        input_tokens = 100
        if len(calls) == 1:
            name, args = inspect("ingress")
        elif len(calls) == 2:
            name, args = finding("Backend is web.", "field:backend")
        else:
            name, args = verify("Backend is web.")
            input_tokens = 2001
        return ModelResponse(parts=[ToolCallPart(name, args)], usage=RequestUsage(input_tokens=input_tokens, output_tokens=100))

    async def sink(event):
        events.append(event)

    with pytest.raises(UsageLimitExceeded):
        asyncio.run(run_incremental_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(input_tokens=2000)))
    assert len(calls) == 3
    assert events[-1]["executionLimit"] == {"kind": "input_tokens", "used": 2201, "maximum": 2000}
    draft = next(event["draft"] for event in reversed(events) if event.get("draft"))
    assert draft["assessment"]["claims"][0]["statement"] == "Backend is web."
    assert not draft["hasConclusion"]


def test_repeated_finding_writes_advance_to_verified_finish(payload):
    backend = finding("Ingress routes to web.", "field:backend", finding_id="route")
    pod = finding("The Pod reports missing DB_HOST.", "log:crash", finding_id="config")
    result, events, calls = run(payload, [
        [inspect("ingress"), inspect("pod")], [backend, pod],
        [backend], [pod], [backend], [finish()],
    ])
    assert {tool.name for tool in calls[-1][1].function_tools} == {"finish"}
    assert [claim.id for claim in result.assessment.claims] == ["route", "config"]
    assert "routes to web" in result.assessment.summary and "DB_HOST" in result.assessment.summary
    assert [event["draft"]["revision"] for event in events if event["stage"] == "draft_updated"] == [1, 2, 3]
    assert result.agentExecution["status"] == "partial"


def test_verify_freezes_findings_even_with_another_write_in_the_same_response(payload):
    result, _, calls = run(payload, [
        [inspect("ingress")],
        [finding("Backend is web.", "field:backend", finding_id="backend"), verify(),
         finding("A late replacement.", "field:backend", finding_id="backend")],
        [finish()],
    ])
    assert {tool.name for tool in calls[-1][1].function_tools} == {"finish"}
    assert [claim.statement for claim in result.assessment.claims] == ["Backend is web."]


def test_native_bedrock_wire_separates_verification_and_finish(payload):
    client = boto3.client("bedrock-runtime", region_name="us-east-1", aws_access_key_id="test", aws_secret_access_key="test")

    def response(calls, turn):
        return {"output": {"message": {"role": "assistant", "content": [
            {"toolUse": {"toolUseId": f"{turn}-{i}", "name": name, "input": args}}
            for i, (name, args) in enumerate(calls)
        ]}}, "stopReason": "tool_use", "usage": {"inputTokens": 100, "outputTokens": 100, "totalTokens": 200},
                "metrics": {"latencyMs": 1}}

    async def sink(_event):
        pass

    with Stubber(client) as stub:
        stub.add_response("converse", response([inspect("ingress")], 1))
        stub.add_response("converse", response([
            finding("Backend is web.", "field:backend"), verify("Backend is web."),
        ], 2))
        stub.add_response("converse", response([finish()], 3))
        model = BedrockConverseModel("qwen.qwen3-32b-v1:0", provider=BedrockProvider(bedrock_client=client))
        result = asyncio.run(run_incremental_agent(request(payload), sink, model=model))
        stub.assert_no_pending_responses()
    assert result.assessment is not None
    assert result.agentExecution is not None
    assert result.assessment.summary == "Backend is web."
    assert result.agentExecution["requests"] == 3
