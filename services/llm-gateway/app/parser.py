"""LLM JSON response parser with markdown-fence and think-block tolerance."""
from __future__ import annotations

import json
import re
from typing import Any


def parse_llm_json(raw: str) -> dict[str, Any]:
    """Extract a JSON object from LLM output, tolerating markdown fences and <think> blocks."""
    # Strip /think.../ blocks (Qwen3 extended thinking in text)
    raw = re.sub(r"<think>.*?</think>", "", raw, flags=re.DOTALL).strip()
    # Strip markdown fences
    raw = re.sub(r"^```[a-z]*\n?", "", raw, flags=re.MULTILINE)
    raw = re.sub(r"\n?```$", "", raw, flags=re.MULTILINE)
    raw = raw.strip()
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        m = re.search(r"\{.*\}", raw, re.DOTALL)
        if m:
            return json.loads(m.group(0))
        raise
