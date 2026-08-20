"""Tests for mockagents.server — server lifecycle management."""

import os
import subprocess
import sys
import tempfile
import threading
import time

import pytest
import yaml

from mockagents.server import MockAgentServer
from mockagents.types import ConfigError

DUMMY_BINARY = "nonexistent-binary-for-test"


def write_temp_yaml(content: dict) -> str:
    """Write YAML to a temp file and return its path (file is closed)."""
    fd, path = tempfile.mkstemp(suffix=".yaml")
    with os.fdopen(fd, "w") as f:
        yaml.dump(content, f)
    return path


def test_find_free_port():
    port = MockAgentServer._find_free_port()
    assert isinstance(port, int)
    assert 0 < port < 65536


def test_find_free_port_unique():
    ports = {MockAgentServer._find_free_port() for _ in range(10)}
    assert len(ports) >= 5


def test_server_url_property():
    server = MockAgentServer.__new__(MockAgentServer)
    server.port = 9999
    assert server.url == "http://127.0.0.1:9999"


def test_server_is_running_false_initially():
    server = MockAgentServer.__new__(MockAgentServer)
    server._process = None
    assert not server.is_running


def test_server_logs_empty_initially():
    server = MockAgentServer.__new__(MockAgentServer)
    server._logs = []
    assert server.logs == []


def test_from_config_file_not_found():
    with pytest.raises(FileNotFoundError, match="not found"):
        MockAgentServer.from_config("/nonexistent/path.yaml")


def test_from_config_invalid_yaml():
    fd, path = tempfile.mkstemp(suffix=".yaml")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(": invalid: yaml: [")
        with pytest.raises(ConfigError, match="YAML parse error"):
            MockAgentServer.from_config(path, binary_path=DUMMY_BINARY)
    finally:
        os.unlink(path)


def test_from_config_wrong_api_version():
    path = write_temp_yaml({"apiVersion": "wrong/v2", "kind": "Agent"})
    try:
        with pytest.raises(ConfigError, match="Invalid apiVersion"):
            MockAgentServer.from_config(path, binary_path=DUMMY_BINARY)
    finally:
        os.unlink(path)


def test_from_config_not_a_mapping():
    fd, path = tempfile.mkstemp(suffix=".yaml")
    try:
        with os.fdopen(fd, "w") as f:
            f.write("- just\n- a\n- list\n")
        with pytest.raises(ConfigError, match="not a YAML mapping"):
            MockAgentServer.from_config(path, binary_path=DUMMY_BINARY)
    finally:
        os.unlink(path)


def test_from_config_valid_file():
    path = write_temp_yaml({
        "apiVersion": "mockagents/v1",
        "kind": "Agent",
        "metadata": {"name": "test"},
        "spec": {
            "protocol": "openai-chat-completions",
            "behavior": {"scenarios": [{"name": "default", "response": {"content": "hi"}}]},
        },
    })
    try:
        server = MockAgentServer.from_config(path, port=9999, binary_path=DUMMY_BINARY)
        assert server.port == 9999
        assert server.agents_dir == os.path.dirname(os.path.abspath(path))
        assert server.binary_path == DUMMY_BINARY
    finally:
        os.unlink(path)


def test_from_config_multiple_files():
    paths = []
    try:
        for i in range(2):
            path = write_temp_yaml({
                "apiVersion": "mockagents/v1",
                "kind": "Agent",
                "metadata": {"name": f"agent-{i}"},
                "spec": {
                    "protocol": "openai-chat-completions",
                    "behavior": {"scenarios": [{"name": "default", "response": {"content": "hi"}}]},
                },
            })
            paths.append(path)

        server = MockAgentServer.from_config(paths, binary_path=DUMMY_BINARY)
        assert server.port > 0
    finally:
        for p in paths:
            os.unlink(p)


def test_find_binary_not_found(tmp_path, monkeypatch):
    from mockagents import _binary

    # Hermetic: empty PATH, no env override, a binary-less cwd + cache, so the
    # result doesn't depend on a locally-built ./mockagents or a primed cache.
    monkeypatch.setenv("PATH", "")
    monkeypatch.delenv("MOCKAGENTS_BINARY", raising=False)
    monkeypatch.delenv("MOCKAGENTS_AUTO_DOWNLOAD", raising=False)
    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr(_binary, "cache_dir", lambda: tmp_path / "empty-cache")
    # BinaryNotFoundError (a FileNotFoundError subclass) now carries actionable
    # install guidance instead of a bare message (RR-04).
    with pytest.raises(FileNotFoundError, match="binary was not found"):
        MockAgentServer._find_binary()


def test_stop_when_not_running():
    server = MockAgentServer.__new__(MockAgentServer)
    server._process = None
    server._logs = []
    server.stop()  # Should not raise.


def test_stop_does_not_block_on_a_live_child(tmp_path):
    """stop() must signal before draining, or it hangs forever.

    Reading a still-running child's stderr blocks until EOF, and a healthy
    server never closes stderr. Draining first therefore hung until something
    else killed the process — which meant every pytest session using the
    session-scoped ``mockagents_server`` fixture hung at teardown, since that
    fixture calls stop() on the way out.

    The child here is a plain Python process that writes one line to stderr and
    then sleeps: enough to have buffered output worth draining, and long-lived
    enough that a drain-first implementation never returns. stop() runs on a
    thread so a regression fails this test in ~15s instead of hanging the suite.
    """
    ready = tmp_path / "child-booted"
    child = (
        "import sys, time, pathlib; "
        "sys.stderr.write('booted'); sys.stderr.flush(); "
        f"pathlib.Path(r'{ready}').write_text('1'); "
        "time.sleep(300)"
    )
    proc = subprocess.Popen(
        [sys.executable, "-c", child],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    # Wait for the child to actually reach its sleep. Without this the test
    # races the interpreter's own startup and can terminate it before it has
    # written anything, which would test nothing.
    deadline = time.monotonic() + 15
    while not ready.exists() and time.monotonic() < deadline:
        assert proc.poll() is None, "child exited before signalling readiness"
        time.sleep(0.02)
    assert ready.exists(), "child never started"

    server = MockAgentServer.__new__(MockAgentServer)
    server._process = proc
    server._logs = []

    finished = threading.Event()

    def run_stop() -> None:
        server.stop()
        finished.set()

    threading.Thread(target=run_stop, daemon=True).start()
    returned = finished.wait(timeout=15)
    if not returned:
        # Unblock the stuck reader so the leaked thread can exit, then fail.
        proc.kill()
        proc.wait(timeout=5)
    assert returned, "stop() blocked on a live child: it must signal before draining"

    assert proc.poll() is not None, "stop() returned but left the child running"
    assert server._process is None
    # The child's stderr was still captured, not thrown away.
    assert any("booted" in entry for entry in server._logs)


def test_client_returns_mock_agent_client():
    server = MockAgentServer.__new__(MockAgentServer)
    server.port = 8080
    client = server.client()
    assert client.base_url == "http://127.0.0.1:8080"
    client.close()
