"""HTTP client for communicating with a MockAgents server."""

from __future__ import annotations

import json
import time
from typing import Any, Generator, Optional
from urllib.parse import quote

import requests

from .types import ChatResponse, PipelineResult, StreamChunk, ToolCall, TokenUsage


class MockAgentClient:
    """HTTP client wrapper for the MockAgents server.

    Supports both OpenAI Chat Completions and Anthropic Messages protocols.

    Args:
        base_url: Server base URL (e.g., "http://localhost:8080").
        timeout: Request timeout in seconds.
    """

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        timeout: float = 30.0,
        api_key: Optional[str] = None,
    ):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.api_key = api_key
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

    def message_stream(
        self,
        messages: list[dict[str, Any]],
        model: str = "claude-sonnet-4-20250514",
        max_tokens: int = 1024,
        system: Optional[str] = None,
        session_id: Optional[str] = None,
        **kwargs: Any,
    ) -> Generator[dict[str, Any], None, None]:
        """Stream Anthropic Messages events.

        Mirrors :meth:`chat_stream` but for the Anthropic wire format.
        Yields parsed event dictionaries (``message_start``,
        ``content_block_start``, ``content_block_delta``,
        ``content_block_stop``, ``message_delta``, ``message_stop``).
        Stops once a ``message_stop`` event is seen.

        Use :meth:`iter_stream` if you want a protocol-agnostic
        :class:`StreamChunk` view instead of raw event dicts.
        """
        payload: dict[str, Any] = {
            "model": model,
            "messages": messages,
            "max_tokens": max_tokens,
            "stream": True,
        }
        if system:
            payload["system"] = system
        payload.update(kwargs)

        headers: dict[str, str] = {
            "Content-Type": "application/json",
            "X-Api-Key": "mock-api-key",
            "Anthropic-Version": "2023-06-01",
        }
        if session_id:
            headers["X-Session-Id"] = session_id

        resp = self._session.post(
            f"{self.base_url}/v1/messages",
            json=payload,
            headers=headers,
            timeout=self.timeout,
            stream=True,
        )
        resp.raise_for_status()

        for line in resp.iter_lines(decode_unicode=True):
            if not line:
                continue
            # Anthropic SSE frames look like:
            #   event: content_block_delta
            #   data: {...}
            # We only care about data lines; the event: line is
            # informational because the type is also embedded in the
            # JSON payload.
            if line.startswith("event: "):
                continue
            if not line.startswith("data: "):
                continue
            data = line[6:]
            try:
                event = json.loads(data)
            except json.JSONDecodeError:
                continue
            yield event
            if event.get("type") == "message_stop":
                return

    def iter_stream(
        self,
        messages: list[dict[str, Any]],
        *,
        protocol: str = "openai",
        model: Optional[str] = None,
        session_id: Optional[str] = None,
        **kwargs: Any,
    ) -> Generator[StreamChunk, None, None]:
        """Iterate a streamed completion as protocol-agnostic
        :class:`StreamChunk` objects.

        ``protocol`` selects the wire format ("openai" or "anthropic").
        The default model is picked per-protocol when ``model`` is
        omitted, so the simplest call is::

            for chunk in client.iter_stream(messages, protocol="anthropic"):
                print(chunk.text, end="", flush=True)

        Each chunk has a ``text`` delta (empty for non-text events),
        an optional ``tool_call_delta`` triple, a ``finish_reason``
        on the terminal chunk, and a ``finished`` flag so callers can
        ``break`` without inspecting strings.
        """
        if protocol == "openai":
            chunks = self.chat_stream(
                messages=messages,
                model=model or "gpt-4o",
                session_id=session_id,
                **kwargs,
            )
            yield from _normalize_openai_stream(chunks)
        elif protocol == "anthropic":
            chunks = self.message_stream(
                messages=messages,
                model=model or "claude-sonnet-4-20250514",
                session_id=session_id,
                **kwargs,
            )
            yield from _normalize_anthropic_stream(chunks)
        else:
            raise ValueError(
                f"unknown protocol {protocol!r}; expected 'openai' or 'anthropic'"
            )

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

    def run_pipeline(
        self,
        name: str,
        input: str,
        session_id: Optional[str] = None,
    ) -> PipelineResult:
        """Execute a loaded pipeline and return its ordered node trajectory."""
        payload: dict[str, Any] = {"input": input}
        if session_id:
            payload["session_id"] = session_id
        headers: dict[str, str] = {"Content-Type": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        resp = self._session.post(
            f"{self.base_url}/api/v1/pipelines/{quote(name, safe='')}/run",
            json=payload,
            headers=headers,
            timeout=self.timeout,
        )
        resp.raise_for_status()
        data = resp.json()
        if not isinstance(data, dict):
            raise ValueError("pipeline response must be a JSON object")
        return PipelineResult.from_wire(data)

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


# --- Stream normalizers (module-level, exercised by iter_stream) ---


def _normalize_openai_stream(
    chunks: Generator[dict[str, Any], None, None],
) -> Generator[StreamChunk, None, None]:
    """Convert raw OpenAI Chat Completions chunks to StreamChunks.

    Each ``choices[0].delta.content`` becomes a StreamChunk with the
    text delta. Tool-call deltas are passed through as
    ``(index, name, arguments_fragment)`` triples. The final chunk
    carries ``finish_reason`` (e.g. "stop", "tool_calls") and
    ``finished=True``.
    """
    for chunk in chunks:
        choices = chunk.get("choices", [])
        if not choices:
            continue
        choice = choices[0]
        delta = choice.get("delta", {}) or {}

        text = delta.get("content") or ""

        tool_call_delta: Optional[tuple[int, str, str]] = None
        for tc in delta.get("tool_calls", []) or []:
            idx = int(tc.get("index", 0))
            func = tc.get("function", {}) or {}
            tool_call_delta = (
                idx,
                func.get("name", "") or "",
                func.get("arguments", "") or "",
            )
            # Multiple tool-call deltas in one chunk are rare; take
            # the first and let the rest stream in subsequent chunks.
            break

        finish_reason = choice.get("finish_reason") or ""
        finished = bool(finish_reason)

        # Skip empty padding chunks (no text, no tool delta, no finish).
        if not text and tool_call_delta is None and not finished:
            continue

        yield StreamChunk(
            text=text,
            tool_call_delta=tool_call_delta,
            finish_reason=finish_reason,
            finished=finished,
            raw=chunk,
        )


def _normalize_anthropic_stream(
    events: Generator[dict[str, Any], None, None],
) -> Generator[StreamChunk, None, None]:
    """Convert raw Anthropic Messages events to StreamChunks.

    Anthropic streams a richer event vocabulary than OpenAI; this
    normalizer collapses it to the same StreamChunk shape:

    - ``content_block_delta`` of type ``text_delta`` -> text chunk.
    - ``content_block_delta`` of type ``input_json_delta`` -> tool
      call delta with the running JSON fragment.
    - ``content_block_start`` of type ``tool_use`` -> tool call delta
      with the tool name (no arguments yet).
    - ``message_delta`` and ``message_stop`` -> finish_reason on the
      terminal chunk.
    """
    current_tool_index = -1
    current_tool_name = ""
    final_stop = ""
    for event in events:
        et = event.get("type", "")

        if et == "content_block_start":
            block = event.get("content_block", {}) or {}
            if block.get("type") == "tool_use":
                current_tool_index = int(event.get("index", current_tool_index + 1))
                current_tool_name = block.get("name", "") or ""
                yield StreamChunk(
                    tool_call_delta=(current_tool_index, current_tool_name, ""),
                    raw=event,
                )

        elif et == "content_block_delta":
            delta = event.get("delta", {}) or {}
            dt = delta.get("type", "")
            if dt == "text_delta":
                text = delta.get("text", "") or ""
                if text:
                    yield StreamChunk(text=text, raw=event)
            elif dt == "input_json_delta":
                fragment = delta.get("partial_json", "") or ""
                if fragment:
                    yield StreamChunk(
                        tool_call_delta=(
                            current_tool_index,
                            current_tool_name,
                            fragment,
                        ),
                        raw=event,
                    )

        elif et == "message_delta":
            delta = event.get("delta", {}) or {}
            stop = delta.get("stop_reason") or ""
            if stop:
                final_stop = stop

        elif et == "message_stop":
            yield StreamChunk(
                finish_reason=final_stop or "end_turn",
                finished=True,
                raw=event,
            )
            return
