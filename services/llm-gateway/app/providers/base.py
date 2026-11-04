"""LLMProvider interface and provider factory."""
from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any


class LLMProvider(ABC):
    @property
    def token_usage(self) -> dict[str, int] | None:
        """Provider-reported token counts, when the SDK exposes them."""
        return None

    @abstractmethod
    async def call(self, prompt: str) -> tuple[str, int]:
        """Call the LLM. Returns (raw_text, thinking_tokens_used)."""

    @abstractmethod
    def model_id(self) -> str:
        """Human-readable model identifier for logging / storage."""


def make_provider(provider_name: str, credentials: dict[str, Any]) -> LLMProvider:
    """Instantiate a provider adapter from provider name + user settings."""
    if provider_name == "copilot":
        from app.providers.copilot import CopilotProvider  # noqa: PLC0415

        return CopilotProvider(credentials)
    if provider_name in {"bedrock", "openai", "anthropic"}:
        from app.providers.litellm_provider import LiteLLMProvider  # noqa: PLC0415

        return LiteLLMProvider(provider_name, credentials)
    raise ValueError(f"Unsupported LLM provider: {provider_name}")
