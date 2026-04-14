// Package mockagents is the Go SDK for MockAgents — a drop-in
// replacement for the OpenAI and Anthropic APIs that lets you write
// deterministic integration tests for Go applications that talk to LLMs
// or multi-agent frameworks.
//
// The SDK surface intentionally mirrors the Python and TypeScript SDKs
// so example code translates one-to-one across languages:
//
//   - [Client] is a net/http-based client for the mock server.
//   - [Server] manages a mockagents binary subprocess with automatic
//     free-port discovery and health-check polling.
//   - [Scenario] and [RunScenario] let you script multi-turn
//     conversations and assert on the aggregated result.
//   - [Expect] integrates with testing.T and provides a fluent matcher
//     API for tool calls, response content, latency, and status codes.
//
// Minimum supported Go version is 1.22.
package mockagents
