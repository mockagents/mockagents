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

> **⚠️ The committed `latest.{json,md}` is STALE — refresh it off-governor.**
>
> The checked-in snapshot predates the `internal/engine` review slice
> (F-SM-007/F-EN-006 et al.): it is missing two benchmarks that arc added
> (`ProcessRequest_MultipleToolCalls`, `ScenarioMatcher_MixedCaseManyScenarios`)
> and a handful of its `allocs/op`/`B/op` figures are out of date (some
> improved, e.g. `ScenarioMatcher_Regex` 6→5 allocs; a couple drifted +1).
>
> It was **not** refreshed on the primary dev machine on purpose: that box
> runs a power governor (`LBGovernor`) that throttles *sustained* bench runs
> ~2.5× uniformly across all benches, so a `make bench-report` there writes
> **unreliable `ns/op`** and would corrupt the baseline. Refresh the committed
> report only on **non-throttled hardware**, where `ns/op` is trustworthy.
> For a quick sanity check anywhere, `make bench` prints results **without**
> writing the files; `allocs/op` and `B/op` are machine-independent and safe
> to compare even under the governor.
>
> Note for reviewers: the 2026-06 multi-pass reviews of `internal/server`,
> `internal/tenancy`, and `internal/audit` touched **no** `internal/engine`
> or `internal/adapter` code, so they cannot affect these numbers — verified
> via `make bench` (allocs/op + B/op unchanged from the current engine code).

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
