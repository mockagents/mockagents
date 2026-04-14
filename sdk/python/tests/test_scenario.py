"""Tests for mockagents.scenario."""

from mockagents.scenario import Scenario, ScenarioResult
from mockagents.types import ChatResponse, Interaction, ToolCall


def test_scenario_creation():
    s = Scenario(
        name="greeting",
        steps=[{"role": "user", "content": "hello"}],
        model="gpt-4o",
    )
    assert s.name == "greeting"
    assert len(s.steps) == 1
    assert s.protocol == "openai"
    assert s.session_id.startswith("scenario-")


def test_scenario_result_responses():
    result = ScenarioResult(
        scenario=Scenario(name="test", steps=[]),
        interactions=[
            Interaction(response=ChatResponse(content="first")),
            Interaction(response=ChatResponse(content="second")),
        ],
    )
    responses = result.responses
    assert len(responses) == 2
    assert responses[0].content == "first"
    assert responses[1].content == "second"


def test_scenario_result_tool_calls():
    result = ScenarioResult(
        scenario=Scenario(name="test", steps=[]),
        interactions=[
            Interaction(response=ChatResponse(
                tool_calls=[
                    ToolCall(name="search", arguments={"q": "a"}),
                    ToolCall(name="lookup", arguments={"id": "1"}),
                ],
            )),
            Interaction(response=ChatResponse(
                tool_calls=[ToolCall(name="search", arguments={"q": "b"})],
            )),
        ],
    )
    assert len(result.tool_calls) == 3


def test_scenario_result_latency():
    result = ScenarioResult(
        scenario=Scenario(name="test", steps=[]),
        interactions=[
            Interaction(latency_ms=10.0, response=ChatResponse()),
            Interaction(latency_ms=20.0, response=ChatResponse()),
        ],
    )
    assert result.latency_ms == 30.0


def test_scenario_result_last_response():
    result = ScenarioResult(
        scenario=Scenario(name="test", steps=[]),
        interactions=[
            Interaction(response=ChatResponse(content="first")),
            Interaction(response=ChatResponse(content="last")),
        ],
    )
    assert result.last_response is not None
    assert result.last_response.content == "last"


def test_scenario_result_last_response_empty():
    result = ScenarioResult(
        scenario=Scenario(name="test", steps=[]),
        interactions=[],
    )
    assert result.last_response is None


def test_scenario_result_content():
    result = ScenarioResult(
        scenario=Scenario(name="test", steps=[]),
        interactions=[
            Interaction(response=ChatResponse(content="Hello")),
            Interaction(response=ChatResponse(content="World")),
        ],
    )
    assert result.content == "Hello World"


def test_scenario_default_protocol():
    s = Scenario(name="test", steps=[])
    assert s.protocol == "openai"
    assert s.model == "gpt-4o"


def test_scenario_anthropic_protocol():
    s = Scenario(name="test", steps=[], protocol="anthropic", model="claude-3")
    assert s.protocol == "anthropic"
    assert s.model == "claude-3"


def test_scenario_session_id_unique():
    s1 = Scenario(name="a", steps=[])
    s2 = Scenario(name="b", steps=[])
    assert s1.session_id != s2.session_id
