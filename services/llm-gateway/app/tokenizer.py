"""Token usage helpers with provider-reported counts and tokenizer estimates."""
from __future__ import annotations

from collections.abc import Mapping
from typing import Literal

import tiktoken

from app.models import TokenUsage


def estimate_token_usage(
    prompt: str,
    completion: str,
    model: str,
    provider_usage: Mapping[str, int] | None = None,
) -> TokenUsage:
    """Use API usage when available and tiktoken estimates for missing counts.

    tiktoken implements OpenAI tokenizers. Counts for other model families are
    estimates; provider-reported usage takes precedence whenever supplied.
    """
    encoding_name: str
    try:
        encoding = tiktoken.encoding_for_model(model.removeprefix("openai/"))
        encoding_name = encoding.name
    except KeyError:
        encoding = tiktoken.get_encoding("cl100k_base")
        encoding_name = "cl100k_base"

    reported = provider_usage if isinstance(provider_usage, Mapping) else {}
    input_tokens = reported.get("inputTokens")
    output_tokens = reported.get("outputTokens")
    if not isinstance(input_tokens, int) or input_tokens < 0:
        input_tokens = None
    if not isinstance(output_tokens, int) or output_tokens < 0:
        output_tokens = None
    input_source: Literal["provider", "tokenizer_estimate"] = "provider" if input_tokens is not None else "tokenizer_estimate"
    output_source: Literal["provider", "tokenizer_estimate"] = "provider" if output_tokens is not None else "tokenizer_estimate"
    if input_tokens is None:
        input_tokens = len(encoding.encode(prompt, disallowed_special=()))
    if output_tokens is None:
        output_tokens = len(encoding.encode(completion, disallowed_special=()))

    return TokenUsage(
        inputTokens=input_tokens,
        outputTokens=output_tokens,
        totalTokens=input_tokens + output_tokens,
        inputSource=input_source,
        outputSource=output_source,
        tokenizer=encoding_name if "tokenizer_estimate" in (input_source, output_source) else None,
    )
