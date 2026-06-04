# Installation

## Binary Download

Download pre-built binaries from [GitHub Releases](https://github.com/mockagents/mockagents/releases):

| Platform | Architecture | Download |
|----------|-------------|----------|
| Linux | x86-64 | `mockagents_linux_amd64.tar.gz` |
| Linux | ARM64 | `mockagents_linux_arm64.tar.gz` |
| macOS | Intel | `mockagents_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `mockagents_darwin_arm64.tar.gz` |
| Windows | x86-64 | `mockagents_windows_amd64.zip` |

## Go Install

```bash
go install github.com/mockagents/mockagents/cmd/mockagents@latest
```

Requires Go 1.26+.

## Docker

```bash
docker pull mockagents/mockagents:latest

# Run with mounted agents
docker run -p 8080:8080 -v ./agents:/agents:ro mockagents/mockagents
```

## SDKs

MockAgents ships three SDKs with matching surfaces (client, scenarios,
assertions, streaming helpers):

```bash
pip install mockagents                                          # Python
npm install mockagents                                          # TypeScript
go get github.com/mockagents/mockagents/sdk/go/mockagents       # Go
```

The Python SDK manages the server binary automatically when used with
`MockAgentServer`; the Go SDK additionally offers `NewInProcessClient` to run an
engine inline (no subprocess).

## Verify Installation

```bash
mockagents --version
```
