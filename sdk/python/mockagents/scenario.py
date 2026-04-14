"""Scenario definition and execution for multi-turn conversation testing."""

from __future__ import annotations

import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Optional

from .client import MockAgentClient
from .types import ChatResponse, Interaction


@dataclass
class Scenario:
    """Defines a multi-turn conversation scenario.

    Args:
        name: Scenario name for identification.
        steps: List of message steps to send. Each step is a dict with
            'role' and 'content' keys, plus optional 'tool_calls'.
        agent_name: Target agent name (used for routing).
        protocol: Protocol to use ("openai" or "anthropic").
        model: Model name for the request.
    """

    name: str
    steps: list[dict[str, Any]]
    agent_name: str = ""
    protocol: str = "openai"
    model: str = "gpt-4o"
    session_id: str = field(default_factory=lambda: f"scenario-{uuid.uuid4().hex[:12]}")


@dataclass
class ScenarioResult:
    """Result of executing a scenario.

    Contains all interactions and provides convenience accessors.
    """

    scenario: Scenario
    interactions: list[Interaction] = field(default_factory=list)

    @property
    def responses(self) -> list[ChatResponse]:
        """All response objects from interactions."""
        return [i.response for i in self.interactions]

    @property
    def tool_calls(self) -> list[Any]:
        """Flattened list of all tool calls across all interactions."""
        calls = []
        for interaction in self.interactions:
            calls.extend(interaction.response.tool_calls)
        return calls

    @property
    def latency_ms(self) -> float:
        """Total latency across all interactions."""
        return sum(i.latency_ms for i in self.interactions)

    @property
    def last_response(self) -> Optional[ChatResponse]:
        """The most recent response."""
        if self.interactions:
            return self.interactions[-1].response
        return None

    @property
    def content(self) -> str:
        """Concatenated content from all responses."""
        return " ".join(r.content for r in self.responses if r.content)


def run_scenario(client: MockAgentClient, scenario: Scenario) -> ScenarioResult:
    """Execute a scenario against a MockAgents server.

    Sends each step's user messages sequentially, accumulating the
    conversation history for context.

    Args:
        client: MockAgentClient connected to the server.
        scenario: Scenario to execute.

    Returns:
        ScenarioResult with all interactions.
    """
    result = ScenarioResult(scenario=scenario)
    conversation: list[dict[str, Any]] = []

    for step in scenario.steps:
        conversation.append(step)

        start = time.monotonic()
        if scenario.protocol == "anthropic":
            response = client.message(
                messages=conversation,
                model=scenario.model,
                session_id=scenario.session_id,
            )
        else:
            response = client.chat(
                messages=conversation,
                model=scenario.model,
                session_id=scenario.session_id,
            )
        latency = (time.monotonic() - start) * 1000

        interaction = Interaction(
            request=step,
            response=response,
            latency_ms=latency,
        )
        result.interactions.append(interaction)

        # Add assistant response to conversation for multi-turn context.
        if response.content:
            conversation.append({"role": "assistant", "content": response.content})

    return result
