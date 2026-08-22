# Review Action Register: adoption backlog restart

Date: 2026-08-22

| ID | Severity | Status | Owner Area | Finding | Evidence | Recommended Action | Validation Expected | Dependencies |
|---|---|---|---|---|---|---|---|---|
| R-001 | P1 | Completed | Pipeline engine | Exact refs could run the wrong sole agent. | `internal/engine/pipeline.go:301` | Exact tenant-aware lookup. | Missing-ref regression returns 422 and partial nodes. | None |
| R-002 | P2 | Completed | Benchmark tooling | Success wording contradicted tolerance behavior. | `tools/benchguard/main.go:203` | Correct output. | Focused Go test/build. | None |
| R-003 | P2 | Completed | Performance docs | Baseline table was stale after `1aacc82`. | `docs/qa/PERFORMANCE-TEST-PLAN.md:83` | Synchronize source/date. | Documentation cross-check. | None |
| R-004 | P1 | Completed | SDKs | Pipeline HTTP result now has typed Python/TS methods and exact node-sequence assertions. | `docs/ADOPTION_REQUIREMENTS.md:59` | Maintain parity. | TS tests pass; Python tests compile and await CI runtime verification. | R13 HTTP route |
| R-005 | P1 | In Progress | VectorMock | Bounded deterministic core, Qdrant/Pinecone profiles, validated declarative fixtures, namespaces, and deterministic partial-result faults are implemented. | `internal/vector/store.go`, `internal/adapter/qdrant.go`, `internal/adapter/pinecone.go` | Add Chroma and migrate the RAG demo. | Core, startup-fixture, fault, Qdrant, and Pinecone contract tests. | Provider profile sequencing |
| R-006 | P0 | Blocked | Distribution | Advertised packages/domain need maintainer authority. | `docs/ADOPTION_REQUIREMENTS.md` R1/R3 | Publish/register with owner credentials. | Install guard green; homepage configured. | Credentials/purchase |
| R-007 | P2 | Deferred | Unified stack | R17-R19 are evidence-gated. | `docs/ADOPTION_REQUIREMENTS.md` Tier 3 | Do not build before three independent partner confirmations. | Partner evidence recorded. | Design partners |

## Status Summary

| Status | Count |
|---|---:|
| Open | 0 |
| In Progress | 1 |
| Blocked | 1 |
| Ready for Verification | 0 |
| Completed | 4 |
| Deferred | 1 |
