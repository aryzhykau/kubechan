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
