"""Tests for mockagents.types."""

from mockagents.types import ChatResponse, ToolCall, TokenUsage, ConfigError, ServerError


def test_chat_response_defaults():
    r = ChatResponse()
    assert r.content == ""
    assert r.model == ""
    assert r.tool_calls == []
    assert r.finish_reason == ""
    assert r.status_code == 200
    assert r.latency_ms == 0.0
    assert not r.has_tool_calls


def test_chat_response_with_tool_calls():
    r = ChatResponse(
        content="hello",
        tool_calls=[ToolCall(id="1", name="search", arguments={"q": "test"})],
    )
    assert r.has_tool_calls
    assert r.tool_calls[0].name == "search"


def test_tool_call_from_openai():
    tc = ToolCall.from_openai({
        "id": "call_123",
        "type": "function",
        "function": {
            "name": "get_weather",
            "arguments": '{"city": "NYC"}',
        },
    })
    assert tc.id == "call_123"
    assert tc.name == "get_weather"
    assert tc.arguments == {"city": "NYC"}


def test_tool_call_from_openai_invalid_json():
    tc = ToolCall.from_openai({
        "id": "call_123",
        "function": {"name": "test", "arguments": "invalid"},
    })
    assert tc.name == "test"
    assert tc.arguments == {}


def test_tool_call_from_anthropic():
    tc = ToolCall.from_anthropic({
        "id": "toolu_123",
        "name": "search",
        "input": {"query": "test"},
    })
    assert tc.id == "toolu_123"
    assert tc.name == "search"
    assert tc.arguments == {"query": "test"}


def test_token_usage_from_openai():
    usage = TokenUsage.from_openai({
        "prompt_tokens": 10,
        "completion_tokens": 20,
        "total_tokens": 30,
    })
    assert usage.prompt_tokens == 10
    assert usage.completion_tokens == 20
    assert usage.total_tokens == 30


def test_token_usage_from_anthropic():
    usage = TokenUsage.from_anthropic({
        "input_tokens": 15,
        "output_tokens": 25,
    })
    assert usage.prompt_tokens == 15
    assert usage.completion_tokens == 25
    assert usage.total_tokens == 40


def test_config_error():
    err = ConfigError("bad config")
    assert str(err) == "bad config"
    assert isinstance(err, Exception)


def test_server_error():
    err = ServerError("server failed")
    assert str(err) == "server failed"
    assert isinstance(err, Exception)
