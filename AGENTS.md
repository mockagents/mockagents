# Working in MockAgents

Read [ARCHITECTURE.md](ARCHITECTURE.md) for request flow, package boundaries,
and adapter rules. Contributor setup and the complete release gate live in
[CONTRIBUTING.md](CONTRIBUTING.md) and [docs/RELEASING.md](docs/RELEASING.md).
Review [SECURITY.md](SECURITY.md) before changing a security boundary. Runtime
configuration is in [the configuration reference](site/docs/reference/configuration.md).

## Repository map

- `cmd/mockagents/`: CLI commands and process wiring.
- `internal/engine/`, `internal/engine/state/`, `internal/runner/`: execution, state, and suite isolation.
- `internal/server/`, `internal/tenancy/`: routes, authorization, quotas, and tenancy stores.
- `internal/recording/`: proxy recording, redaction, cassette persistence, and replay.
- `internal/a2a/`, `internal/vector/`, `internal/adapter/`: A2A and provider contracts.
- `sdk/{go,python,typescript,npx,vitest}/`: public SDKs, process managers, and installers.
- `deploy/`, `.github/workflows/`, `.goreleaser.yml`: deployment and release supply chain.
- `schema/`, `docs/api-spec.yaml`, `site/docs/`: schemas, API contract, and guides.
- `gui/`: web console; `examples/` and `conformance/`: fixtures and compatibility tests.

## Risky surfaces

Treat tenant identity from request context as authoritative; never trust a
client-selected tenant ID. Key mutation, credential caching,
`internal/server/route_authz.go`, recording/redaction, agent or pipeline file
writes, SDK subprocess/download code, Helm topology, and release workflows need
focused security and failure-path tests. Config/type changes require schema,
API, SDK, example, and generated-document review together. Keep credentials,
databases, generated packages, and local runbooks out of patches.

## Verification

Run the smallest focused test while editing, then the applicable complete gate:

```bash
go vet ./...
go test ./... -count=1 -timeout 5m
go run ./tools/driftcheck
go run ./tools/liquidcheck
go build ./cmd/mockagents
./mockagents validate examples/
```

Run `make test-race` on Linux with a C compiler for concurrency-sensitive Go
changes. SDK changes require their package build and tests; `make test-all`
covers Go, Python, examples, and TypeScript. GUI changes require `make
gui-verify`, including Playwright. Deployment changes require `make helm-lint`,
`make helm-template`, and a local container smoke test. Release evidence must
refer to the exact candidate SHA and follow [docs/RELEASING.md](docs/RELEASING.md).

Review in two passes: first check each changed file's invariants, then trace
cross-component contracts, security, failure handling, deployment, and tests.
