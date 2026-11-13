"""Offline agent tests: use the actual PydanticAI loop, never a remote model."""
from __future__ import annotations

import asyncio
from copy import deepcopy
import json
from unittest.mock import patch

from fastapi.testclient import TestClient
import pytest
import boto3
from botocore.stub import Stubber
from pydantic_ai.messages import ModelResponse, ToolCallPart, TextPart
from pydantic_ai.exceptions import UnexpectedModelBehavior, UsageLimitExceeded
from pydantic_ai.models.function import FunctionModel
from pydantic_ai.models.bedrock import BedrockConverseModel
from pydantic_ai.providers.bedrock import BedrockProvider
from pydantic_ai.usage import RequestUsage

from app.agent import AgentBudget, make_agent_model, project_agent_citations, run_snapshot_agent, validate_agent_assessment
from app.models import AnalyzeRequest, AssessmentV2
from app.snapshot import SnapshotIndex, encoded
from main import app


@pytest.fixture
def payload():
    def resource(kind, name):
        return {"kind": kind, "namespace": "demo", "name": name}

    nodes = [{"id": identity, "type": "resource", "state": "present", "resource": resource(kind, name), "sourceIds": refs}
             for identity, kind, name, refs in [("ingress", "Ingress", "web", ["field:backend"]), ("service", "Service", "web", ["field:selector"]), ("pod", "Pod", "web-1", ["field:phase", "log:crash"]), ("isolated", "Deployment", "other", ["field:hidden"]), ("secret", "Secret", "passwords", ["field:secret", "field:exists"])]]
    edges = [{"id": identity, "from": source, "to": target, "type": kind, "state": "resolved", "origin": "observed", "sourceIds": refs}
             for identity, source, target, kind, refs in [("route", "ingress", "service", "routes_to", ["field:backend"]), ("select", "service", "pod", "selects", ["field:selector"]), ("uses", "pod", "secret", "uses_secret", ["field:exists"])]]
    def field(identity, kind, name, path, value):
        return {"id": identity, "type": "resource_field", "resource": resource(kind, name), "path": path, "value": value}
    sources = [field("field:backend", "Ingress", "web", "/backend", "web"), field("field:selector", "Service", "web", "/selector/app", "web"), field("field:phase", "Pod", "web-1", "/status/phase", "Running"), field("field:hidden", "Deployment", "other", "/replicas", 42), field("field:secret", "Secret", "passwords", "/data/password", "DO-NOT-EXPOSE"), field("field:exists", "Secret", "passwords", "/exists", True)]
    sources += [{"id": "log:crash", "type": "log_line", "resource": resource("Pod", "web-1"), "path": "/logs/main/current/1", "line": {"number": 1, "text": "required configuration DB_HOST is missing"}, "truncated": True}, {"id": "event:crash", "type": "event", "resource": resource("Pod", "web-1"), "event": {"reason": "BackOff", "message": "Back-off restarting failed container"}}]
    return {"schemaVersion": 3, "collectedAt": "2026-11-05T10:00:00Z", "rootResource": resource("Ingress", "web"), "graph": {"schemaVersion": 2, "entryResourceId": "ingress", "nodes": nodes, "edges": edges, "candidates": [], "policy": {}, "truncated": False}, "sources": sources}


def revealed(payload):
    index = SnapshotIndex(payload)
    index.bootstrap()
    index.neighbors("ingress")
    index.neighbors("service")
    index.observations("pod", "log_line")
    index.acknowledge_delivery()
    return index


def assessment(**changes):
    return AssessmentV2.model_validate({"summary": "Task failure requires more evidence", "confidence": 0.5, "claims": [], **changes})


def request(payload):
    return AnalyzeRequest(evidenceId="ev-test", diagnosticRunId="run-test", analysisRequestId="request-test", attemptId="attempt-test", analysisMode="snapshot-agent-v1", payload=payload)


@pytest.fixture
def dependency_payload():
    def ref(kind, name):
        return {"kind": kind, "name": name, "namespace": "demo"}
    refs = {"workload": ref("Deployment", "web"), "config": ref("ConfigMap", "web-config"),
            "secret": ref("Secret", "web-credentials"), "pvc": ref("PersistentVolumeClaim", "uploads"),
            "pod": ref("Pod", "web-1"), "class": {"apiGroup": "storage.k8s.io", "kind": "StorageClass", "name": "missing-class"}}
    sources, nodes, edges = [], [], []
    for number, (identity, kind, name, exists) in enumerate([
        ("config", "ConfigMap", "web-config", False), ("secret", "Secret", "web-credentials", False),
        ("pvc", "PersistentVolumeClaim", "uploads", True),
    ]):
        ids = []
        for field, value in {"exists": exists, "kind": kind, "name": name, "fieldPath": "/spec/template/spec"}.items():
            source_id = f"field:{identity}-{field}"
            ids.append(source_id)
            sources.append({"id": source_id, "type": "resource_field", "resource": refs["workload"],
                            "path": f"/dependencies/{number}/{field}", "value": value})
        nodes.append({"id": identity, "type": "resource" if exists else "reference", "state": "degraded" if exists else "missing",
                      "resource": refs[identity], "sourceIds": ids if not exists else []})
        edges.append({"id": f"edge:{identity}", "from": "workload", "to": identity, "type": "dependency",
                      "origin": "observed", "state": "resolved" if exists else "broken", "sourceIds": ids})
    for identity, owner, path, value in [
        ("field:pvc-phase", "pvc", "/phase", "Pending"), ("field:pvc-class", "pvc", "/storageClass", "missing-class"),
        ("field:class-exists", "pvc", "/storageClassExists", False), ("field:pod-phase", "pod", "/status/phase", "Pending"),
    ]:
        sources.append({"id": identity, "type": "resource_field", "resource": refs[owner], "path": path, "value": value})
    for owner, reason, message in [("pod", "FailedScheduling", "pod has unbound immediate PersistentVolumeClaims"),
                                    ("pvc", "ProvisioningFailed", 'storageclass.storage.k8s.io "missing-class" not found')]:
        sources.append({"id": f"event:{owner}", "type": "event", "resource": refs[owner],
                        "event": {"type": "Warning", "reason": reason, "message": message}})
    for i in range(15):
        sources.append({"id": f"field:aaa-metadata-{i}", "type": "resource_field", "resource": refs["workload"],
                        "path": f"/labels/label-{i}", "value": "metadata"})
    nodes += [{"id": identity, "type": "reference" if identity == "class" else "resource",
               "state": {"pod": "degraded", "class": "missing", "workload": "present"}[identity], "resource": refs[identity],
               "sourceIds": [s["id"] for s in sources if s["resource"] == refs[identity]]} for identity in ("workload", "pod", "class")]
    edges += [{"id": "edge:pod", "from": "workload", "to": "pod", "type": "manages", "state": "resolved", "origin": "observed", "sourceIds": []},
              {"id": "edge:class", "from": "pvc", "to": "class", "type": "uses_storage_class", "state": "broken", "origin": "observed",
               "sourceIds": ["field:pvc-class", "field:class-exists"]}]
    return {"schemaVersion": 3, "rootResource": refs["workload"], "sources": sources,
            "graph": {"schemaVersion": 2, "entryResourceId": "workload", "nodes": nodes, "edges": edges, "candidates": []}}


def test_describe_uses_stable_source_order_and_preserves_pagination(dependency_payload):
    index = SnapshotIndex(dependency_payload)
    index.bootstrap()
    first = index.observations("workload", "resource_field", limit=12)
    assert len(first["items"]) == 12
    expected = sorted(s["id"] for s in dependency_payload["sources"] if s["resource"] == dependency_payload["rootResource"])
    assert [s["id"] for s in first["items"]] == expected[:12]
    second = index.observations("workload", "resource_field", limit=25, cursor=first["nextCursor"])
    assert len(second["items"]) == 15
    assert not {s["id"] for s in first["items"]} & {s["id"] for s in second["items"]}
    assert [s["id"] for s in second["items"]] == expected[12:]
    assert not index.exposed.sourceIds


def test_missing_reference_describe_preserves_owner_provenance_and_delivery(dependency_payload):
    original = deepcopy(dependency_payload)
    index = SnapshotIndex(dependency_payload)
    index.bootstrap()
    with pytest.raises(ValueError):
        index.observations("config", "resource_field")
    index.neighbors("workload")
    for identity in ("config", "secret"):
        page = index.observations(identity, "resource_field")
        assert page["availability"] == "collected" and page["returnedCount"] == 4
        assert {s["resource"]["kind"] for s in page["items"]} == {"Deployment"}
        assert next(s["value"] for s in page["items"] if s["path"].endswith("/exists")) is False
    index.neighbors("pvc")
    page = index.observations("class", "resource_field")
    assert {s["id"] for s in page["items"]} == {"field:pvc-class", "field:class-exists"}
    assert {s["resource"]["kind"] for s in page["items"]} == {"PersistentVolumeClaim"}
    output = assessment(claims=[{"statement": "Configured StorageClass is absent", "verdict": "supported",
                                 "citations": [{"sourceId": "field:class-exists"}]}])
    with pytest.raises(ValueError, match="unread"):
        validate_agent_assessment(output, index)
    index.acknowledge_delivery()
    validate_agent_assessment(output, index)
    assert dependency_payload == original


@pytest.mark.parametrize("kind", ["Service", "ServiceAccount", "Node", "PersistentVolumeClaim"])
def test_reference_source_resolution_does_not_depend_on_resource_kind(payload, kind):
    owner, reference = deepcopy(payload["graph"]["nodes"][:2])
    reference.update(type="reference", state="missing", resource={"kind": kind, "name": "missing", "namespace": "demo"}, sourceIds=["field:backend"])
    payload["graph"].update(nodes=[owner, reference], candidates=[], edges=[{
        "id": "reference-edge", "from": owner["id"], "to": reference["id"], "origin": "observed",
        "type": "dependency", "state": "broken", "sourceIds": ["field:backend"],
    }])
    payload["sources"] = [s for s in payload["sources"] if s["id"] == "field:backend"]
    index = SnapshotIndex(payload)
    index.bootstrap()
    index.neighbors(owner["id"])
    result = index.observations(reference["id"], "resource_field")
    assert result["availability"] == "collected" and result["items"] == payload["sources"]
    assert result["items"][0]["resource"] == owner["resource"]


def test_reference_describe_excludes_secret_contents_and_inferred_or_hidden_edges(dependency_payload):
    nodes = {n["id"]: n for n in dependency_payload["graph"]["nodes"]}
    secret_value = {"id": "field:private", "type": "resource_field", "resource": nodes["secret"]["resource"],
                    "path": "/data/password", "value": "PRIVATE-SECRET"}
    proposed = {"id": "field:proposed", "type": "resource_field", "resource": nodes["workload"]["resource"],
                "path": "/hypothesis", "value": "PROPOSED-ONLY"}
    hidden = {**proposed, "id": "field:hidden-proof", "value": "HIDDEN-PROOF"}
    dependency_payload["sources"] += [secret_value, proposed, hidden]
    nodes["secret"]["sourceIds"].append("field:private")
    dependency_payload["graph"]["edges"] += [
        {"id": "edge:inferred", "from": "workload", "to": "secret", "origin": "inferred", "type": "possible_dependency", "sourceIds": ["field:proposed"]},
        {"id": "edge:hidden", "from": "config", "to": "secret", "origin": "observed", "type": "dependency", "sourceIds": ["field:hidden-proof"]},
    ]
    index = SnapshotIndex(dependency_payload)
    index.bootstrap()
    index.neighbors("workload")
    result = index.observations("secret", "resource_field")
    assert result["returnedCount"] == 4
    assert all(text not in encoded(result) for text in ("PRIVATE-SECRET", "PROPOSED-ONLY", "HIDDEN-PROOF"))


def test_offline_loop_can_read_and_cite_independent_dependency_blockers(dependency_payload):
    turns = []
    actions = [("get_graph_neighbors", {"node_id": "workload"}), ("get_resource", {"node_id": "workload", "path_prefix": "/dependencies/"}),
               ("get_events", {"node_id": "pod"}), ("get_resource", {"node_id": "pvc"}),
               ("get_events", {"node_id": "pvc"}), ("get_graph_neighbors", {"node_id": "pvc"}),
               ("get_resource", {"node_id": "class"})]
    async def model(messages, info):
        stage = len(turns)
        turns.append(True)
        if stage == 0:
            assert "no resource kind or detector predetermines the cause" in str(messages)
        if stage < len(actions):
            name, output = actions[stage]
        else:
            output = assessment(summary="Missing configuration and a PVC provisioning failure are independent blockers", claims=[
                {"id": "config", "statement": "The configured ConfigMap is absent", "verdict": "supported", "citations": [{"sourceId": "field:config-exists"}, {"sourceId": "field:config-name"}]},
                {"id": "secret", "statement": "The configured Secret is absent", "verdict": "supported", "citations": [{"sourceId": "field:secret-exists"}, {"sourceId": "field:secret-name"}]},
                {"id": "storage", "statement": "PVC provisioning failed because its StorageClass is absent", "verdict": "supported", "citations": [{"sourceId": "event:pvc"}, {"sourceId": "field:class-exists"}]},
                {"id": "scheduling", "statement": "The Pod cannot schedule with its unbound PVC", "verdict": "supported", "citations": [{"sourceId": "event:pod"}]},
            ]).model_dump()
            name = info.output_tools[0].name
        return ModelResponse(parts=[ToolCallPart(name, output)], usage=RequestUsage(input_tokens=100, output_tokens=100))
    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(dependency_payload), sink, model=FunctionModel(model)))
    assert result.assessment is not None and result.agentExecution is not None
    assert len(result.assessment.claims) == 4 and result.agentExecution["requests"] == 8
    assert result.agentExecution["status"] == "completed"
    assert result.assessment.claims[2].citations[0].quote == 'storageclass.storage.k8s.io "missing-class" not found'


def test_bootstrap_excludes_graph_and_unread_values(payload):
    original = deepcopy(payload)
    index = SnapshotIndex(payload)
    start = index.bootstrap()
    assert [node["id"] for node in start["startNodes"]] == ["ingress"]
    assert "DO-NOT-EXPOSE" not in encoded(start)
    assert "DB_HOST" not in encoded(start)
    assert not index.exposed.nodeIds
    index.acknowledge_delivery()
    assert index.exposed.nodeIds == {"ingress"}
    assert not index.exposed.sourceIds
    assert payload == original


def test_progressive_neighbors_preserve_origin_and_proposals(payload):
    payload["graph"]["edges"][1].update(origin="inferred", strength="possible", score=50)
    payload["graph"]["candidates"] = [{"id": "candidate", "sourceNodeId": "ingress", "referenceNodeId": "service", "candidateNodeId": "isolated", "strength": "possible", "score": 50, "reasons": [{"sourceIds": ["field:hidden"]}]}]
    index = SnapshotIndex(payload)
    index.bootstrap()
    with pytest.raises(ValueError):
        index.observations("pod", "log_line")
    first = index.neighbors("ingress")
    assert len(first["items"]) == 2
    assert index.neighbors("service")["items"][1]["edge"]["origin"] == "inferred"
    assert not index.exposed.sourceIds
    index.acknowledge_delivery()
    assert {"route", "select"} <= index.exposed.edgeIds
    assert index.exposed.candidateIds == {"candidate"}
    assert "field:hidden" not in index.exposed.sourceIds


def test_cursor_is_stable_and_scoped_to_snapshot_and_query(payload):
    index = revealed(payload)
    page = index.neighbors("service", limit=1)
    assert page["nextCursor"]
    assert index.neighbors("service", limit=1)["nextCursor"] == page["nextCursor"]
    next_page = index.neighbors("service", cursor=page["nextCursor"], limit=1)
    assert page["items"][0]["id"] != next_page["items"][0]["id"]
    for args in [{"node_id": "pod", "cursor": page["nextCursor"]}, {"node_id": "service", "cursor": "invalid"}]:
        with pytest.raises(ValueError):
            index.neighbors(**args)
    changed = deepcopy(payload)
    changed["sources"][0]["value"] = "different"
    with pytest.raises(ValueError):
        revealed(changed).neighbors("service", cursor=page["nextCursor"])


def test_missing_logs_and_no_matches_are_distinct(payload):
    index = revealed(payload)
    assert index.observations("service", "log_line")["availability"] == "not_collected"
    no_match = index.observations("pod", "log_line", contains="not present")
    assert no_match["availability"] == "collected" and no_match["matchingCount"] == 0
    assert no_match["truncated"]
    previous = index.observations("pod", "log_line", previous=True)
    assert previous["matchingCount"] == 0 and previous["availability"] == "not_collected"


def test_secret_values_remain_unavailable(payload):
    index = revealed(payload)
    index.neighbors("pod")
    fields = index.observations("secret", "resource_field")
    assert "DO-NOT-EXPOSE" not in encoded(fields)
    assert [s["id"] for s in fields["items"]] == ["field:exists"]
    with pytest.raises(ValueError):
        index.read_sources(["field:secret"])


def test_atomic_source_is_never_silently_clipped(payload):
    payload["sources"][0]["value"] = "x" * 20_000
    index = SnapshotIndex(payload)
    index.bootstrap()
    result = index.read_sources(["field:backend"])
    assert result["items"] == [] and result["omittedSourceIds"] == ["field:backend"]
    index.acknowledge_delivery()
    assert "field:backend" not in index.exposed.sourceIds


@pytest.mark.parametrize("change", ["missing_entry", "duplicate_source", "dangling_edge", "no_sources"])
def test_invalid_snapshot_is_rejected(payload, change):
    if change == "missing_entry": payload["graph"]["entryResourceId"] = "unknown"
    if change == "duplicate_source": payload["sources"].append(payload["sources"][0])
    if change == "dangling_edge": payload["graph"]["edges"][0]["to"] = "unknown"
    if change == "no_sources": payload["sources"] = []
    with pytest.raises(ValueError): SnapshotIndex(payload)


@pytest.mark.parametrize("source_id", ["field:hidden", "field:selector", "field:invented", "s0"])
def test_rejects_existing_but_unread_and_unknown_citations(payload, source_id):
    index = revealed(payload)
    output = assessment(claims=[{"id": "c1", "statement": "Claim", "verdict": "supported", "citations": [{"sourceId": source_id}]}])
    with pytest.raises(ValueError, match="unread evidence"):
        validate_agent_assessment(output, index)


def test_exact_quotes_and_empty_cause(payload):
    index = revealed(payload)
    output = assessment(claims=[{"id": "c1", "statement": "Missing configuration", "verdict": "supported", "citations": [{"sourceId": "log:crash", "quote": "DB_HOST is missing"}]}])
    validate_agent_assessment(output, index)
    output.claims[0].citations[0].quote = "database is down"
    with pytest.raises(ValueError, match="quote"):
        validate_agent_assessment(output, index)


def test_snapshot_excerpt_projection_is_exact_audited_and_immutable(payload):
    index = revealed(payload)
    index.observations("pod", "resource_field")
    index.observations("pod", "event")
    index.acknowledge_delivery()
    output = assessment(
        claims=[{"statement": "Missing configuration", "verdict": "supported", "citations": [{"sourceId": "log:crash", "quote": "PRIVATE-MODEL-QUOTE"}]}],
        alternativeHypotheses=[{"statement": "Alternative", "verdict": "partially_supported", "citations": [{"sourceId": "field:phase", "quote": "not the phase"}]}],
        remediation=[{"order": 1, "title": "Inspect", "citations": [{"sourceId": "event:crash", "quote": "not the event"}]}])
    original, original_snapshot = output.model_dump(), deepcopy(index.payload)
    projected, audit = project_agent_citations(output, index)
    assert projected.claims[0].citations[0].quote == "required configuration DB_HOST is missing"
    assert projected.alternativeHypotheses[0].citations[0].quote == "Running"
    assert projected.remediation[0].citations[0].quote == "Back-off restarting failed container"
    assert audit == {"policy": "snapshot-excerpt-v1", "maxChars": 512, "count": 3, "ignoredModelQuotes": 3}
    assert output.model_dump() == original and index.payload == original_snapshot
    validate_agent_assessment(projected, index)


@pytest.mark.parametrize("value", [False, 0, {"enabled": False, "offset": 9007199254740993}, "界" * 600, None])
def test_snapshot_excerpt_projection_handles_atomic_values_and_exact_prefixes(payload, value):
    payload["sources"][0]["value"] = value
    index = SnapshotIndex(payload)
    index.bootstrap()
    index.observations("ingress", "resource_field")
    index.acknowledge_delivery()
    output = assessment(claims=[{"statement": "Collected field", "verdict": "supported", "citations": [{"sourceId": "field:backend"}]}])
    projected, _audit = project_agent_citations(output, index)
    expected = "" if value is None else value if isinstance(value, str) else encoded(value)
    assert projected.claims[0].citations[0].quote == expected[:512]
    validate_agent_assessment(projected, index)


@pytest.mark.parametrize("source_id", ["field:hidden", "field:selector", "field:invented", "field:secret"])
def test_snapshot_projection_cannot_hydrate_unread_unknown_or_secret_values(payload, source_id):
    index = revealed(payload)
    output = assessment(claims=[{"statement": "Untrusted", "verdict": "supported", "citations": [{"sourceId": source_id, "quote": "invented"}]}])
    with pytest.raises(ValueError, match="unread evidence"):
        project_agent_citations(output, index)


def test_model_quote_cannot_consume_a_repair_or_become_stored_evidence(payload, caplog):
    turns, progress = [], []
    async def model(_messages, info):
        turns.append(True)
        if len(turns) == 1:
            return ModelResponse(parts=[ToolCallPart("get_resource", {"node_id": "ingress"})])
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, assessment(claims=[{"statement": "The backend is web", "verdict": "supported", "citations": [{"sourceId": "field:backend", "quote": "PRIVATE-MODEL-QUOTE"}]}]).model_dump())])
    async def sink(event): progress.append(event)
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    assert result.assessment is not None and result.agentExecution is not None
    assert result.assessment.claims[0].citations[0].quote == "web" and len(turns) == 2
    assert result.agentExecution["citationQuotes"]["ignoredModelQuotes"] == 1
    assert result.agentExecution["validationIssues"] == []
    assert "PRIVATE-MODEL-QUOTE" not in caplog.text and "PRIVATE-MODEL-QUOTE" not in encoded(progress)
    assert any(event.get("reasonCode") == "snapshot_quotes" for event in progress)


def test_causal_overlay_requires_coherent_seen_relationships_and_root_evidence(payload):
    index = revealed(payload)
    output = assessment(claims=[{"id": "c1", "statement": "Pod lacks configuration", "verdict": "supported", "citations": [{"sourceId": "log:crash"}]}], causalPath={"entryResourceId": "ingress", "rootCauseEntityId": "pod", "steps": [{"order": 1, "entityId": "pod", "edgeId": "select", "claimId": "c1", "role": "root_cause"}, {"order": 2, "entityId": "service", "edgeId": "route"}, {"order": 3, "entityId": "ingress"}]})
    validate_agent_assessment(output, index)
    assert output.causalPath is not None
    output.causalPath.steps[0].edgeId = "route"
    with pytest.raises(ValueError, match="adjacent"):
        validate_agent_assessment(output, index)


def test_real_pydantic_loop_expands_graph_and_reads_logs(payload):
    calls, progress = [], []
    async def model(messages, info):
        stage = len(calls)
        calls.append(messages)
        actions = [("get_graph_neighbors", {"node_id": "ingress"}), ("get_graph_neighbors", {"node_id": "service"}), ("get_logs", {"node_id": "pod"})]
        if stage < len(actions):
            name, arguments = actions[stage]
        else:
            name, arguments = info.output_tools[0].name, assessment(claims=[{"id": "c1", "statement": "Pod logs report missing configuration", "verdict": "supported", "citations": [{"sourceId": "log:crash"}]}]).model_dump()
        return ModelResponse(parts=[ToolCallPart(name, arguments, tool_call_id=f"call-{stage}")], usage=RequestUsage(input_tokens=100, output_tokens=20))

    async def sink(event): progress.append(event)
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    assert result.assessment is not None and result.tokenUsage is not None and result.agentExecution is not None and result.prompt is not None
    assert result.assessmentVersion == 2 and result.assessment.claims[0].citations[0].sourceId == "log:crash"
    assert result.tokenUsage.inputTokens == 400 and result.tokenUsage.outputTokens == 80
    assert result.agentExecution["exposed"]["nodeIds"] == ["ingress", "pod", "service"]
    assert result.agentExecution["exposed"]["sourceIds"] == ["log:crash"]
    assert len(result.agentExecution["contextJournal"]) == 4
    assert "DB_HOST" not in result.prompt
    assert [event["stage"] for event in progress].count("model_request") == 4
    log_events = [event for event in progress if event["stage"] == "get_logs"]
    assert log_events[0]["target"] == {"nodeId": "pod", "resource": {"kind": "Pod", "namespace": "demo", "name": "web-1"}}
    assert log_events[1]["availability"] == "collected" and log_events[1]["returnedCount"] == 1
    assert "DB_HOST" not in encoded(progress) and "DO-NOT-EXPOSE" not in encoded(progress)
    assert payload["graph"]["nodes"][3]["id"] == "isolated"


def test_agent_does_not_fall_back_to_legacy_after_unread_citations(payload):
    async def model(_messages, info):
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, assessment(claims=[{"statement": "Hidden fact", "verdict": "supported", "citations": [{"sourceId": "field:hidden"}]}]).model_dump())])
    with patch("app.agent.make_agent_model", return_value=FunctionModel(model)):
        response = TestClient(app).post("/analyze", json=request(payload).model_dump())
    assert response.status_code == 502
    assert "Hidden fact" not in response.text


def test_complete_json_text_is_validated_as_assessment_v2(payload):
    async def model(_messages, _info):
        return ModelResponse(parts=[TextPart(assessment().model_dump_json())])
    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    assert result.assessment is not None and result.citationValidation is not None
    assert result.assessmentVersion == 2 and result.citationValidation.valid


def test_json_text_after_resource_read_keeps_exact_citations(payload):
    turns = []
    async def model(_messages, _info):
        turns.append(True)
        if len(turns) == 1:
            return ModelResponse(parts=[ToolCallPart("get_resource", {"node_id": "ingress"})])
        return ModelResponse(parts=[TextPart(assessment(claims=[{
            "id": "c1", "statement": "The configured backend is web", "verdict": "supported",
            "citations": [{"sourceId": "field:backend", "quote": "web"}],
        }]).model_dump_json())])
    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    assert result.assessment is not None and result.agentExecution is not None
    assert len(turns) == 2
    assert result.assessment.claims[0].citations[0].sourceId == "field:backend"
    assert result.agentExecution["exposed"]["sourceIds"] == ["field:backend"]


@pytest.mark.parametrize("failure", ["json", "schema", "unread", "graph"])
def test_json_text_failures_stay_strict_and_have_safe_diagnostics(payload, failure, caplog):
    progress, turns = [], []
    output = assessment(claims=[{"id": "c1", "statement": "PRIVATE-MODEL-CONTENT", "verdict": "supported", "citations": [{"sourceId": "field:backend"}]}]).model_dump()
    if failure == "schema": output["claims"][0]["verdict"] = "PRIVATE-MODEL-CONTENT"
    if failure == "unread": output["claims"][0]["citations"][0]["sourceId"] = "field:hidden"
    if failure == "graph": output["causalPath"] = {"entryResourceId": "ingress", "rootCauseEntityId": "isolated", "steps": [{"order": 1, "entityId": "isolated"}]}
    text = "PRIVATE-MODEL-CONTENT is not JSON" if failure == "json" else json.dumps(output)
    async def model(_messages, _info):
        turns.append(True)
        if len(turns) == 1:
            return ModelResponse(parts=[ToolCallPart("get_resource", {"node_id": "ingress"})])
        return ModelResponse(parts=[TextPart(text)])
    async def sink(event): progress.append(event)
    with pytest.raises(UnexpectedModelBehavior):
        asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    expected = {"json": "output_json_invalid", "schema": "output_schema_invalid", "unread": "unread_citation", "graph": "invalid_causal_path"}[failure]
    assert any(event.get("reasonCode") == expected for event in progress)
    assert expected in caplog.text and "PRIVATE-MODEL-CONTENT" not in caplog.text
    if failure == "schema":
        rejection = next(event for event in progress if event.get("reasonCode") == expected)
        assert rejection["validationFields"] == [{"path": "claims.0.verdict", "kind": "literal_error"}]
    assert len(turns) == 4


def test_native_output_schema_failure_has_safe_diagnostics(payload, caplog):
    progress = []
    async def model(_messages, info):
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, {"summary": "PRIVATE-MODEL-CONTENT"})])
    async def sink(event): progress.append(event)
    with pytest.raises(UnexpectedModelBehavior):
        asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    assert any(event.get("reasonCode") == "output_schema_invalid" for event in progress)
    assert "output_schema_invalid" in caplog.text and "PRIVATE-MODEL-CONTENT" not in caplog.text


@pytest.mark.parametrize("failure", ["unread", "graph", "missing_citations"])
def test_output_repair_receives_actionable_feedback_and_keeps_integrity(payload, failure):
    turns, feedback = [], []
    valid = assessment(claims=[{"id": "c1", "statement": "Backend is web", "verdict": "supported", "citations": [{"sourceId": "field:backend"}]}]).model_dump()
    invalid = deepcopy(valid)
    if failure == "unread": invalid["claims"][0]["citations"][0]["sourceId"] = "field:invented"
    if failure == "graph": invalid["causalPath"] = {"entryResourceId": "ingress", "rootCauseEntityId": "isolated", "steps": [{"order": 1, "entityId": "isolated"}]}
    if failure == "missing_citations": invalid["claims"][0]["citations"] = []
    async def model(messages, info):
        turns.append(True)
        if len(turns) == 1:
            return ModelResponse(parts=[ToolCallPart("get_resource", {"node_id": "ingress"})])
        if len(turns) == 3:
            feedback.append(str(messages[-1]))
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, invalid if len(turns) == 2 else valid)])
    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    assert result.assessment is not None and result.agentExecution is not None
    assert result.assessment.claims[0].citations[0].sourceId == "field:backend"
    assert len(result.agentExecution["validationIssues"]) == 1 and len(turns) == 3
    expected = {"unread": "Delivered source IDs", "graph": "causalPath=null", "missing_citations": "claims.0.citations"}[failure]
    assert expected in feedback[0]


@pytest.mark.parametrize("failure", ["summary_then_citations", "announcement_then_comment"])
def test_late_output_errors_fit_reserved_finalization_turns(payload, failure):
    turns, final_attempts, repair_feedback = [], [], []
    actions = [("get_resource", {"node_id": "ingress"}),
               ("get_graph_neighbors", {"node_id": "ingress"}),
               ("get_resource", {"node_id": "service"}),
               ("get_graph_neighbors", {"node_id": "service"}),
               ("get_resource", {"node_id": "pod"})]
    async def model(messages, info):
        turns.append(bool(info.function_tools))
        if info.function_tools:
            name, arguments = actions[len(turns) - 1]
            return ModelResponse(parts=[ToolCallPart(name, arguments)])
        final_attempts.append(True)
        assert "Return the complete AssessmentV2 now" in str(messages[-1])
        output = assessment(claims=[{"id": "c1", "statement": "The backend is web", "verdict": "supported", "citations": [{"sourceId": "field:backend"}]}],
                            nodeInsights=[{"nodeId": "ingress", "relevance": "relevant", "claimIds": ["c1"]}]).model_dump()
        output["nodeInsights"][0].pop("comment")
        if failure == "announcement_then_comment" and len(final_attempts) == 1:
            return ModelResponse(parts=[TextPart("I will now write the assessment.")])
        if failure == "summary_then_citations":
            if len(final_attempts) == 1:
                output.pop("summary")
            elif len(final_attempts) == 2:
                output["claims"][0]["citations"] = []
                repair_feedback.append(str(messages[-1]))
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, output)])
    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(requests=8)))
    assert result.assessment is not None and result.agentExecution is not None
    assert len(turns) == (8 if failure == "summary_then_citations" else 7)
    assert turns == [True] * 5 + [False] * len(final_attempts)
    assert result.assessment.nodeInsights[0].comment == ""
    assert result.assessment.claims[0].citations[0].sourceId == "field:backend"
    if repair_feedback:
        assert "summary" in repair_feedback[0] and "supported/partially_supported" in repair_feedback[0]


def test_request_exhaustion_does_not_emit_a_nonexistent_model_turn(payload):
    progress, turns = [], []
    async def model(_messages, info):
        turns.append(True)
        if info.function_tools:
            return ModelResponse(parts=[ToolCallPart("get_resource", {"node_id": "ingress"})])
        output = assessment(claims=[{"statement": "Uncited claim", "verdict": "supported"}]).model_dump()
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, output)])
    async def sink(event): progress.append(event)
    with pytest.raises(UsageLimitExceeded):
        asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(requests=3)))
    assert len(turns) == 3
    assert [event["turn"] for event in progress if event["stage"] == "model_request"] == [1, 2, 3]
    assert progress[-1]["executionLimit"] == {"kind": "requests", "used": 3, "maximum": 3}


@pytest.mark.parametrize("exhaust", [False, True])
def test_40_request_budget_allows_37_reads_and_finalization_through_request_40(payload, exhaust):
    turns, progress = [], []
    for i in range(37):
        payload["sources"].append({"id": f"field:check-{i}", "type": "resource_field",
                                   "resource": payload["rootResource"], "path": f"/checks/{i}/value", "value": i})

    async def model(_messages, info):
        turn = len(turns)
        turns.append(bool(info.function_tools))
        if info.function_tools:
            name, output = "get_resource", {"node_id": "ingress", "path_prefix": f"/checks/{turn}/"}
        else:
            output = assessment(claims=[{"statement": "Check 0 has value zero", "verdict": "supported",
                                         "citations": [{"sourceId": "field:check-0"}]}]).model_dump()
            if turn == 37:
                output.pop("summary")
            elif turn == 38:
                output["claims"][0]["citations"] = []
            name = info.output_tools[0].name
            if exhaust and turn == 39:
                name, output = "get_resource", {"node_id": "ingress"}
        return ModelResponse(parts=[ToolCallPart(name, output)],
                             usage=RequestUsage(input_tokens=6000 + turn * 125, output_tokens=100))

    async def sink(event): progress.append(event)
    if exhaust:
        with pytest.raises(UsageLimitExceeded):
            asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(requests=40)))
        assert progress[-1]["executionLimit"] == {"kind": "requests", "used": 40, "maximum": 40}
    else:
        result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(requests=40)))
        assert result.agentExecution is not None and result.assessment is not None and result.tokenUsage is not None
        assert result.agentExecution["requests"] == 40 and result.agentExecution["toolCalls"] == 37
        assert result.agentExecution["status"] == "partial"
        assert result.tokenUsage.inputTokens == 337_500
        assert result.assessment.claims[0].citations[0].quote == "0"
    assert turns == [True] * 37 + [False] * 3
    assert [event["turn"] for event in progress if event["stage"] == "model_request"] == list(range(1, 41))


@pytest.mark.parametrize("kind", ["input_tokens", "output_tokens"])
def test_token_limit_progress_identifies_the_limit_without_raw_sdk_errors(payload, kind):
    progress = []
    async def model(_messages, info):
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, assessment().model_dump())],
                             usage=RequestUsage(input_tokens=100, output_tokens=100))
    async def sink(event): progress.append(event)
    budget = AgentBudget(input_tokens=50 if kind == "input_tokens" else 1000,
                         output_tokens=50 if kind == "output_tokens" else 1000)
    with pytest.raises(UsageLimitExceeded):
        asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=budget))
    assert progress[-1]["executionLimit"] == {"kind": kind, "used": 100, "maximum": 50}


def test_missing_display_comment_does_not_bypass_evidence_checks(payload):
    index = revealed(payload)
    output = assessment(nodeInsights=[{"nodeId": "service", "relevance": "relevant"}])
    with pytest.raises(ValueError, match="require evidence"):
        validate_agent_assessment(output, index)
    output = assessment(nodeInsights=[{"nodeId": "service", "relevance": "not_relevant"}])
    with pytest.raises(ValueError, match="uninspected"):
        validate_agent_assessment(output, index)


def test_activity_distinguishes_uncollected_previous_logs(payload):
    payload["graph"]["entryResourceId"] = "pod"
    progress, turns = [], []
    async def model(_messages, info):
        turns.append(True)
        if len(turns) == 1:
            return ModelResponse(parts=[ToolCallPart("get_logs", {"node_id": "pod", "previous": True})])
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, assessment().model_dump())])
    async def sink(event): progress.append(event)
    asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
    event = next(event for event in progress if event["stage"] == "get_logs" and event["phase"] == "completed")
    assert event["previousLogs"] and event["availability"] == "not_collected"
    assert event["returnedCount"] == 0 and event["target"]["nodeId"] == "pod"


def test_stream_has_server_events_and_one_validated_result(payload):
    turns = []
    async def model(_messages, info):
        turns.append(True)
        if len(turns) == 1:
            return ModelResponse(parts=[ToolCallPart("inspect", {"resource_id": "ingress"})])
        if len(turns) == 3:
            return ModelResponse(parts=[ToolCallPart("finish", {})])
        return ModelResponse(parts=[
            ToolCallPart("record_finding", {"statement": "Ingress routes to web.", "source_ids": ["field:backend"]}),
            ToolCallPart("verify", {"summary": "Ingress routes to web.", "gaps": ["The downstream cause remains inconclusive."]}),
        ])
    with patch("app.agent.make_agent_model", return_value=FunctionModel(model)):
        response = TestClient(app).post("/analyze/stream", json=request(payload).model_dump())
    frames = [json.loads(line) for line in response.text.splitlines()]
    assert response.status_code == 200
    events = [frame["event"] for frame in frames if frame["type"] == "progress"]
    assert [event["producerSeq"] for event in events] == list(range(1, len(events) + 1))
    assert all(event["runId"] == "run-test" and event["attemptId"] == "attempt-test" for event in events)
    assert len([frame for frame in frames if frame["type"] == "result"]) == 1
    assert frames[-1]["result"]["assessmentVersion"] == 2
    assert frames[-1]["result"]["assessment"]["claims"][0]["citations"][0]["quote"] == "web"


def test_timeout_stops_agent_without_assessment(payload):
    async def model(_messages, _info):
        await asyncio.sleep(1)
        return ModelResponse(parts=[])
    async def sink(_event): pass
    with pytest.raises(TimeoutError):
        asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(seconds=0.01)))


def test_provider_credentials_are_call_local_and_retries_bounded(payload):
    req = request(payload)
    req.credentials = {"modelId": "qwen3-32b", "accessKeyId": "test-access", "secretAccessKey": "test-secret", "region": "eu-central-1"}
    with patch("app.agent.boto3.Session") as session:
        model = make_agent_model(req)
    assert model.model_name == "qwen.qwen3-32b-v1:0"
    assert session.call_args.kwargs["aws_access_key_id"] == "test-access"
    assert session.return_value.client.call_args.kwargs["config"].retries["total_max_attempts"] == 1


@pytest.mark.parametrize("budget", [AgentBudget(requests=3), AgentBudget(tool_calls=1), AgentBudget()])
def test_budgets_and_repeated_reads_force_a_partial_final_answer(payload, budget):
    async def model(_messages, info):
        if info.function_tools:
            return ModelResponse(parts=[ToolCallPart("get_resource", {"node_id": "ingress"})])
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, assessment().model_dump())])
    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=budget))
    assert result.agentExecution is not None
    assert result.agentExecution["status"] == "partial"
    assert result.agentExecution["requests"] <= budget.requests
    assert result.agentExecution["toolCalls"] <= min(3, budget.tool_calls)


@pytest.mark.parametrize("pressure", ["input", "output"])
@pytest.mark.parametrize("repair", [False, True])
def test_token_pressure_reserves_a_final_answer_and_one_repair(payload, pressure, repair):
    turns, final_attempts = [], []
    # Keep exercising token pressure independently of the production defaults.
    budget = AgentBudget(input_tokens=48_000, output_tokens=12_000, per_request_output=4_000)
    actions = [("get_resource", {"node_id": "ingress"}),
               ("get_graph_neighbors", {"node_id": "ingress"}),
               ("get_resource", {"node_id": "service"}),
               ("get_graph_neighbors", {"node_id": "service"}),
               ("get_resource", {"node_id": "pod"}),
               ("get_logs", {"node_id": "pod"})]

    async def model(_messages, info):
        turn = len(turns)
        turns.append(bool(info.function_tools))
        if info.function_tools and turn < len(actions):
            name, arguments = actions[turn]
        else:
            final_attempts.append(turn)
            citation = "field:invented" if repair and len(final_attempts) == 1 else "field:backend"
            name, arguments = info.output_tools[0].name, assessment(claims=[{
                "id": "c1", "statement": "Ingress backend is configured", "verdict": "supported",
                "citations": [{"sourceId": citation}],
            }]).model_dump()
        usage = RequestUsage(input_tokens=6000 + turn * 1000 if pressure == "input" else 100,
                             output_tokens=2500 if pressure == "output" else 20)
        return ModelResponse(parts=[ToolCallPart(name, arguments, tool_call_id=f"call-{turn}")], usage=usage)

    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=budget))
    assert result.agentExecution is not None and result.tokenUsage is not None
    assert result.agentExecution["status"] == "partial"
    assert result.tokenUsage.inputTokens <= budget.input_tokens
    assert result.tokenUsage.outputTokens <= budget.output_tokens
    assert len(final_attempts) == (2 if repair else 1)
    assert not any(turns[final_attempts[0]:])


def test_request_pressure_reserves_output_repair(payload):
    final_attempts = []

    async def model(_messages, info):
        if info.function_tools:
            return ModelResponse(parts=[ToolCallPart("get_resource", {"node_id": "ingress"})])
        final_attempts.append(True)
        citation = "field:invented" if len(final_attempts) == 1 else "field:backend"
        return ModelResponse(parts=[ToolCallPart(info.output_tools[0].name, assessment(claims=[{
            "statement": "Ingress backend is configured", "verdict": "supported",
            "citations": [{"sourceId": citation}],
        }]).model_dump())])

    async def sink(_event): pass
    result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(requests=3)))
    assert result.agentExecution is not None
    assert result.agentExecution["status"] == "partial"
    assert result.agentExecution["requests"] == 3
    assert len(final_attempts) == 2


@pytest.mark.parametrize("closed_calls", [1, 2, 3])
def test_closed_read_tool_can_be_corrected_without_executing_it(payload, closed_calls):
    progress, turns = [], []
    actions = [("get_resource", {"node_id": "ingress"}), ("get_graph_neighbors", {"node_id": "ingress"}),
               ("get_resource", {"node_id": "service"}), ("get_graph_neighbors", {"node_id": "service"}),
               ("get_resource", {"node_id": "pod"})]
    async def model(_messages, info):
        turns.append(bool(info.function_tools))
        if len(turns) <= len(actions):
            name, arguments = actions[len(turns) - 1]
        elif len(turns) <= len(actions) + closed_calls:
            name, arguments = "get_logs", {"node_id": "pod"}
        else:
            name, arguments = info.output_tools[0].name, assessment(claims=[{"statement": "Backend is web", "verdict": "supported", "citations": [{"sourceId": "field:backend"}]}]).model_dump()
        return ModelResponse(parts=[ToolCallPart(name, arguments)])
    async def sink(event): progress.append(event)
    if closed_calls == 3:
        with pytest.raises(UnexpectedModelBehavior):
            asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(requests=8)))
    else:
        result = asyncio.run(run_snapshot_agent(request(payload), sink, model=FunctionModel(model), budget=AgentBudget(requests=8)))
        assert result.agentExecution is not None and result.assessment is not None
        assert result.agentExecution["requests"] == 6 + closed_calls
        assert result.agentExecution["toolCalls"] == 5
        assert "log:crash" not in result.agentExecution["exposed"]["sourceIds"]
        assert result.assessment.claims[0].citations[0].sourceId == "field:backend"
    assert len(turns) <= 8 and turns == [True] * 5 + [False] * (len(turns) - 5)
    assert not any(event["stage"] == "get_logs" for event in progress)
    assert sum(event.get("reasonCode") == "unavailable_tool" for event in progress) == closed_calls


def test_pending_values_cannot_authorize_citations(payload):
    index = SnapshotIndex(payload)
    index.bootstrap()
    index.acknowledge_delivery()
    index.read_sources(["field:backend"])
    output = assessment(claims=[{"statement": "Configured backend", "verdict": "supported", "citations": [{"sourceId": "field:backend"}]}])
    with pytest.raises(ValueError): validate_agent_assessment(output, index)
    index.acknowledge_delivery()
    validate_agent_assessment(output, index)


def test_cancellation_exits_without_more_tools_or_output(payload):
    started = asyncio.Event()
    async def model(_messages, _info):
        started.set()
        await asyncio.sleep(10)
        return ModelResponse(parts=[])
    async def sink(_event): pass
    async def scenario():
        task = asyncio.create_task(run_snapshot_agent(request(payload), sink, model=FunctionModel(model)))
        await started.wait()
        task.cancel()
        with pytest.raises(asyncio.CancelledError): await task
    asyncio.run(scenario())


def test_stream_provider_errors_are_sanitized(payload):
    with patch("app.agent.make_agent_model", side_effect=RuntimeError("SECRET-DO-NOT-EXPOSE")):
        response = TestClient(app).post("/analyze/stream", json=request(payload).model_dump())
    frames = [json.loads(line) for line in response.text.splitlines()]
    assert frames[-1] == {"type": "error", "code": "provider_error"}
    assert "SECRET-DO-NOT-EXPOSE" not in response.text


def test_context_byte_budget_is_enforced_before_delivery(payload):
    index = SnapshotIndex(payload)
    index.max_context_bytes = 1
    with pytest.raises(ValueError, match="context budget"):
        index.bootstrap()
    assert index.journal == [] and index.exposed.nodeIds == set()


def test_native_bedrock_adapter_round_trip_without_network(payload):
    client = boto3.client("bedrock-runtime", region_name="us-east-1", aws_access_key_id="test", aws_secret_access_key="test")
    def response(name, arguments, number):
        return {"output": {"message": {"role": "assistant", "content": [{"toolUse": {"toolUseId": f"tool-{number}", "name": name, "input": arguments}}]}}, "stopReason": "tool_use", "usage": {"inputTokens": 100, "outputTokens": 20, "totalTokens": 120}, "metrics": {"latencyMs": 1}}
    output = assessment(claims=[{"statement": "The configured backend is web", "verdict": "supported", "citations": [{"sourceId": "field:backend", "quote": "web"}]}])
    async def sink(_event): pass
    with Stubber(client) as stub:
        stub.add_response("converse", response("get_resource", {"node_id": "ingress"}, 1))
        stub.add_response("converse", response("final_result", output.model_dump(), 2))
        result = asyncio.run(run_snapshot_agent(request(payload), sink, model=BedrockConverseModel("qwen.qwen3-32b-v1:0", provider=BedrockProvider(bedrock_client=client))))
        stub.assert_no_pending_responses()
    assert result.assessment is not None and result.tokenUsage is not None
    assert result.assessment.claims[0].citations[0].sourceId == "field:backend"
    assert result.tokenUsage.totalTokens == 240


def test_empty_healthy_path_remains_compatible_and_nonempty_null_path_fails():
    healthy = assessment(causalPath={"rootCauseEntityId": None, "steps": []})
    assert healthy.causalPath is None
    with pytest.raises(ValueError):
        assessment(causalPath={"entryResourceId": "ingress", "rootCauseEntityId": None, "steps": [{"order": 1, "entityId": "pod"}]})


def test_disconnected_causal_nodes_and_uninspected_relevance_are_rejected(payload):
    index = revealed(payload)
    output = assessment(claims=[{"id": "c1", "statement": "Missing configuration", "verdict": "supported", "citations": [{"sourceId": "log:crash"}]}], causalPath={"entryResourceId": "ingress", "rootCauseEntityId": "pod", "steps": [{"order": 1, "entityId": "pod", "claimId": "c1", "role": "root_cause"}, {"order": 2, "entityId": "ingress"}]})
    with pytest.raises(ValueError, match="delivered relationship"):
        validate_agent_assessment(output, index)
    output.causalPath = None
    output.nodeInsights = []
    output = assessment(nodeInsights=[{"nodeId": "service", "relevance": "not_relevant", "comment": "Excluded without looking"}])
    with pytest.raises(ValueError, match="uninspected"):
        validate_agent_assessment(output, index)
