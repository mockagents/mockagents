# Installation

## Binary Download

Download pre-built binaries from [GitHub Releases](https://github.com/mockagents/mockagents/releases):

| Platform | Architecture | Download (`<version>` is the release, e.g. `0.5.0`) |
|----------|-------------|----------|
| Linux | x86-64 | `mockagents_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `mockagents_<version>_linux_arm64.tar.gz` |
| macOS | Intel | `mockagents_<version>_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `mockagents_<version>_darwin_arm64.tar.gz` |
| Windows | x86-64 | `mockagents_<version>_windows_amd64.zip` |

Each release also publishes `checksums.txt`; verify the archive with
`sha256sum --check --ignore-missing checksums.txt`.

## Go Install

```bash
go install github.com/mockagents/mockagents/cmd/mockagents@latest
```

Requires Go 1.26+.

## Docker

!!! warning "Not published yet"
    The container image, the Python and TypeScript packages, `npx`, `pipx` and
    Homebrew are built in-tree but not yet published to their registries, so
    the commands below fail until a release completes those steps (the
    [README install table](https://github.com/mockagents/mockagents#install)
    tracks each one). Use a prebuilt binary or `go install` in the meantime, or
    build the image yourself with `docker build -t mockagents/mockagents .`.

```bash
docker pull mockagents/mockagents:latest

# Run with mounted agents
docker run -p 8080:8080 -v ./agents:/agents:ro mockagents/mockagents

# Persist interaction/audit logs across restarts (the image runs from /data)
docker run -p 8080:8080 -v ./agents:/agents:ro -v mockagents-data:/data mockagents/mockagents
```

The container runs as a non-root user from the `/data` volume, so SQLite
state and `mockagents init` scaffolds land there. Set
`MOCKAGENTS_DATA_DIR=/some/dir` to relocate state elsewhere.

## SDKs

MockAgents ships three SDKs with matching surfaces (client, scenarios,
assertions, streaming helpers):

```bash
pip install mockagents                                          # Python
npm install @mockagents/sdk                                          # TypeScript
go get github.com/mockagents/mockagents/sdk/go/mockagents       # Go
```

The Python SDK manages the server binary automatically when used with
`MockAgentServer`; the Go SDK additionally offers `NewInProcessClient` to run an
engine inline (no subprocess).

## Verify Installation

```bash
mockagents --version
```
