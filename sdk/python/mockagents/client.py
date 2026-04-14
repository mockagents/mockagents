"""HTTP client for communicating with a MockAgents server."""

from __future__ import annotations

import json
import time
from typing import Any, Generator, Optional

import requests

from .types import ChatResponse, ToolCall, TokenUsage


class MockAgentClient:
    """HTTP client wrapper for the MockAgents server.

    Supports both OpenAI Chat Completions and Anthropic Messages protocols.

    Args:
        base_url: Server base URL (e.g., "http://localhost:8080").
        timeout: Request timeout in seconds.
    """

    def __init__(self, base_url: str = "http://localhost:8080", timeout: float = 30.0):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self._session = requests.Session()

    def chat(
        self,
        messages: list[dict[str, Any]],
        model: str = "gpt-4o",
        stream: bool = False,
        session_id: Optional[str] = None,
        tools: Optional[list[dict[str, Any]]] = None,
        tool_choice: Optional[Any] = None,
        **kwargs: Any,
    ) -> ChatResponse:
        """Send a chat completion request (OpenAI format).

        Args:
            messages: Conversation messages.
            model: Model name to route to.
            stream: Enable SSE streaming.
            session_id: Session ID for conversation state.
            tools: Tool definitions.
            tool_choice: Tool choice setting.

        Returns:
            Parsed ChatResponse.
        """
        payload: dict[str, Any] = {"model": model, "messages": messages, "stream": stream}
        if tools:
            payload["tools"] = tools
        if tool_choice is not None:
            payload["tool_choice"] = tool_choice
        payload.update(kwargs)

        headers: dict[str, str] = {"Content-Type": "application/json"}
        if session_id:
            headers["X-Session-Id"] = session_id

        start = time.monotonic()
        resp = self._session.post(
            f"{self.base_url}/v1/chat/completions",
            json=payload,
            headers=headers,
            timeout=self.timeout,
            stream=stream,
        )
        latency_ms = (time.monotonic() - start) * 1000

        if stream:
            return self._parse_openai_stream(resp, latency_ms)

        resp.raise_for_status()
        return self._parse_openai_response(resp.json(), resp.status_code, latency_ms)

    def chat_stream(
        self,
        messages: list[dict[str, Any]],
        model: str = "gpt-4o",
        session_id: Optional[str] = None,
        **kwargs: Any,
    ) -> Generator[dict[str, Any], None, None]:
        """Stream chat completion chunks (OpenAI format).

        Yields parsed chunk dictionaries.
        """
        payload: dict[str, Any] = {"model": model, "messages": messages, "stream": True}
        payload.update(kwargs)

        headers: dict[str, str] = {"Content-Type": "application/json"}
        if session_id:
            headers["X-Session-Id"] = session_id

        resp = self._session.post(
            f"{self.base_url}/v1/chat/completions",
            json=payload,
            headers=headers,
            timeout=self.timeout,
            stream=True,
        )
        resp.raise_for_status()

        for line in resp.iter_lines(decode_unicode=True):
            if not line or not line.startswith("data: "):
                continue
            data = line[6:]  # Strip "data: " prefix
            if data == "[DONE]":
                break
            try:
                yield json.loads(data)
            except json.JSONDecodeError:
                continue

    def message(
        self,
        messages: list[dict[str, Any]],
        model: str = "claude-sonnet-4-20250514",
        max_tokens: int = 1024,
        system: Optional[str] = None,
        stream: bool = False,
        session_id: Optional[str] = None,
        tools: Optional[list[dict[str, Any]]] = None,
        **kwargs: Any,
    ) -> ChatResponse:
        """Send a messages request (Anthropic format).

        Args:
            messages: Conversation messages.
            model: Model name to route to.
            max_tokens: Maximum tokens to generate.
            system: System prompt.
            stream: Enable SSE streaming.
            session_id: Session ID for conversation state.
            tools: Tool definitions.

        Returns:
            Parsed ChatResponse.
        """
        payload: dict[str, Any] = {
            "model": model,
            "messages": messages,
            "max_tokens": max_tokens,
            "stream": stream,
        }
        if system:
            payload["system"] = system
        if tools:
            payload["tools"] = tools
        payload.update(kwargs)

        headers: dict[str, str] = {
            "Content-Type": "application/json",
            "X-Api-Key": "mock-api-key",
            "Anthropic-Version": "2023-06-01",
        }
        if session_id:
            headers["X-Session-Id"] = session_id

        start = time.monotonic()
        resp = self._session.post(
            f"{self.base_url}/v1/messages",
            json=payload,
            headers=headers,
            timeout=self.timeout,
            stream=stream,
        )
        latency_ms = (time.monotonic() - start) * 1000

        if stream:
            return self._parse_anthropic_stream(resp, latency_ms)

        resp.raise_for_status()
        return self._parse_anthropic_response(resp.json(), resp.status_code, latency_ms)

    def health(self) -> dict[str, Any]:
        """Check server health."""
        resp = self._session.get(f"{self.base_url}/api/v1/health", timeout=self.timeout)
        resp.raise_for_status()
        return resp.json()

    def list_agents(self) -> list[dict[str, Any]]:
        """List all loaded agents."""
        resp = self._session.get(f"{self.base_url}/api/v1/agents", timeout=self.timeout)
        resp.raise_for_status()
        return resp.json()

    def get_agent(self, name: str) -> dict[str, Any]:
        """Get a specific agent definition."""
        resp = self._session.get(f"{self.base_url}/api/v1/agents/{name}", timeout=self.timeout)
        resp.raise_for_status()
        return resp.json()

    def reload_agent(self, name: str) -> dict[str, Any]:
        """Reload an agent from disk."""
        resp = self._session.post(
            f"{self.base_url}/api/v1/agents/{name}/reload", timeout=self.timeout
        )
        resp.raise_for_status()
        return resp.json()

    def close(self) -> None:
        """Close the underlying HTTP session."""
        self._session.close()

    def __enter__(self) -> MockAgentClient:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    # --- Response Parsers ---

    def _parse_openai_response(
        self, data: dict[str, Any], status_code: int, latency_ms: float
    ) -> ChatResponse:
        choices = data.get("choices", [])
        if not choices:
            return ChatResponse(raw=data, status_code=status_code, latency_ms=latency_ms)

        message = choices[0].get("message", {})
        content = message.get("content") or ""

        tool_calls = []
        for tc in message.get("tool_calls", []):
            tool_calls.append(ToolCall.from_openai(tc))

        usage = TokenUsage.from_openai(data.get("usage", {}))

        return ChatResponse(
            content=content,
            model=data.get("model", ""),
            tool_calls=tool_calls,
            finish_reason=choices[0].get("finish_reason", ""),
            usage=usage,
            raw=data,
            status_code=status_code,
            latency_ms=latency_ms,
        )

    def _parse_openai_stream(
        self, resp: requests.Response, latency_ms: float
    ) -> ChatResponse:
        content_parts: list[str] = []
        tool_call_map: dict[int, dict[str, Any]] = {}
        finish_reason = ""
        model = ""

        for line in resp.iter_lines(decode_unicode=True):
            if not line or not line.startswith("data: "):
                continue
            data = line[6:]
            if data == "[DONE]":
                break
            try:
                chunk = json.loads(data)
            except json.JSONDecodeError:
                continue

            model = model or chunk.get("model", "")
            choices = chunk.get("choices", [])
            if not choices:
                continue
            delta = choices[0].get("delta", {})
            if "content" in delta and delta["content"]:
                content_parts.append(delta["content"])
            if "finish_reason" in choices[0] and choices[0]["finish_reason"]:
                finish_reason = choices[0]["finish_reason"]
            for tc in delta.get("tool_calls", []):
                idx = tc.get("index", 0)
                if idx not in tool_call_map:
                    tool_call_map[idx] = {"id": "", "name": "", "arguments": ""}
                if tc.get("id"):
                    tool_call_map[idx]["id"] = tc["id"]
                func = tc.get("function", {})
                if func.get("name"):
                    tool_call_map[idx]["name"] = func["name"]
                if func.get("arguments"):
                    tool_call_map[idx]["arguments"] += func["arguments"]

        tool_calls = []
        for idx in sorted(tool_call_map.keys()):
            tc_data = tool_call_map[idx]
            args = tc_data["arguments"]
            try:
                args = json.loads(args) if args else {}
            except json.JSONDecodeError:
                args = {}
            tool_calls.append(ToolCall(id=tc_data["id"], name=tc_data["name"], arguments=args))

        total_latency = (time.monotonic() * 1000) - (time.monotonic() * 1000 - latency_ms)

        return ChatResponse(
            content="".join(content_parts),
            model=model,
            tool_calls=tool_calls,
            finish_reason=finish_reason,
            raw={},
            status_code=resp.status_code,
            latency_ms=latency_ms,
        )

    def _parse_anthropic_response(
        self, data: dict[str, Any], status_code: int, latency_ms: float
    ) -> ChatResponse:
        content_parts: list[str] = []
        tool_calls: list[ToolCall] = []

        for block in data.get("content", []):
            block_type = block.get("type", "")
            if block_type == "text":
                content_parts.append(block.get("text", ""))
            elif block_type == "tool_use":
                tool_calls.append(ToolCall.from_anthropic(block))

        usage = TokenUsage.from_anthropic(data.get("usage", {}))

        return ChatResponse(
            content=" ".join(content_parts) if content_parts else "",
            model=data.get("model", ""),
            tool_calls=tool_calls,
            finish_reason=data.get("stop_reason", ""),
            usage=usage,
            raw=data,
            status_code=status_code,
            latency_ms=latency_ms,
        )

    def _parse_anthropic_stream(
        self, resp: requests.Response, latency_ms: float
    ) -> ChatResponse:
        content_parts: list[str] = []
        tool_calls: list[ToolCall] = []
        stop_reason = ""
        model = ""
        current_tool: Optional[dict[str, Any]] = None

        for line in resp.iter_lines(decode_unicode=True):
            if not line:
                continue
            if line.startswith("event: "):
                continue
            if not line.startswith("data: "):
                continue
            data = line[6:]
            try:
                event = json.loads(data)
            except json.JSONDecodeError:
                continue

            event_type = event.get("type", "")
            if event_type == "message_start":
                msg = event.get("message", {})
                model = msg.get("model", "")
            elif event_type == "content_block_start":
                block = event.get("content_block", {})
                if block.get("type") == "tool_use":
                    current_tool = {
                        "id": block.get("id", ""),
                        "name": block.get("name", ""),
                        "input_json": "",
                    }
            elif event_type == "content_block_delta":
                delta = event.get("delta", {})
                delta_type = delta.get("type", "")
                if delta_type == "text_delta":
                    content_parts.append(delta.get("text", ""))
                elif delta_type == "input_json_delta" and current_tool:
                    current_tool["input_json"] += delta.get("partial_json", "")
            elif event_type == "content_block_stop":
                if current_tool:
                    args = {}
                    try:
                        args = json.loads(current_tool["input_json"])
                    except (json.JSONDecodeError, TypeError):
                        pass
                    tool_calls.append(
                        ToolCall(
                            id=current_tool["id"],
                            name=current_tool["name"],
                            arguments=args,
                        )
                    )
                    current_tool = None
            elif event_type == "message_delta":
                delta = event.get("delta", {})
                stop_reason = delta.get("stop_reason", stop_reason)
            elif event_type == "message_stop":
                break

        return ChatResponse(
            content="".join(content_parts),
            model=model,
            tool_calls=tool_calls,
            finish_reason=stop_reason,
            raw={},
            status_code=resp.status_code,
            latency_ms=latency_ms,
        )
