"""Tests for mockagents.client — response parsing logic."""

from mockagents.client import MockAgentClient


def test_parse_openai_response():
    client = MockAgentClient.__new__(MockAgentClient)
    data = {
        "id": "chatcmpl-abc123",
        "object": "chat.completion",
        "created": 1234567890,
        "model": "gpt-4o",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Hello!",
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": 10,
            "completion_tokens": 5,
            "total_tokens": 15,
        },
    }
    resp = client._parse_openai_response(data, 200, 12.5)

    assert resp.content == "Hello!"
    assert resp.model == "gpt-4o"
    assert resp.finish_reason == "stop"
    assert resp.status_code == 200
    assert resp.latency_ms == 12.5
    assert resp.usage.prompt_tokens == 10
    assert resp.usage.completion_tokens == 5
    assert not resp.has_tool_calls


def test_parse_openai_response_with_tool_calls():
    client = MockAgentClient.__new__(MockAgentClient)
    data = {
        "id": "chatcmpl-abc123",
        "model": "gpt-4o",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": None,
                    "tool_calls": [
                        {
                            "id": "call_123",
                            "type": "function",
                            "function": {
                                "name": "get_weather",
                                "arguments": '{"city": "NYC"}',
                            },
                        }
                    ],
                },
                "finish_reason": "tool_calls",
            }
        ],
        "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
    }
    resp = client._parse_openai_response(data, 200, 10.0)

    assert resp.content == ""
    assert resp.has_tool_calls
    assert len(resp.tool_calls) == 1
    assert resp.tool_calls[0].name == "get_weather"
    assert resp.tool_calls[0].arguments == {"city": "NYC"}
    assert resp.finish_reason == "tool_calls"


def test_parse_openai_response_empty_choices():
    client = MockAgentClient.__new__(MockAgentClient)
    resp = client._parse_openai_response({"choices": []}, 200, 5.0)
    assert resp.content == ""
    assert resp.status_code == 200


def test_parse_anthropic_response():
    client = MockAgentClient.__new__(MockAgentClient)
    data = {
        "id": "msg_abc123",
        "type": "message",
        "role": "assistant",
        "content": [
            {"type": "text", "text": "Hello!"},
        ],
        "model": "claude-3-opus",
        "stop_reason": "end_turn",
        "usage": {"input_tokens": 10, "output_tokens": 5},
    }
    resp = client._parse_anthropic_response(data, 200, 8.0)

    assert resp.content == "Hello!"
    assert resp.model == "claude-3-opus"
    assert resp.finish_reason == "end_turn"
    assert resp.usage.prompt_tokens == 10
    assert resp.usage.completion_tokens == 5
    assert not resp.has_tool_calls


def test_parse_anthropic_response_with_tool_use():
    client = MockAgentClient.__new__(MockAgentClient)
    data = {
        "id": "msg_abc123",
        "type": "message",
        "content": [
            {"type": "text", "text": "Searching..."},
            {
                "type": "tool_use",
                "id": "toolu_123",
                "name": "search",
                "input": {"query": "test"},
            },
        ],
        "model": "claude-3-opus",
        "stop_reason": "tool_use",
        "usage": {"input_tokens": 10, "output_tokens": 5},
    }
    resp = client._parse_anthropic_response(data, 200, 10.0)

    assert resp.content == "Searching..."
    assert resp.has_tool_calls
    assert len(resp.tool_calls) == 1
    assert resp.tool_calls[0].name == "search"
    assert resp.tool_calls[0].arguments == {"query": "test"}
    assert resp.finish_reason == "tool_use"


def test_parse_anthropic_response_multiple_text_blocks():
    client = MockAgentClient.__new__(MockAgentClient)
    data = {
        "id": "msg_123",
        "content": [
            {"type": "text", "text": "Part 1"},
            {"type": "text", "text": "Part 2"},
        ],
        "model": "claude-3",
        "stop_reason": "end_turn",
        "usage": {},
    }
    resp = client._parse_anthropic_response(data, 200, 5.0)
    assert resp.content == "Part 1 Part 2"
