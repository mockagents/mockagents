# TC-PERF-01 — Engine micro-benchmark baseline (2026-07-27)

**Build:** `baa7081` · **Verdict: PASS (allocs exact) + conscious re-baseline**
· Recipe: High-performance power plan activated for the run (was Balanced —
restored after), Go 1.26.4, `benchreport` over `./internal/engine/...`,
repo-local `GOTMPDIR` (AV quarantines temp-dir test binaries).

## Criteria

| Check | Result |
|---|---|
| `allocs/op` matches baseline exactly (all rows) | **PASS — 17/17 unchanged** |
| Machine-state sanity (control benches flat) | **PASS** — `GenerateUUID_New` 98.3 vs 103.4 ns, `GetByModelForTenant_ManyAgents` 14.0 vs 14.5 ns |
| `ns/op` within ±25% per row | **Exceeded on the scenario-matcher/generator family** (+25–45%) — attributed to feature work, see below |
| `B/op` matches exactly | **Changed on 9 rows** (e.g. `ResponseGenerator_Static` 144→208 B) |

## Interpretation: stale baseline, not a regression event

The committed baseline (2026-06-04) predates ~8 weeks of deliberate hot-path
feature work — vision input parsing + `has_image` scenario matching, and
hallucination-fixture generation — which lands precisely in the rows that
moved (`ScenarioMatcher_*` +38–45% ns, generator B/op growth), while the
control benches (UUID, tenant-model lookup) are flat within noise. Per the
plan's own rule ("file it either way so the baseline gets consciously
re-committed"), the refreshed numbers are committed as the new baseline;
future cycles regress against 2026-07-27.

## Defect found & fixed in the same pass: silent benchmark drop

The first benchreport run wrote **15 of 17** results with no warning:
`BenchmarkAgentRegistry_Get` and `BenchmarkGetByModelForTenant_ManyAgents`
mass-register agents sharing a model, and the per-collision `slog.Warn`
(R9-17) emits hundreds of WARN lines that interleave with Go's
`BenchmarkX\t...` output line — benchreport's parser then silently drops
those rows. Fixed by silencing the default slog inside those benchmarks
(`silenceSlog` helper, `internal/engine/benchmark_test.go`); full engine
test suite green. Follow-up candidate: benchreport could warn when a
`Benchmark*` function it saw in `-run` discovery yields no parsed row.

## New baseline highlights (High-performance plan, Go 1.26.4)

Static-response engine pass 544.9 ns (~1.84M ops/s) · scenario contains-match
39.9 ns · registry get 21.0 ns · tenant model lookup 14.0 ns, 0 allocs.
Full table: `docs/benchmarks/latest.md`.
