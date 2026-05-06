"""Runtime configuration loaded from environment variables."""
from __future__ import annotations

import os

# Server-side prompt truncation limit — infrastructure tuning, not provider-specific.
EVIDENCE_TOKEN_BUDGET: int = int(os.getenv("EVIDENCE_TOKEN_BUDGET", "120000"))

# Model ID aliases → canonical bedrock-runtime model IDs.
_INFERENCE_PROFILES: dict[str, str] = {
    "qwen3-32b": "qwen.qwen3-32b-v1:0",
    "qwen3-235b": "qwen.qwen3-235b-a22b-2507-v1:0",
}


def resolve_model_id(model_id: str) -> str:
    return _INFERENCE_PROFILES.get(model_id, model_id)
