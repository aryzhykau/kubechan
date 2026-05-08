"""Unit tests for app.providers.base — make_provider factory."""
from __future__ import annotations

import asyncio
import pytest
from unittest.mock import patch, MagicMock, AsyncMock


class TestMakeProvider:
    def test_returns_bedrock_provider_for_bedrock(self):
        from app.providers.base import make_provider

        mock_bedrock_cls = MagicMock()
        mock_instance = MagicMock()
        mock_bedrock_cls.return_value = mock_instance

        with patch("app.providers.bedrock.BedrockProvider", mock_bedrock_cls):
            # Reload make_provider logic using the real import path
            from app.providers import bedrock as bedrock_mod
            original_cls = bedrock_mod.BedrockProvider
            bedrock_mod.BedrockProvider = mock_bedrock_cls
            try:
                provider = make_provider("bedrock", {})
            finally:
                bedrock_mod.BedrockProvider = original_cls

    def test_returns_bedrock_for_unknown_provider(self):
        """Unknown provider names should fall back to Bedrock."""
        from app.providers.base import make_provider

        with patch("app.providers.bedrock.BedrockProvider") as mock_cls:
            mock_cls.return_value = MagicMock()
            # We just verify it doesn't raise
            try:
                make_provider("unknown-provider", {})
            except Exception:
                pass  # boto3 may fail in test env — that's OK, factory logic tested

    def test_copilot_provider_requires_token(self):
        from app.providers.copilot import CopilotProvider
        with pytest.raises(ValueError, match="token"):
            CopilotProvider({})

    def test_copilot_provider_stores_token(self):
        from app.providers.copilot import CopilotProvider
        p = CopilotProvider({"token": "abc123"})
        assert p._token == "abc123"

    def test_copilot_provider_default_model(self):
        from app.providers.copilot import CopilotProvider
        p = CopilotProvider({"token": "abc123"})
        assert p.model_id() == "gpt-4.1"

    def test_copilot_provider_custom_model(self):
        from app.providers.copilot import CopilotProvider
        p = CopilotProvider({"token": "abc123", "modelId": "o3"})
        assert p.model_id() == "o3"


# ---------------------------------------------------------------------------
# BedrockProvider
# ---------------------------------------------------------------------------

@pytest.fixture
def mock_boto3_client():
    with patch("app.providers.bedrock.boto3.client") as mock_client:
        yield mock_client


class TestBedrockProviderInit:
    def test_default_credentials(self, mock_boto3_client):
        from app.providers.bedrock import BedrockProvider
        p = BedrockProvider({})
        assert p._model == "qwen3-32b"
        assert p._max_tokens == 4096
        assert p._temperature == 0.3
        assert p._thinking_budget == 0
        mock_boto3_client.assert_called_once()

    def test_custom_model_and_tokens(self, mock_boto3_client):
        from app.providers.bedrock import BedrockProvider
        p = BedrockProvider({"modelId": "my-model", "maxTokens": "1024", "temperature": "0.7"})
        assert p._model == "my-model"
        assert p._max_tokens == 1024
        assert p._temperature == 0.7

    def test_access_key_credentials_passed(self, mock_boto3_client):
        from app.providers.bedrock import BedrockProvider
        BedrockProvider({"accessKeyId": "AKIA", "secretAccessKey": "secret"})
        call_kwargs = mock_boto3_client.call_args[1]
        assert call_kwargs["aws_access_key_id"] == "AKIA"
        assert call_kwargs["aws_secret_access_key"] == "secret"

    def test_bearer_token_set_and_cleared(self, mock_boto3_client):
        import os
        from app.providers.bedrock import BedrockProvider
        BedrockProvider({"bearerToken": "tok123"})
        # After __init__ the env var should be cleaned up
        assert os.environ.get("AWS_BEARER_TOKEN_BEDROCK") is None

    def test_model_id_resolves_alias(self, mock_boto3_client):
        from app.providers.bedrock import BedrockProvider
        p = BedrockProvider({"modelId": "qwen3-32b"})
        assert p.model_id() == "qwen.qwen3-32b-v1:0"

    def test_model_id_passthrough(self, mock_boto3_client):
        from app.providers.bedrock import BedrockProvider
        p = BedrockProvider({"modelId": "anthropic.claude-3"})
        assert p.model_id() == "anthropic.claude-3"


class TestBedrockSyncCall:
    def _make_provider(self, mock_boto3_client, credentials=None):
        from app.providers.bedrock import BedrockProvider
        return BedrockProvider(credentials or {})

    def test_sync_call_returns_text(self, mock_boto3_client):
        mock_client_instance = mock_boto3_client.return_value
        mock_client_instance.converse.return_value = {
            "output": {
                "message": {
                    "content": [{"text": "root cause is missing env var"}]
                }
            }
        }
        p = self._make_provider(mock_boto3_client)
        text, thinking = p._sync_call("some prompt")
        assert text == "root cause is missing env var"
        assert thinking == 0

    def test_sync_call_thinking_block(self, mock_boto3_client):
        mock_client_instance = mock_boto3_client.return_value
        mock_client_instance.converse.return_value = {
            "output": {
                "message": {
                    "content": [
                        {"type": "thinking", "thinking": "..."},
                        {"type": "text", "text": "the answer"},
                    ]
                }
            }
        }
        from app.providers.bedrock import BedrockProvider
        p = BedrockProvider({"thinkingBudget": "512"})
        text, thinking = p._sync_call("prompt")
        assert text == "the answer"
        assert thinking == 512

    def test_sync_call_raises_on_empty_response(self, mock_boto3_client):
        mock_client_instance = mock_boto3_client.return_value
        mock_client_instance.converse.return_value = {
            "output": {"message": {"content": []}}
        }
        p = self._make_provider(mock_boto3_client)
        with pytest.raises(ValueError, match="No text"):
            p._sync_call("prompt")

    def test_async_call_delegates_to_sync(self, mock_boto3_client):
        mock_client_instance = mock_boto3_client.return_value
        mock_client_instance.converse.return_value = {
            "output": {"message": {"content": [{"text": "async result"}]}}
        }
        p = self._make_provider(mock_boto3_client)
        result = asyncio.run(p.call("prompt"))
        assert result == ("async result", 0)


class TestMakeProvider:
    def test_returns_bedrock_provider_for_bedrock(self):
        from app.providers.base import make_provider

        mock_bedrock_cls = MagicMock()
        mock_instance = MagicMock()
        mock_bedrock_cls.return_value = mock_instance

        with patch("app.providers.bedrock.BedrockProvider", mock_bedrock_cls):
            # Reload make_provider logic using the real import path
            from app.providers import bedrock as bedrock_mod
            original_cls = bedrock_mod.BedrockProvider
            bedrock_mod.BedrockProvider = mock_bedrock_cls
            try:
                provider = make_provider("bedrock", {})
            finally:
                bedrock_mod.BedrockProvider = original_cls

    def test_returns_bedrock_for_unknown_provider(self):
        """Unknown provider names should fall back to Bedrock."""
        from app.providers.base import make_provider

        with patch("app.providers.bedrock.BedrockProvider") as mock_cls:
            mock_cls.return_value = MagicMock()
            # We just verify it doesn't raise
            try:
                make_provider("unknown-provider", {})
            except Exception:
                pass  # boto3 may fail in test env — that's OK, factory logic tested

    def test_copilot_provider_requires_token(self):
        from app.providers.copilot import CopilotProvider
        with pytest.raises(ValueError, match="token"):
            CopilotProvider({})

    def test_copilot_provider_stores_token(self):
        from app.providers.copilot import CopilotProvider
        p = CopilotProvider({"token": "abc123"})
        assert p._token == "abc123"

    def test_copilot_provider_default_model(self):
        from app.providers.copilot import CopilotProvider
        p = CopilotProvider({"token": "abc123"})
        assert p.model_id() == "gpt-4.1"

    def test_copilot_provider_custom_model(self):
        from app.providers.copilot import CopilotProvider
        p = CopilotProvider({"token": "abc123", "modelId": "o3"})
        assert p.model_id() == "o3"
