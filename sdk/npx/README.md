# mockagents (npx launcher)

Run the [MockAgents](https://github.com/mockagents/mockagents) mock server with
no install — a drop-in mock for the OpenAI, Anthropic & Gemini APIs for testing
AI agents.

```bash
npx mockagents start --agents-dir ./agents
# then point your app at it (no code changes):
export OPENAI_BASE_URL=http://localhost:8080/v1
```

On first run this downloads the platform-matched `mockagents` binary from GitHub
Releases (sha256-verified, fail-closed) and caches it by requested version,
operating system, and architecture; subsequent runs reuse only that exact slot.
Downloads publish atomically after verification, so concurrent or interrupted
installs cannot expose a partial executable. All arguments pass through to the binary
(`start`, `validate`, `test`, `record`, `replay`, `mcp`, …).

**Binary resolution order:** `$MOCKAGENTS_BINARY` → `$MOCKAGENTS_BIN` → the npx
cache. To use an existing binary instead of downloading, set
`MOCKAGENTS_BINARY=/path/to/mockagents` (`MOCKAGENTS_BIN`, the name the
TypeScript and Go SDKs also read, works too).

On Windows the release `.zip` is extracted with the `tar.exe` in
`%SystemRoot%\System32` (bsdtar, Windows 10 1803+), called by absolute path, so
a GNU `tar` earlier on `PATH` (Git Bash) cannot break the install.

The launcher forwards `SIGINT`, `SIGTERM` and `SIGHUP` to the server and exits
with the server's exit code (or signal), so stopping `npx mockagents` from a
process supervisor or a cancelled CI step stops the server too.

**Other installs:** `brew install mockagents/tap/mockagents`,
`docker run -p 8080:8080 mockagents/mockagents`, or
`go install github.com/mockagents/mockagents/cmd/mockagents@latest`.

> This package is the CLI launcher. The TypeScript SDK (the in-process client
> library) is published separately as `@mockagents/sdk`.

License: Apache-2.0.
