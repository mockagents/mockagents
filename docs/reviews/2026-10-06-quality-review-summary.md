# Quality review summary: full application, 2026-10-06

Date: 2026-10-06. Baseline: `origin/main` `9fa4db7`, reviewed in an isolated worktree.
Companion documents: [action register](2026-10-06-quality-action-register.md) (every
finding, its owner area and its planned iteration) and [resolution plan](2026-10-06-quality-resolution-plan.md)
(iteration order, structural redesigns, test strategy, open-source artifacts). Per-area
evidence is in [the appendices](2026-10-06-quality/).

## Scope and method

Six reviewers worked in parallel, independently and read-only. Each covered one area and
cross-checked earlier reviews so known items were re-verified, not re-reported:

| Area | Packages / surfaces | Appendix |
|---|---|---|
| Request hot path | `engine`, `adapter`, `streaming`, `toolschema`, `chaos`, `conversion`, `vector` | [01](2026-10-06-quality/01-engine-adapters.md) |
| Control plane and security | `server`, `tenancy`, `quota`, `oidcauth`, `audit`, `clientip`, `metrics`, `pricing`, `observability` | 02: held privately until the S-01 advisory is published; S-02 to S-07 are in the register |
| Protocols and storage | `mcp`, `mcpadmin`, `a2a`, `realtime`, `recording`, `storage`, `runner`, `contract`, `drift` | [03](2026-10-06-quality/03-protocols-storage.md) |
| Authoring and CLI | `cmd/mockagents`, `config`, `cli`, `types`, `schema/`, `examples/`, `tools/` | [04](2026-10-06-quality/04-cli-config.md) |
| Client packages | Python / TypeScript / Go SDKs, `sdk/vitest`, `sdk/npx`, `gui/` | [05](2026-10-06-quality/05-sdk-gui.md) |
| Open-source standards | community files, licensing, CI/CD, release supply chain, docs accuracy | [06](2026-10-06-quality/06-oss-delivery.md) |

Evidence bar: every finding carries a current `file:line` and a concrete failure scenario.
Most S1/S2 items were reproduced, either with throwaway tests compiled in through
`go test -overlay` (nothing written to the repo), against a scratch binary on
`127.0.0.1`, or with the SDKs running against a real server. Items that were only read
are marked READ or PLAUSIBLE in the appendices.

## Baseline health

| Check | Result |
|---|---|
| `go build ./...`, `go vet ./...` | Pass |
| `go test ./... -count=1` (Windows, Go 1.26.6) | Pass, all packages |
| Total Go statement coverage | **76.5 %** |
| Lowest Go packages | `cmd/mockagents` 38.9 %, `oidcauth` 42.9 %, `tenancy` 54.0 % (Postgres store 0 % without a DSN), Go SDK 67.0 %, `vector` 68.2 %, `types` 0 % |
| Python SDK / TS SDK / vitest / npx / GUI tests | 148 / 77 / 9 (+3 skipped) / 5 / 329 pass |
| Estimated OpenSSF Scorecard | about 5.5 / 10 |

The build is green, the core architecture is sound and many earlier defences hold up under
re-test: centralised route floors with panic-on-undeclared-route, store-boundary key
authorisation, credential-version cache invalidation, the per-IP bcrypt failure limiter,
trusted-proxy client IP, path-traversal confinement on writes, SHA-pinned actions and
least-privilege workflow tokens. The findings below are about the places where the tool
**reports success when it should report failure**, where tenancy or metering has a side
door, and where the open-source delivery process has not caught up with the code.

## Findings at a glance

| Area | S1 | S2 | S3 | Total |
|---|---:|---:|---:|---:|
| Request hot path (E-) | 0 | 9 | 14 | 23 |
| Control plane and security (S-) | 1 | 3 | 3 | 7 |
| Protocols and storage (P-) | 1 | 9 | 11 | 21 |
| Authoring and CLI (C-) | 2 | 11 | 14 | 27 |
| SDKs and GUI (K-) | 4 | 7 | 16 | 27 |
| Open-source delivery (O-) | 2 | 17 | 14 | 33 |
| **Total new** | **10** | **56** | **72** | **138** |

The Python streaming `raise_for_status` gap, carried from the 2026-09-03 audit, is promoted
to S1 (K-27) because it makes negative assertions false-pass on a server error. In addition,
the 2026-09-03 LOW tier was re-verified: of the 44 items, 33 are still present, 4 are
partial, 6 are fixed and 1 (L-08) was not re-checked. Two Mediums that audit counted as fixed are still present
(M-27, M-32) and one is partial (M-30).

Severity: S1 = security exposure, data loss, crash, or a false pass/false success in a
testing tool. S2 = significant incorrectness, spec violation, or a broken process a user or
contributor will hit. S3 = minor defect, maintainability, or missing test.

## The ten S1 findings

| ID | Finding | Status |
|---|---|---|
| S-01 | Tenant-isolation bypass on one adapter family (details withheld until the fix ships, per [SECURITY.md](../../SECURITY.md)) | Private fix track |
| P-01 | `mockagents test` false-passes: misspelled TestSuite keys (`args:`, `assertion:`) are dropped silently, leaving zero or partial assertions, and the run reports PASS | Iteration 1 |
| C-01 | Multi-document YAML is truncated to its first document: `validate` exits 0 on an invalid later document and `start` never serves it | Iteration 1 |
| C-03 | `init --force` deletes user files in `agents/` and `tests/`; on Windows the in-place guard is defeated by path case | Iteration 1 |
| K-01 | All three SDKs drop the API key on some calls (Python on everything but pipelines, TS on streams, Go always); in multi-tenant mode the call is answered by a different agent instead of failing | Iteration 6 |
| K-02 | Python outcome assertions pass if *any* turn matches; TS, Go and the YAML runner check the final turn | Iteration 6 |
| K-03 | Python `to_have_tool_call(name, {k: None})` passes for an absent argument; every SDK collapses malformed arguments to `{}` | Iteration 6 |
| K-27 | Python `chat(stream=True)` swallows HTTP errors, so `to_have_tool_call_count(0)` passes on a 500 | Iteration 6 |
| O-01 | Install commands and the Helm default image point at Docker Hub / npm / PyPI names nobody has claimed | Maintainer account action |
| O-02 | Release archives and the image ship no third-party licence notices for statically linked MIT/BSD/ISC/Apache code | Iteration 7 |

## Cross-cutting themes

1. **Silent acceptance is the dominant defect class.** Unknown YAML keys are dropped (C-04,
   P-01, L-41), later YAML documents are dropped (C-01), templates are not parsed until the
   first request (C-09), streaming bounds are not checked (C-08), `validate` succeeds on an
   empty directory (C-11), privacy knobs fail open (C-05), and SDK assertions pass on
   absent data (K-02, K-03, K-27). For a testing tool, each of these turns into a green
   CI run that asserted nothing. The fix is structural: strict decoding everywhere, one
   `LoadAndValidate` entry point, and one assertion-semantics contract shared by the YAML
   runner and the three SDKs.
2. **Metering and tenancy have in-process side doors.** Quota and spend are enforced by HTTP
   middleware keyed on a hand-maintained path list, but batches (up to 100,000
   sub-requests), pipeline runs and unknown Gemini methods reach the engine without passing
   it (S-02, S-03, S-04, E-04, E-06). The same "path list instead of route metadata"
   pattern was the root of several 2026-09-03 Highs.
3. **Seven provider adapters copy one skeleton and have drifted.** Stream physics and fault
   injection apply to some wires and not others (E-08), tool-argument chunking corrupts
   multi-byte characters (E-02), error types differ (E-14), and session identity is decided
   in five places (E-01, E-23).
4. **Bounded state is hand-rolled per protocol and has gaps.** Realtime items (M-32/P-07),
   A2A non-terminal tasks (P-09), MCP subscriptions (P-13) and streamable sessions (P-16)
   each implement a different, partial bound. Count-only caps ignore bytes (E-15).
5. **Schema and Go disagree.** Five hand-written JSON schemas reject documents the server
   accepts (C-20), while the Go validator accepts what the schema forbids (C-04, C-08,
   C-19). Only two kinds are generated from Go.
6. **The open-source process lags the code.** No review gate on `main`, a release-verification
   workflow that has never run and fails on its first job (O-05), an install-path monitor
   red for 26 days (O-06), the wrong docs site served on GitHub Pages (O-10), no signatures,
   SBOM or provenance (O-14), no lint or fuzz gates (O-15, O-25), missing governance files
   (O-11), and one unanswered external pull request (O-08).

## Test-coverage assessment

Coverage numbers are adequate in the engine and protocol packages (77-97 %), but the
untested code is concentrated exactly where the defects are:

- **CLI exit-code contract.** `runValidate` 1.9 %, `runTest` 9.6 %, every other `run*` 0 %.
  Binary tests only assert happy-path exit 0, which is how C-01, C-10, C-11 and C-16 went
  unnoticed.
- **Negative and false-pass edges.** No test uses a misspelled key, a second YAML document,
  an empty directory, an absent tool argument, an error response under a negative
  assertion, or a multi-byte character across a stream chunk boundary.
- **Cross-SDK contract.** No SDK test drives a real server, so the wire, encoding and auth
  defects (K-01, K-04, K-05, K-07) were invisible.
- **Security-critical branches.** `RequireRole` 403 path, `Resolve` cache-version-mismatch
  branch, `GetLog` tenant-mismatch 404, `oidcauth` provider path (`New`, `Exchange`,
  `AuthCodeURL`), and the Postgres tenancy store outside the dedicated CI job.
- **No fuzz targets** anywhere, including the YAML loader, request decoders, SSE parsers
  and the tool-schema validator.

The [resolution plan](2026-10-06-quality-resolution-plan.md#test-strategy) turns these into
a test strategy with per-iteration targets.

## What changed in the repository during this review

Only these review documents. All fixes ship through the iterations in the resolution plan,
each as its own pull request with tests that fail before the fix and pass after it.
