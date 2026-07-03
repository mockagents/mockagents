# mockagents (Go SDK)

Go SDK for [MockAgents](https://github.com/mockagents/mockagents). Spin
up mock AI agents as a subprocess, point your OpenAI / Anthropic client
code at them, and write deterministic integration tests without burning
real LLM tokens.

## Install

```bash
go get github.com/mockagents/mockagents/sdk/go/mockagents
```

Requires Go **1.26** or later. The `mockagents` Go binary must be
available on your `PATH` or at `./mockagents` (or set `MOCKAGENTS_BIN`).
Build it from the repo with `make build` or `go install
github.com/mockagents/mockagents/cmd/mockagents@latest`.

## Quick start

```go
package agent_test

import (
    "context"
    "testing"
    "time"

    "github.com/mockagents/mockagents/sdk/go/mockagents"
)

func TestOrderLookupHappyPath(t *testing.T) {
    server, err := mockagents.NewServer(mockagents.ServerOptions{
        AgentsDir: "./agents",
    })
    if err != nil {
        t.Fatal(err)
    }
    if err := server.Start(context.Background(), 10*time.Second); err != nil {
        t.Fatal(err)
    }
    defer server.Stop(5 * time.Second)

    client := server.Client()
    scenario := mockagents.NewScenario("order-lookup", []mockagents.ScenarioStep{
        {Role: "user", Content: "where is my order?"},
    })
    result, err := mockagents.RunScenario(context.Background(), client, scenario)
    if err != nil {
        t.Fatal(err)
    }

    mockagents.ExpectScenario(t, result).
        ToHaveContentContaining("shipped").
        ToHaveToolCall("lookup_order", map[string]any{"order_id": "ORD-1"}).
        ToHaveLatencyLessThanMs(1000)
}
```

## API surface

| Symbol                                        | Purpose                                                                                         |
| --------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `Server` / `NewServer` / `Start` / `Stop`     | Subprocess manager with free-port discovery and health-check polling.                           |
| `Client` / `NewClient`                        | `net/http` client for `/v1/chat/completions`, `/v1/messages`, and the management API.           |
| `Chat` / `Message`                            | Typed requests returning `*ChatResponse`.                                                       |
| `Scenario` / `RunScenario`                    | Declarative multi-turn conversation runner with automatic session scoping.                      |
| `Expect` / `ExpectScenario`                   | `testing.TB`-integrated fluent matchers: `ToHaveContentContaining`, `ToHaveFinishReason`, `ToHaveStatusCode`, `ToHaveLatencyLessThanMs`, `ToHaveToolCallCount`, `ToHaveToolCall`. |
| `FindFreePort` / `FindBinary`                 | Helpers exposed for advanced use cases and custom test harnesses.                               |

## Known limitations

- **No framework adapters** — because Go applications typically call
  OpenAI/Anthropic APIs directly, there is no equivalent to the LangChain
  or CrewAI adapters shipped by the Python and TS SDKs. The `Client` type
  is already the only surface you need.

Two former v1 limitations are gone: streaming **is** wrapped (`IterStream`
yields protocol-agnostic `StreamChunk`s), and **in-process mode exists** —
`NewInProcessClient` spins up an engine + `httptest.Server` inline with no
subprocess (chat protocols + `/v1/models` + health). See the
[Go SDK guide](https://mockagents.github.io/mockagents/sdk/go-sdk/).
