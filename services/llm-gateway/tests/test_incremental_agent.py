"""Offline integration tests for the four-tool snapshot investigation."""
import asyncio
import json

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
from tests.test_snapshot_agent import dependency_payload as dependency_payload, payload as payload, request

INVESTIGATION_TOOLS = {"inspect", "read_more", "record_finding", "verify"}
TOOLS = INVESTIGATION_TOOLS | {"finish"}


def inspect(node):
    return "inspect", {"resource_id": node}


def finding(statement, *sources, finding_id="", verdict="supported"):
    return "record_finding", {"statement": statement, "source_ids": list(sources), "finding_id": finding_id, "verdict": verdict}


def verify(summary="", **details):
    return "verify", {"summary": summary, **details}


def finish():
    return "finish", {}


def returned(messages, tool):
    part = next(part for part in messages[-1].parts if isinstance(part, ToolReturnPart) and part.tool_name == tool)
    return json.loads(part.content) if isinstance(part.content, str) else part.content


def run(payload, actions, budget=None):
    progress, calls = [], []

    async def model(messages, info):
        turn = len(calls)
        calls.append((messages, info))
        assert not info.output_tools
        assert {tool.name for tool in info.function_tools} <= TOOLS
        assert turn < len(actions), "unexpected extra model request"
        batch = actions[turn](messages) if callable(actions[turn]) else actions[turn]
        return ModelResponse(parts=[ToolCallPart(name, args, tool_call_id=f"{turn}-{i}") for i, (name, args) in enumerate(batch)],
                             usage=RequestUsage(input_tokens=100, output_tokens=100))

    async def sink(event):
        progress.append(event)

    result = asyncio.run(run_incremental_agent(request(payload), sink, model=FunctionModel(model), budget=budget))
    return result, progress, calls


def test_verification_and_finish_preserve_exact_citations_in_a_cascade(payload):
    def service(messages):
        overview = returned(messages, "inspect")
        assert overview["related"][0] == {"type": "routes_to", "state": "resolved", "origin": "observed", "resourceState": "present",
                                          "resourceId": "service", "direction": "out"}
        return [inspect("service")]

    result, events, calls = run(payload, [
        [inspect("ingress")], service, [inspect("pod")],
        [finding("The Pod reports missing DB_HOST.", "log:crash"),
         verify("The Pod reports missing DB_HOST.", remediation=["Check the expected DB_HOST configuration."])],
        [finish()],
    ])
    assert {tool.name for tool in calls[0][1].function_tools} == INVESTIGATION_TOOLS
    assert result.agentExecution["toolProtocol"] == "snapshot-verify-v1"
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
    assert {tool.name for tool in calls[-2][1].function_tools} == {"record_finding", "verify"}
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
    assert {tool.name for tool in calls[2][1].function_tools} == {"inspect", "record_finding", "verify"}
    assert len(result.assessment.claims) == 2
    assert "isolated" not in result.agentExecution["investigationGraph"]["nodeIds"]


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
    assert result.assessment.summary == "Backend is web."
    assert result.agentExecution["requests"] == 3
