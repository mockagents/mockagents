# Cycle 2 — Phase 1: machine prep (2026-07-28)

**Plan:** MA-QA-PTP-001 v1.2 §9 · **Status: complete** (one open team action)

## Primary machine profile (all cycle-2 numbers on this box unless noted)

| Item | Value |
|---|---|
| CPU | Intel Core Ultra 9 285K — 24 physical / 24 logical cores |
| RAM | 31.5 GB |
| OS | Windows 11 Pro 10.0.26200 |
| Power plan | Balanced at rest; **High performance activated per timed run** and restored after (§4.1 — Balanced throttles ~1.4×) |
| Go | go1.26.4 windows/amd64 |
| Python | 3.12.10 · Locust 2.43.4 |
| k6 | **v2.1.0 — installed this phase** (`winget install GrafanaLabs.k6`); enables the phase-2 k6-vs-Python client cross-validation |

Same machine class as the cycle-1 runs and the 2026-07-27 baselines →
regression comparisons are valid per §8.

## Known AV interference on this box (mitigations in place)

Norton AI Agent Protection actively inspects this machine. Cycle-1-verified
impacts + mitigations, all still required for cycle 2:

1. **Freshly built exes in temp dirs get quarantined** (`go run` fails) →
   build into the repo dir (`go build -o ./x.exe`) and set
   `GOTMPDIR=$(pwd)/.gotmp` for `go test`/bench runs.
2. **Instantaneous loopback connection bursts get killed pre-HTTP**
   (TC-PERF-09 dial-burst artifact) → stagger connection ramps; k6's
   ramping executors do this naturally.
3. DB-file deletions between runs trigger approval prompts → expected;
   part of the §4.4 clean-slate recipe.

## Open team action (carried into phase 2/§9 exit review)

- **Second-baseline box for TC-PERF-01:** identify a non-AV, non-throttled
  machine (clean laptop or quiet VM with fixed CPU clocks) and run
  `make bench-report` there once, per §9 phase 1. Owner: QA. Until then the
  primary-box off-governor baseline (2026-07-27) remains the only anchor —
  acceptable, since allocs/op is machine-independent.
