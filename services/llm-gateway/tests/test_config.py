"""Unit tests for app.config — resolve_model_id."""
from __future__ import annotations

from app.config import resolve_model_id


class TestResolveModelId:
    def test_known_alias_qwen3_32b(self):
        assert resolve_model_id("qwen3-32b") == "qwen.qwen3-32b-v1:0"

    def test_known_alias_qwen3_235b(self):
        assert resolve_model_id("qwen3-235b") == "qwen.qwen3-235b-a22b-2507-v1:0"

    def test_unknown_id_returned_as_is(self):
        assert resolve_model_id("anthropic.claude-3-5-sonnet") == "anthropic.claude-3-5-sonnet"

    def test_empty_string_returned_as_is(self):
        assert resolve_model_id("") == ""

    def test_already_canonical_id_returned_as_is(self):
        assert resolve_model_id("qwen.qwen3-32b-v1:0") == "qwen.qwen3-32b-v1:0"
