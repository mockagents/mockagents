"""Wire-level tests: the client against a real local HTTP server.

The other client tests call the private parsers directly, which left request
construction (headers, credentials, URL encoding) and byte-level stream
handling untested. These run every call path over a socket.
"""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable

import pytest
import requests

from mockagents import MockAgentClient, Scenario, expect, run_scenario
from mockagents.client import DEFAULT_ANTHROPIC_MODEL, DEFAULT_OPENAI_MODEL

Route = Callable[["_Handler"], None]


class _Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"  # the body ends when the connection closes
    routes: dict[tuple[str, str], Route] = {}
    seen: list[dict[str, Any]] = []

    def _record(self) -> None:
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        self.seen.append({
            "method": self.command,
            "path": self.path,
            "headers": {k.lower(): v for k, v in self.headers.items()},
            "body": json.loads(body) if body else None,
        })
        route = self.routes.get((self.command, self.path.split("?")[0]))
        if route is None:
            self.send_response(404)
            self.end_headers()
            return
        route(self)

    do_GET = _record
    do_POST = _record

    def log_message(self, *args: Any) -> None:
        pass

    # -- response helpers --

    def json_reply(self, obj: Any, status: int = 200, headers: dict[str, str] | None = None) -> None:
        data = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(data)

    def sse_reply(self, raw: bytes, headers: dict[str, str] | None = None, piece: int = 0) -> None:
        # No charset on purpose: that is what made requests fall back to
        # ISO-8859-1 (review K-05).
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        step = piece or len(raw)
        for i in range(0, len(raw), step):
            self.wfile.write(raw[i:i + step])
            self.wfile.flush()


@pytest.fixture
def server():
    _Handler.routes = {}
    _Handler.seen = []
    httpd = ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
    thread = threading.Thread(target=httpd.serve_forever, kwargs={"poll_interval": 0.02}, daemon=True)
    thread.start()
    try:
        yield httpd, f"http://127.0.0.1:{httpd.server_address[1]}"
    finally:
        httpd.shutdown()
        httpd.server_close()


def _openai_json(content: str = "ok") -> Route:
    return lambda h: h.json_reply({
        "model": "gpt-4o",
        "choices": [{"message": {"role": "assistant", "content": content}, "finish_reason": "stop"}],
    })


def _openai_sse(*frames: str, done: bool = True) -> bytes:
    out = "".join(f"data: {f}\n\n" for f in frames)
    if done:
        out += "data: [DONE]\n\n"
    return out.encode("utf-8")


def _anthropic_json(text: str = "ok") -> Route:
    return lambda h: h.json_reply({
        "model": "claude", "stop_reason": "end_turn",
        "content": [{"type": "text", "text": text}],
    })


ANTHROPIC_SSE = (
    'event: message_start\ndata: {"type":"message_start","message":{"model":"claude"}}\n\n'
    'event: content_block_delta\ndata: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}\n\n'
    'event: message_stop\ndata: {"type":"message_stop"}\n\n'
).encode()


# --- K-01: the credential reaches every call path ---


def test_api_key_sent_on_every_call_path(server):
    _, url = server
    _Handler.routes = {
        ("POST", "/v1/chat/completions"): lambda h: (
            h.sse_reply(_openai_sse('{"choices":[{"delta":{"content":"x"}}]}'))
            if h.seen[-1]["body"].get("stream") else _openai_json()(h)
        ),
        ("POST", "/v1/messages"): lambda h: (
            h.sse_reply(ANTHROPIC_SSE) if h.seen[-1]["body"].get("stream") else _anthropic_json()(h)
        ),
        ("GET", "/api/v1/health"): lambda h: h.json_reply({"status": "ok"}),
        ("GET", "/api/v1/agents"): lambda h: h.json_reply([]),
        ("GET", "/api/v1/agents/a%2Fb"): lambda h: h.json_reply({}),
        ("POST", "/api/v1/agents/a%2Fb/reload"): lambda h: h.json_reply({}),
    }
    client = MockAgentClient(url, api_key="tenant-key")
    msgs = [{"role": "user", "content": "x"}]
    client.chat(msgs)
    client.chat(msgs, stream=True)
    list(client.chat_stream(msgs))
    client.message(msgs)
    client.message(msgs, stream=True)
    list(client.message_stream(msgs))
    list(client.iter_stream(msgs, protocol="openai"))
    list(client.iter_stream(msgs, protocol="anthropic"))
    client.health()
    client.list_agents()
    client.get_agent("a/b")  # also: the name is URL-encoded
    client.reload_agent("a/b")

    assert len(_Handler.seen) == 12
    for req in _Handler.seen:
        assert req["headers"].get("authorization") == "Bearer tenant-key", req["path"]
    for req in _Handler.seen:
        if req["path"] == "/v1/messages":
            # The Anthropic auth header carries the real key, not the placeholder.
            assert req["headers"]["x-api-key"] == "tenant-key"


def test_no_api_key_sends_no_authorization(server):
    _, url = server
    _Handler.routes = {("POST", "/v1/messages"): _anthropic_json()}
    MockAgentClient(url).message([{"role": "user", "content": "x"}])
    headers = _Handler.seen[0]["headers"]
    assert "authorization" not in headers
    assert headers["x-api-key"] == "mock-api-key"


# --- K-05: streamed text is decoded as UTF-8 ---


@pytest.mark.parametrize("piece", [1, 3, 0])
def test_stream_decodes_utf8_split_across_chunks(server, piece):
    _, url = server
    text = "café ☕ 日本"
    frame = json.dumps({"choices": [{"delta": {"content": text}}]}, ensure_ascii=False)
    _Handler.routes = {("POST", "/v1/chat/completions"): lambda h: h.sse_reply(_openai_sse(frame), piece=piece)}
    resp = MockAgentClient(url).chat([{"role": "user", "content": "x"}], stream=True)
    assert resp.content == text
    expect(resp).to_have_response_containing("café")


# --- K-27: a failed streamed request raises instead of looking empty ---


@pytest.mark.parametrize("call", ["chat", "message"])
def test_streamed_error_status_raises(server, call):
    _, url = server
    fail = lambda h: h.json_reply({"error": {"message": "boom"}}, status=500)  # noqa: E731
    _Handler.routes = {("POST", "/v1/chat/completions"): fail, ("POST", "/v1/messages"): fail}
    client = MockAgentClient(url)
    with pytest.raises(requests.HTTPError):
        getattr(client, call)([{"role": "user", "content": "x"}], stream=True)


# --- K-04: tool errors are read from the response header ---


@pytest.mark.parametrize("stream", [False, True])
def test_tool_errors_header_feeds_to_have_tool_error(server, stream):
    _, url = server
    headers = {"X-Mockagents-Tool-Errors": "lookup_order=NOT_FOUND,other=bad+code%2C+x"}

    def route(h: _Handler) -> None:
        if stream:
            h.sse_reply(_openai_sse('{"choices":[{"delta":{"content":"x"}}]}'), headers=headers)
        else:
            h.json_reply({"choices": [{"message": {"content": "x"}}]}, headers=headers)

    _Handler.routes = {("POST", "/v1/chat/completions"): route}
    resp = MockAgentClient(url).chat([{"role": "user", "content": "x"}], stream=stream)
    assert [(e.tool, e.code) for e in resp.tool_errors] == [
        ("lookup_order", "NOT_FOUND"), ("other", "bad code, x"),
    ]
    expect(resp).to_have_tool_error("NOT_FOUND")
    expect(resp).to_have_tool_error("NOT_FOUND", tool="lookup_order")
    with pytest.raises(AssertionError, match="TIMEOUT"):
        expect(resp).to_have_tool_error("TIMEOUT")
    with pytest.raises(AssertionError):
        expect(resp).to_have_tool_error("NOT_FOUND", tool="other")


def test_no_tool_errors_header_means_no_tool_errors(server):
    _, url = server
    _Handler.routes = {("POST", "/v1/chat/completions"): _openai_json()}
    resp = MockAgentClient(url).chat([{"role": "user", "content": "x"}])
    assert resp.tool_errors == []
    with pytest.raises(AssertionError, match="Expected tool error"):
        expect(resp).to_have_tool_error("NOT_FOUND")


# --- K-14: injected stream faults are observable ---


def test_truncated_and_malformed_openai_stream_is_flagged(server):
    _, url = server
    raw = _openai_sse('{"choices":[{"delta":{"content":"Hel"}}]}', done=False)
    raw += b'data: {"mockagents_fault":"malformed",\n\n'
    _Handler.routes = {("POST", "/v1/chat/completions"): lambda h: h.sse_reply(raw)}
    resp = MockAgentClient(url).chat([{"role": "user", "content": "x"}], stream=True)
    assert resp.content == "Hel"
    assert resp.truncated is True
    assert resp.malformed_frames == 1


def test_complete_streams_are_not_flagged(server):
    _, url = server
    _Handler.routes = {
        ("POST", "/v1/chat/completions"): lambda h: h.sse_reply(_openai_sse('{"choices":[{"delta":{"content":"a"}}]}')),
        ("POST", "/v1/messages"): lambda h: h.sse_reply(ANTHROPIC_SSE),
    }
    client = MockAgentClient(url)
    for resp in (
        client.chat([{"role": "user", "content": "x"}], stream=True),
        client.message([{"role": "user", "content": "x"}], stream=True),
    ):
        assert resp.truncated is False
        assert resp.malformed_frames == 0


def test_truncated_anthropic_stream_is_flagged(server):
    _, url = server
    raw = ANTHROPIC_SSE.split(b"event: message_stop")[0]
    _Handler.routes = {("POST", "/v1/messages"): lambda h: h.sse_reply(raw)}
    resp = MockAgentClient(url).message([{"role": "user", "content": "x"}], stream=True)
    assert resp.content == "hi"
    assert resp.truncated is True


# --- SSE framing per the event-stream spec ---


def test_sse_framing_variants(server):
    _, url = server
    raw = (
        'data:{"choices":[{"delta":{"content":"a"}}]}\r\n\r\n'  # no space, CRLF
        'data: {"choices":[{"delta":\n'  # one event over two data lines
        'data: {"content":"b"}}]}\n\n'
        ': heartbeat comment\n\n'
        'data: [DONE]\n\n'
    ).encode()
    _Handler.routes = {("POST", "/v1/chat/completions"): lambda h: h.sse_reply(raw)}
    resp = MockAgentClient(url).chat([{"role": "user", "content": "x"}], stream=True)
    assert resp.content == "ab"
    assert resp.malformed_frames == 0
    assert resp.truncated is False


def test_streamed_malformed_tool_arguments_are_kept_raw(server):
    _, url = server
    frame = json.dumps({"choices": [{"delta": {"tool_calls": [
        {"index": 0, "id": "call_1", "function": {"name": "search", "arguments": "{not json"}},
    ]}}]})
    _Handler.routes = {("POST", "/v1/chat/completions"): lambda h: h.sse_reply(_openai_sse(frame))}
    resp = MockAgentClient(url).chat([{"role": "user", "content": "x"}], stream=True)
    tc = resp.tool_calls[0]
    assert (tc.arguments, tc.raw_arguments, tc.arguments_valid) == ({}, "{not json", False)
    expect(resp).to_have_malformed_tool_arguments("search")


# --- K-07: run_scenario sends what the TS and Go runners send ---


def test_run_scenario_sends_only_user_steps(server):
    _, url = server
    replies = iter(["first reply", "second reply"])
    _Handler.routes = {("POST", "/v1/chat/completions"): lambda h: _openai_json(next(replies))(h)}
    scenario = Scenario(name="s", steps=[
        {"role": "system", "content": "be brief"},
        {"role": "user", "content": "one"},
        {"role": "assistant", "content": "scripted"},
        {"role": "user", "content": "two"},
    ])
    result = run_scenario(MockAgentClient(url), scenario)

    assert len(_Handler.seen) == 2
    roles = [[m["role"] for m in req["body"]["messages"]] for req in _Handler.seen]
    assert roles == [
        ["system", "user"],
        ["system", "user", "assistant", "assistant", "user"],
    ]
    assert _Handler.seen[1]["body"]["messages"][2]["content"] == "first reply"
    assert all(req["body"]["model"] == DEFAULT_OPENAI_MODEL for req in _Handler.seen)
    assert len(result.interactions) == 2
    expect(result).to_have_response_containing("second reply")


def test_run_scenario_anthropic_hoists_system_and_uses_anthropic_model(server):
    _, url = server
    _Handler.routes = {("POST", "/v1/messages"): _anthropic_json("hi")}
    scenario = Scenario(name="s", protocol="anthropic", steps=[
        {"role": "system", "content": "be brief"},
        {"role": "user", "content": "one"},
    ])
    run_scenario(MockAgentClient(url), scenario)
    body = _Handler.seen[0]["body"]
    assert body["system"] == "be brief"
    assert [m["role"] for m in body["messages"]] == ["user"]
    assert body["model"] == DEFAULT_ANTHROPIC_MODEL


def test_run_scenario_keeps_empty_assistant_turns(server):
    _, url = server
    _Handler.routes = {("POST", "/v1/chat/completions"): lambda h: h.json_reply({"choices": [{"message": {
        "content": None,
        "tool_calls": [{"id": "c1", "function": {"name": "search", "arguments": "{}"}}],
    }, "finish_reason": "tool_calls"}]})}
    scenario = Scenario(name="s", steps=[
        {"role": "user", "content": "one"},
        {"role": "user", "content": "two"},
    ])
    run_scenario(MockAgentClient(url), scenario)
    roles = [m["role"] for m in _Handler.seen[1]["body"]["messages"]]
    assert roles == ["user", "assistant", "user"]


@pytest.mark.parametrize("steps", [[], [{"role": "system", "content": "x"}]])
def test_run_scenario_rejects_scenarios_without_user_steps(steps):
    with pytest.raises(ValueError, match="at least one user step"):
        run_scenario(MockAgentClient("http://127.0.0.1:9"), Scenario(name="s", steps=steps))
