"""AWS Bedrock LLM provider (Converse API, sync boto3 offloaded to thread)."""
from __future__ import annotations

import asyncio
import json
import os
from typing import Any

import boto3
from botocore.config import Config

from app.config import resolve_model_id
from app.providers.base import LLMProvider

# In-code defaults — only used when the user hasn't configured a value in their provider settings.
_DEFAULT_REGION = "us-east-1"
_DEFAULT_MODEL_ID = "qwen3-32b"
_DEFAULT_MAX_TOKENS = 4096
_DEFAULT_TEMPERATURE = 0.3
_DEFAULT_THINKING_BUDGET = 0


class BedrockProvider(LLMProvider):
    def __init__(self, credentials: dict[str, Any]) -> None:
        region = credentials.get("region") or _DEFAULT_REGION
        bearer_token = credentials.get("bearerToken") or ""
        access_key = credentials.get("accessKeyId") or ""
        secret_key = credentials.get("secretAccessKey") or ""
        self._model = credentials.get("modelId") or _DEFAULT_MODEL_ID
        self._max_tokens = int(credentials.get("maxTokens") or _DEFAULT_MAX_TOKENS)
        self._temperature = float(credentials.get("temperature") or _DEFAULT_TEMPERATURE)
        self._thinking_budget = int(credentials.get("thinkingBudget") or _DEFAULT_THINKING_BUDGET)

        kwargs: dict[str, Any] = {
            "region_name": region,
            "config": Config(retries={"max_attempts": 3, "mode": "adaptive"}),
        }
        if access_key and secret_key:
            kwargs["aws_access_key_id"] = access_key
            kwargs["aws_secret_access_key"] = secret_key

        # Bearer token (Bedrock API key) is read by botocore from AWS_BEARER_TOKEN_BEDROCK.
        # Inject it around client creation so it doesn't bleed into other sessions.
        old_token = os.environ.get("AWS_BEARER_TOKEN_BEDROCK")
        if bearer_token:
            os.environ["AWS_BEARER_TOKEN_BEDROCK"] = bearer_token
        try:
            self._bedrock = boto3.client("bedrock-runtime", **kwargs)
        finally:
            if bearer_token:
                if old_token is None:
                    os.environ.pop("AWS_BEARER_TOKEN_BEDROCK", None)
                else:
                    os.environ["AWS_BEARER_TOKEN_BEDROCK"] = old_token

    def model_id(self) -> str:
        return resolve_model_id(self._model)

    def _sync_call(self, prompt: str) -> tuple[str, int]:
        inference_config: dict[str, Any] = {"maxTokens": self._max_tokens, "temperature": self._temperature}
        if self._thinking_budget > 0:
            inference_config["thinkingConfig"] = {"thinkingBudgetTokens": self._thinking_budget}

        response = self._bedrock.converse(
            modelId=self.model_id(),
            messages=[{"role": "user", "content": [{"text": prompt}]}],
            inferenceConfig=inference_config,
        )

        output = response.get("output", {})
        message = output.get("message", {})
        content = message.get("content", [])

        text = ""
        thinking_tokens = 0
        for block in content:
            if block.get("type") == "thinking":
                thinking_tokens = self._thinking_budget
            elif block.get("type") == "text":
                text = block.get("text", "")
            elif "text" in block:
                text = block["text"]

        if not text:
            raise ValueError(f"No text in Bedrock response: {json.dumps(response)[:500]}")

        return text, thinking_tokens

    async def call(self, prompt: str) -> tuple[str, int]:
        return await asyncio.to_thread(self._sync_call, prompt)
