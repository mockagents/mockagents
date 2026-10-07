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
        steps: List of message steps. Each step is a dict with 'role' and
            'content' keys. Each ``user`` step sends one request; other steps
            (system, assistant, tool) are context for the requests after them.
            At least one ``user`` step is required.
        agent_name: Target agent name (used for routing).
        protocol: Protocol to use ("openai" or "anthropic").
        model: Model name for the request. ``None`` uses the client's default
            for the protocol.
    """

    name: str
    steps: list[dict[str, Any]]
    agent_name: str = ""
    protocol: str = "openai"
    model: Optional[str] = None
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

    Sends one request per ``user`` step, carrying the conversation so far.
    Every reply is appended to the conversation as an assistant turn before
    the next step, so later requests see the history the way the server does.
    This matches the TypeScript and Go runners, so a scenario makes the same
    requests, and the same assertions hold, in every SDK.

    Under ``protocol="anthropic"``, system steps are sent as the ``system``
    parameter, since the Messages API has no system role.

    Args:
        client: MockAgentClient connected to the server.
        scenario: Scenario to execute.

    Returns:
        ScenarioResult with one interaction per user step.

    Raises:
        ValueError: If the scenario has no user step (it would send nothing,
            so every count or sequence assertion on it would pass vacuously),
            or names an unknown protocol.
    """
    if not any(step.get("role") == "user" for step in scenario.steps):
        raise ValueError(f"scenario {scenario.name!r} needs at least one user step")
    if scenario.protocol not in ("openai", "anthropic"):
        raise ValueError(
            f"unknown protocol {scenario.protocol!r}; expected 'openai' or 'anthropic'"
        )
    result = ScenarioResult(scenario=scenario)
    conversation: list[dict[str, Any]] = []
    system_parts: list[str] = []
    anthropic = scenario.protocol == "anthropic"

    for step in scenario.steps:
        if anthropic and step.get("role") == "system":
            system_parts.append(str(step.get("content", "")))
            continue
        conversation.append(step)
        if step.get("role") != "user":
            continue

        start = time.monotonic()
        if anthropic:
            kwargs: dict[str, Any] = {}
            if scenario.model:
                kwargs["model"] = scenario.model
            response = client.message(
                messages=list(conversation),
                system="\n\n".join(system_parts) or None,
                session_id=scenario.session_id,
                **kwargs,
            )
        else:
            kwargs = {}
            if scenario.model:
                kwargs["model"] = scenario.model
            response = client.chat(
                messages=list(conversation),
                session_id=scenario.session_id,
                **kwargs,
            )
        latency = (time.monotonic() - start) * 1000

        result.interactions.append(
            Interaction(request=step, response=response, latency_ms=latency)
        )
        # Always record the reply, even an empty (tool-call-only) one, so the
        # roles keep alternating.
        conversation.append({"role": "assistant", "content": response.content})

    return result
