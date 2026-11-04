"""Unit tests for app.providers.base — make_provider factory."""
from __future__ import annotations

import asyncio
import pytest
from unittest.mock import patch, MagicMock, AsyncMock


class TestMakeProvider:
    def test_returns_litellm_provider_for_supported_api_providers(self):
        from app.providers.base import make_provider
        from app.providers.litellm_provider import LiteLLMProvider

        for name in ("bedrock", "openai", "anthropic"):
            assert isinstance(make_provider(name, {}), LiteLLMProvider)

    def test_unknown_provider_is_rejected(self):
        from app.providers.base import make_provider
        with pytest.raises(ValueError, match="Unsupported LLM provider"):
            make_provider("unknown-provider", {})

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


class TestLiteLLMProvider:
    def test_api_model_names_are_provider_qualified(self):
        from app.providers.litellm_provider import LiteLLMProvider
        assert LiteLLMProvider("openai", {"modelId": "gpt-4.1"}).model_id() == "openai/gpt-4.1"
        assert LiteLLMProvider("anthropic", {"modelId": "claude-sonnet-4-6"}).model_id() == "anthropic/claude-sonnet-4-6"

    def test_bedrock_alias_is_resolved(self):
        from app.providers.litellm_provider import LiteLLMProvider
        assert LiteLLMProvider("bedrock", {"modelId": "qwen3-32b"}).model_id() == "bedrock/qwen.qwen3-32b-v1:0"

    def test_api_key_is_passed_per_call_not_environment(self):
        from app.providers.litellm_provider import LiteLLMProvider
        provider = LiteLLMProvider("openai", {"apiKey": "test-secret", "modelId": "gpt-4.1"})
        params = provider._call_params()
        assert params["api_key"] == "test-secret"
        assert "OPENAI_API_KEY" not in __import__("os").environ

    def test_bedrock_iam_credentials_and_region_are_call_local(self):
        from app.providers.litellm_provider import LiteLLMProvider
        provider = LiteLLMProvider("bedrock", {
            "accessKeyId": "AKIA", "secretAccessKey": "secret", "region": "eu-west-1",
        })
        params = provider._call_params()
        assert params["aws_access_key_id"] == "AKIA"
        assert params["aws_secret_access_key"] == "secret"
        assert params["aws_region_name"] == "eu-west-1"

    def test_bedrock_bearer_key_uses_litellm_api_key(self):
        from app.providers.litellm_provider import LiteLLMProvider
        assert LiteLLMProvider("bedrock", {"bearerToken": "br-key"})._call_params()["api_key"] == "br-key"

    def test_call_extracts_text_and_reasoning_usage(self):
        from app.providers.litellm_provider import LiteLLMProvider
        from types import SimpleNamespace
        response = SimpleNamespace(
            choices=[SimpleNamespace(message=SimpleNamespace(content="assessment JSON"))],
            usage=SimpleNamespace(completion_tokens_details=SimpleNamespace(reasoning_tokens=17)),
        )
        provider = LiteLLMProvider("openai", {"apiKey": "k"})
        with patch("app.providers.litellm_provider.acompletion", new=AsyncMock(return_value=response)) as call:
            assert asyncio.run(provider.call("prompt")) == ("assessment JSON", 17)
        assert call.call_args.kwargs["messages"] == [{"role": "user", "content": "prompt"}]


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

    def test_bearer_token_uses_unsigned_client_and_injects_auth_header(self, mock_boto3_client):
        from botocore import UNSIGNED
        from app.providers.bedrock import BedrockProvider
        BedrockProvider({"bearerToken": "tok123"})

        call_kwargs = mock_boto3_client.call_args.kwargs
        assert call_kwargs["config"].signature_version == UNSIGNED

        register = mock_boto3_client.return_value.meta.events.register
        register.assert_called_once()
        event_name, handler = register.call_args.args
        assert event_name == "before-sign.bedrock-runtime"

        request = MagicMock()
        request.headers = {}
        handler(request)
        assert request.headers["Authorization"] == "Bearer tok123"

    def test_iam_client_keeps_sigv4_auth(self, mock_boto3_client):
        from botocore import UNSIGNED
        from app.providers.bedrock import BedrockProvider
        BedrockProvider({"accessKeyId": "AKIA", "secretAccessKey": "secret"})

        call_kwargs = mock_boto3_client.call_args.kwargs
        assert call_kwargs["config"].signature_version != UNSIGNED
        mock_boto3_client.return_value.meta.events.register.assert_not_called()

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
