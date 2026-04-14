"""Type definitions for the MockAgents Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Optional


@dataclass
class ChatResponse:
    """Represents a parsed response from a chat completion or messages API call."""

    content: str = ""
    model: str = ""
    tool_calls: list[ToolCall] = field(default_factory=list)
    finish_reason: str = ""
    usage: TokenUsage = field(default_factory=lambda: TokenUsage())
    raw: dict[str, Any] = field(default_factory=dict)
    status_code: int = 200
    latency_ms: float = 0.0

    @property
    def has_tool_calls(self) -> bool:
        return len(self.tool_calls) > 0


@dataclass
class ToolCall:
    """Represents a tool call from an agent response."""

    id: str = ""
    name: str = ""
    arguments: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_openai(cls, data: dict[str, Any]) -> ToolCall:
        """Parse from OpenAI tool_calls format."""
        import json

        func = data.get("function", {})
        args = func.get("arguments", "{}")
        if isinstance(args, str):
            try:
                args = json.loads(args)
            except (json.JSONDecodeError, TypeError):
                args = {}
        return cls(
            id=data.get("id", ""),
            name=func.get("name", ""),
            arguments=args,
        )

    @classmethod
    def from_anthropic(cls, data: dict[str, Any]) -> ToolCall:
        """Parse from Anthropic tool_use content block."""
        return cls(
            id=data.get("id", ""),
            name=data.get("name", ""),
            arguments=data.get("input", {}),
        )


@dataclass
class ToolError:
    """Represents a tool error from an agent response."""

    code: str = ""
    message: str = ""


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
class Interaction:
    """A single request-response interaction with the mock server."""

    request: dict[str, Any] = field(default_factory=dict)
    response: ChatResponse = field(default_factory=ChatResponse)
    latency_ms: float = 0.0


class ConfigError(Exception):
    """Raised when agent configuration is invalid."""

    pass


class ServerError(Exception):
    """Raised when the mock server encounters an error."""

    pass
