"""Assertion and decoding semantics the 2026-10-06 review found divergent
from the YAML runner and the TypeScript and Go SDKs (K-02, K-03, K-15)."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest

from mockagents import McpClient, McpEvent, expect
from mockagents.scenario import Scenario, ScenarioResult
from mockagents.types import ChatResponse, Interaction, ToolCall


def _result(*responses: ChatResponse) -> ScenarioResult:
    return ScenarioResult(
        scenario=Scenario(name="t", steps=[]),
        interactions=[Interaction(response=r) for r in responses],
    )


# --- K-02: outcome assertions read the final turn ---


def test_response_containing_reads_final_turn():
    result = _result(ChatResponse(content="error: payment failed"), ChatResponse(content="all good"))
    with pytest.raises(AssertionError, match="final response"):
        expect(result).to_have_response_containing("error")
    expect(result).to_have_response_containing("good")
    expect(result).to_have_any_response_containing("error")


def test_any_response_containing_fails_when_no_turn_matches():
    with pytest.raises(AssertionError, match="none of the 2"):
        expect(_result(ChatResponse(content="a"), ChatResponse(content="b"))).to_have_any_response_containing("z")


def test_status_and_finish_reason_read_final_turn():
    result = _result(
        ChatResponse(status_code=200, finish_reason="stop"),
        ChatResponse(status_code=201, finish_reason="length"),
    )
    expect(result).to_have_status(201).to_have_finish_reason("length")
    with pytest.raises(AssertionError):
        expect(result).to_have_status(200)
    with pytest.raises(AssertionError):
        expect(result).to_have_finish_reason("stop")


def test_outcome_assertions_on_empty_result_fail():
    empty = _result()
    for check in (
        lambda e: e.to_have_response_containing(""),
        lambda e: e.to_have_status(200),
        lambda e: e.to_have_finish_reason(""),
    ):
        with pytest.raises(AssertionError, match="has none"):
            check(expect(empty))


# --- K-03: missing argument is not None ---


def test_tool_call_missing_argument_does_not_match_none():
    result = _result(ChatResponse(tool_calls=[ToolCall(name="search", arguments={"q": "x"})]))
    with pytest.raises(AssertionError):
        expect(result).to_have_tool_call("search", {"filter": None})
    explicit = _result(ChatResponse(tool_calls=[ToolCall(name="search", arguments={"filter": None})]))
    expect(explicit).to_have_tool_call("search", {"filter": None})


def test_tool_call_numbers_compare_by_value():
    result = _result(ChatResponse(tool_calls=[ToolCall(name="search", arguments={"limit": 5.0})]))
    expect(result).to_have_tool_call("search", {"limit": 5})


@pytest.mark.parametrize("raw, args, valid", [
    ('{"a": 1}', {"a": 1}, True),
    ("", {}, True),
    (None, {}, True),
    ({"a": 1}, {"a": 1}, True),
    ("{not json", {}, False),
    ("[1]", {}, False),  # valid JSON, but not an object: no AttributeError
    ("null", {}, False),
    ([1], {}, False),
])
def test_tool_call_from_raw(raw, args, valid):
    tc = ToolCall.from_raw("id", "fn", raw)
    assert tc.arguments == args
    assert tc.arguments_valid is valid
    if isinstance(raw, str):
        assert tc.raw_arguments == raw


def test_tool_call_from_openai_and_anthropic_keep_raw_arguments():
    tc = ToolCall.from_openai({"id": "c", "function": {"name": "f", "arguments": "{bad"}})
    assert (tc.arguments, tc.raw_arguments, tc.arguments_valid) == ({}, "{bad", False)
    tc = ToolCall.from_anthropic({"id": "t", "name": "f", "input": ["not", "an", "object"]})
    assert tc.arguments == {} and tc.arguments_valid is False
    tc = ToolCall.from_anthropic({"id": "t", "name": "f", "input": {"k": "v"}})
    assert tc.arguments == {"k": "v"} and tc.raw_arguments == '{"k": "v"}'


def test_malformed_tool_arguments_assertion():
    good = ToolCall.from_raw("1", "a", "{}")
    bad = ToolCall.from_raw("2", "b", "{oops")
    result = _result(ChatResponse(tool_calls=[good, bad]))
    expect(result).to_have_malformed_tool_arguments()
    expect(result).to_have_malformed_tool_arguments("b")
    with pytest.raises(AssertionError, match="malformed"):
        expect(result).to_have_malformed_tool_arguments("a")


# --- ValueExpect.to_contain no longer stringifies ---


def test_to_contain_strings_and_collections():
    expect("hello world").to_contain("world")
    expect(["a", "b"]).to_contain("b")
    expect({"status": 1}).to_contain("status")
    with pytest.raises(AssertionError):
        expect(["a"]).to_contain("z")


@pytest.mark.parametrize("value", [None, 42, 1.5])
def test_to_contain_rejects_non_containers(value):
    with pytest.raises(TypeError):
        expect(value).to_contain("None")


def test_to_contain_on_string_needs_string():
    with pytest.raises(TypeError):
        expect("123").to_contain(1)


# --- K-15: a handler that returns None still replies ---


def test_dispatch_request_none_result_posts_empty_object():
    client = McpClient(base_url="http://test")
    event = McpEvent(kind="request", payload={
        "jsonrpc": "2.0", "id": 9, "method": "roots/list", "params": {},
    })
    with patch.object(client._session, "post") as post:
        post.return_value = MagicMock(raise_for_status=MagicMock(return_value=None))
        assert client.dispatch_request(event, {"roots/list": lambda params: None}) == {}
    assert post.call_args.kwargs["json"] == {"jsonrpc": "2.0", "id": 9, "result": {}}


def test_mcp_client_sends_api_key_on_session():
    client = McpClient(api_key="k")
    assert client._session.headers["Authorization"] == "Bearer k"
    assert client.base_url == "http://127.0.0.1:8080"
