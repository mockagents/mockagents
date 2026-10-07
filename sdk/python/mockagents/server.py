"""MockAgentServer — manages the MockAgents Go binary as a subprocess."""

from __future__ import annotations

import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import warnings
import weakref
from typing import Any, Optional

import yaml

from ._binary import ensure_binary
from .client import MockAgentClient
from .types import ConfigError, ServerError

_MAX_LOG_BYTES = 8 * 1024 * 1024


class MockAgentServer:
    """Manages the lifecycle of a MockAgents server subprocess.

    The server runs the Go binary as a child process with automatic port
    selection and health check polling.

    Args:
        agents_dir: Directory containing agent YAML definitions.
        port: Server port. 0 means auto-select a free port.
        binary_path: Path to the mockagents binary. Auto-detected if None.
        log_level: Server log level (debug, info, warn, error).
        config_path: Deprecated and ignored: ``mockagents start`` has no
            project-config option. Passing it emits a DeprecationWarning.
        auto_download: When the binary isn't found, download the matching
            release binary from GitHub and cache it (Playwright-style). Also
            enabled by setting MOCKAGENTS_AUTO_DOWNLOAD=1. Default False, which
            instead raises BinaryNotFoundError with install instructions.

    Example:
        with MockAgentServer(agents_dir="./agents") as server:
            client = server.client()
            resp = client.chat([{"role": "user", "content": "hello"}])
    """

    def __init__(
        self,
        agents_dir: str = "./agents",
        port: int = 0,
        binary_path: Optional[str] = None,
        log_level: str = "warn",
        config_path: Optional[str] = None,
        auto_download: bool = False,
    ):
        self.agents_dir = os.path.abspath(agents_dir)
        self.port = port if port != 0 else self._find_free_port()
        self.auto_download = auto_download
        # Resolve (or download) the binary up front so failures surface a clear,
        # actionable BinaryNotFoundError at construction — not a cryptic
        # FileNotFoundError later.
        self.binary_path = binary_path or ensure_binary(auto_download=auto_download)
        self.log_level = log_level
        if config_path is not None:
            warnings.warn(
                "MockAgentServer(config_path=...) has no effect and is deprecated; "
                "use agents_dir, or MockAgentServer.from_config() to serve specific files",
                DeprecationWarning,
                stacklevel=2,
            )
        self.config_path = config_path
        self._process: Optional[subprocess.Popen[bytes]] = None
        self._logs: list[str] = []
        self._log_bytes = 0
        self._log_threads: list[threading.Thread] = []
        self._log_lock = threading.Lock()

    @classmethod
    def from_config(
        cls,
        config_path: str | list[str],
        port: int = 0,
        binary_path: Optional[str] = None,
        log_level: str = "warn",
        auto_download: bool = False,
    ) -> MockAgentServer:
        """Create a MockAgentServer that serves exactly the given YAML file(s).

        The files are copied into a private temporary directory, which is
        served as the agents directory and removed when the server object is
        garbage collected. So only the listed files load, even when they sit
        next to other YAML files or in different directories.

        Args:
            config_path: Path to agent YAML file(s). Can be a single path
                or a list of paths. A file may hold several ``---``-separated
                documents, as the server accepts.
            port: Server port (0 for auto-select).
            binary_path: Path to mockagents binary.
            log_level: Server log level.

        Returns:
            Configured MockAgentServer instance.

        Raises:
            ConfigError: If any YAML file is invalid or the list is empty.
            FileNotFoundError: If a file does not exist.
        """
        paths = [config_path] if isinstance(config_path, str) else list(config_path)
        if not paths:
            raise ConfigError("from_config needs at least one agent config file")

        abs_paths = [os.path.abspath(p) for p in paths]
        for abs_path in abs_paths:
            _check_agent_file(abs_path)

        agents_dir = tempfile.mkdtemp(prefix="mockagents-agents-")
        try:
            for i, abs_path in enumerate(abs_paths):
                # The index prefix keeps same-named files from different
                # directories apart and preserves the caller's order.
                dest = os.path.join(agents_dir, f"{i:03d}-{os.path.basename(abs_path)}")
                shutil.copyfile(abs_path, dest)
            server = cls(
                agents_dir=agents_dir,
                port=port,
                binary_path=binary_path,
                log_level=log_level,
                auto_download=auto_download,
            )
        except BaseException:
            shutil.rmtree(agents_dir, ignore_errors=True)
            raise
        weakref.finalize(server, shutil.rmtree, agents_dir, ignore_errors=True)
        return server

    def start(self, timeout: float = 10.0) -> None:
        """Start the MockAgents server subprocess.

        Args:
            timeout: Maximum seconds to wait for the server to be ready.

        Raises:
            ServerError: If the server fails to start.
            TimeoutError: If the server doesn't respond within timeout.
        """
        if self._process is not None:
            raise ServerError("Server is already running")

        with self._log_lock:
            self._logs = []
            self._log_bytes = 0

        cmd = [
            self.binary_path,
            "start",
            "--port", str(self.port),
            "--log-level", self.log_level,
            "--agents-dir", self.agents_dir,
        ]

        try:
            self._process = subprocess.Popen(
                cmd,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=os.environ.copy(),
            )
        except OSError as e:
            raise ServerError(f"Failed to start server: {e}") from e

        self._log_threads = []
        for stream in (self._process.stdout, self._process.stderr):
            if stream is not None:
                thread = threading.Thread(target=self._drain_stream, args=(stream,), daemon=True)
                thread.start()
                self._log_threads.append(thread)

        try:
            self._wait_for_ready(timeout)
        except BaseException:
            self.stop()
            raise

    def stop(self) -> None:
        """Stop the MockAgents server subprocess.

        Signals FIRST, then drains the pipes. The order matters: reading a
        still-running child's stderr blocks until EOF, and a healthy server
        never closes stderr, so draining first hung forever. That made every
        pytest session using the ``mockagents_server`` fixture hang at
        teardown, since the session-scoped fixture calls this on the way out.

        Dedicated reader threads continuously drain stdout and stderr while the
        process runs, so a chatty server cannot fill a pipe and deadlock either
        startup or exit.
        """
        if self._process is None:
            return

        proc = self._process

        try:
            if sys.platform == "win32":
                proc.terminate()
            else:
                proc.send_signal(signal.SIGTERM)
        except OSError:
            pass  # already gone

        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                # Keep the handle so callers can observe and retry cleanup.
                # Reporting success here would leak a live child while
                # is_running falsely claimed the server had stopped.
                raise ServerError("Server process did not exit after kill")
        except (OSError, ValueError):
            pass  # pipes already closed

        self._process = None

        log_threads = getattr(self, "_log_threads", [])
        for thread in log_threads:
            thread.join(timeout=2)
        self._log_threads = []
        if not log_threads:
            # Backward-compatible fallback for manually supplied Popen handles.
            for stream in (proc.stdout, proc.stderr):
                if stream is not None:
                    try:
                        data = stream.read()
                    except (OSError, ValueError):
                        data = b""
                    if data:
                        self._logs.append(data.decode("utf-8", errors="replace"))
        for stream in (proc.stdout, proc.stderr):
            if stream is not None:
                try:
                    stream.close()
                except (OSError, ValueError):
                    pass

    def client(self, api_key: Optional[str] = None) -> MockAgentClient:
        """Create a MockAgentClient connected to this server.

        Args:
            api_key: Credential sent on every request (multi-tenant mode).

        Returns:
            Configured MockAgentClient instance.
        """
        return MockAgentClient(base_url=self.url, api_key=api_key)

    @property
    def url(self) -> str:
        """The server's base URL.

        Uses 127.0.0.1 (not localhost) to match the Go binary's IPv4-only
        default bind; localhost can resolve to ::1 first on dual-stack hosts,
        adding connection-refused fallback latency to health polls/requests.
        """
        return f"http://127.0.0.1:{self.port}"

    @property
    def is_running(self) -> bool:
        """Whether the server process is running."""
        return self._process is not None and self._process.poll() is None

    @property
    def logs(self) -> list[str]:
        """Captured server log output."""
        lock = getattr(self, "_log_lock", None)
        if lock is None:
            return list(self._logs)
        with lock:
            return list(self._logs)

    def __enter__(self) -> MockAgentServer:
        self.start()
        return self

    def __exit__(self, *args: Any) -> None:
        self.stop()

    # --- Private Helpers ---

    def _wait_for_ready(self, timeout: float) -> None:
        """Poll the health endpoint until the server is ready."""
        import requests as req

        deadline = time.monotonic() + timeout
        url = f"{self.url}/api/v1/health"
        last_error: Optional[Exception] = None

        while time.monotonic() < deadline:
            # Check if process died.
            if self._process and self._process.poll() is not None:
                for thread in self._log_threads:
                    thread.join(timeout=0.5)
                with self._log_lock:
                    output = "".join(self._logs)
                raise ServerError(
                    f"Server process exited with code {self._process.returncode}: {output}"
                )

            try:
                resp = req.get(url, timeout=1)
                if resp.status_code == 200:
                    return
            except req.ConnectionError as e:
                last_error = e
            except req.Timeout as e:
                last_error = e

            time.sleep(0.1)

        raise TimeoutError(
            f"Server not ready after {timeout}s on port {self.port}: {last_error}"
        )

    def _drain_stream(self, stream: Any) -> None:
        """Continuously drain one child pipe so verbose output cannot block it."""
        while True:
            try:
                chunk = stream.read(64 * 1024)
            except (OSError, ValueError):
                return
            if not chunk:
                return
            text = chunk.decode("utf-8", errors="replace")
            with self._log_lock:
                self._logs.append(text)
                self._log_bytes = getattr(self, "_log_bytes", 0) + len(text.encode("utf-8"))
                while self._log_bytes > _MAX_LOG_BYTES and len(self._logs) > 1:
                    removed = self._logs.pop(0)
                    self._log_bytes -= len(removed.encode("utf-8"))

    @staticmethod
    def _find_free_port() -> int:
        """Find an available TCP port."""
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.bind(("127.0.0.1", 0))
            return s.getsockname()[1]

    @staticmethod
    def _find_binary() -> str:
        """Resolve the mockagents binary path (back-compat shim).

        Prefer ``mockagents._binary.ensure_binary`` / ``find_binary``; this
        raises BinaryNotFoundError (a FileNotFoundError subclass) with install
        guidance when the binary is absent.
        """
        return ensure_binary()


def _check_agent_file(abs_path: str) -> None:
    """Fail fast on a missing file or one that is not MockAgents YAML."""
    if not os.path.exists(abs_path):
        raise FileNotFoundError(f"Agent config not found: {abs_path}")
    try:
        with open(abs_path, "r", encoding="utf-8-sig") as f:
            docs = [d for d in yaml.safe_load_all(f) if d is not None]
    except yaml.YAMLError as e:
        raise ConfigError(f"YAML parse error in {abs_path}: {e}") from e
    if not docs:
        raise ConfigError(f"Invalid agent config (empty file): {abs_path}")
    for doc in docs:
        if not isinstance(doc, dict):
            raise ConfigError(f"Invalid agent config (not a YAML mapping): {abs_path}")
        if doc.get("apiVersion") != "mockagents/v1":
            raise ConfigError(
                f"Invalid apiVersion in {abs_path}: expected 'mockagents/v1'"
            )
