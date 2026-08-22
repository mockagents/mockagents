"""Tests for mockagents.assertions."""

import pytest

from mockagents.assertions import expect, PipelineExpect, ResponseExpect, ValueExpect
from mockagents.scenario import ScenarioResult, Scenario
from mockagents.types import ChatResponse, Interaction, PipelineNodeResult, PipelineResult, ToolCall


def make_result(*responses: ChatResponse) -> ScenarioResult:
    """Helper to create a ScenarioResult from ChatResponse objects."""
    scenario = Scenario(name="test", steps=[])
    interactions = [
        Interaction(request={}, response=r, latency_ms=r.latency_ms)
        for r in responses
    ]
    return ScenarioResult(scenario=scenario, interactions=interactions)


# --- expect() dispatch ---

def test_expect_scenario_result():
    result = make_result(ChatResponse(content="hi"))
    assert isinstance(expect(result), ResponseExpect)


def test_expect_chat_response():
    response = ChatResponse(content="hi")
    assert isinstance(expect(response), ResponseExpect)


def test_expect_value():
    assert isinstance(expect(42), ValueExpect)
    assert isinstance(expect("hello"), ValueExpect)


# --- to_have_response_containing ---

def test_to_have_response_containing_pass():
    result = make_result(ChatResponse(content="Hello, world!"))
    expect(result).to_have_response_containing("Hello")
    expect(result).to_have_response_containing("world")


def test_to_have_response_containing_fail():
    result = make_result(ChatResponse(content="Hello"))
    with pytest.raises(AssertionError, match="Expected response containing"):
        expect(result).to_have_response_containing("goodbye")


def test_to_have_response_containing_multiple_responses():
    result = make_result(
        ChatResponse(content="first"),
        ChatResponse(content="second with target"),
    )
    expect(result).to_have_response_containing("target")


# --- to_have_tool_call ---

def test_to_have_tool_call_name_only():
    result = make_result(ChatResponse(
        tool_calls=[ToolCall(id="1", name="search", arguments={"q": "test"})],
    ))
    expect(result).to_have_tool_call("search")


def test_to_have_tool_call_with_arguments():
    result = make_result(ChatResponse(
        tool_calls=[ToolCall(id="1", name="search", arguments={"q": "test", "limit": 10})],
    ))
    expect(result).to_have_tool_call("search", {"q": "test"})


def test_to_have_tool_call_fail_wrong_name():
    result = make_result(ChatResponse(
        tool_calls=[ToolCall(id="1", name="search")],
    ))
    with pytest.raises(AssertionError, match="Expected tool call"):
        expect(result).to_have_tool_call("get_weather")


def test_to_have_tool_call_fail_wrong_args():
    result = make_result(ChatResponse(
        tool_calls=[ToolCall(id="1", name="search", arguments={"q": "test"})],
    ))
    with pytest.raises(AssertionError, match="Expected tool call"):
        expect(result).to_have_tool_call("search", {"q": "other"})


def test_to_have_tool_call_no_tool_calls():
    result = make_result(ChatResponse(content="no tools"))
    with pytest.raises(AssertionError, match="Expected tool call"):
        expect(result).to_have_tool_call("search")


# --- to_have_status ---

def test_to_have_status_pass():
    result = make_result(ChatResponse(status_code=200))
    expect(result).to_have_status(200)


def test_to_have_status_fail():
    result = make_result(ChatResponse(status_code=200))
    with pytest.raises(AssertionError, match="Expected status code"):
        expect(result).to_have_status(404)


# --- to_have_finish_reason ---

def test_to_have_finish_reason_pass():
    result = make_result(ChatResponse(finish_reason="stop"))
    expect(result).to_have_finish_reason("stop")


def test_to_have_finish_reason_fail():
    result = make_result(ChatResponse(finish_reason="stop"))
    with pytest.raises(AssertionError, match="Expected finish reason"):
        expect(result).to_have_finish_reason("tool_calls")


# --- ValueExpect ---

def test_to_be_less_than_pass():
    expect(5).to_be_less_than(10)


def test_to_be_less_than_fail():
    with pytest.raises(AssertionError, match="to be less than"):
        expect(10).to_be_less_than(5)


def test_to_be_greater_than_pass():
    expect(10).to_be_greater_than(5)


def test_to_be_greater_than_fail():
    with pytest.raises(AssertionError, match="to be greater than"):
        expect(5).to_be_greater_than(10)


def test_to_equal_pass():
    expect("hello").to_equal("hello")
    expect(42).to_equal(42)


def test_to_equal_fail():
    with pytest.raises(AssertionError, match="to equal"):
        expect("hello").to_equal("world")


def test_to_contain_pass():
    expect("hello world").to_contain("world")


def test_to_contain_fail():
    with pytest.raises(AssertionError, match="to contain"):
        expect("hello").to_contain("world")


# --- Chaining ---

def test_chaining_response_assertions():
    result = make_result(ChatResponse(
        content="Hello! Let me search.",
        tool_calls=[ToolCall(id="1", name="search", arguments={"q": "test"})],
        status_code=200,
        finish_reason="tool_calls",
    ))
    (
        expect(result)
        .to_have_response_containing("Hello")
        .to_have_tool_call("search")
        .to_have_status(200)
        .to_have_finish_reason("tool_calls")
    )


def test_chaining_value_assertions():
    expect(50).to_be_greater_than(10).to_be_less_than(100)


# --- ChatResponse direct ---

def test_expect_chat_response_directly():
    response = ChatResponse(content="Hello!", status_code=200)
    expect(response).to_have_response_containing("Hello").to_have_status(200)


# --- Trajectory assertions (tool_call_sequence / tool_call_count) ---
#
# These mirror the `kind: TestSuite` YAML assertions of the same names, so the
# semantics are pinned to the Go runner's: read the AGGREGATE across every turn,
# and compare the sequence for full equality rather than as a subsequence.

def _call(name: str) -> ToolCall:
    return ToolCall(id=f"id-{name}", name=name, arguments={})


def test_tool_call_sequence_exact_match():
    result = make_result(
        ChatResponse(tool_calls=[_call("search")]),
        ChatResponse(tool_calls=[_call("summarize")]),
    )
    expect(result).to_have_tool_call_sequence(["search", "summarize"])


def test_tool_call_sequence_aggregates_across_turns():
    """A multi-turn trajectory is every call in order, not just the last turn."""
    result = make_result(
        ChatResponse(tool_calls=[_call("a"), _call("b")]),
        ChatResponse(tool_calls=[_call("c")]),
    )
    expect(result).to_have_tool_call_sequence(["a", "b", "c"])


def test_tool_call_sequence_rejects_wrong_order():
    result = make_result(ChatResponse(tool_calls=[_call("summarize"), _call("search")]))
    with pytest.raises(AssertionError) as exc:
        expect(result).to_have_tool_call_sequence(["search", "summarize"])
    # The message must name what actually happened, not just that it failed.
    assert "summarize" in str(exc.value) and "search" in str(exc.value)


def test_tool_call_sequence_is_not_a_subsequence_check():
    """An extra call breaks the sequence — full equality, matching the runner."""
    result = make_result(
        ChatResponse(tool_calls=[_call("search"), _call("rerank"), _call("summarize")])
    )
    with pytest.raises(AssertionError):
        expect(result).to_have_tool_call_sequence(["search", "summarize"])


def test_tool_call_sequence_empty():
    expect(make_result(ChatResponse())).to_have_tool_call_sequence([])


def test_tool_call_count_total():
    result = make_result(
        ChatResponse(tool_calls=[_call("a"), _call("b")]),
        ChatResponse(tool_calls=[_call("a")]),
    )
    expect(result).to_have_tool_call_count(3)


def test_tool_call_count_rejects_mismatch():
    result = make_result(ChatResponse(tool_calls=[_call("a")]))
    with pytest.raises(AssertionError) as exc:
        expect(result).to_have_tool_call_count(2)
    assert "got 1" in str(exc.value)


def test_tool_call_count_by_name():
    result = make_result(
        ChatResponse(tool_calls=[_call("search"), _call("search"), _call("summarize")])
    )
    expect(result).to_have_tool_call_count(2, name="search")
    expect(result).to_have_tool_call_count(1, name="summarize")
    expect(result).to_have_tool_call_count(0, name="absent")


def test_tool_call_count_by_name_rejects_mismatch():
    result = make_result(ChatResponse(tool_calls=[_call("search")]))
    with pytest.raises(AssertionError) as exc:
        expect(result).to_have_tool_call_count(2, name="search")
    assert "search" in str(exc.value)


def test_trajectory_assertions_chain():
    result = make_result(
        ChatResponse(content="done", tool_calls=[_call("search"), _call("summarize")])
    )
    (
        expect(result)
        .to_have_tool_call_sequence(["search", "summarize"])
        .to_have_tool_call_count(2)
        .to_have_tool_call_count(1, name="search")
        .to_have_response_containing("done")
    )


def test_trajectory_assertions_on_bare_chat_response():
    response = ChatResponse(tool_calls=[_call("search")])
    expect(response).to_have_tool_call_sequence(["search"]).to_have_tool_call_count(1)


def test_pipeline_node_sequence_exact_and_chainable():
    result = PipelineResult(nodes=[
        PipelineNodeResult(node_id="plan"),
        PipelineNodeResult(node_id="research"),
        PipelineNodeResult(node_id="write"),
    ])
    assert isinstance(expect(result), PipelineExpect)
    expect(result).to_have_node_sequence(["plan", "research", "write"])


def test_pipeline_node_sequence_rejects_wrong_order():
    result = PipelineResult(nodes=[
        PipelineNodeResult(node_id="a"),
        PipelineNodeResult(node_id="b"),
    ])
    with pytest.raises(AssertionError, match="node sequence"):
        expect(result).to_have_node_sequence(["b", "a"])
