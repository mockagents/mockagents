# Resolution plan: 2026-10-06 quality review

Companion to the [review summary](2026-10-06-quality-review-summary.md) and the
[action register](2026-10-06-quality-action-register.md). This plan orders the work into
iterations. Each iteration is one pull request, has an explicit exit gate, and adds the
tests that would have caught its findings. The register records which iteration closes
each finding and its status as the loop progresses.

## Principles

1. **Fail closed in a testing tool.** Anything that can make a test or validation report
   success without checking what the author wrote is treated as S1, whatever its blast
   radius looks like.
2. **Fix the class, not the instance.** Where several findings share a root cause, the
   iteration lands the shared mechanism (strict decode, a metering seam, a bounded store)
   and the instances follow from it.
3. **A regression test per finding.** It must fail on the baseline and pass after the fix.
   Repro tests written during review (`go test -overlay`) are promoted into the package.
4. **Compatibility is explicit.** Behaviour that tightens (an unknown YAML key becoming an
   error) is called out under `### Changed` in the CHANGELOG with the migration step, never
   slipped in.
5. **Do not regress what works.** Hot-path changes rerun the benchmark guard; route-policy
   changes keep the panic-on-undeclared-route invariant; security fixes keep the existing
   tenancy conformance suite green on both stores.

## Gate for every iteration

| Step | Command |
|---|---|
| Build and vet | `go build ./...` · `go vet ./...` |
| Full Go suite | `go test ./... -count=1 -timeout 12m` (Linux CI adds `-race`) |
| Examples | `mockagents validate examples/` |
| Contracts and docs | `make docs-check` (drift, liquidcheck, contractcheck, handoffcatalog, doccheck) |
| SDK / GUI, when touched | `make test-python`, `make test-typescript`, `cd sdk/vitest && npm test`, `make gui-verify` |
| Docs site, when nav changes | `mkdocs build --strict` from `site/` |
| Hot path, when touched | benchmark guard against `docs/benchmarks/latest.json` (Linux CI only) |

## Iterations

Iterations 1-3 remove the false-success and side-door classes and come first. 4-6 harden
fidelity and protocol state. 7 is the open-source delivery work. 8 raises coverage. 9 is
the structural refactor that makes the earlier fixes stick.

### Iteration 1: authoring truth (loader, validator, CLI)

Findings: P-01, C-01, C-04, L-41, L-09, C-11, C-12, C-23, C-14, C-15, C-02, C-03 (L-42),
C-06, C-07, C-10, C-16, C-18, C-21, C-22, C-24, C-25, P-18.

Work:
- Reject a file that holds more than one YAML document, naming the line of the second
  `---`. (Multi-document support is deliberately not added: the write API rewrites a
  source file per agent, which would destroy sibling documents.)
- Strict decoding in every loader and in `ValidateBytes`: an unknown key is a validation
  error with a line number, for all seven kinds. `start` already skips invalid documents
  with an error log, so a typo now disables the document visibly instead of silently.
  The MCP management path (`mcpadmin`) and contract JSON decode strictly too.
- TestSuite cases with zero assertions are rejected.
- Duplicate-name detection for every kind, keyed on (tenant, name); de-duplicate overlapping
  CLI inputs by file identity.
- `validate`: non-zero exit on zero documents, a single JSON document on stdout in
  `--format json`, unknown formats rejected, exit 2 for a missing path.
- `mcp` and `a2a` run their kind validators before serving.
- `init --force` never removes user files; the in-place check uses file identity.
- `logs` honours `MOCKAGENTS_DATA_DIR` and opens read-only; `rm` requires `--yes`
  off a TTY; the root command prints an error once; `slog.SetDefault` honours
  `--json-logs` / `--log-level`.

Exit: a table-driven binary test pins exit codes and streams for validate / test /
contract / drift across {valid, invalid, load error, empty, missing path}.

### Iteration 2: security boundary and metering

Findings: S-01 (private track, see below), S-02, S-03, S-04, E-04, E-06, S-05, S-06, C-05,
C-17, L-31, L-28, L-10, L-24, L-26.

Work:
- S-01 fix and regression test land through a GitHub security advisory before any public
  description.
- An engine-level metering seam (`Meter`: `AllowRequest`, `CheckSpend`, `AddSpend`)
  consulted by every path that reaches the engine: HTTP adapters, batch dispatch, pipeline
  nodes and Realtime. The HTTP quota middleware becomes a thin adapter over it.
- Gemini dispatches on method (`generateContent`, `streamGenerateContent`, `countTokens`;
  404 otherwise). Batch size is capped and processed asynchronously with a cancellable
  server-lifetime context.
- Unknown models are priced from a configurable fallback instead of $0, logged once.
- `MOCKAGENTS_LOG_BODIES` and the remaining knobs go through the strict `env.go` helpers.
- Realtime cookie principals are refused when CORS is wildcard; stream metrics are
  tenant-scoped; `auth.denied` events are visible to the platform role; session GC; cache
  invalidation scoped to the affected tenant.

Exit: a route-family test asserts that each billable route, batch sub-request and pipeline
node consumes quota and accrues spend, and that a tenant at its cap is rejected on every
path.

### Iteration 3: wire fidelity

Findings: E-01, E-02, E-03, E-05, E-07, E-09, E-10, E-11, E-12, E-14, E-17, E-18, E-20,
E-21, E-22, E-23, L-13, L-14, L-21.

Work: rune-safe argument chunking shared by all emitters; Anthropic `content` is always an
array; role-less Gemini contents default to `user`; strict `tool_choice` keyed on the wire
protocol; Responses sessions carried with the stored response; stream/non-stream usage
parity; error `type` mapped by status; deterministic strict-schema messages; `/v1/models`
de-duplicated.

Exit: a per-provider fidelity matrix test (multi-byte args, empty content, engine-error
mapping, strict forcing) that iterates the adapter registry.

### Iteration 4: protocol and recording robustness

Findings: P-02 to P-17, P-19, P-20, L-36, L-38, L-39, L-40, L-43, L-44, M-27, M-32, M-30.

Work: decode `Content-Encoding` on record and import, never replay framing headers; flush
advances only after a successful write; canonical query in the request hash; boundary-
anchored redaction; one ordered MCP outbound queue; per-session MCP state; realtime
item/history caps and enforced expiry; G.711 byte rates; A2A idle-task expiry; JSON-RPC
envelope rules shared by MCP and A2A; panic recovery in tool handlers; server timeouts on
the standalone `mcp` and `a2a` commands.

Exit: property tests for every bounded store ("never exceeds N or bytes", "idle entries
reclaimed") and a JSON-RPC envelope table run over every transport.

### Iteration 5: schema, validator and runtime parity

Findings: C-08, C-09, C-13, C-19, C-20, E-13, E-16, E-19.

Work: generate all seven schemas from Go types (extending `tools/contractcheck`, which
already generates two) with overlays for bounds and enums; enforce streaming bounds and
NaN rejection in Go; parse templates at validation time; validate tool response rules;
lint shared models; clamp every latency distribution; keep chaos maxima below the server
write timeout.

Exit: a reflection-based field-parity test for every kind, and `examples/` plus negative
fixtures validated by both the Go validator and the JSON schemas with identical verdicts.

### Iteration 6: SDKs and GUI

Findings: K-01 to K-27 and the carried GUI bullets (CSP and HSTS, raw upstream errors,
`?error=` spoofing, HEAD probe, editor navigation guard, `yamlPath` parsing).

Work: one request helper per SDK that always sends credentials; outcome assertions read the
final turn in all SDKs; missing-key semantics and raw arguments exposed; Python streams
raise on non-2xx and decode UTF-8; Go numeric argument comparison; Python scenario runner
aligned with TS and Go; npx lockfile and Windows extraction; CJS entry points or documented
ESM-only Jest; GUI ESLint gate.

Exit: a **cross-SDK contract suite**. One YAML fixture plus the same scenarios and
assertions in Python, TypeScript and Go run against a binary built once in CI, with
identical expected verdicts, including multi-tenant, streaming and false-pass cases.

### Iteration 7: open-source delivery and supply chain

Findings: O-02 to O-33.

Files to add: `.github/CODEOWNERS`, `.github/PULL_REQUEST_TEMPLATE.md`, `GOVERNANCE.md`,
`MAINTAINERS.md`, `SUPPORT.md`, `THIRD_PARTY_NOTICES.md` (generated, drift-checked),
`LICENSE` and `NOTICE` copies in each SDK package, `sdk/npx/package-lock.json`,
`.golangci.yml`, `.editorconfig`, `site/requirements.txt`, `.github/workflows/scorecard.yml`,
`.github/workflows/dependency-review.yml`, `CITATION.cff`, a public `ROADMAP.md`.

Files to change: `CODE_OF_CONDUCT.md` (full Contributor Covenant 2.1 text and a conduct
contact), `SECURITY.md` (response SLA, disclosure policy, full scope), `CONTRIBUTING.md`
(DCO, Node 22, opt-in maintainer hook, live good-first-issue link), `CHANGELOG.md`
(`[Unreleased]`, compare links, Keep a Changelog headings), `Dockerfile` (supported Alpine
by digest, fixed UID, `-trimpath`, OCI labels, version build argument), `.goreleaser.yml`
(`-trimpath`, `mod_timestamp`, SBOM, keyless signing, licence files in archives), workflow
timeouts and `cancel-in-progress` on pull requests only, `verify.yml` exercised on pull
requests, CI running the full `make docs-check`, JS SDK jobs, `gofmt`/`golangci-lint`,
`ruff`, `shellcheck`, `actionlint`; `main.go` falls back to the module version so
`go install` reports a real version; `.gitignore`, `.gitattributes` (`eol=lf`),
`.dockerignore`; `docker-compose.yml` mounts `./examples` read-only.

Exit: GitHub community profile at 100 %, every workflow green on a pull request including
`verify.yml`, and a Scorecard run published.

### Iteration 8: coverage and fuzzing

Targets (Go statement coverage): `cmd/mockagents` 38.9 % to at least 65 %, `oidcauth`
42.9 % to at least 80 % (fake provider), `tenancy` covered by the Postgres conformance job
on every pull request, `vector` 68.2 % to at least 85 %, Go SDK 67 % to at least 80 %,
total 76.5 % to at least 82 %.

Fuzz targets: `ValidateBytes`, the OpenAI / Anthropic / Gemini request decoders, the SSE
parsers in the Go SDK and the streaming package, `toolschema.Validate`, and redaction
(never panics, never leaks a key shape). A scheduled job runs each for 60 seconds.

Tests for the CI gate itself: `tools/benchguard` and `tools/benchreport` comparison logic.

### Iteration 9: structural redesign

These remove the root causes behind several iterations' findings. Each is a separate pull
request so it can be reviewed and reverted on its own.

| Redesign | Removes | Shape |
|---|---|---|
| R1 Route policy table | the `isLLMProviderPath` / `skipAuth` / floor lists (F2, S-02, E-04, E-06) | `adapter.Route{Pattern, Handler, Policy{Open, Billable, MinRole}}`; middleware derives from it; one test asserts every route has a policy |
| R2 Engine metering seam | in-process quota and spend side doors (S-02 to S-04) | `engine.Meter` consulted in `ProcessRequestContext` |
| R3 Adapter dispatch | seven copies of the handler skeleton (E-08, E-12, E-14, E-21) | `provider` interface + one `dispatch`; matrix test over the registry |
| R4 Exported stream plan | private pacer and duplicate chunkers (E-02, E-08, L-15) | `streaming.Plan` consumed by every emitter, including Bedrock and Ollama |
| R5 Bounded store primitive | hand-rolled caps (E-15, P-07, P-09, P-13, P-16) | `internal/bounded`: LRU + TTL + byte budget + per-tenant option |
| R6 JSON-RPC core | MCP and A2A envelope drift (P-12, L-39, L-38) | `internal/jsonrpc` with panic recovery and context propagation |
| R7 One load-and-validate entry point | each CLI surface choosing its own validators (C-02, C-12) | `config.LoadAndValidate(dir, opts) (*Documents, Report)` |
| R8 Session-identity policy | five places deciding session keys (E-01, E-23) | `engine.SessionKey` |
| R9 Shared state seam (from the 2026-09-03 audit, R2) | process-local state under HA | `state.Store` / logs / audit backends behind interfaces |

## Security track

S-01 is a cross-tenant read/write/delete on one adapter family, reproduced by the review.
Because the repository is public, the fix follows [SECURITY.md](../../SECURITY.md): a
private GitHub security advisory with a temporary private fork, the fix and regression test
reviewed there, then a release and a published advisory. The public register and these
documents name only the ID until then.

## Maintainer-only actions

These need repository or registry owner access and cannot be done in a pull request:

| ID | Action |
|---|---|
| O-01 | Claim Docker Hub org `mockagents`, npm `mockagents` and the `@mockagents` scope, PyPI `mockagents` (placeholder releases are fine), and the Homebrew tap. Until images exist, the chart default moves to `ghcr.io` in iteration 7. |
| O-07 | Ruleset on `main`: pull request required, required status checks, linear history. Tag ruleset on `refs/tags/v*`. Required reviewers on the `pypi` environment and a new `release` environment. |
| O-08 | Respond to external PR #143 and approve its workflow runs; merge or close Dependabot PRs #148 and #152-#155. |
| O-09 | Enable Dependabot security updates; triage the 18 open CodeQL high alerts; bump `google.golang.org/grpc` to v1.83.2. |
| O-10 | Switch GitHub Pages to the "GitHub Actions" source so the MkDocs site is served instead of the Jekyll render of `docs/`. |
| O-26 | Set the repository homepage to the docs site and apply the topics from `docs/RELEASING.md`. |
| O-28 | Close secret-scanning alert #1 as "used in tests". |
| O-11 | Recruit a second maintainer (bus factor is 1); register for the OpenSSF Best Practices badge. |

## Test strategy

The current suites prove happy paths well. The gaps are negative and false-pass paths, so
the strategy adds four test families rather than more of the same:

1. **False-pass fixtures.** For every authoring surface and every SDK assertion, a fixture
   that *should* fail: misspelled key, second document, empty directory, absent argument,
   error response under a negative assertion. Each must produce a non-zero exit or a failed
   assertion. These live next to the code they guard and are listed in the register.
2. **Matrix tests over registries.** Provider adapters, routes and bounded stores are
   enumerated from their registries, so a new provider or route is covered automatically and
   cannot silently skip a policy.
3. **Cross-SDK contract suite.** Shared fixtures, identical verdicts across Python,
   TypeScript, Go and the YAML runner, run against a real binary in CI.
4. **Fuzzing** on every parser that takes untrusted input.

Coverage targets are in iteration 8; they are floors, not goals, and the false-pass
fixtures matter more than the percentage.
