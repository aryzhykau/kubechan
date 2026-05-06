"""LLMProvider interface and provider factory."""
from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any


class LLMProvider(ABC):
    @abstractmethod
    async def call(self, prompt: str) -> tuple[str, int]:
        """Call the LLM. Returns (raw_text, thinking_tokens_used)."""

    @abstractmethod
    def model_id(self) -> str:
        """Human-readable model identifier for logging / storage."""


def make_provider(provider_name: str, credentials: dict[str, Any]) -> LLMProvider:
    """Instantiate the appropriate LLMProvider from provider name + credentials."""
    # Import here to avoid circular imports and keep provider modules independent.
    from app.providers.copilot import CopilotProvider  # noqa: PLC0415
    from app.providers.bedrock import BedrockProvider  # noqa: PLC0415

    if provider_name == "copilot":
        return CopilotProvider(credentials)
    # Default: bedrock (even if provider_name is empty or "bedrock")
    return BedrockProvider(credentials)
