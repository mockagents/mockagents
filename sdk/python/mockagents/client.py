"""HTTP client for communicating with a MockAgents server."""

from __future__ import annotations

import json
import time
from dataclasses import dataclass
from typing import Any, Generator, Iterator, Optional
from urllib.parse import quote, unquote_plus

import requests

from .types import ChatResponse, PipelineResult, StreamChunk, ToolCall, ToolError, TokenUsage

#: Model sent by the OpenAI-protocol calls when none is given.
DEFAULT_OPENAI_MODEL = "gpt-4o"
#: Model sent by the Anthropic-protocol calls when none is given. The
#: TypeScript and Go SDKs use the same value, so a script routes to the same
#: agent in every language.
DEFAULT_ANTHROPIC_MODEL = "claude-sonnet-4-20250514"
#: ``anthropic-version`` header value sent on Anthropic calls.
ANTHROPIC_VERSION = "2023-06-01"
#: Response header listing simulated tool calls that resolved to an error.
TOOL_ERRORS_HEADER = "X-Mockagents-Tool-Errors"


class MockAgentClient:
    """HTTP client wrapper for the MockAgents server.

    Supports both OpenAI Chat Completions and Anthropic Messages protocols.

    Args:
        base_url: Server base URL (e.g., "http://127.0.0.1:8080").
        timeout: Request timeout in seconds (per connect and per read, so a
            long stream is fine as long as it keeps producing data).
        api_key: Credential sent on every request, streaming and management
            calls included, as ``Authorization: Bearer``. In multi-tenant mode
            it selects the tenant's agents; leaving it out reaches only the
            global ones.
    """

    def __init__(
        self,
        base_url: str = "http://127.0.0.1:8080",
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
        model: str = DEFAULT_OPENAI_MODEL,
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
            stream: Enable SSE streaming; the stream is aggregated into the
                returned ChatResponse.
            session_id: Session ID for conversation state.
            tools: Tool definitions.
            tool_choice: Tool choice setting.

        Returns:
            Parsed ChatResponse.

        Raises:
            requests.HTTPError: On a non-2xx status, streamed or not.
        """
        payload: dict[str, Any] = {"model": model, "messages": messages, "stream": stream}
        if tools:
            payload["tools"] = tools
        if tool_choice is not None:
            payload["tool_choice"] = tool_choice
        payload.update(kwargs)

        start = time.monotonic()
        resp = self._post(
            "/v1/chat/completions", payload, self._headers(session_id), stream=stream
        )
        latency_ms = (time.monotonic() - start) * 1000

        if stream:
            return self._parse_openai_stream(resp, latency_ms)
        return self._parse_openai_response(
            resp.json(), resp.status_code, latency_ms, headers=resp.headers
        )

    def chat_stream(
        self,
        messages: list[dict[str, Any]],
        model: str = DEFAULT_OPENAI_MODEL,
        session_id: Optional[str] = None,
        **kwargs: Any,
    ) -> Generator[dict[str, Any], None, None]:
        """Stream chat completion chunks (OpenAI format).

        Yields parsed chunk dictionaries. Frames that are not a JSON object
        are skipped; use ``chat(stream=True)`` to have them counted.
        """
        payload: dict[str, Any] = {"model": model, "messages": messages, "stream": True}
        payload.update(kwargs)

        resp = self._post("/v1/chat/completions", payload, self._headers(session_id), stream=True)
        yield from _iter_sse_json(resp, _StreamStats())

    def message_stream(
        self,
        messages: list[dict[str, Any]],
        model: str = DEFAULT_ANTHROPIC_MODEL,
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

        resp = self._post(
            "/v1/messages", payload, self._headers(session_id, anthropic=True), stream=True
        )
        for event in _iter_sse_json(resp, _StreamStats()):
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
        ``break`` without inspecting strings. A stream that ends without
        a chunk whose ``finished`` is True was truncated.
        """
        if protocol == "openai":
            chunks = self.chat_stream(
                messages=messages,
                model=model or DEFAULT_OPENAI_MODEL,
                session_id=session_id,
                **kwargs,
            )
            yield from _normalize_openai_stream(chunks)
        elif protocol == "anthropic":
            chunks = self.message_stream(
                messages=messages,
                model=model or DEFAULT_ANTHROPIC_MODEL,
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
        model: str = DEFAULT_ANTHROPIC_MODEL,
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
            stream: Enable SSE streaming; the stream is aggregated into the
                returned ChatResponse.
            session_id: Session ID for conversation state.
            tools: Tool definitions.

        Returns:
            Parsed ChatResponse.

        Raises:
            requests.HTTPError: On a non-2xx status, streamed or not.
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

        start = time.monotonic()
        resp = self._post(
            "/v1/messages", payload, self._headers(session_id, anthropic=True), stream=stream
        )
        latency_ms = (time.monotonic() - start) * 1000

        if stream:
            return self._parse_anthropic_stream(resp, latency_ms)
        return self._parse_anthropic_response(
            resp.json(), resp.status_code, latency_ms, headers=resp.headers
        )

    def health(self) -> dict[str, Any]:
        """Check server health."""
        return self._get_json("/api/v1/health")

    def list_agents(self) -> list[dict[str, Any]]:
        """List all loaded agents."""
        return self._get_json("/api/v1/agents")

    def get_agent(self, name: str) -> dict[str, Any]:
        """Get a specific agent definition."""
        return self._get_json(f"/api/v1/agents/{quote(name, safe='')}")

    def reload_agent(self, name: str) -> dict[str, Any]:
        """Reload an agent from disk."""
        resp = self._session.post(
            f"{self.base_url}/api/v1/agents/{quote(name, safe='')}/reload",
            headers=self._headers(),
            timeout=self.timeout,
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
        resp = self._session.post(
            f"{self.base_url}/api/v1/pipelines/{quote(name, safe='')}/run",
            json=payload,
            headers=self._headers(),
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

    # --- Request helpers ---

    def _headers(self, session_id: Optional[str] = None, anthropic: bool = False) -> dict[str, str]:
        """Headers for every request: the credential is added here, in one
        place, so no call path can drop it (review K-01)."""
        headers: dict[str, str] = {"Content-Type": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        if anthropic:
            # Anthropic clients authenticate with x-api-key. Without a key a
            # placeholder keeps the request shaped like a real client's.
            headers["X-Api-Key"] = self.api_key or "mock-api-key"
            headers["Anthropic-Version"] = ANTHROPIC_VERSION
        if session_id:
            headers["X-Session-Id"] = session_id
        return headers

    def _post(
        self, path: str, payload: dict[str, Any], headers: dict[str, str], stream: bool
    ) -> requests.Response:
        resp = self._session.post(
            f"{self.base_url}{path}",
            json=payload,
            headers=headers,
            timeout=self.timeout,
            stream=stream,
        )
        # Raise before reading a stream too: an error body parsed as an empty
        # stream would let negative assertions pass (review K-27).
        resp.raise_for_status()
        if stream:
            # SSE is UTF-8 by definition; requests would otherwise decode a
            # text/* body that names no charset as ISO-8859-1 (review K-05).
            resp.encoding = "utf-8"
        return resp

    def _get_json(self, path: str) -> Any:
        resp = self._session.get(
            f"{self.base_url}{path}", headers=self._headers(), timeout=self.timeout
        )
        resp.raise_for_status()
        return resp.json()

    # --- Response Parsers ---

    def _parse_openai_response(
        self,
        data: dict[str, Any],
        status_code: int,
        latency_ms: float,
        headers: Optional[Any] = None,
    ) -> ChatResponse:
        meta = _response_meta(headers)
        choices = data.get("choices", [])
        if not choices:
            return ChatResponse(raw=data, status_code=status_code, latency_ms=latency_ms, **meta)

        message = choices[0].get("message", {})
        content = message.get("content") or ""

        tool_calls = []
        for tc in message.get("tool_calls") or []:
            tool_calls.append(ToolCall.from_openai(tc))

        usage = TokenUsage.from_openai(data.get("usage", {}))

        return ChatResponse(
            content=content,
            model=data.get("model", ""),
            tool_calls=tool_calls,
            finish_reason=choices[0].get("finish_reason") or "",
            usage=usage,
            raw=data,
            status_code=status_code,
            latency_ms=latency_ms,
            **meta,
        )

    def _parse_openai_stream(
        self, resp: requests.Response, latency_ms: float
    ) -> ChatResponse:
        stats = _StreamStats()
        content_parts: list[str] = []
        tool_call_map: dict[int, dict[str, Any]] = {}
        finish_reason = ""
        model = ""
        usage = TokenUsage()

        for chunk in _iter_sse_json(resp, stats):
            model = model or chunk.get("model", "")
            if isinstance(chunk.get("usage"), dict):
                usage = TokenUsage.from_openai(chunk["usage"])
            choices = chunk.get("choices") or []
            if not choices:
                continue
            delta = choices[0].get("delta") or {}
            if delta.get("content"):
                content_parts.append(delta["content"])
            if choices[0].get("finish_reason"):
                finish_reason = choices[0]["finish_reason"]
            for tc in delta.get("tool_calls") or []:
                idx = tc.get("index", 0)
                if idx not in tool_call_map:
                    tool_call_map[idx] = {"id": "", "name": "", "arguments": ""}
                if tc.get("id"):
                    tool_call_map[idx]["id"] = tc["id"]
                func = tc.get("function") or {}
                if func.get("name"):
                    tool_call_map[idx]["name"] = func["name"]
                if func.get("arguments"):
                    tool_call_map[idx]["arguments"] += func["arguments"]

        tool_calls = [
            ToolCall.from_raw(tc["id"], tc["name"], tc["arguments"])
            for _, tc in sorted(tool_call_map.items())
        ]

        return ChatResponse(
            content="".join(content_parts),
            model=model,
            tool_calls=tool_calls,
            finish_reason=finish_reason,
            usage=usage,
            raw={},
            status_code=resp.status_code,
            latency_ms=latency_ms,
            truncated=not stats.completed,
            malformed_frames=stats.malformed_frames,
            **_response_meta(resp.headers),
        )

    def _parse_anthropic_response(
        self,
        data: dict[str, Any],
        status_code: int,
        latency_ms: float,
        headers: Optional[Any] = None,
    ) -> ChatResponse:
        content_parts: list[str] = []
        tool_calls: list[ToolCall] = []

        for block in data.get("content") or []:
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
            finish_reason=data.get("stop_reason") or "",
            usage=usage,
            raw=data,
            status_code=status_code,
            latency_ms=latency_ms,
            **_response_meta(headers),
        )

    def _parse_anthropic_stream(
        self, resp: requests.Response, latency_ms: float
    ) -> ChatResponse:
        stats = _StreamStats()
        content_parts: list[str] = []
        tool_calls: list[ToolCall] = []
        stop_reason = ""
        model = ""
        input_tokens = output_tokens = 0
        current_tool: Optional[dict[str, Any]] = None

        for event in _iter_sse_json(resp, stats):
            event_type = event.get("type", "")
            if event_type == "message_start":
                msg = event.get("message") or {}
                model = msg.get("model", "")
                input_tokens = (msg.get("usage") or {}).get("input_tokens", 0)
            elif event_type == "content_block_start":
                block = event.get("content_block") or {}
                if block.get("type") == "tool_use":
                    current_tool = {
                        "id": block.get("id", ""),
                        "name": block.get("name", ""),
                        "input_json": "",
                    }
            elif event_type == "content_block_delta":
                delta = event.get("delta") or {}
                delta_type = delta.get("type", "")
                if delta_type == "text_delta":
                    content_parts.append(delta.get("text", ""))
                elif delta_type == "input_json_delta" and current_tool:
                    current_tool["input_json"] += delta.get("partial_json", "")
            elif event_type == "content_block_stop":
                if current_tool:
                    tool_calls.append(
                        ToolCall.from_raw(
                            current_tool["id"], current_tool["name"], current_tool["input_json"]
                        )
                    )
                    current_tool = None
            elif event_type == "message_delta":
                delta = event.get("delta") or {}
                stop_reason = delta.get("stop_reason") or stop_reason
                output_tokens = (event.get("usage") or {}).get("output_tokens", output_tokens)
            elif event_type == "message_stop":
                stats.completed = True
                break

        return ChatResponse(
            content="".join(content_parts),
            model=model,
            tool_calls=tool_calls,
            finish_reason=stop_reason,
            usage=TokenUsage.from_anthropic(
                {"input_tokens": input_tokens, "output_tokens": output_tokens}
            ),
            raw={},
            status_code=resp.status_code,
            latency_ms=latency_ms,
            truncated=not stats.completed,
            malformed_frames=stats.malformed_frames,
            **_response_meta(resp.headers),
        )


# --- SSE parsing ---


@dataclass
class _StreamStats:
    """What a stream's framing said about how it ended."""

    completed: bool = False
    malformed_frames: int = 0


def _iter_sse_data(resp: requests.Response) -> Iterator[str]:
    """Yield the data of each SSE event, per the WHATWG event-stream rules:
    consecutive ``data:`` lines are joined with newlines, one optional space
    after the colon is stripped, and a blank line dispatches the event.
    Comment and ``event:``/``id:``/``retry:`` lines carry nothing the callers
    need (the event type is repeated inside each JSON payload)."""
    data_lines: list[str] = []
    for line in resp.iter_lines(decode_unicode=True):
        if line is None:
            continue
        if line == "":
            if data_lines:
                yield "\n".join(data_lines)
                data_lines = []
            continue
        if line.startswith(":"):
            continue
        name, _, value = line.partition(":")
        if name == "data":
            data_lines.append(value[1:] if value.startswith(" ") else value)
    if data_lines:
        # A final event without its blank line: deliver it rather than drop
        # it; the missing terminal frame still marks the stream truncated.
        yield "\n".join(data_lines)


def _iter_sse_json(resp: requests.Response, stats: _StreamStats) -> Iterator[dict[str, Any]]:
    """Yield each SSE event's JSON object until ``[DONE]``. Frames that are
    not a JSON object are counted in ``stats`` and skipped."""
    for data in _iter_sse_data(resp):
        if data == "[DONE]":
            stats.completed = True
            return
        try:
            event = json.loads(data)
        except json.JSONDecodeError:
            event = None
        if not isinstance(event, dict):
            stats.malformed_frames += 1
            continue
        yield event


def _response_meta(headers: Optional[Any]) -> dict[str, Any]:
    """ChatResponse fields that come from the response headers."""
    if not headers:
        return {}
    plain = {str(k): str(v) for k, v in headers.items()}
    return {"headers": plain, "tool_errors": _parse_tool_errors(headers.get(TOOL_ERRORS_HEADER))}


def _parse_tool_errors(value: Optional[str]) -> list[ToolError]:
    """Decode ``X-Mockagents-Tool-Errors``: comma-separated ``tool=code``
    pairs, each side query-escaped."""
    if not value:
        return []
    errors = []
    for pair in value.split(","):
        tool, _, code = pair.partition("=")
        errors.append(ToolError(code=unquote_plus(code), tool=unquote_plus(tool)))
    return errors


# --- Stream normalizers (module-level, exercised by iter_stream) ---


def _normalize_openai_stream(
    chunks: Iterator[dict[str, Any]],
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
    events: Iterator[dict[str, Any]],
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
