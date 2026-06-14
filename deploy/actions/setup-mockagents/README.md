# `setup-mockagents` — GitHub Actions composite action

Install the MockAgents CLI and (optionally) start it as a background mock
server for the rest of the job. The action exports `OPENAI_BASE_URL`,
`ANTHROPIC_BASE_URL` and `MOCKAGENTS_BASE_URL`, so your existing test suite
runs against deterministic, zero-cost mocks **without code changes** — just
point your SDK clients at the standard base-URL env vars (most already do).

Pairs with the [`mockagents-test`](../mockagents-test/README.md) action when
you want to run a `kind:TestSuite` and emit a JUnit report instead.

## Minimal usage

```yaml
jobs:
  agent-e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Start MockAgents
        uses: mockagents/mockagents/deploy/actions/setup-mockagents@main
        with:
          agents-dir: ./agents

      # OPENAI_BASE_URL / ANTHROPIC_BASE_URL are now exported — your existing
      # tests hit the mock with no further wiring.
      - run: pytest
```

## Testing your working tree (or before a release exists)

The default install path is `go install …@latest`, which needs a published
release. To exercise the action against a **local checkout** — your own
working tree, or this repo's CI before `v0.1.0` is tagged — set `source-path`
and the action builds the CLI from source instead:

```yaml
      - uses: actions/checkout@v4
      - uses: ./deploy/actions/setup-mockagents
        with:
          source-path: ${{ github.workspace }}
          agents-dir: ./examples
```

## Inputs

| Name          | Default     | Purpose                                                                                  |
| ------------- | ----------- | ---------------------------------------------------------------------------------------- |
| `version`     | `latest`    | mockagents CLI version to `go install`. Pin a tag (e.g. `v0.1.0`) for reproducibility.   |
| `source-path` | *(empty)*   | Build from a local checkout (`go build ./cmd/mockagents`) instead of installing a release. Takes precedence over `version`. |
| `agents-dir`  | `./agents`  | Directory of `kind:Agent` YAML definitions to load.                                      |
| `port`        | `8080`      | Port the mock server listens on.                                                         |
| `start`       | `true`      | Start the server in the background. Set `false` to only install the CLI.                 |
| `go-version`  | `1.26`      | Go version used to build/install the CLI.                                                |
| `export-env`  | `true`      | Export `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL` / `MOCKAGENTS_BASE_URL` to `GITHUB_ENV`. |

## Outputs

| Name       | Purpose                                                          |
| ---------- | --------------------------------------------------------------- |
| `base-url` | Base URL of the running mock server (e.g. `http://127.0.0.1:8080`). |

## Exported environment

When `start: true` and `export-env: true` (both defaults), these are written
to `GITHUB_ENV` for every subsequent step:

| Variable              | Value                  |
| --------------------- | ---------------------- |
| `MOCKAGENTS_BASE_URL` | `http://127.0.0.1:<port>`    |
| `OPENAI_BASE_URL`     | `http://127.0.0.1:<port>/v1` |
| `OPENAI_API_KEY`      | `mock-key`             |
| `ANTHROPIC_BASE_URL`  | `http://127.0.0.1:<port>`    |
| `ANTHROPIC_API_KEY`   | `mock-key`             |

> The server binds `127.0.0.1` (IPv4) and is health-polled on
> `/api/v1/health` until ready (~20s budget); the step fails with the server
> log if it never comes up.

## Why composite and not Docker?

Composite actions run directly on the runner, sharing Go's build cache with
other steps and starting in seconds rather than pulling a container. The
trade-off is needing a Go toolchain, which `actions/setup-go@v5` provisions
for free and most Go pipelines already have.
