# Perf-guard fire drill (2026-08-03)

**Plan:** MA-QA-PTP-001 v1.5 §9.4 · **Result: guard fires correctly — exit 1.**
**Residual gap: workflow-level failure propagation not exercised** (see below).

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

## Residual gap — stated plainly

The drill could **not** be run as a real PR: this repo's pre-push hook enforces
a branch model where feature branches stay local
(`Feature branches stay local in this repo's branch model`). Bypassing it with
`--no-verify` would have meant overriding a deliberate repo guard without
authorization, so it was not done.

Consequently:

- ✅ **Proven:** `benchguard` detects a real regression and exits non-zero, and
  its message is fit for a reviewer.
- ✅ **Already proven separately:** the workflow itself runs and completes
  (green run, 2026-08-02).
- ❌ **Not proven:** that a non-zero `benchguard` exit fails the *GitHub
  Actions job* and blocks the PR. This is standard Actions behaviour (a
  non-zero step exit fails the job, and no `continue-on-error` is set), so the
  risk is low — but it is inference, not observation.

**To close the last gap** (~5 minutes, needs a decision on the branch policy):
open a real PR from a drill branch — either by pushing with `--no-verify`
deliberately, or from a fork — and confirm the job goes red. Worth doing once
before the first release candidate.

## Cleanup

Drill branch deleted; `main` verified free of drill code; scratch artifacts
removed. Nothing was pushed to the remote.
