"""Unit tests for app.parser — parse_llm_json."""
from __future__ import annotations

import json
import pytest

from app.parser import parse_llm_json


class TestPlainJson:
    def test_plain_json_object(self):
        raw = '{"key": "value", "num": 42}'
        result = parse_llm_json(raw)
        assert result == {"key": "value", "num": 42}

    def test_json_with_leading_trailing_whitespace(self):
        raw = '  \n  {"a": 1}  \n  '
        assert parse_llm_json(raw) == {"a": 1}

    def test_nested_json(self):
        raw = json.dumps({"outer": {"inner": [1, 2, 3]}, "flag": True})
        assert parse_llm_json(raw) == {"outer": {"inner": [1, 2, 3]}, "flag": True}


class TestMarkdownFences:
    def test_json_fenced_with_json_language(self):
        raw = "```json\n{\"k\": \"v\"}\n```"
        assert parse_llm_json(raw) == {"k": "v"}

    def test_json_fenced_without_language(self):
        raw = "```\n{\"k\": \"v\"}\n```"
        assert parse_llm_json(raw) == {"k": "v"}

    def test_json_fenced_with_python_language(self):
        raw = "```python\n{\"k\": \"v\"}\n```"
        assert parse_llm_json(raw) == {"k": "v"}

    def test_fenced_json_with_extra_whitespace(self):
        raw = "```json\n  {\"x\": 99}\n  \n```"
        result = parse_llm_json(raw)
        assert result["x"] == 99


class TestThinkBlocks:
    def test_think_block_stripped(self):
        raw = "<think>some internal reasoning</think>\n{\"a\": 1}"
        assert parse_llm_json(raw) == {"a": 1}

    def test_think_block_with_newlines_stripped(self):
        raw = "<think>\nmultiline\nthinking\nhere\n</think>\n{\"b\": 2}"
        assert parse_llm_json(raw) == {"b": 2}

    def test_think_block_before_fenced_json(self):
        raw = "<think>reasoning</think>\n```json\n{\"c\": 3}\n```"
        assert parse_llm_json(raw) == {"c": 3}


class TestFuzzyExtraction:
    def test_json_embedded_in_prose(self):
        raw = 'Here is my analysis: {"result": "ok", "score": 0.9} and that is it.'
        result = parse_llm_json(raw)
        assert result == {"result": "ok", "score": 0.9}

    def test_json_after_preamble(self):
        raw = "Sure, here you go:\n\n{\"answer\": 42}"
        result = parse_llm_json(raw)
        assert result["answer"] == 42


class TestErrors:
    def test_invalid_json_raises(self):
        with pytest.raises(Exception):
            parse_llm_json("not json at all")

    def test_empty_string_raises(self):
        with pytest.raises(Exception):
            parse_llm_json("")

    def test_think_block_only_no_json_raises(self):
        with pytest.raises(Exception):
            parse_llm_json("<think>just thinking</think>")
