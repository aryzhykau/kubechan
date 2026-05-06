"""GitHub Copilot LLM provider (github-copilot-sdk)."""
from __future__ import annotations

from typing import Any

import copilot as copilot_sdk
from copilot.generated.session_events import AssistantMessageData, PermissionRequest
from copilot.session import PermissionRequestResult

from app.providers.base import LLMProvider


class CopilotProvider(LLMProvider):
    def __init__(self, credentials: dict[str, Any]) -> None:
        token = credentials.get("token") or ""
        if not token:
            raise ValueError("Copilot credentials must include 'token'")
        self._token = token
        self._model = credentials.get("modelId") or "gpt-4.1"

    def model_id(self) -> str:
        return self._model

    async def call(self, prompt: str) -> tuple[str, int]:
        def _approve_all(req: PermissionRequest, _env: dict) -> PermissionRequestResult:
            return PermissionRequestResult(kind="approve-once")

        config = copilot_sdk.SubprocessConfig(github_token=self._token)
        async with copilot_sdk.CopilotClient(config) as client:
            async with await client.create_session(
                on_permission_request=_approve_all,
                model=self._model,
            ) as session:
                response = await session.send_and_wait(prompt, timeout=300.0)
                if response is None:
                    raise ValueError("No response received from Copilot")
                if isinstance(response.data, AssistantMessageData):
                    return response.data.content, 0
                raise ValueError(f"Unexpected Copilot response type: {type(response.data)}")
