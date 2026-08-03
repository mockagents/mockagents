# TC-PERF-16 redo — pipeline topology with latency-injected nodes (2026-07-29)

**Question cycle 3 could not answer:** does `topology: parallel` actually run
nodes concurrently? Cycle 3 measured sub-millisecond per-node execution, so the
sequential-vs-parallel delta was scheduling noise. **Answer: yes — 2.00×.**

## Method

The fix was to give each node real, *known* cost so the topologies become
distinguishable. Two agents with fixed 100 ms chaos latency
(`distribution: fixed, min_ms: 100, max_ms: 100` under `spec.behavior.chaos`),
wired into two otherwise-identical 2-node pipelines:

| Fixture | Topology | Nodes |
|---|---|---|
| `slow-seq-pipeline` | `sequential` | `slownode-a` → `slownode-b` |
| `slow-par-pipeline` | `parallel` | `slownode-a`, `slownode-b` |

Five cases per suite, run through `mockagents test --format json`; the runner
reports per-case execution latency, which excludes CLI startup.

**Falsifiable prediction stated before running:** sequential ≈ 200 ms
(2 × node), parallel ≈ 100 ms (1 × node) if it truly parallelizes; parallel
≈ 200 ms would mean the topology is nominal only.

## Result

| Topology | Per-case latency (5 cases) | Average |
|---|---|---|
| sequential | 200.8 · 201.0 · 200.8 · 200.9 · 200.8 ms | **200.9 ms** |
| parallel | 100.6 · 100.7 · 100.6 · 100.2 · 100.2 ms | **100.5 ms** |

**Speedup 2.00× — exactly one node-time for two nodes.** 10/10 cases passed;
run-to-run spread is ±0.5 ms, i.e. the measurement is tight enough that the
conclusion is not in question.

Two things this confirms beyond the headline:

- **Sequential is genuinely additive** (200.9 ≈ 2 × 100.4), so the executor is
  not accidentally parallelizing a sequential topology.
- **Chaos latency fires on the runner path**, not just the HTTP path — worth
  knowing, since it makes injected latency a usable instrument for any future
  runner-side timing test.

## Why cycle 3 couldn't see this

With the `examples/` agents each node cost <1 ms, so both topologies finished
in ~2 ms and the difference was goroutine scheduling noise. The lesson is
general enough to keep: **when comparing execution strategies, the unit of
work must be large enough to dominate the harness.** Instrument with injected
latency rather than trying to measure a difference smaller than the noise
floor.

## Status

TC-PERF-16's open question is **closed**. The cycle-3 result (execution is
free; ~250 ms invocation is CLI startup and agents-dir loading) stands
unchanged — this redo only settles the topology question that cycle 3
explicitly flagged as inconclusive.
