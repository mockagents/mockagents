# Developer onboarding and handoff checklist

Read [CONTRIBUTING.md](../../CONTRIBUTING.md), [ARCHITECTURE.md](../../ARCHITECTURE.md), and repository [AGENTS.md](../../AGENTS.md) before making changes. Read [SECURITY.md](../../SECURITY.md) for security-boundary work. This workspace uses Go 1.26.1 as the language floor and pins toolchain 1.26.6 in `go.mod`; use the repository-selected toolchain rather than a version copied from an older guide.

## First working session

From the repository root, with Go available:

```bash
go version
go build ./cmd/mockagents
go run ./cmd/mockagents validate examples/
go test ./internal/engine/... ./internal/config/... -count=1
go run ./cmd/mockagents start --agents-dir examples
```

The last command remains in the foreground. In a second terminal, request `http://127.0.0.1:8080/api/v1/health`, then `/api/v1/ready`, `/api/v1/identity`, and `/v1/models`. Use a returned model in the [API examples](api.md). Stop the server before reusing its port. On Windows a built executable is `mockagents.exe`, invoked as `.\mockagents.exe`; `go run` avoids that shell distinction.

If a restricted environment denies the default Go build cache, select a writable cache for the current shell, for example PowerShell `$env:GOCACHE = Join-Path $env:TEMP 'mockagents-go-cache'`. This does not require changing repository dependencies or tracked files. Dependency/toolchain download failures still require accessible module cache or network.

For the optional console, use the Node toolchain supported by the current lockfile/workflows, then in `gui` run `npm ci` and `npm run dev`. It uses port 3001 and a backend selected by `MOCKAGENTS_API_URL`. Python SDK work requires Python 3.10+ and its declared development dependencies. The npm SDK's declared Node floor and the current GUI/build-tool requirements are distinct; prefer the CI-pinned toolchain for reproducible contributor builds.

`make setup` installs contributor tools and enables tracked git hooks. Its public-remote branch policy is described in CONTRIBUTING; inspect that policy before pushing. A local analysis/build does not require publishing a branch.

## Trace one request

Start at [OpenAI Routes/handler](../../internal/adapter/openai.go), follow `InboundRequest` into [engine.go](../../internal/engine/engine.go), then [matcher](../../internal/engine/scenario_matcher.go), [generator](../../internal/engine/response_generator.go), [tool processor](../../internal/engine/tool_processor.go), and [session transaction](../../internal/engine/state/session.go). Return to the adapter for JSON/stream serialization and the server for capture. Compare the same request with an Anthropic fixture to see what stays neutral and what must differ.

For a write, trace [agent_write_handlers.go](../../internal/server/agent_write_handlers.go) through decoding/validation, ownership, revision checks, persistence and registry replacement. For a pipeline, compare declaration order with actual [executor](../../internal/engine/pipeline.go) traversal and the GUI's returned-node display. These are useful first changes because their boundaries are observable.

## Change-impact checklist

| Change | Read/update together | Verification |
| --- | --- | --- |
| Definition field | Type, default, validator, schema, OpenAPI, SDK/GUI types, examples/docs | Focused tests + drift + full applicable gate |
| Provider wire | Adapter, stream serializer, error mapper, normalization, SDK/conformance | Positive, malformed, streaming, cancellation and tenant paths |
| Managed API | Mount/floor, ownership, handler model, store, GUI client/capabilities | Role/owner matrix, unknown resource, dependency failure |
| Fixture edits | Parser, strict-field checks, ETag, path confinement, atomic write | Stale revision, invalid body, read-only/missing file, restart persistence |
| Sessions/concurrency | Scope, store pointer semantics, cleanup, turn transaction | Isolation/failure tests; Linux race gate |
| Installer/subprocess | Version/checksum/archive, argv, temp cleanup, exit handling | Package build/tests and install smoke |
| Deployment/release | Topology, probes, secrets, package versions, immutable artifacts | Helm/container and exact-candidate release gate |

## Handoff acceptance

- A new developer can build, validate examples, start the service, and identify the responding model/scenario.
- They can explain which listener, state store, auth policy and protocol they are changing.
- Their tests distinguish fixture self-tests from client/wire integration tests and exercise relevant failures.
- Model/route changes have synchronized docs and regenerable catalogs.
- Verification evidence identifies the actual source revision and states unrun checks explicitly.
- Operator steps cover persistence, rollback/restart consequences and secret ownership without embedding credentials.

Use [testing](testing.md) for commands, [review evidence](../reviews/2026-09-18-review-summary.md) for this handoff's results, and [glossary](glossary.md) for terms.
