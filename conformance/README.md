# MCP conformance

This directory holds the assets that prove the MockAgents Streamable-HTTP MCP
server speaks the protocol correctly, using the official
[`@modelcontextprotocol/conformance`](https://www.npmjs.com/package/@modelcontextprotocol/conformance)
suite. The [`mcp-conformance`](../.github/workflows/mcp-conformance.yml) workflow
runs it in CI on every change to the MCP code or these files.

| Path | What it is |
|---|---|
| `server/mcp-conformance-server.yaml` | A `kind: MCPServer` fixture whose tools, resources, and prompts are named and shaped to satisfy the suite's static-content scenarios. Lives in its own directory so `--agents-dir` / `validate` don't try to load the baseline as an agent. |
| `expected-failures.yml` | The conformance baseline: scenarios a *static* declarative mock cannot satisfy (server-initiated sampling / elicitation / progress / log notifications mid-call, and stateful URI templates). |

## Run it locally

```bash
go build -o mockagents ./cmd/mockagents

# 1. serve the fixture
./mockagents mcp --transport http --port 8081 --agents-dir conformance/server &

# 2. run the suite against /mcp, gated by the baseline
npx @modelcontextprotocol/conformance server \
  --url http://127.0.0.1:8081/mcp \
  --expected-failures conformance/expected-failures.yml
```

## How the gate behaves

The runner's exit code (and therefore CI) reacts to the baseline like this:

| Scenario result | In baseline? | Outcome |
|---|---|---|
| Fails | yes | exit 0 — expected failure |
| Fails | no | exit 1 — **regression** |
| Passes | yes | exit 1 — **stale baseline**, remove the entry |
| Passes | no | exit 0 — normal pass |

So both directions are guarded: a wire-fidelity regression fails the build, and
a fix that starts passing a baselined scenario also fails until the entry is
removed from `expected-failures.yml`.

## Closing baseline gaps

The baselined scenarios need behaviour a static mock doesn't model today. They
become addressable as the mock grows:

- **sampling / elicitation / progress / logging during a tool call** — these are
  *server-initiated* JSON-RPC requests/notifications mid-call. The mock can be
  driven through them out-of-band (`Server.EmitNotification` / `POST /mcp/notify`),
  but the suite drives them in-band from a tool handler.
- **`resources/templates/read`** — needs RFC 6570 URI-template resources with
  per-request parameter substitution.

When one is implemented, delete its line from `expected-failures.yml` in the
same change (CI enforces this).
