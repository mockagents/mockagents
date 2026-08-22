# Review Action Register: adoption backlog restart

Date: 2026-08-22

| ID | Severity | Status | Owner Area | Finding | Evidence | Recommended Action | Validation Expected | Dependencies |
|---|---|---|---|---|---|---|---|---|
| R-001 | P1 | Completed | Pipeline engine | Exact refs could run the wrong sole agent. | `internal/engine/pipeline.go:301` | Exact tenant-aware lookup. | Missing-ref regression returns 422 and partial nodes. | None |
| R-002 | P2 | Completed | Benchmark tooling | Success wording contradicted tolerance behavior. | `tools/benchguard/main.go:203` | Correct output. | Focused Go test/build. | None |
| R-003 | P2 | Completed | Performance docs | Baseline table was stale after `1aacc82`. | `docs/qa/PERFORMANCE-TEST-PLAN.md:83` | Synchronize source/date. | Documentation cross-check. | None |
| R-004 | P1 | Completed | SDKs | Pipeline HTTP result now has typed Python/TS methods and exact node-sequence assertions. | `docs/ADOPTION_REQUIREMENTS.md:59` | Maintain parity. | TS tests pass; Python tests compile and await CI runtime verification. | R13 HTTP route |
| R-005 | P1 | Completed | VectorMock | Bounded deterministic core, Qdrant/Pinecone/Chroma profiles, fixtures, namespaces, partial faults, and migrated RAG demo are implemented. | `internal/vector/store.go`, `internal/adapter/{qdrant,pinecone,chroma}.go`, `demo/rag-agent` | Maintain contracts. | Core, startup, fault, provider, and demo tests. | None |
| R-006 | P0 | Blocked | Distribution | Advertised packages/domain need maintainer authority. | `docs/ADOPTION_REQUIREMENTS.md` R1/R3 | Publish/register with owner credentials. | Install guard green; homepage configured. | Credentials/purchase |
| R-007 | P2 | Deferred | Unified stack | R17-R19 are evidence-gated. | `docs/ADOPTION_REQUIREMENTS.md` Tier 3 | Do not build before three independent partner confirmations. | Partner evidence recorded. | Design partners |
| R-008 | P1 | In Progress | Search/rerank/moderation | Cohere v2 rerank and existing moderation are mounted through the common registry. | `internal/adapter/cohere_rerank.go`, `internal/adapter/moderations.go` | Add declarative Tavily search fixtures, then shared faults. | Provider contract and no-egress tests. | Fixture schema |

## Status Summary

| Status | Count |
|---|---:|
| Open | 0 |
| In Progress | 1 |
| Blocked | 1 |
| Ready for Verification | 0 |
| Completed | 4 |
| Deferred | 1 |
