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
Releases (sha256-verified, fail-closed) and caches it; subsequent runs reuse the
cache. All arguments are passed straight through to the binary
(`start`, `validate`, `test`, `record`, `replay`, `mcp`, …).

**Binary resolution order:** `$MOCKAGENTS_BINARY` → the npx cache. To use an
existing binary instead of downloading, set `MOCKAGENTS_BINARY=/path/to/mockagents`.

**Other installs:** `brew install mockagents/tap/mockagents`,
`docker run -p 8080:8080 mockagents/mockagents`, or
`go install github.com/mockagents/mockagents/cmd/mockagents@latest`.

> This package is the CLI launcher. The TypeScript SDK (the in-process client
> library) is published separately as `@mockagents/sdk`.

License: Apache-2.0.
