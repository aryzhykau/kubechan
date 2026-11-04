"""LiteLLM SDK adapter for API-key and AWS IAM-backed model providers."""
from __future__ import annotations

from typing import Any

from litellm import acompletion

from app.config import resolve_model_id
from app.providers.base import LLMProvider

_DEFAULTS: dict[str, dict[str, Any]] = {
    "openai": {"modelId": "gpt-4.1", "maxTokens": 4096, "temperature": 0.3},
    "anthropic": {"modelId": "claude-sonnet-4-6", "maxTokens": 4096, "temperature": 0.3},
    "bedrock": {"modelId": "qwen3-32b", "maxTokens": 4096, "temperature": 0.3},
}


class LiteLLMProvider(LLMProvider):
    """Call one provider using LiteLLM's common async completion interface.

    Credentials are passed as call-local parameters. In particular, API keys
    are never copied to process-wide environment variables, so concurrent users
    cannot overwrite each other's credentials.
    """

    def __init__(self, provider: str, credentials: dict[str, Any]) -> None:
        if provider not in _DEFAULTS:
            raise ValueError(f"Unsupported LiteLLM provider: {provider}")
        self._provider = provider
        self._credentials = credentials
        self._model = str(credentials.get("modelId") or _DEFAULTS[provider]["modelId"])
        self._max_tokens = int(credentials.get("maxTokens") or _DEFAULTS[provider]["maxTokens"])
        self._temperature = float(credentials.get("temperature") or _DEFAULTS[provider]["temperature"])
        self._thinking_budget = int(credentials.get("thinkingBudget") or 0)
        self._token_usage: dict[str, int] | None = None

    @property
    def token_usage(self) -> dict[str, int] | None:
        return self._token_usage

    def model_id(self) -> str:
        model = resolve_model_id(self._model) if self._provider == "bedrock" else self._model
        if model.startswith(f"{self._provider}/"):
            return model
        return f"{self._provider}/{model}"

    def _call_params(self) -> dict[str, Any]:
        params: dict[str, Any] = {
            "model": self.model_id(),
            "messages": [{"role": "user", "content": ""}],
            "max_tokens": self._max_tokens,
            "num_retries": 0,
        }
        creds = self._credentials
        if self._provider == "bedrock":
            bearer = creds.get("bearerToken")
            access_key = creds.get("accessKeyId")
            secret_key = creds.get("secretAccessKey")
            if bearer:
                params["api_key"] = bearer
            if access_key and secret_key:
                params["aws_access_key_id"] = access_key
                params["aws_secret_access_key"] = secret_key
            if creds.get("sessionToken"):
                params["aws_session_token"] = creds["sessionToken"]
            params["aws_region_name"] = creds.get("region") or "us-east-1"
        else:
            api_key = creds.get("apiKey")
            if not api_key:
                raise ValueError(f"{self._provider} credentials must include 'apiKey'")
            params["api_key"] = api_key

        if creds.get("baseUrl"):
            params["api_base"] = creds["baseUrl"]
        if creds.get("temperature") not in (None, ""):
            params["temperature"] = self._temperature
        return params

    async def call(self, prompt: str) -> tuple[str, int]:
        params = self._call_params()
        params["messages"] = [{"role": "user", "content": prompt}]
        if self._thinking_budget > 0 and self._provider == "bedrock":
            params["thinking"] = {"type": "enabled", "budget_tokens": self._thinking_budget}

        response = await acompletion(**params)
        choices = getattr(response, "choices", None) or []
        if not choices:
            raise ValueError("No choices in LiteLLM response")
        message = getattr(choices[0], "message", None)
        content = getattr(message, "content", None)
        if isinstance(content, list):
            content = "".join(
                str(block.get("text", "")) for block in content if isinstance(block, dict)
            )
        if not isinstance(content, str) or not content.strip():
            raise ValueError("No text in LiteLLM response")

        usage = getattr(response, "usage", None)
        prompt_tokens = getattr(usage, "prompt_tokens", None)
        completion_tokens = getattr(usage, "completion_tokens", None)
        self._token_usage = {
            key: int(value)
            for key, value in (
                ("inputTokens", prompt_tokens),
                ("outputTokens", completion_tokens),
            )
            if value is not None
        }
        details = getattr(usage, "completion_tokens_details", None)
        thinking_tokens = int(getattr(details, "reasoning_tokens", 0) or 0)
        if not thinking_tokens and self._thinking_budget and self._provider == "bedrock":
            # Bedrock models do not consistently report a separate reasoning
            # token count through the common response shape.
            thinking_tokens = self._thinking_budget
        return content, thinking_tokens
