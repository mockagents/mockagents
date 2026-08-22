# Per-file Analysis: main and R13 restart

Date: 2026-08-22

| File | Responsibility | Local Status | Findings | Test/Validation Gap | Action |
|---|---|---|---|---|---|
| `docs/benchmarks/README.md` | Baseline provenance and interpretation | OK | Linux CI re-baseline rationale is clear and consistent with JSON/Markdown. | CI artifacts are not stored in-repo. | No code action. |
| `docs/benchmarks/latest.json` | Machine-readable baseline | OK | 17 rows, Linux/amd64, Go 1.26.4; intentional matcher B/op increase recorded. | Reproduction depends on CI runner artifacts. | No action. |
| `docs/benchmarks/latest.md` | Human-readable generated baseline | OK | Values mirror JSON. | Generator has no end-to-end golden comparison in this review. | No action. |
| `tools/benchguard/main.go` | Allocation regression gate | Fixed | Success message overstated B/op equality. | No dedicated CLI-output test. | Add a future output test. |
| `internal/engine/pipeline.go` | Pipeline topology execution | Fixed | Exact refs previously inherited anonymous single-agent fallback. | Tenant-specific endpoint e2e remains desirable. | Exact tenant-aware preflight added. |
| `internal/server/pipeline_handlers.go` | Pipeline management/execution HTTP contract | Added | Validates one JSON object and preserves partial results. | Python runtime test environment is unavailable locally. | Verify Python tests in CI. |
| `internal/server/route_authz.go` | Central management RBAC | OK | Run route deliberately viewer-gated. | Covered by snapshot. | None. |
| `docs/ADOPTION_REQUIREMENTS.md` | Backlog source of truth | Updated | Tier 1 and R13 complete; R11 first slice tracked in progress. | Remaining R11 profiles/faults are explicit. | Continue R11. |
| `internal/vector/store.go` | Provider-neutral deterministic vector state/ranking | Added | Bounded collections, atomic upsert, typed filters, stable ties, defensive copies, startup fixture seeding, namespaces, and deterministic partial-result truncation. | Persistence remains process-local by design. | Add Chroma adapter next. |
| `internal/adapter/qdrant.go` | Qdrant-shaped HTTP compatibility surface | Added | Collection/point/search/fetch/delete and error envelopes. | Profile covers a bounded subset, not full Qdrant. | Add client contract coverage and document subset. |

## Notes

- No P0 finding was observed.
- The latest commit is documentation/data only; its main cross-file defect was stale baseline metadata outside the commit's touched files.
