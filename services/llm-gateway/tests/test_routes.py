"""Unit tests for app.routes — /healthz, /readyz, and /analyze endpoint."""
from __future__ import annotations

import json
import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from fastapi.testclient import TestClient

from main import app


@pytest.fixture
def client():
    return TestClient(app)


# ---------------------------------------------------------------------------
# Health endpoints
# ---------------------------------------------------------------------------

class TestHealthEndpoints:
    def test_healthz(self, client):
        resp = client.get("/healthz")
        assert resp.status_code == 200
        assert resp.json() == {"status": "ok"}

    def test_readyz(self, client):
        resp = client.get("/readyz")
        assert resp.status_code == 200
        assert resp.json() == {"status": "ok"}


# ---------------------------------------------------------------------------
# /analyze — success path
# ---------------------------------------------------------------------------

_VALID_ANALYZE_BODY = {
    "evidenceId": "ev-001",
    "diagnosticRunId": "dr-001",
    "incidentId": "inc-001",
    "provider": "bedrock",
    "credentials": {},
    "payload": {
        "rootResource": {"kind": "Deployment", "name": "my-app"},
        "rootResourceEvents": [],
        "problemCases": [],
        "workloadPodLogs": [],
        "pvcInfos": [],
        "relatedResourceEvidence": [],
    },
}

_VALID_LLM_JSON = json.dumps({
    "openingRant": "Oh great, another disaster.",
    "likelyRootCause": "Missing environment variable DB_HOST.",
    "evidenceChain": "Pod failed to start. Env var DB_HOST not set. Connection refused.",
    "recommendation": "1. Set DB_HOST\n2. Redeploy",
    "closingInsult": "You're welcome, I guess.",
    "confidence": 0.85,
    "needsMoreInfo": False,
    "suggestedResources": [],
})


class TestAnalyzeSuccess:
    def test_assessment_citations_are_validated(self, client):
        source = {"id": "field:valid", "type": "resource_field", "resource": {"kind": "Ingress", "namespace": "demo", "name": "web"}, "path": "/spec/backend", "value": "missing"}
        graph = {
            "entryResourceId": "resource:apps:Deployment:demo:web",
            "nodes": [{"id": "resource:apps:Deployment:demo:web"}, {"id": "resource:networking.k8s.io:Ingress:demo:web"}],
            "edges": [{"id": "edge:broken"}],
            "candidates": [{"id": "candidate:service"}],
        }
        body = {**_VALID_ANALYZE_BODY, "payload": {**_VALID_ANALYZE_BODY["payload"], "sources": [source], "graph": graph}}
        assessment = {
            "version": 2, "summary": "Wrong backend", "confidence": 0.95,
            "claims": [{"id": "c1", "statement": "Backend is missing", "verdict": "supported", "citations": [
                {"sourceId": "valid", "relevance": "configured value"},
                {"sourceId": "field:invented", "relevance": "hallucinated"},
            ]}],
            "alternativeHypotheses": [], "missingEvidence": [], "remediation": [],
            "causalPath": {
                "entryResourceId": "resource:apps:Deployment:demo:web",
                "rootCauseEntityId": "resource:networking.k8s.io:Ingress:demo:web",
                "steps": [{"order": 1, "entityId": "resource:networking.k8s.io:Ingress:demo:web", "edgeId": "edge:broken", "candidateId": "candidate:service", "claimId": "c1", "role": "root_cause"}],
            },
        }
        llm_json = json.dumps({**json.loads(_VALID_LLM_JSON), "assessmentVersion": 2, "assessment": assessment})
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))
        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=body)
        data = resp.json()
        assert data["assessmentVersion"] == 2
        assert data["citationValidation"]["invalid"] == 1
        assert data["assessment"]["confidence"] == 0.69
        assert data["confidence"] == 0.69
        assert [c["sourceId"] for c in data["assessment"]["claims"][0]["citations"]] == ["field:valid"]
        assert data["assessment"]["causalPath"]["rootCauseEntityId"] == "resource:networking.k8s.io:Ingress:demo:web"

    def test_invalid_causal_path_is_removed(self, client):
        source = {"id": "field:valid", "type": "resource_field", "value": "x"}
        graph = {"entryResourceId": "resource:apps:Deployment:demo:web", "nodes": [{"id": "resource:apps:Deployment:demo:web"}], "edges": [], "candidates": []}
        body = {**_VALID_ANALYZE_BODY, "payload": {**_VALID_ANALYZE_BODY["payload"], "sources": [source], "graph": graph}}
        assessment = {
            "version": 2, "summary": "Bad path", "confidence": 0.8, "claims": [],
            "alternativeHypotheses": [], "missingEvidence": [], "remediation": [],
            "causalPath": {"entryResourceId": graph["entryResourceId"], "rootCauseEntityId": "resource:invented", "steps": [{"order": 1, "entityId": "resource:invented", "role": "root_cause"}]},
        }
        llm_json = json.dumps({**json.loads(_VALID_LLM_JSON), "assessment": assessment})
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))
        with patch("app.routes.make_provider", return_value=mock_provider):
            data = client.post("/analyze", json=body).json()
        assert data["assessmentVersion"] == 2
        assert data["assessment"]["causalPath"] is None

    def test_node_insights_are_constrained_to_snapshot(self, client):
        source = {"id": "field:valid", "type": "resource_field", "value": "x"}
        node_id = "resource:apps:Deployment:demo:web"
        graph = {"entryResourceId": node_id, "nodes": [{"id": node_id}], "edges": [], "candidates": []}
        body = {**_VALID_ANALYZE_BODY, "payload": {**_VALID_ANALYZE_BODY["payload"], "sources": [source], "graph": graph}}
        assessment = {
            "version": 2, "summary": "Deployment is relevant", "confidence": 0.8,
            "claims": [{"id": "c1", "statement": "Observed", "verdict": "supported", "citations": [{"sourceId": "field:valid"}]}],
            "alternativeHypotheses": [], "missingEvidence": [], "remediation": [],
            "nodeInsights": [
                {"nodeId": node_id, "relevance": "relevant", "comment": "This node matters.", "claimIds": ["c1", "invented"], "sourceIds": ["field:valid", "invented"]},
                {"nodeId": node_id, "relevance": "context", "comment": "Duplicate.", "claimIds": [], "sourceIds": []},
                {"nodeId": "resource:invented", "relevance": "not_relevant", "comment": "Unknown.", "claimIds": [], "sourceIds": []},
            ],
        }
        llm_json = json.dumps({**json.loads(_VALID_LLM_JSON), "assessment": assessment})
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))
        with patch("app.routes.make_provider", return_value=mock_provider):
            data = client.post("/analyze", json=body).json()
        assert data["assessment"]["nodeInsights"] == [{
            "nodeId": node_id,
            "relevance": "relevant",
            "comment": "This node matters.",
            "claimIds": ["c1"],
            "sourceIds": ["field:valid"],
        }]

    def test_causal_path_recovers_graph_backed_citation(self, client):
        source = {"id": "field:ingress-backend", "type": "resource_field", "value": "missing-service"}
        graph = {
            "entryResourceId": "resource:apps:Deployment:demo:web",
            "nodes": [
                {"id": "resource:apps:Deployment:demo:web"},
                {"id": "reference:core:Service:demo:missing-service"},
            ],
            "edges": [{
                "id": "edge:routes-to",
                "from": "resource:apps:Deployment:demo:web",
                "to": "reference:core:Service:demo:missing-service",
                "sourceIds": ["field:ingress-backend"],
            }],
            "candidates": [],
        }
        body = {**_VALID_ANALYZE_BODY, "payload": {**_VALID_ANALYZE_BODY["payload"], "sources": [source], "graph": graph}}
        assessment = {
            "version": 2, "summary": "Missing backend", "confidence": 0.9,
            "claims": [{"id": "c1", "statement": "Backend does not exist", "verdict": "supported", "citations": [{"sourceId": "invented"}]}],
            "alternativeHypotheses": [], "missingEvidence": [], "remediation": [],
            "causalPath": {
                "entryResourceId": graph["entryResourceId"],
                "rootCauseEntityId": "reference:core:Service:demo:missing-service",
                "steps": [{"order": 1, "entityId": "reference:core:Service:demo:missing-service", "edgeId": "edge:routes-to", "claimId": "c1", "role": "root_cause"}],
            },
        }
        llm_json = json.dumps({**json.loads(_VALID_LLM_JSON), "assessment": assessment})
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))
        with patch("app.routes.make_provider", return_value=mock_provider):
            data = client.post("/analyze", json=body).json()
        assert data["assessment"]["claims"][0]["verdict"] == "supported"
        assert data["assessment"]["claims"][0]["citations"] == [{
            "sourceId": "field:ingress-backend", "quote": "", "relevance": "Deterministic graph evidence",
        }]

    def test_assessment_accepts_alternative_hypothesis_without_id(self, client):
        source = {"id": "field:pod-phase", "type": "resource_field", "value": "Running"}
        body = {**_VALID_ANALYZE_BODY, "payload": {**_VALID_ANALYZE_BODY["payload"], "sources": [source]}}
        assessment = {
            "version": 2,
            "summary": "The pod is healthy",
            "confidence": 0.9,
            "claims": [],
            "alternativeHypotheses": [{
                "statement": "The pod is not running",
                "confidence": 0.3,
                "citations": [],
            }],
            "missingEvidence": ["No Service named web exists in the namespace"],
            "remediation": [],
        }
        llm_json = json.dumps({
            **json.loads(_VALID_LLM_JSON),
            "assessmentVersion": 2,
            "assessment": assessment,
            "recommendation": [{"title": "Inspect pod", "command": "kubectl get pod web"}],
        })
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))
        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=body)

        data = resp.json()
        assert data["assessmentVersion"] == 2
        assert data["assessment"]["alternativeHypotheses"][0]["id"] == ""
        assert data["assessment"]["alternativeHypotheses"][0]["verdict"] == "unsupported"
        assert data["assessment"]["missingEvidence"][0]["description"] == "No Service named web exists in the namespace"
        assert data["recommendation"] == "1. Inspect pod — `kubectl get pod web`"

    def test_returns_200_with_valid_response(self, client):
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "qwen3-32b"
        mock_provider.call = AsyncMock(return_value=(_VALID_LLM_JSON, 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.status_code == 200
        data = resp.json()
        assert data["evidenceId"] == "ev-001"
        assert data["model"] == "qwen3-32b"
        assert data["likelyRootCause"] == "Missing environment variable DB_HOST."
        assert data["confidence"] == 0.85
        assert data["needsMoreInfo"] is False

    def test_confidence_clamped_above_1(self, client):
        llm_json = json.dumps({**json.loads(_VALID_LLM_JSON), "confidence": 5.0})
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.json()["confidence"] == 1.0

    def test_confidence_clamped_below_0(self, client):
        llm_json = json.dumps({**json.loads(_VALID_LLM_JSON), "confidence": -2.0})
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.json()["confidence"] == 0.0

    def test_thinking_tokens_forwarded(self, client):
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(_VALID_LLM_JSON, 512))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.json()["thinkingBudgetUsed"] == 512

    def test_suggested_resources_parsed(self, client):
        llm_json = json.dumps({
            **json.loads(_VALID_LLM_JSON),
            "needsMoreInfo": True,
            "suggestedResources": [{"kind": "ConfigMap", "reason": "missing config"}],
        })
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        data = resp.json()
        assert data["needsMoreInfo"] is True
        assert data["suggestedResources"][0]["kind"] == "ConfigMap"

    def test_needs_more_info_false_when_no_suggested_resources(self, client):
        """needsMoreInfo should be False if LLM says true but suggestedResources is empty."""
        llm_json = json.dumps({
            **json.loads(_VALID_LLM_JSON),
            "needsMoreInfo": True,
            "suggestedResources": [],
        })
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.json()["needsMoreInfo"] is False

    def test_list_field_joined_to_string(self, client):
        """Fields that are lists should be joined into a string."""
        llm_json = json.dumps({
            **json.loads(_VALID_LLM_JSON),
            "recommendation": ["step 1", "step 2", "step 3"],
        })
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        rec = resp.json()["recommendation"]
        assert "step 1" in rec
        assert "step 2" in rec

    def test_markdown_fenced_response_parsed(self, client):
        llm_json = f"```json\n{_VALID_LLM_JSON}\n```"
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=(llm_json, 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.status_code == 200


# ---------------------------------------------------------------------------
# /analyze — error paths
# ---------------------------------------------------------------------------

class TestAnalyzeErrors:
    def test_provider_call_failure_returns_502(self, client):
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(side_effect=RuntimeError("connection refused"))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.status_code == 502
        assert "LLM provider error" in resp.json()["detail"]

    def test_invalid_json_from_llm_returns_502(self, client):
        mock_provider = MagicMock()
        mock_provider.model_id.return_value = "test-model"
        mock_provider.call = AsyncMock(return_value=("not valid json at all", 0))

        with patch("app.routes.make_provider", return_value=mock_provider):
            resp = client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert resp.status_code == 502
        assert "parse error" in resp.json()["detail"]

    def test_missing_required_field_returns_422(self, client):
        body = {k: v for k, v in _VALID_ANALYZE_BODY.items() if k != "payload"}
        resp = client.post("/analyze", json=body)
        assert resp.status_code == 422


# ---------------------------------------------------------------------------
# /analyze — provider selection
# ---------------------------------------------------------------------------

class TestProviderSelection:
    def test_bedrock_provider_selected_by_default(self, client):
        captured = {}

        def fake_make_provider(name, creds):
            captured["name"] = name
            mock = MagicMock()
            mock.model_id.return_value = "bedrock-model"
            mock.call = AsyncMock(return_value=(_VALID_LLM_JSON, 0))
            return mock

        with patch("app.routes.make_provider", side_effect=fake_make_provider):
            client.post("/analyze", json=_VALID_ANALYZE_BODY)

        assert captured["name"] == "bedrock"

    def test_copilot_provider_selected_when_specified(self, client):
        captured = {}

        def fake_make_provider(name, creds):
            captured["name"] = name
            mock = MagicMock()
            mock.model_id.return_value = "copilot-model"
            mock.call = AsyncMock(return_value=(_VALID_LLM_JSON, 0))
            return mock

        body = {**_VALID_ANALYZE_BODY, "provider": "copilot"}
        with patch("app.routes.make_provider", side_effect=fake_make_provider):
            client.post("/analyze", json=body)

        assert captured["name"] == "copilot"
