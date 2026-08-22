# Review Summary: main, last commit, and adoption backlog restart

Date: 2026-08-22
Reviewer: Codex
Branch/Commit: `main` at `1aacc82`

## Overall Status

| Area | Status | Notes |
|---|---|---|
| Per-file pass | Completed | Reviewed all three files changed by `1aacc82` plus the R13 implementation surface. |
| Cross-file integration pass | Completed | Checked benchmark report/docs/guard and pipeline route/executor/auth/tests/docs. |
| Tests/build checks | Completed | Focused packages and the full Go repository suite pass. |
| Release/demo readiness | Not Ready | R1/R3 remain externally blocked; R11 and R13 are complete. |

## Findings

| ID | Severity | Status | Owner Area | Summary | Evidence | Recommended Action |
|---|---|---|---|---|---|---|
| R-001 | P1 | Completed | Pipeline engine | A stale node ref could silently execute the only visible agent because generic engine fallback was applied to exact pipeline refs. | `internal/engine/pipeline.go:301`; regression test in `internal/server/pipeline_handlers_test.go` | Resolve every pipeline ref exactly and tenant-aware before invocation. |
| R-002 | P2 | Completed | Benchmark tooling | Benchguard's success text claimed B/op matched exactly even though the guard permits configured tolerance. | `tools/benchguard/main.go:203` | Report exact alloc matching and tolerance-bounded B/op accurately. |
| R-003 | P2 | Completed | Performance docs | The performance-plan baseline table still identified the retired July Windows baseline after `1aacc82` moved it to August Linux CI. | `docs/qa/PERFORMANCE-TEST-PLAN.md:83` | Update the baseline table with the current source/date. |
| R-004 | P1 | Completed | SDKs | Typed Python/TypeScript pipeline clients and exact node-sequence assertions now consume the HTTP contract. | `docs/ADOPTION_REQUIREMENTS.md:59` | Maintain parity tests. |
| R-005 | P1 | Completed | VectorMock | Deterministic bounded core, Qdrant/Pinecone/Chroma profiles, fixtures, namespaces, partial-result faults, and migrated RAG demo are implemented. | `internal/vector/store.go`; `internal/adapter/{qdrant,pinecone,chroma}.go`; `demo/rag-agent` | Maintain provider contract tests. |

## Completed Scope

- Verified `1aacc82` changes only the committed benchmark baseline and its documentation.
- Added `POST /api/v1/pipelines/{name}/run` with input validation, cancellation/tenant propagation, isolated implicit sessions, ordered nodes, and partial error results.
- Added exact pipeline ref resolution and focused regression coverage.
- Synchronized RBAC, management, README, testing, benchmark, and adoption documentation.

## Incomplete Or Deferred Scope

- R1 package publishing and R3 domain/social preview require maintainer-owned credentials or purchase authority.
- R17-R19 stay deferred until the stated three-design-partner gate is met.

## Validation Evidence

| Check | Result | Notes |
|---|---|---|
| `go test ./internal/server ./internal/engine ./tools/benchguard` | Pass | Workspace-local Go cache used because the default Windows cache was access-denied. |
| `go test ./...` | Pass | All Go packages passed. |
| TypeScript `npm run test -- --run` | Pass | 65 tests passed. |
| TypeScript `npm run build` | Pass | Published source type-check/build passed. |
| Python `compileall` | Pass | SDK and test sources compile. |
| Python `pytest` | Blocked | Installed launcher points to a missing Python; bundled runtime lacks `pytest` and `requests`. |
| `git diff --check` | Pass | No whitespace errors; Git reports expected LF/CRLF conversion warnings on Windows. |

## Next Actions

1. Pick R12: search, rerank, and moderation mounts.
