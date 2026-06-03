# Benchmarks

This directory is the published home for the MockAgents hot-path
benchmark numbers referenced by **US-12.2** in `product-backlog.md`.

> **Optimizing?** See **`docs/PERFORMANCE.md`** — the performance handoff
> guide: the prioritized optimization backlog (grounded in `file:line`), the
> "already optimized — don't redo" list, and the scaling ceilings. This README
> is *how to measure*; that doc is *what to change*.

## How to refresh

```bash
make bench-report          # regenerates latest.json + latest.md
```

Under the hood `tools/benchreport` shells out to
`go test -run=^$ -bench=. -benchmem ./internal/engine/...`, parses the
output, and writes two artifacts into this directory:

- **`latest.json`** — structured results (schema v1) suitable for
  trend-tracking in CI. Consumers pin on `schema_version`.
- **`latest.md`** — human-readable snapshot with a results table;
  what reviewers skim on a PR.

The GOMAXPROCS suffix is stripped from benchmark names before parsing
so results from a laptop and a CI runner stay comparable.

> **Baseline freshness:** `latest.{json,md}` was last refreshed
> **2026-06-03** off-governor (see the "Release 2026-06-03 refresh" note
> below) and is current with the engine + security + perf work through
> that date.
>
> **Refreshing on the primary dev machine:** that box ships only the
> Windows **Balanced** power plan, which throttles `ns/op` ~1.4× uniformly
> vs. a clean run — enough to corrupt a committed `ns/op` baseline. To
> refresh there, temporarily activate a High-performance plan, run
> `make bench-report`, then restore Balanced (the recipe is in the
> 2026-06-03 note). `allocs/op` and `B/op` are machine-independent, so for
> a quick sanity check anywhere `make bench` prints results **without**
> writing the files and those two columns are always safe to compare.

## Scope

The benchmarked hot paths are the four surfaces a production request
hits on its way through the mock:

| Area                 | Benchmarks                                          |
| -------------------- | --------------------------------------------------- |
| End-to-end `ProcessRequest` | static, template, tool-call, regex, fallback |
| Scenario matcher     | content_contains, regex, default                    |
| Response generator   | static, template                                    |
| Tool processor       | single call                                         |
| Agent registry       | name lookup                                         |

End-to-end numbers include agent resolve + session update + scenario
match + response generation + (optional) tool call, so they are the
closest thing this repo ships to an apples-to-apples latency number
for "mock answered a request".

## How to interpret ns/op

Divide one second by the `ns/op` column to get ops/sec (already
pre-computed in the Markdown table). A clean p50 on a modern laptop
should land in the **sub-millisecond** range for all hot-path
benchmarks. Target envelope:

| Benchmark family     | Target ns/op | Notes                     |
| -------------------- | -----------: | ------------------------- |
| Registry lookup      |       < 100  | Hot path on every request |
| Scenario matcher     |     < 1,000  | Fast path (content_contains) |
| ProcessRequest (static) |  < 2,000  | Full pipeline, no tools   |
| ProcessRequest (tools)  |  < 5,000  | Full pipeline with 1 tool |

Regressions outside the target envelope should be investigated with
the profiling workflow below before a release tag.

## Profiling workflow

When a benchmark regresses, capture a CPU profile from the exact same
run and use it to find the hot frames:

```bash
go test -run=^$ -bench=BenchmarkProcessRequest_StaticResponse \
    -cpuprofile=cpu.out ./internal/engine/...
go tool pprof -top -cum cpu.out | head -30
```

For allocation-heavy benchmarks use `-memprofile=mem.out` and
`go tool pprof -alloc_space`. The `-cum` column shows the callers that
account for the most time including their callees, which is almost
always where the real regression lives.

Commit findings (top hotspots, fix notes) into this README under a
new `## Release yyyy-mm-dd profile notes` section, so future releases
have a reference point instead of re-discovering the same bottlenecks.

## Release 2026-06-03 refresh

First off-governor refresh since the v0.2/v0.3 engine, security, and
performance work landed (the prior baseline was dated 2026-04-14). Same
machine (Go 1.26.1, windows/amd64, Intel Core Ultra 9 285K), captured
under a temporary High-performance power plan, then Balanced restored:

```powershell
# create + activate a High-performance plan (Win 11 hides it by default)
$hp = (powercfg -duplicatescheme 8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c) -replace '.*: ([0-9a-f-]{36}).*','$1'
powercfg /setactive $hp
make bench-report
# restore Balanced and remove the temp plan — leaves the box as found
powercfg /setactive 381b4222-f694-41f0-9685-ff5bb260df2e
powercfg -delete $hp
```

What moved vs. the 2026-04-14 baseline (a 7-week cumulative diff, **not**
a single change):

- **`allocs/op` — the reliable signal — is flat within ±1 everywhere**,
  with three benches improving (`WithToolCalls` 23→22, `ToolCallProcessor`
  12→10, `ScenarioMatcher_Regex` 6→5). No machine-independent regression.
- Two benches that the engine-review arc added now appear in the baseline:
  `ProcessRequest_MultipleToolCalls` and
  `ScenarioMatcher_MixedCaseManyScenarios`.
- `GetByModelForTenant_ManyAgents` lands at **14.3 ns/op, 0 allocs/op**,
  confirming the PERF-01 tenant-model index (was an O(n) scan).
- `ns/op` swings (±5–30 %) are run-to-run + full-sweep thermal noise, not
  code: a single-bench probe of `StaticResponse` off-governor measured
  476.9 ns vs. the 528.2 ns full-sweep figure (the ~1.1× full-sweep
  thermal effect). All ProcessRequest families stay inside the target
  envelope below.

## Release 2026-04-14 micro-optimization slice

Follow-up to the baseline profile below. Five zero-risk changes landed
together and were measured via a clean `make bench-report` diff on
the same machine (Go 1.26.1, windows/amd64, Intel Core Ultra 9 285K):

| Benchmark                              | Before ns/op | After ns/op |       Δ |
| -------------------------------------- | -----------: | ----------: | ------: |
| `ProcessRequest_StaticResponse`        |        557.7 |   **500.8** | -10.2 % |
| `ProcessRequest_TemplateResponse`      |       1617.0 |  **1309.0** | -19.0 % |
| `ProcessRequest_WithToolCalls`         |       2226.0 |  **1815.0** | -18.5 % |
| `ProcessRequest_RegexMatch`            |        924.6 |   **794.0** | -14.1 % |
| `ProcessRequest_DefaultFallback`       |        752.1 |   **569.0** | -24.3 % |
| `ScenarioMatcher_ContentContains`      |         75.6 |    **28.8** | -61.9 % |
| `ScenarioMatcher_Regex`                |        702.1 |   **564.0** | -19.7 % |
| `ScenarioMatcher_Default`              |        282.2 |   **102.8** | -63.6 % |
| `ResponseGenerator_Template`           |       1021.0 |   **924.3** |  -9.5 % |

Allocations dropped in lockstep: `ProcessRequest_StaticResponse`
went from 12 allocs/op to 9, `ProcessRequest_DefaultFallback` from
15 to 9, `ScenarioMatcher_Default` from 5 to 1.

Changes in this slice:

1. **Session slice pre-sizing** — `NewSession` now allocates the
   `Messages` slice with `cap=16` so a typical 3–8 turn conversation
   never pays the growslice tax. (`internal/engine/state/session.go`)
2. **Tracer NoOp bypass** — `observability.IsEnabled()` returns false
   when no OTEL exporter is configured. `engine.ProcessRequestContext`
   caches the flag locally and skips every `SetAttributes` /
   `RecordError` call when disabled, eliminating the variadic
   `[]attribute.KeyValue` slice allocation on the hot path.
   (`internal/observability/tracing.go`, `internal/engine/engine.go`)
3. **Lazy captures map in matcher** — `ScenarioMatcher.evaluate` now
   only allocates a `map[string]string` when a regex with *named*
   capture groups actually matches. ContentContains-only scenarios
   (the common case) get a shared `matchedSentinel` singleton and
   pay zero map allocations. (`internal/engine/scenario_matcher.go`)
4. **bytes.Buffer pool in response generator** — `sync.Pool` recycles
   buffers across template renders; the static (no-`{{`) path is
   unchanged and never touches the pool. (`internal/engine/response_generator.go`)
5. **O(1) `GetByModel` via parallel index** — `AgentRegistry` now
   maintains a `byModel` map alongside the name map, kept in sync on
   Register/Remove, so adapter lookups skip the O(n) scan.
   (`internal/engine/agent_registry.go`)

No regressions in `ResponseGenerator_Static` (small +8.7 % is timing
noise — that path short-circuits before the pool) or
`AgentRegistry_Get` (flat at ~14 ns/op). `ToolCallProcessor` was not
targeted by this slice and stays flat. All 21 Go test packages pass.

## Release 2026-04-14 profile notes

Captured with:

```bash
go test -run=^$ -bench=BenchmarkProcessRequest_StaticResponse \
    -benchtime=2s -cpuprofile=cpu.out ./internal/engine/
go tool pprof -top -cum engine.test.exe cpu.out | head -25
```

Baseline **594.8 ns/op** for the full static-response pipeline on
`windows/amd64`, Intel Core Ultra 9 285K, Go 1.26.1.

Top cumulative frames:

1. **GC scan / mark dominates (~54 % cum)** —
   `runtime.scanObject` + `runtime.gcDrain` together soak up more
   than half the CPU. The benchmark drives allocation fast enough
   that the background GC cannot stay out of the hot path. This is
   *not* a code regression: the same percentage drops sharply at
   realistic request rates because a real server is not a tight loop
   over `ProcessRequest`. The number to watch is the non-GC portion.
2. **`Engine.ProcessRequestContext` (~39 % cum)** — the engine
   itself, as expected. Inside this frame the next tier is:
3. **`state.(*Session).AppendUserMessage` (~10 % cum)** —
   `growslice` while appending to the in-memory session history.
   Pre-sizing the history slice (or capping it with a ring buffer)
   is the most obvious optimization lever for the next release cycle
   if the target envelope tightens below 500 ns/op.
4. **`runtime.mallocgc` (~15 % cum)** — consistent with the GC
   pressure above; the engine allocates ~12 objects per request
   (see `B/op` / `allocs/op` columns in `latest.md`).

**No action required for this release.** All ProcessRequest
benchmarks are well inside the target envelope (557–2226 ns/op vs. a
<5,000 ns/op target). The profile is kept here as a baseline so the
*next* release can diff against a known-good starting point.
