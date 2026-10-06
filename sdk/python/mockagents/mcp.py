"""MCP bidirectional transport helpers for the Python SDK.

The MockAgents server speaks JSON-RPC 2.0 over HTTP (`POST /mcp`) and
stdio. v0.3 added a *bidirectional* channel that lets the server push
JSON-RPC requests and notifications out to a subscribed client — the
mechanism that powers `sampling/createMessage` and `roots/list`.

Before this module, exercising that channel from a Python test meant
hand-rolling an SSE parser on top of ``requests.Response.iter_lines``
and a matching ``POST /mcp/response`` call. This module wraps both
halves:

- :class:`McpClient` — session-backed facade with ``connect()``
  (opens the SSE stream), ``send_response()`` (posts the reply for
  a server-initiated request), and ``dispatch_request()`` (routes
  one event through a ``method -> handler`` map and auto-posts the
  result).
- :class:`McpEventStream` — a context-managed iterator over
  :class:`McpEvent` objects. Heartbeat comments (``:heartbeat``) are
  skipped; multi-line ``data:`` frames are joined per the SSE spec;
  malformed payloads are dropped rather than raising so a
  misbehaving server cannot break a test harness mid-stream.
- :class:`McpEvent` — one parsed frame. ``kind`` is ``"request"`` or
  ``"notification"``; ``payload`` is the decoded JSON-RPC object.
  Convenience properties (``is_request``, ``request_id``,
  ``method``, ``params``) keep call sites short.

The goal is parity with the raw HTTP surface: anything a hand-rolled
``requests`` loop can do, this module can do in a third of the lines.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Callable, Generator, Optional

import requests


@dataclass
class McpEvent:
    """One parsed SSE frame from ``/mcp/events``.

    ``kind`` is the SSE ``event:`` line — the MockAgents server emits
    ``"request"`` for server-initiated JSON-RPC requests (with an id
    that the client must echo in its reply) and ``"notification"`` for
    fire-and-forget notifications (no id).

    ``payload`` is the raw JSON-RPC object. For requests the shape is
    ``{"jsonrpc": "2.0", "id": ..., "method": ..., "params": {...}}``;
    for notifications the ``id`` is absent.
    """

    kind: str
    payload: dict[str, Any] = field(default_factory=dict)

    @property
    def is_request(self) -> bool:
        """True when this event is a server-initiated request that
        expects a reply. False for notifications."""
        return self.kind == "request" and "id" in self.payload

    @property
    def is_notification(self) -> bool:
        """True when this event is a fire-and-forget notification."""
        return self.kind == "notification" or "id" not in self.payload

    @property
    def request_id(self) -> Any:
        """The JSON-RPC ``id`` field, or ``None`` for notifications."""
        return self.payload.get("id")

    @property
    def method(self) -> str:
        """The JSON-RPC ``method`` field, or ``""`` when absent."""
        return str(self.payload.get("method", ""))

    @property
    def params(self) -> dict[str, Any]:
        """The JSON-RPC ``params`` object, or an empty dict when absent.
        Always returns a dict so handlers can use ``params.get(...)``
        without first checking for None."""
        p = self.payload.get("params")
        return p if isinstance(p, dict) else {}


class McpEventStream:
    """Context-managed iterator over ``/mcp/events`` SSE frames.

    Use via :meth:`McpClient.connect` rather than instantiating
    directly::

        with client.connect() as stream:
            for event in stream:
                if event.is_request:
                    client.dispatch_request(event, handlers)

    The iterator terminates when the underlying HTTP connection
    closes (server shutdown, timeout, or an explicit ``stream.close()``
    / ``__exit__``). Heartbeats and comment lines are silently
    skipped so callers never see them.
    """

    def __init__(self, response: requests.Response) -> None:
        self._resp = response
        # SSE is UTF-8 by definition; requests would otherwise decode a
        # text/* body that names no charset as ISO-8859-1 (review K-05).
        self._resp.encoding = "utf-8"
        self._closed = False

    def __enter__(self) -> "McpEventStream":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the underlying HTTP response. Safe to call multiple
        times — the second and later calls are no-ops."""
        if not self._closed:
            try:
                self._resp.close()
            except Exception:  # pragma: no cover — defensive only
                pass
            self._closed = True

    def __iter__(self) -> Generator[McpEvent, None, None]:
        return self._iter()

    def _iter(self) -> Generator[McpEvent, None, None]:
        event_name = ""
        data_lines: list[str] = []
        for raw in self._resp.iter_lines(decode_unicode=True):
            if raw is None:
                # requests yields None on keepalive in some versions.
                continue
            line = raw
            if line == "":
                # Blank line = frame terminator. Emit what we have.
                if data_lines:
                    payload = _try_json("\n".join(data_lines))
                    if payload is not None:
                        yield McpEvent(kind=event_name or "message", payload=payload)
                event_name = ""
                data_lines = []
                continue
            if line.startswith(":"):
                # SSE comment / heartbeat.
                continue
            if line.startswith("event:"):
                event_name = line[len("event:"):].strip()
            elif line.startswith("data:"):
                chunk = line[len("data:"):]
                # SSE strips exactly one leading space after the colon.
                if chunk.startswith(" "):
                    chunk = chunk[1:]
                data_lines.append(chunk)
        # Drain any trailing frame the server emitted without a
        # terminating blank line (unusual but legal).
        if data_lines:
            payload = _try_json("\n".join(data_lines))
            if payload is not None:
                yield McpEvent(kind=event_name or "message", payload=payload)


class McpClient:
    """HTTP client for the MockAgents MCP bidirectional transport.

    One client owns a ``requests.Session`` and exposes three entry
    points:

    - :meth:`connect` opens a long-lived SSE subscription to
      ``GET /mcp/events``.
    - :meth:`send_response` POSTs a JSON-RPC reply to
      ``POST /mcp/response``, unblocking the matching ``SendRequest``
      caller on the server.
    - :meth:`dispatch_request` is a convenience that looks up a
      handler by method name, calls it with the request params, and
      auto-posts the result (or a ``-32601`` / ``-32603`` error when
      the lookup fails or the handler raises).

    Most test harnesses only need ``connect()`` + ``dispatch_request``.
    Callers that want full control over the response envelope can
    bypass the dispatcher and call ``send_response`` directly.
    """

    def __init__(
        self,
        base_url: str = "http://127.0.0.1:8080",
        *,
        timeout: Optional[float] = None,
        api_key: Optional[str] = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self._session = requests.Session()
        if api_key:
            # Session-wide, so every call carries it (review K-01).
            self._session.headers["Authorization"] = f"Bearer {api_key}"

    def close(self) -> None:
        """Close the underlying session. Idempotent."""
        self._session.close()

    def __enter__(self) -> "McpClient":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    def connect(self) -> McpEventStream:
        """Open a subscription to ``GET /mcp/events`` and return a
        parsed event stream. The stream must be explicitly closed
        (via ``with`` or ``.close()``) so the HTTP connection is
        released back to the session pool."""
        resp = self._session.get(
            f"{self.base_url}/mcp/events",
            headers={"Accept": "text/event-stream"},
            stream=True,
            timeout=self.timeout,
        )
        resp.raise_for_status()
        return McpEventStream(resp)

    def send_response(
        self,
        request_id: Any,
        *,
        result: Optional[dict[str, Any]] = None,
        error: Optional[dict[str, Any]] = None,
    ) -> None:
        """POST a JSON-RPC reply for a server-initiated request.

        Exactly one of ``result`` or ``error`` must be supplied. The
        server routes the reply to the matching ``SendRequest`` caller
        by id; passing an unknown id returns 404 (surfaced here as
        ``requests.HTTPError``).
        """
        if (result is None) == (error is None):
            raise ValueError("exactly one of result= or error= is required")
        body: dict[str, Any] = {"jsonrpc": "2.0", "id": request_id}
        if result is not None:
            body["result"] = result
        if error is not None:
            body["error"] = error
        resp = self._session.post(
            f"{self.base_url}/mcp/response",
            json=body,
            timeout=self.timeout,
        )
        resp.raise_for_status()

    def dispatch_request(
        self,
        event: McpEvent,
        handlers: dict[str, Callable[[dict[str, Any]], dict[str, Any]]],
    ) -> Any:
        """Route a single server-initiated request through a handler
        map and POST the matching response. Returns the handler's
        return value on success.

        - If ``event`` is not a request, raises ``ValueError`` — the
          caller should branch on ``event.is_request`` first.
        - If no handler matches ``event.method``, a JSON-RPC
          ``-32601`` error is POSTed and ``KeyError(method)`` is
          raised.
        - If the handler raises, a JSON-RPC ``-32603`` error is POSTed
          carrying the exception string and the original exception is
          re-raised so the test can still see the failure.
        """
        if not event.is_request:
            raise ValueError("dispatch_request called on a non-request event")
        handler = handlers.get(event.method)
        if handler is None:
            self.send_response(
                event.request_id,
                error={
                    "code": -32601,
                    "message": f"method {event.method!r} not handled",
                },
            )
            raise KeyError(event.method)
        try:
            result = handler(event.params)
            if result is None:
                # A handler with nothing to return still owes the server a
                # reply; without one SendRequest blocks until it times out.
                # Mirrors the Go SDK (review K-15).
                result = {}
        except Exception as exc:
            self.send_response(
                event.request_id,
                error={
                    "code": -32603,
                    "message": str(exc),
                },
            )
            raise
        self.send_response(event.request_id, result=result)
        return result


def _try_json(data: str) -> Optional[dict[str, Any]]:
    """Decode JSON defensively. Returns None on any parse error so
    callers can continue draining the stream. Non-dict payloads are
    wrapped as ``{"__raw__": value}`` so typed accessors still work
    on ``McpEvent.payload``."""
    try:
        parsed = json.loads(data)
    except json.JSONDecodeError:
        return None
    if isinstance(parsed, dict):
        return parsed
    return {"__raw__": parsed}
