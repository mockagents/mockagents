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
    `GET /api/v1/health`. The management calls (`ListAgents`, `GetAgent`,
    `ReloadAgent`, `RotateMyAPIKey`) return a 404 `*HTTPError`, and Gemini,
    Responses, Realtime, MCP, auth/tenancy and the server-wide chaos policy
    are absent. Use `NewServer` below when a test needs them.

## Server (subprocess)

```go
server, err := mockagents.NewServer(mockagents.ServerOptions{AgentsDir: "./agents"})
if err != nil { t.Fatal(err) }
if err := server.Start(ctx, 10*time.Second); err != nil { t.Fatal(err) }
defer server.Stop(5 * time.Second)

client := server.Client()
```

**Options:** `AgentsDir`, `Port` (0 = auto), `BinaryPath`
(auto-detected, see below), `LogLevel` (default `warn`).
`server.URL()` (always `http://127.0.0.1:<port>`, matching the binary's
IPv4-only bind), `server.Logs()`, and `server.IsRunning()` help debugging;
`FindFreePort()` / `FindBinary()` are exported. `Start` fails as soon as the
child exits before becoming healthy, with its logs in the error, rather than
waiting out the timeout. `Stop` sends `os.Interrupt` (SIGINT) on Unix and kills
the process on Windows, escalating to `Kill` after the timeout.

`FindBinary` checks `MOCKAGENTS_BINARY`, then `MOCKAGENTS_BIN` (the same names,
in the same order, as the Python and TypeScript SDKs and `npx mockagents`),
then `./mockagents` in the working directory, then `PATH`. Parent directories
are not searched; point an env var at a monorepo build instead.

## Client

```go
client := mockagents.NewClient(mockagents.ClientOptions{
    BaseURL: "http://localhost:8080",   // default; Timeout defaults to 30s
    APIKey:  os.Getenv("MOCKAGENTS_API_KEY"), // multi-tenant servers only
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
    mockagents.MessageOptions{Model: "claude-sonnet-4-20250514", System: "You are helpful."},
)
```

The default Anthropic model is `mockagents.DefaultAnthropicModel`
(`claude-sonnet-4-20250514`), the same in all three SDKs. `APIKey` is sent as
`Authorization: Bearer <key>` on **every** request (chat, messages, streams and
management calls) and as `X-Api-Key` on Anthropic calls. Without it, a
multi-tenant server routes LLM calls to a different (global) agent rather than
failing.

Management helpers: `Health`, `ListAgents`, `GetAgent`, `ReloadAgent`, and
`RotateMyAPIKey` (self-service key rotation against a
[multi-tenant](../guides/management-api.md) server).

### Tool calls and round trips

Each `ToolCall` carries `Arguments` (decoded), `RawArguments` (the exact wire
text) and `ArgumentsValid`. Malformed or non-object arguments, as produced by a
`raw_arguments` fault fixture, leave `Arguments` nil with `ArgumentsValid`
false instead of looking like a call with no arguments.

`ChatMessage.ToolCalls` carries an assistant turn's calls, so a tool round trip
can be replayed (strict-tools id validation requires it):

```go
first, _ := client.Chat(ctx, history, mockagents.ChatOptions{Tools: tools})
history = append(history, mockagents.AssistantMessage(first))
for _, call := range first.ToolCalls {
    history = append(history, mockagents.ChatMessage{
        Role: "tool", ToolCallID: call.ID, Content: runTool(call),
    })
}
second, _ := client.Chat(ctx, history, mockagents.ChatOptions{Tools: tools})
```

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

After the loop, `stream.Truncated()` reports a body that ended without its
terminal event (a `streaming.truncateAfter` fault), `stream.MalformedFrames()`
counts skipped non-JSON frames (a `streaming.malformed` fault), and
`stream.Completed()` reports a clean finish. All three exist on both
`RawEventStream` and `ChunkStream`. CRLF, LF and bare-CR line endings are all
framed identically.

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
aggregate if you want to inspect it directly. `ToHaveToolCallCountByName(name,
n)` narrows the count to one tool (an SDK-only convenience, like the
two-argument TypeScript form).

`ToHaveToolCall` compares argument values as JSON, so `5`, `int64(5)`, `5.0`
and `json.Number("5")` all match a wire argument of `5`. A key the call omitted
never matches, not even an expected `nil`.

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
