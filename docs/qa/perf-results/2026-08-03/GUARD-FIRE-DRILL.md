# Perf-guard fire drill (2026-08-03)

**Plan:** MA-QA-PTP-001 v1.5 §9.4 · **Result: guard fires correctly — exit 1,
and the CI job goes red on a real PR. No residual gap.**

## Why this drill exists

`tools/benchguard` was verified before merge against **hand-mutated JSON**, and
has since run **green** in CI. Neither proves it fails on a real code change.
A gate nobody has watched bite is a gate nobody should trust.

## What was done

On a scratch branch, one escaping allocation was added to the
`ResponseGenerator.Generate` hot path — a package-level `[]byte` sink appended
per call, i.e. the shape of an accidental regression rather than a synthetic
one. `go test ./internal/engine/...` stayed green, so the drill isolates the
guard rather than a broken build.

Then the **exact command chain from `.github/workflows/perf-guard.yml`** was
run against that build:

```bash
go run ./tools/benchreport -pkg ./internal/engine/... -out <tmp>
go run ./tools/benchguard -baseline docs/benchmarks/latest.json \
    -candidate <tmp>/latest.json -summary <summary>
```

## Result

**Guard exit code 1** — the CI job would fail. It reported **8 blocking
changes**, every one an `allocs/op` increase of exactly +1, which is precisely
what a single extra allocation on a shared hot path produces:

| Benchmark | allocs/op |
|---|---|
| `ResponseGenerator_Static` | 1 → 2 |
| `ResponseGenerator_Template` | 8 → 9 |
| `ProcessRequest_StaticResponse` | 10 → 11 |
| `ProcessRequest_DefaultFallback` | 10 → 11 |
| `ProcessRequest_RegexMatch` | 14 → 15 |
| `ProcessRequest_TemplateResponse` | 17 → 18 |
| `ProcessRequest_WithToolCalls` | 18 → 19 |
| `ProcessRequest_MultipleToolCalls` | 37 → 38 |

Three things worth noting about the output quality:

1. **The message is actionable** — it names each benchmark and the exact
   delta, and prints the command to re-baseline if the change is intentional.
2. **The blast radius is visible.** One allocation in a shared helper surfaced
   across eight benchmarks, which tells a reviewer *where* the cost landed, not
   just that something moved.
3. **The ns/op noise stayed non-blocking.** Fifteen ns/op rows moved
   +96–189% in the same run (the drill box was loaded), and all were correctly
   demoted to informational notes. Had ns/op been gated, this drill would have
   produced a wall of failures that buried the eight real ones — a live
   demonstration of why the v1.5 §4.1 rule exists.

## Part 2 — real PR run (gap closed)

The first pass could not open a PR: a pre-push hook enforces
`Feature branches stay local in this repo's branch model`. With **explicit
authorization** to use the override the hook itself documents, the drill was
re-run end to end.

**PR [#36](https://github.com/mockagents/mockagents/pull/36)** ·
**run [30929813756](https://github.com/mockagents/mockagents/actions/runs/30929813756)**

| Check | Result |
|---|---|
| **Engine benchmark guard** | ❌ **fail** (55 s) |
| Lint | ✅ pass |
| Python SDK Tests | ✅ pass |
| Go Tests | pending at capture time |

The failing step reproduced the local verdict exactly — 8 blocking
`allocs/op` changes, each +1, with the re-baseline instruction printed and
every ns/op move demoted to a non-blocking note:

```
### ❌ 8 blocking change(s)
- BenchmarkProcessRequest_DefaultFallback: allocs/op 10 -> 11
- BenchmarkProcessRequest_MultipleToolCalls: allocs/op 37 -> 38
- BenchmarkProcessRequest_RegexMatch: allocs/op 14 -> 15
- BenchmarkProcessRequest_StaticResponse: allocs/op 10 -> 11
- BenchmarkProcessRequest_TemplateResponse: allocs/op 17 -> 18
- BenchmarkProcessRequest_WithToolCalls: allocs/op 18 -> 19
- BenchmarkResponseGenerator_Static: allocs/op 1 -> 2
- BenchmarkResponseGenerator_Template: allocs/op 8 -> 9
If these are intentional, regenerate the baseline in this PR:
### Notes (non-blocking)
```

**That other checks passed while the guard failed** is the detail that matters:
the red came from the guard, not from a broken build, so the signal a reviewer
sees is unambiguous.

## Verified end to end

- ✅ `benchguard` detects a real regression and exits non-zero.
- ✅ The workflow runs on a PR touching `internal/`.
- ✅ **A non-zero exit fails the Actions job and surfaces as a failed PR
  check** — previously inference, now observed.
- ✅ The failure message is actionable and correctly scoped.

Re-run this drill whenever the guard's thresholds change (§9.4).

## Cleanup

PR #36 closed unmerged with the result recorded in a closing comment; drill
branch deleted locally **and** on the remote; `main` verified free of drill
code; scratch artifacts removed. The only remote trace is the closed PR and
its CI run, which are the evidence.
