# Go SDK Guide

```bash
go get github.com/mockagents/mockagents/sdk/go/mockagents
```

Requires Go 1.26+. The Go SDK has the same surface as the Python and
TypeScript SDKs (client, scenarios, assertions, streaming) plus one thing the
others can't do: **`NewInProcessClient`** runs the mock engine inside your test
process — no subprocess, no port, sub-millisecond startup.

## In-process mode (Go-only)

```go
import "github.com/mockagents/mockagents/sdk/go/mockagents"

func TestOrderLookup(t *testing.T) {
    client, err := mockagents.NewInProcessClient(mockagents.InProcessOptions{
        AgentsDir: "./agents",
    })
    if err != nil {
        t.Fatal(err)
    }
    defer client.Close()

    resp, err := client.Chat(context.Background(),
        []mockagents.ChatMessage{{Role: "user", Content: "where is my order?"}},
        mockagents.ChatOptions{Model: "gpt-4o"},
    )
    if err != nil {
        t.Fatal(err)
    }
    mockagents.Expect(t, resp).
        ToHaveContentContaining("shipped").
        ToHaveToolCall("lookup_order", map[string]any{"order_id": "ORD-1"})
}
```

`NewInProcessClient` loads the agents directory, builds an engine, and mounts
the OpenAI (`POST /v1/chat/completions`) and Anthropic (`POST /v1/messages`)
adapters on an `httptest.Server`. `client.BaseURL()` is a real URL — point the
official OpenAI/Anthropic Go SDKs at it too. Options: `AgentsDir` (required),
`Logger` (*slog.Logger, discards by default), `SessionTTL`.

!!! note "In-process scope"
    The in-process mux serves the two chat protocols, `GET /v1/models`, and
    `GET /api/v1/health`. Management routes (list/reload agents), Gemini,
    Realtime, etc. need the full server — use `NewServer` below.

## Server (subprocess)

```go
server, err := mockagents.NewServer(mockagents.ServerOptions{AgentsDir: "./agents"})
if err != nil { t.Fatal(err) }
if err := server.Start(ctx, 10*time.Second); err != nil { t.Fatal(err) }
defer server.Stop(5 * time.Second)

client := server.Client()
```

**Options:** `AgentsDir`, `Port` (0 = auto), `BinaryPath`
(auto-detected; `MOCKAGENTS_BIN` honored), `LogLevel` (default `warn`).
`server.URL()`, `server.Logs()`, and `server.IsRunning()` help debugging;
`FindFreePort()` / `FindBinary()` are exported.

## Client

```go
client := mockagents.NewClient(mockagents.ClientOptions{
    BaseURL: "http://localhost:8080",   // default; Timeout defaults to 30s
})

// OpenAI Chat Completions (default model gpt-4o)
resp, err := client.Chat(ctx,
    []mockagents.ChatMessage{{Role: "user", Content: "hello"}},
    mockagents.ChatOptions{Model: "gpt-4o", SessionID: "conv-1"},
)
fmt.Println(resp.Content, resp.FinishReason, resp.Usage.TotalTokens)

// Anthropic Messages
resp, err = client.Message(ctx,
    []mockagents.ChatMessage{{Role: "user", Content: "hello"}},
    mockagents.MessageOptions{Model: "claude-3-5-sonnet-latest", System: "You are helpful."},
)
```

Management helpers: `Health`, `ListAgents`, `GetAgent`, `ReloadAgent`, and
`RotateMyAPIKey` (self-service key rotation against a
[multi-tenant](../guides/management-api.md) server).

## Streaming

Raw SSE streams (`ChatStream` / `MessageStream`) or the protocol-agnostic
`IterStream`, which yields normalized `StreamChunk`s via the standard Go
scanner idiom:

```go
stream, err := client.IterStream(ctx,
    []mockagents.ChatMessage{{Role: "user", Content: "hello"}},
    mockagents.IterStreamOptions{Protocol: "openai", Model: "gpt-4o"},
)
if err != nil { t.Fatal(err) }
defer stream.Close()

for stream.Next() {
    chunk := stream.Value()
    fmt.Print(chunk.Text)
    if chunk.Finished {
        fmt.Println("\nfinish:", chunk.FinishReason)
    }
}
if err := stream.Err(); err != nil { t.Fatal(err) }
```

```go
type StreamChunk struct {
    Text          string
    ToolCallDelta *ToolCallDelta // Index, Name, Fragment
    FinishReason  string
    Finished      bool
    Raw           map[string]any
}
```

`IterStreamOptions.Protocol` is `"openai"` (default) or `"anthropic"`.

## Scenarios

```go
scenario := mockagents.NewScenario("greeting-flow", []mockagents.ScenarioStep{
    {Role: "user", Content: "hello"},
    {Role: "user", Content: "help me with billing"},
})

result, err := mockagents.RunScenario(ctx, client, scenario)
fmt.Println(result.LastContent(), result.TotalLatencyMs)
```

Scenarios default to the OpenAI protocol and a random per-scenario session id
(so `turn_number` matching works across steps); set `scenario.Protocol =
mockagents.ProtocolAnthropic` for the Anthropic surface. With an in-process
client, pass the embedded client: `RunScenario(ctx, client.Client, scenario)`.

## Assertions

`Expect` / `ExpectScenario` integrate with `testing.TB` — failures call
`t.Errorf` (non-fatal, the chain keeps evaluating):

```go
mockagents.ExpectScenario(t, result).
    ToHaveContentContaining("shipped").
    ToHaveToolCall("lookup_order", map[string]any{"order_id": "ORD-1"}).
    ToHaveToolCallCount(1).
    ToHaveFinishReason("stop").
    ToHaveStatusCode(200).
    ToHaveLatencyLessThanMs(1000)
```

Outcome checks and trajectory checks read different things, and the difference
matters on a multi-turn scenario. `ToHaveContentContaining`,
`ToHaveFinishReason` and `ToHaveStatusCode` read the **last** response.
`ToHaveToolCall`, `ToHaveToolCallCount` and `ToHaveToolCallSequence` read the
**aggregate across every turn**, in invocation order, which is what the Python
and TypeScript SDKs do and what `tool_call_sequence` / `tool_call_count` mean
in a `kind: TestSuite` document. A check written here transfers to YAML
unchanged.

```go
mockagents.ExpectScenario(t, result).
    ToHaveToolCallSequence([]string{"get_weather", "search_orders"}).
    ToHaveToolCallCount(2)
```

The sequence is compared for full equality, not as a subsequence: an
unexpected extra call fails it. `result.ToolCalls()` returns the same
aggregate if you want to inspect it directly.

## Parity with the other SDKs

| Capability | Python | TypeScript | Go |
|---|---|---|---|
| Server manager (subprocess) | `MockAgentServer` | `MockAgentServer` | `NewServer` |
| OpenAI `chat` / Anthropic `message` | yes | yes | yes |
| Protocol-agnostic streaming | `iter_stream` | `iterStream` | `IterStream` |
| Normalized `StreamChunk` | yes | yes | yes |
| Scenarios + runner | yes | yes | yes |
| Fluent assertions | `expect()` (raises) | `expect()` (throws) | `Expect` (`t.Errorf`) |
| Trajectory assertions (aggregate across turns) | `to_have_tool_call_sequence` | `toHaveToolCallSequence` | `ToHaveToolCallSequence` |
| Test integration | pytest plugin | Vitest/Jest | `testing.TB` native |
| **In-process engine (no subprocess)** | — | — | **`NewInProcessClient`** |
