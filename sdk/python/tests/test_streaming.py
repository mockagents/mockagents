"""Tests for the Anthropic-streaming and protocol-agnostic helpers
that close the chat_stream parity gap."""

from __future__ import annotations

from typing import Any
from unittest.mock import MagicMock, patch

import pytest

from mockagents.client import (
    MockAgentClient,
    _normalize_anthropic_stream,
    _normalize_openai_stream,
)
from mockagents.types import StreamChunk


# --- StreamChunk dataclass ---


def test_stream_chunk_defaults():
    c = StreamChunk()
    assert c.text == ""
    assert c.tool_call_delta is None
    assert c.finish_reason == ""
    assert c.finished is False
    assert c.raw == {}


def test_stream_chunk_equality():
    a = StreamChunk(text="hi", finish_reason="stop", finished=True)
    b = StreamChunk(text="hi", finish_reason="stop", finished=True)
    assert a == b


# --- _normalize_openai_stream ---


def _openai_chunks(*deltas: dict[str, Any]) -> list[dict[str, Any]]:
    """Wrap a list of delta dicts as full OpenAI chunk shapes."""
    out = []
    for d in deltas:
        out.append({"choices": [d]})
    return out


def test_normalize_openai_text_only():
    raw = _openai_chunks(
        {"delta": {"content": "Hello"}},
        {"delta": {"content": " world"}},
        {"delta": {}, "finish_reason": "stop"},
    )
    chunks = list(_normalize_openai_stream(iter(raw)))
    assert len(chunks) == 3
    assert chunks[0].text == "Hello"
    assert chunks[1].text == " world"
    assert chunks[2].finished is True
    assert chunks[2].finish_reason == "stop"


def test_normalize_openai_skips_padding():
    """Chunks with no content + no tool delta + no finish are dropped."""
    raw = _openai_chunks(
        {"delta": {}},
        {"delta": {"content": "data"}},
        {"delta": {}},
        {"delta": {}, "finish_reason": "stop"},
    )
    chunks = list(_normalize_openai_stream(iter(raw)))
    assert len(chunks) == 2
    assert chunks[0].text == "data"
    assert chunks[1].finished


def test_normalize_openai_tool_call_delta():
    raw = _openai_chunks(
        {
            "delta": {
                "tool_calls": [
                    {
                        "index": 0,
                        "function": {"name": "search", "arguments": '{"q":'},
                    }
                ]
            }
        },
        {
            "delta": {
                "tool_calls": [
                    {"index": 0, "function": {"arguments": '"hi"}'}}
                ]
            }
        },
        {"delta": {}, "finish_reason": "tool_calls"},
    )
    chunks = list(_normalize_openai_stream(iter(raw)))
    assert chunks[0].tool_call_delta == (0, "search", '{"q":')
    assert chunks[1].tool_call_delta == (0, "", '"hi"}')
    assert chunks[2].finish_reason == "tool_calls"


# --- _normalize_anthropic_stream ---


def test_normalize_anthropic_text_only():
    events = [
        {"type": "message_start", "message": {"model": "claude-3-5-sonnet"}},
        {"type": "content_block_start", "index": 0, "content_block": {"type": "text"}},
        {
            "type": "content_block_delta",
            "delta": {"type": "text_delta", "text": "Hello"},
        },
        {
            "type": "content_block_delta",
            "delta": {"type": "text_delta", "text": " world"},
        },
        {"type": "content_block_stop", "index": 0},
        {"type": "message_delta", "delta": {"stop_reason": "end_turn"}},
        {"type": "message_stop"},
    ]
    chunks = list(_normalize_anthropic_stream(iter(events)))
    text_chunks = [c for c in chunks if c.text]
    assert "".join(c.text for c in text_chunks) == "Hello world"
    assert chunks[-1].finished
    assert chunks[-1].finish_reason == "end_turn"


def test_normalize_anthropic_tool_use():
    events = [
        {"type": "message_start", "message": {"model": "claude-3-5-sonnet"}},
        {
            "type": "content_block_start",
            "index": 0,
            "content_block": {
                "type": "tool_use",
                "id": "tool_1",
                "name": "get_weather",
            },
        },
        {
            "type": "content_block_delta",
            "index": 0,
            "delta": {"type": "input_json_delta", "partial_json": '{"city":'},
        },
        {
            "type": "content_block_delta",
            "index": 0,
            "delta": {"type": "input_json_delta", "partial_json": '"Tokyo"}'},
        },
        {"type": "content_block_stop", "index": 0},
        {"type": "message_delta", "delta": {"stop_reason": "tool_use"}},
        {"type": "message_stop"},
    ]
    chunks = list(_normalize_anthropic_stream(iter(events)))
    # First chunk: tool_use start (name, no args yet).
    starts = [c for c in chunks if c.tool_call_delta and c.tool_call_delta[2] == ""]
    assert len(starts) == 1
    assert starts[0].tool_call_delta[1] == "get_weather"
    # Subsequent chunks: argument fragments accumulate as JSON.
    fragments = [c for c in chunks if c.tool_call_delta and c.tool_call_delta[2]]
    assert "".join(c.tool_call_delta[2] for c in fragments) == '{"city":"Tokyo"}'
    # Terminal chunk has finish_reason and finished=True.
    assert chunks[-1].finished
    assert chunks[-1].finish_reason == "tool_use"


def test_normalize_anthropic_message_stop_terminates():
    """Events after message_stop must not be yielded."""
    events = [
        {"type": "message_start", "message": {}},
        {"type": "message_stop"},
        # This should never be reached.
        {
            "type": "content_block_delta",
            "delta": {"type": "text_delta", "text": "after"},
        },
    ]
    chunks = list(_normalize_anthropic_stream(iter(events)))
    # Only the message_stop chunk should appear.
    assert len(chunks) == 1
    assert chunks[0].finished


# --- MockAgentClient.message_stream + iter_stream (HTTP mocked) ---


def _fake_sse_response(lines: list[str], status_code: int = 200) -> MagicMock:
    """Build a MagicMock that mimics requests.Response.iter_lines."""
    resp = MagicMock()
    resp.status_code = status_code
    resp.iter_lines.return_value = iter(lines)
    resp.raise_for_status.return_value = None
    resp.headers = {}
    return resp


def test_message_stream_parses_anthropic_sse():
    lines = [
        "event: message_start",
        'data: {"type":"message_start","message":{"model":"claude-x"}}',
        "",
        "event: content_block_start",
        'data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}',
        "",
        "event: content_block_delta",
        'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}',
        "",
        "event: message_stop",
        'data: {"type":"message_stop"}',
    ]
    client = MockAgentClient()
    with patch.object(client._session, "post", return_value=_fake_sse_response(lines)):
        events = list(client.message_stream(messages=[{"role": "user", "content": "x"}]))
    types_seen = [e.get("type") for e in events]
    assert types_seen == [
        "message_start",
        "content_block_start",
        "content_block_delta",
        "message_stop",
    ]


def test_message_stream_stops_after_message_stop():
    """Trailing events past message_stop must be ignored."""
    lines = [
        'data: {"type":"message_stop"}',
        "",
        'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"leak"}}',
        "",
    ]
    client = MockAgentClient()
    with patch.object(client._session, "post", return_value=_fake_sse_response(lines)):
        events = list(client.message_stream(messages=[{"role": "user", "content": "x"}]))
    assert len(events) == 1
    assert events[0]["type"] == "message_stop"


def test_message_stream_skips_malformed_data():
    lines = [
        'data: {"type":"message_start","message":{}}',
        "",
        "data: not-json-at-all",
        "",
        'data: {"type":"message_stop"}',
        "",
    ]
    client = MockAgentClient()
    with patch.object(client._session, "post", return_value=_fake_sse_response(lines)):
        events = list(client.message_stream(messages=[]))
    assert len(events) == 2
    assert events[0]["type"] == "message_start"
    assert events[1]["type"] == "message_stop"


def test_iter_stream_openai_end_to_end():
    """iter_stream(protocol='openai') must produce StreamChunks."""
    lines = [
        'data: {"choices":[{"delta":{"content":"a"}}]}',
        "",
        'data: {"choices":[{"delta":{"content":"b"}}]}',
        "",
        'data: {"choices":[{"delta":{},"finish_reason":"stop"}]}',
        "",
        "data: [DONE]",
        "",
    ]
    client = MockAgentClient()
    with patch.object(client._session, "post", return_value=_fake_sse_response(lines)):
        chunks = list(client.iter_stream(messages=[], protocol="openai"))
    assert "".join(c.text for c in chunks) == "ab"
    assert chunks[-1].finished
    assert chunks[-1].finish_reason == "stop"


def test_iter_stream_anthropic_end_to_end():
    lines = [
        'data: {"type":"message_start","message":{}}',
        "",
        'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}',
        "",
        'data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}',
        "",
        'data: {"type":"message_stop"}',
        "",
    ]
    client = MockAgentClient()
    with patch.object(client._session, "post", return_value=_fake_sse_response(lines)):
        chunks = list(client.iter_stream(messages=[], protocol="anthropic"))
    text = "".join(c.text for c in chunks)
    assert text == "hi"
    assert chunks[-1].finished
    assert chunks[-1].finish_reason == "end_turn"


def test_iter_stream_rejects_unknown_protocol():
    client = MockAgentClient()
    with pytest.raises(ValueError, match="unknown protocol"):
        # Generator must be consumed for the body to run.
        list(client.iter_stream(messages=[], protocol="bogus"))
