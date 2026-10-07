"""Type definitions for the MockAgents Python SDK."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Optional


@dataclass
class ChatResponse:
    """Represents a parsed response from a chat completion or messages API call.

    Attributes:
        tool_errors: Simulated tool calls whose fixture resolved to an error,
            read from the server's ``X-Mockagents-Tool-Errors`` header.
        headers: Response headers, for the other ``X-Mockagents-*`` signals
            (hallucination fixtures, strict-tools warnings).
        truncated: Streamed responses only: the stream ended without its
            terminal frame (``[DONE]`` / ``message_stop``), as an injected
            ``truncateAfter`` fault does.
        malformed_frames: Streamed responses only: data frames that were not
            a JSON object and were skipped (an injected ``malformed`` fault).
    """

    content: str = ""
    model: str = ""
    tool_calls: list[ToolCall] = field(default_factory=list)
    finish_reason: str = ""
    usage: TokenUsage = field(default_factory=lambda: TokenUsage())
    raw: dict[str, Any] = field(default_factory=dict)
    status_code: int = 200
    latency_ms: float = 0.0
    tool_errors: list[ToolError] = field(default_factory=list)
    headers: dict[str, str] = field(default_factory=dict)
    truncated: bool = False
    malformed_frames: int = 0

    @property
    def has_tool_calls(self) -> bool:
        return len(self.tool_calls) > 0


@dataclass
class ToolCall:
    """Represents a tool call from an agent response.

    Attributes:
        arguments: The decoded arguments object. ``{}`` when the arguments
            were absent or could not be decoded as a JSON object.
        raw_arguments: The arguments exactly as they came over the wire (an
            OpenAI arguments string, or the streamed fragments joined), so a
            malformed-arguments fixture stays observable.
        arguments_valid: False when ``raw_arguments`` is not a JSON object.
    """

    id: str = ""
    name: str = ""
    arguments: dict[str, Any] = field(default_factory=dict)
    raw_arguments: str = ""
    arguments_valid: bool = True

    @classmethod
    def from_raw(cls, id: str, name: str, raw: Any) -> ToolCall:
        """Build a ToolCall from wire arguments: a JSON string, an already
        decoded object, or nothing."""
        if raw is None or raw == "":
            return cls(id=id, name=name)
        if isinstance(raw, dict):
            return cls(id=id, name=name, arguments=raw, raw_arguments=json.dumps(raw))
        if not isinstance(raw, str):
            return cls(id=id, name=name, raw_arguments=json.dumps(raw), arguments_valid=False)
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            parsed = None
        if not isinstance(parsed, dict):
            return cls(id=id, name=name, raw_arguments=raw, arguments_valid=False)
        return cls(id=id, name=name, arguments=parsed, raw_arguments=raw)

    @classmethod
    def from_openai(cls, data: dict[str, Any]) -> ToolCall:
        """Parse from OpenAI tool_calls format."""
        func = data.get("function") or {}
        return cls.from_raw(data.get("id", ""), func.get("name", ""), func.get("arguments"))

    @classmethod
    def from_anthropic(cls, data: dict[str, Any]) -> ToolCall:
        """Parse from Anthropic tool_use content block."""
        return cls.from_raw(data.get("id", ""), data.get("name", ""), data.get("input"))


@dataclass
class ToolError:
    """A simulated tool call whose fixture resolved to an error."""

    code: str = ""
    message: str = ""
    tool: str = ""


@dataclass
class TokenUsage:
    """Token usage information."""

    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0

    @classmethod
    def from_openai(cls, data: dict[str, Any]) -> TokenUsage:
        return cls(
            prompt_tokens=data.get("prompt_tokens", 0),
            completion_tokens=data.get("completion_tokens", 0),
            total_tokens=data.get("total_tokens", 0),
        )

    @classmethod
    def from_anthropic(cls, data: dict[str, Any]) -> TokenUsage:
        input_tokens = data.get("input_tokens", 0)
        output_tokens = data.get("output_tokens", 0)
        return cls(
            prompt_tokens=input_tokens,
            completion_tokens=output_tokens,
            total_tokens=input_tokens + output_tokens,
        )


@dataclass
class StreamChunk:
    """A protocol-agnostic chunk of a streamed completion.

    Both OpenAI Chat Completions and Anthropic Messages stream events
    are normalized into this shape by ``MockAgentClient.iter_stream``,
    so user code can iterate a stream the same way regardless of which
    provider the mock is impersonating.

    Attributes:
        text: Incremental text delta. Empty string for non-text events.
        tool_call_delta: ``(index, name, arguments_fragment)`` triple
            when the event carries a partial tool call. ``None``
            otherwise. ``arguments_fragment`` is the raw JSON fragment
            as the provider streams it; callers that need the parsed
            arguments should wait for ``finished`` and use
            ``ChatResponse.tool_calls`` from a non-streamed call, or
            accumulate the fragments themselves.
        finish_reason: Set on the final chunk only. Empty otherwise.
        finished: True on the terminal chunk so consumers can break
            out of the loop without inspecting ``finish_reason``.
        raw: The original event dict from the wire, for callers that
            need provider-specific fields the normalization dropped.
    """

    text: str = ""
    tool_call_delta: Optional[tuple[int, str, str]] = None
    finish_reason: str = ""
    finished: bool = False
    raw: dict[str, Any] = field(default_factory=dict)


@dataclass
class Interaction:
    """A single request-response interaction with the mock server."""

    request: dict[str, Any] = field(default_factory=dict)
    response: ChatResponse = field(default_factory=ChatResponse)
    latency_ms: float = 0.0


@dataclass
class PipelineNodeResult:
    """One agent invocation in an HTTP pipeline run."""

    node_id: str = ""
    agent_name: str = ""
    response: dict[str, Any] = field(default_factory=dict)
    latency_ns: int = 0

    @classmethod
    def from_wire(cls, data: dict[str, Any]) -> PipelineNodeResult:
        return cls(
            node_id=str(data.get("node_id", "")),
            agent_name=str(data.get("agent_name", "")),
            response=data.get("response") if isinstance(data.get("response"), dict) else {},
            latency_ns=int(data.get("latency", 0)),
        )


@dataclass
class PipelineResult:
    """Typed result returned by ``MockAgentClient.run_pipeline``."""

    pipeline_name: str = ""
    topology: str = ""
    nodes: list[PipelineNodeResult] = field(default_factory=list)
    latency_ns: int = 0

    @property
    def node_sequence(self) -> list[str]:
        """Ordered node ids executed by the pipeline."""
        return [node.node_id for node in self.nodes]

    @classmethod
    def from_wire(cls, data: dict[str, Any]) -> PipelineResult:
        raw_nodes = data.get("nodes", [])
        nodes = [
            PipelineNodeResult.from_wire(node)
            for node in raw_nodes
            if isinstance(node, dict)
        ] if isinstance(raw_nodes, list) else []
        return cls(
            pipeline_name=str(data.get("pipeline_name", "")),
            topology=str(data.get("topology", "")),
            nodes=nodes,
            latency_ns=int(data.get("latency", 0)),
        )


class ConfigError(Exception):
    """Raised when agent configuration is invalid."""

    pass


class ServerError(Exception):
    """Raised when the mock server encounters an error."""

    pass
